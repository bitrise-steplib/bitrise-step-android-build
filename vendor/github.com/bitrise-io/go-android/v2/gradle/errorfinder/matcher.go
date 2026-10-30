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
	// diagnosticMark is what a tool writes in front of the problem it found. A cause's own lines are
	// reported only when they carry it, so that prose stays out of the reason.
	diagnosticMark = "error: "
	// caretLine marks the column of the line above it, and points nowhere once the reason is reindented.
	caretLine = "^"
	// taskLinePrefix and incubatingPrefix open the lines Gradle writes about the run itself. Both of its
	// streams feed the matcher, so one of them can arrive while the failure block is open, where it is
	// neither the problem nor one of its causes.
	taskLinePrefix   = "> Task :"
	incubatingPrefix = "[Incubating] "

	maxCompilerErrors = 3
	maxTestFailures   = 3
	maxBlockLines     = 10
	maxDetailLines    = 10
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
	// detail is the rest of the line's message, which Gradle writes under it at its own column.
	detail []string
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
	switch detail := m.causeDetailLines(); {
	case len(detail) > 0:
		return m.nestUnderBlock(m.effectiveBlockLines(), detail)
	case len(m.compilerErrors) > 0:
		return m.nestUnderBlock(m.blockLinesWithoutAnsweredCause(), m.compilerErrorLines())
	case len(m.failedTests) > 0:
		return m.nestUnderBlock(m.blockLinesWithoutAnsweredCause(), m.failedTestLines())
	case len(m.blockLines) > 0:
		return m.nestUnderBlock(m.effectiveBlockLines(), nil)
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

	if strings.HasPrefix(line, taskLinePrefix) || strings.HasPrefix(line, incubatingPrefix) {
		return false
	}

	indent := len(line) - len(strings.TrimLeft(line, " "))
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len(m.blockLines) >= maxBlockLines {
		return true
	}

	// Gradle opens the block with the problem and starts each nested cause with "> ". What follows a
	// line without that mark is the rest of its message.
	switch cause, isCause := strings.CutPrefix(trimmed, "> "); {
	case len(m.blockLines) == 0:
		m.addBlockLine(trimmed, 0)
	case isCause:
		m.addBlockLine(cause, indent/gradleCauseIndent+1)
	default:
		last := &m.blockLines[len(m.blockLines)-1]
		last.detail = append(last.detail, trimmed)
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

// nestUnderBlock puts the findings under the Gradle failure block, one level deeper than its last line.
func (m *Matcher) nestUnderBlock(block []blockLine, findings []string) []string {
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

// causeDetailLines returns the rest of the deepest cause's message, when it is the failing tool's own
// diagnostics. A cause whose message carries on in prose keeps it out of the reason: a dependency that
// cannot be resolved lists the repositories searched for it, which says no more than the cause itself.
func (m *Matcher) causeDetailLines() []string {
	if len(m.blockLines) < 2 {
		return nil
	}

	detail := m.blockLines[len(m.blockLines)-1].detail
	if !slices.ContainsFunc(detail, func(line string) bool { return strings.Contains(line, diagnosticMark) }) {
		return nil
	}

	var lines []string
	for _, line := range detail {
		if line == caretLine {
			continue
		}

		lines = append(lines, line)
	}

	if more := len(lines) - maxDetailLines; more > 0 {
		lines = append(lines[:maxDetailLines], fmt.Sprintf("... and %d more lines", more))
	}

	return lines
}

// effectiveBlockLines adds the line under the problem when Gradle gave no causes for it. A task that
// does not exist is reported that way, and the problem on its own reads "Selection failed".
func (m *Matcher) effectiveBlockLines() []blockLine {
	if len(m.blockLines) != 1 || len(m.blockLines[0].detail) == 0 {
		return m.blockLines
	}

	return []blockLine{m.blockLines[0], {text: m.blockLines[0].detail[0], depth: 1}}
}

// blockLinesWithoutAnsweredCause drops the deepest cause, the one that findings taken from the log
// answer: for a Kotlin failure it only says "Compilation error. See log for more details".
func (m *Matcher) blockLinesWithoutAnsweredCause() []blockLine {
	block := m.effectiveBlockLines()
	if len(block) < 2 {
		return block
	}

	return block[:len(block)-1]
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
	// The detail sits beside its test rather than under it: the reasons state one path down to the
	// findings, and a line stepping back out to the next test could not be shown as a step back.
	for _, t := range m.failedTests[:min(len(m.failedTests), maxTestFailures)] {
		lines = append(lines, t.name)
		if t.detail != "" {
			lines = append(lines, t.detail)
		}
	}

	if more := len(m.failedTests) - maxTestFailures; more > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more failing tests", more))
	}

	return lines
}
