// Package errorfinder reads the output of a Gradle run and reports the lines that say why the build
// failed, so a step can report them as its failure reason instead of a bare exit status.
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

// Matcher turns the lines of a Gradle run into the findings behind the failure. It is a state machine,
// so it has to see the lines in the order they were written, and one at a time.
type Matcher struct {
	compilerErrors []compilerError
	failedTests    []failedTest
	blockLines     []string
	failedTasks    []string

	inBlock         bool
	afterFailedTest bool
}

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

// NewMatcher returns a matcher for the output of one Gradle run.
func NewMatcher() *Matcher {
	return &Matcher{}
}

// ConsumeLine reads the next line of the run's output.
func (m *Matcher) ConsumeLine(line string) {
	line = ansiRegexp.ReplaceAllString(line, "")

	if m.inBlock && m.consumeBlockLine(line) {
		return
	}

	afterFailedTest := m.afterFailedTest
	m.afterFailedTest = false

	if line == wentWrongHeader {
		m.inBlock = true

		return
	}
	if match := failedTaskRegexp.FindStringSubmatch(line); match != nil {
		if !slices.Contains(m.failedTasks, match[1]) {
			m.failedTasks = append(m.failedTasks, match[1])
		}

		return
	}
	if match := kotlinErrorRegexp.FindStringSubmatch(line); match != nil {
		m.addCompilerError(compilerError{file: match[1], line: match[2], column: match[3], message: match[4]})

		return
	}
	if match := kotlinPathErrorRegexp.FindStringSubmatch(line); match != nil {
		m.addCompilerError(compilerError{file: match[1], line: match[2], column: match[3], message: match[4]})

		return
	}
	if match := javacErrorRegexp.FindStringSubmatch(line); match != nil {
		m.addCompilerError(compilerError{file: match[1], line: match[2], message: match[3]})

		return
	}
	if failedTestRegexp.MatchString(line) {
		m.failedTests = append(m.failedTests, failedTest{name: line})
		m.afterFailedTest = true

		return
	}
	if afterFailedTest && strings.HasPrefix(line, " ") {
		m.failedTests[len(m.failedTests)-1].detail = strings.TrimSpace(line)
	}
}

// Reasons returns the findings, most specific first, or nothing when no line matched, in which case
// the caller keeps its own generic message rather than showing a guess.
//
// Compiler errors come before the failure block because for Kotlin the block only says
// "Compilation error. See log for more details".
func (m *Matcher) Reasons() []string {
	switch {
	case len(m.compilerErrors) > 0:
		return m.compilerErrorLines()
	case len(m.failedTests) > 0:
		return m.failedTestLines()
	case len(m.blockLines) > 0:
		return m.blockLines
	case len(m.failedTasks) > 0:
		return []string{fmt.Sprintf("Gradle task %s failed", strings.Join(m.failedTasks, ", "))}
	default:
		return nil
	}
}

// consumeBlockLine collects the lines of the "* What went wrong:" block and reports whether the block
// is still open. Gradle closes it with its next section header, "* Try:".
func (m *Matcher) consumeBlockLine(line string) bool {
	if strings.HasPrefix(line, "* ") {
		m.inBlock = false

		return false
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len(m.blockLines) >= maxBlockLines || slices.Contains(m.blockLines, trimmed) {
		return true
	}

	// Gradle opens the block with the problem and starts each nested cause with "> ". The rest is detail
	// (searched locations, "Required by") that makes the message long without saying more.
	if len(m.blockLines) == 0 || strings.HasPrefix(trimmed, "> ") {
		m.blockLines = append(m.blockLines, trimmed)
	}

	return true
}

func (m *Matcher) addCompilerError(e compilerError) {
	if !slices.Contains(m.compilerErrors, e) {
		m.compilerErrors = append(m.compilerErrors, e)
	}
}

func (m *Matcher) compilerErrorLines() []string {
	lines := m.failedTaskLines()

	for _, e := range m.compilerErrors[:min(len(m.compilerErrors), maxCompilerErrors)] {
		location := e.file + ":" + e.line
		if e.column != "" {
			location += ":" + e.column
		}
		lines = append(lines, location+": "+e.message)
	}

	if more := len(m.compilerErrors) - maxCompilerErrors; more > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more compile errors", more))
	}

	return lines
}

func (m *Matcher) failedTestLines() []string {
	lines := m.failedTaskLines()

	for _, t := range m.failedTests[:min(len(m.failedTests), maxTestFailures)] {
		lines = append(lines, t.name)
		if t.detail != "" {
			lines = append(lines, "  "+t.detail)
		}
	}

	if more := len(m.failedTests) - maxTestFailures; more > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more failing tests", more))
	}

	return lines
}

// failedTaskLines names the task the findings belong to, which the findings themselves do not say.
func (m *Matcher) failedTaskLines() []string {
	for _, line := range m.blockLines {
		if strings.HasPrefix(line, executionFailedPrefix) {
			return []string{line}
		}
	}

	return nil
}
