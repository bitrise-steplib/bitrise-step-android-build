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
	wentWrongHeader = "* What went wrong:"

	// gradleCauseIndent is how far Gradle indents a cause under the one it belongs to.
	gradleCauseIndent = 3
	// reasonIndent is how far a reported line is indented under the one it is a detail of.
	reasonIndent = "  "

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
	blockLines     []blockLine
	blockDetail    string
	compilerErrors []compilerError
	failedTests    []failedTest
	failedTasks    []string

	inBlock         bool
	afterFailedTest bool
}

// blockLine is one line of the "* What went wrong:" block. Gradle opens the block with the problem and
// indents each cause under the one it belongs to, which is the shape of the answer.
type blockLine struct {
	text  string
	depth int
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

	if strings.TrimSpace(line) == wentWrongHeader {
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

// Reasons returns the findings, nested under the Gradle failure they belong to, or nothing when no line
// matched, in which case the caller keeps its own generic message rather than showing a guess.
func (m *Matcher) Reasons() []string {
	switch {
	case len(m.compilerErrors) > 0:
		return m.nestUnderBlock(m.compilerErrorLines())
	case len(m.failedTests) > 0:
		return m.nestUnderBlock(m.failedTestLines())
	case len(m.blockLines) > 0:
		return m.nestUnderBlock(nil)
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

	indent := len(line) - len(strings.TrimLeft(line, " "))
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len(m.blockLines) >= maxBlockLines {
		return true
	}

	// Gradle opens the block with the problem and starts each nested cause with "> ". The rest is detail
	// (searched locations, "Required by") that makes the message long without saying more.
	switch cause, isCause := strings.CutPrefix(trimmed, "> "); {
	case len(m.blockLines) == 0:
		m.addBlockLine(trimmed, 0)
	case isCause:
		m.addBlockLine(cause, indent/gradleCauseIndent+1)
	case len(m.blockLines) == 1 && m.blockDetail == "":
		m.blockDetail = trimmed
	}

	return true
}

func (m *Matcher) addBlockLine(text string, depth int) {
	if slices.ContainsFunc(m.blockLines, func(l blockLine) bool { return l.text == text }) {
		return
	}

	m.blockLines = append(m.blockLines, blockLine{text: text, depth: depth})
}

func (m *Matcher) addCompilerError(e compilerError) {
	if !slices.Contains(m.compilerErrors, e) {
		m.compilerErrors = append(m.compilerErrors, e)
	}
}

// nestUnderBlock puts the findings under the Gradle failure block, one level deeper than its last
// cause. That cause is dropped, because it is the one the findings answer: for a Kotlin failure it only
// says "Compilation error. See log for more details".
func (m *Matcher) nestUnderBlock(findings []string) []string {
	block := m.effectiveBlockLines()
	if len(findings) > 0 && len(block) > 1 {
		block = block[:len(block)-1]
	}

	var lines []string
	depth := 0
	for _, line := range block {
		lines = append(lines, strings.Repeat(reasonIndent, line.depth)+line.text)
		depth = line.depth + 1
	}
	for _, finding := range findings {
		lines = append(lines, strings.Repeat(reasonIndent, depth)+finding)
	}

	return lines
}

// effectiveBlockLines adds the line under the problem when Gradle gave no causes for it. A task that
// does not exist is reported that way, and the problem on its own reads "Selection failed".
func (m *Matcher) effectiveBlockLines() []blockLine {
	if len(m.blockLines) != 1 || m.blockDetail == "" {
		return m.blockLines
	}

	return []blockLine{m.blockLines[0], {text: m.blockDetail, depth: 1}}
}

func (m *Matcher) compilerErrorLines() []string {
	var lines []string
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
	var lines []string
	for _, t := range m.failedTests[:min(len(m.failedTests), maxTestFailures)] {
		lines = append(lines, t.name)
		if t.detail != "" {
			lines = append(lines, reasonIndent+t.detail)
		}
	}

	if more := len(m.failedTests) - maxTestFailures; more > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more failing tests", more))
	}

	return lines
}
