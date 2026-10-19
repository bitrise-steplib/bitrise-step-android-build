package errorfinder

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	wentWrongHeader       = "* What went wrong:"
	executionFailedPrefix = "Execution failed for task "

	maxCompilerErrors = 3
	maxTestFailures   = 3
	maxBlockLines     = 10
)

var (
	ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	// Kotlin states the location in two formats, depending on the compiler version.
	kotlinErrorRegexp     = regexp.MustCompile(`^e: file://(.+):(\d+):(\d+):? (.+)$`)
	kotlinPathErrorRegexp = regexp.MustCompile(`^e: (/.+): \((\d+), (\d+)\): (.+)$`)
	javacErrorRegexp      = regexp.MustCompile(`^(/.+\.java):(\d+): error: (.+)$`)
	failedTaskRegexp      = regexp.MustCompile(`^> Task (\S+) FAILED$`)
	failedTestRegexp      = regexp.MustCompile(`^(\S+) > (.+) FAILED$`)
)

// compilerError is one error of the Kotlin or Java compiler. The parts are kept apart from the
// rendered line so that the structured error contract can use them.
type compilerError struct {
	file    string
	line    string
	column  string
	message string
}

type failedTest struct {
	name   string
	detail string
}

// parser turns the lines of a Gradle run into the findings behind the failure. It is a state machine,
// so it has to see the lines in order; the collector's lock keeps it to one line at a time.
type parser struct {
	compilerErrors []compilerError
	failedTests    []failedTest
	blockLines     []string
	failedTasks    []string

	inBlock         bool
	afterFailedTest bool
}

func (p *parser) consumeLine(line string) {
	line = strings.TrimRight(ansiRegexp.ReplaceAllString(line, ""), " \t\r")

	if p.inBlock && p.consumeBlockLine(line) {
		return
	}

	afterFailedTest := p.afterFailedTest
	p.afterFailedTest = false

	if line == wentWrongHeader {
		p.inBlock = true

		return
	}
	if m := failedTaskRegexp.FindStringSubmatch(line); m != nil {
		if !slices.Contains(p.failedTasks, m[1]) {
			p.failedTasks = append(p.failedTasks, m[1])
		}

		return
	}
	if m := kotlinErrorRegexp.FindStringSubmatch(line); m != nil {
		p.addCompilerError(compilerError{file: m[1], line: m[2], column: m[3], message: m[4]})

		return
	}
	if m := kotlinPathErrorRegexp.FindStringSubmatch(line); m != nil {
		p.addCompilerError(compilerError{file: m[1], line: m[2], column: m[3], message: m[4]})

		return
	}
	if m := javacErrorRegexp.FindStringSubmatch(line); m != nil {
		p.addCompilerError(compilerError{file: m[1], line: m[2], message: m[3]})

		return
	}
	if failedTestRegexp.MatchString(line) {
		p.failedTests = append(p.failedTests, failedTest{name: line})
		p.afterFailedTest = true

		return
	}
	if afterFailedTest && strings.HasPrefix(line, " ") {
		p.failedTests[len(p.failedTests)-1].detail = strings.TrimSpace(line)
	}
}

// consumeBlockLine collects the lines of the "* What went wrong:" block and reports whether the block
// is still open. Gradle closes it with its next section header, "* Try:".
func (p *parser) consumeBlockLine(line string) bool {
	if strings.HasPrefix(line, "* ") {
		p.inBlock = false

		return false
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len(p.blockLines) >= maxBlockLines || slices.Contains(p.blockLines, trimmed) {
		return true
	}

	// Gradle opens the block with the problem and starts each nested cause with "> ". The rest is detail
	// (searched locations, "Required by") that makes the message long without saying more.
	if len(p.blockLines) == 0 || strings.HasPrefix(trimmed, "> ") {
		p.blockLines = append(p.blockLines, trimmed)
	}

	return true
}

func (p *parser) addCompilerError(e compilerError) {
	if !slices.Contains(p.compilerErrors, e) {
		p.compilerErrors = append(p.compilerErrors, e)
	}
}

// errors returns the findings, most specific first. Compiler errors come before the failure block
// because for Kotlin the block only says "Compilation error. See log for more details".
func (p *parser) errors() []string {
	switch {
	case len(p.compilerErrors) > 0:
		return p.compilerErrorLines()
	case len(p.failedTests) > 0:
		return p.failedTestLines()
	case len(p.blockLines) > 0:
		return p.blockLines
	case len(p.failedTasks) > 0:
		return []string{fmt.Sprintf("Gradle task %s failed", strings.Join(p.failedTasks, ", "))}
	default:
		return nil
	}
}

func (p *parser) compilerErrorLines() []string {
	lines := p.failedTaskLines()

	for _, e := range p.compilerErrors[:min(len(p.compilerErrors), maxCompilerErrors)] {
		location := e.file + ":" + e.line
		if e.column != "" {
			location += ":" + e.column
		}
		lines = append(lines, location+": "+e.message)
	}

	if more := len(p.compilerErrors) - maxCompilerErrors; more > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more compile errors", more))
	}

	return lines
}

func (p *parser) failedTestLines() []string {
	lines := p.failedTaskLines()

	for _, t := range p.failedTests[:min(len(p.failedTests), maxTestFailures)] {
		lines = append(lines, t.name)
		if t.detail != "" {
			lines = append(lines, "  "+t.detail)
		}
	}

	if more := len(p.failedTests) - maxTestFailures; more > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more failing tests", more))
	}

	return lines
}

// failedTaskLines names the task the findings belong to, which the findings themselves do not say.
func (p *parser) failedTaskLines() []string {
	for _, line := range p.blockLines {
		if strings.HasPrefix(line, executionFailedPrefix) {
			return []string{line}
		}
	}

	return nil
}
