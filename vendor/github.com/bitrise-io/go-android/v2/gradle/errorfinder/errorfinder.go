// Package errorfinder collects the lines of a Gradle run's output that say why the build failed, so a
// step can report them as its failure reason instead of a bare exit status.
package errorfinder

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

const (
	// maxLineLength bounds how much of a line is held back while its newline is missing. Failure lines are
	// short, so a longer line is not one: the rest of it is dropped instead of buffered.
	maxLineLength = 64 * 1024

	// maxErrorBytes leaves room for the step's own wrapping layers inside the 1 KB cap of
	// BITRISE_FAILED_STEP_ERROR_MESSAGE.
	maxErrorBytes = 800

	truncatedMarker = "... (truncated)"
)

// Collector keeps the lines of a Gradle run's output that say why it failed.
//
// Gradle splits those lines over both streams: the FAILURE block and the compiler errors go to stderr,
// the failed task and failing test lines to stdout. os/exec copies the two streams on separate
// goroutines, so each stream gets its own writer, holding its own partial line, over shared state.
type Collector struct {
	mu     sync.Mutex
	parser parser

	stdout *streamWriter
	stderr *streamWriter
}

// streamWriter feeds one output stream to the collector, line by line.
type streamWriter struct {
	collector *Collector
	pending   []byte
	skipping  bool
}

// NewCollector ...
func NewCollector() *Collector {
	collector := &Collector{}
	collector.stdout = &streamWriter{collector: collector}
	collector.stderr = &streamWriter{collector: collector}

	return collector
}

// StdoutWriter returns the writer the command's stdout has to be teed into.
func (c *Collector) StdoutWriter() io.Writer {
	return c.stdout
}

// StderrWriter returns the writer the command's stderr has to be teed into.
func (c *Collector) StderrWriter() io.Writer {
	return c.stderr
}

// Errors returns the failure reason found so far, or nothing when no line matched, in which case the
// caller keeps its own generic message rather than showing a guess.
//
// The end of the output is treated as the end of a line, so it is safe to call more than once.
func (c *Collector) Errors() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stdout.flush()
	c.stderr.flush()

	return capLines(c.parser.errors())
}

// Write never fails and always reports the whole chunk as written, so it can not interrupt the
// io.MultiWriter it is part of and cost the build its log output.
func (w *streamWriter) Write(p []byte) (int, error) {
	w.collector.mu.Lock()
	defer w.collector.mu.Unlock()

	n := len(p)
	for len(p) > 0 {
		line, rest, found := bytes.Cut(p, []byte{'\n'})
		w.appendPending(line)
		if !found {
			break
		}

		w.finishLine()
		p = rest
	}

	return n, nil
}

func (w *streamWriter) appendPending(b []byte) {
	if w.skipping {
		return
	}
	if len(w.pending)+len(b) > maxLineLength {
		w.pending = w.pending[:0]
		w.skipping = true

		return
	}

	w.pending = append(w.pending, b...)
}

func (w *streamWriter) flush() {
	if len(w.pending) == 0 && !w.skipping {
		return
	}

	w.finishLine()
}

// finishLine hands the completed line to the parser. A skipped (overlong) line is not a failure line,
// but it still ends an open block, so it is handed over as an empty line.
func (w *streamWriter) finishLine() {
	line := ""
	if !w.skipping {
		line = string(w.pending)
	}
	w.pending = w.pending[:0]
	w.skipping = false

	w.collector.parser.consumeLine(line)
}

func capLines(lines []string) []string {
	var capped []string
	remaining := maxErrorBytes

	for _, line := range lines {
		if len(line) > remaining {
			if len(capped) == 0 {
				capped = append(capped, strings.ToValidUTF8(line[:remaining], ""))
			}
			capped = append(capped, truncatedMarker)

			break
		}

		capped = append(capped, line)
		remaining -= len(line) + 1
	}

	return capped
}
