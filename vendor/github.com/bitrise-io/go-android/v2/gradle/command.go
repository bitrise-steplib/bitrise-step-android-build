package gradle

import (
	"errors"
	"io"
	"os/exec"

	"github.com/bitrise-io/go-android/v2/gradle/errorfinder"
	"github.com/bitrise-io/go-utils/v2/command"
)

// Command is a gradlew invocation whose error states why Gradle failed.
type Command struct {
	command   command.Command
	collector *errorfinder.Collector
}

// NewCommand creates the command described by name, args and opts, with both of its output streams
// watched for the reason a failing Gradle run gives.
//
// name and args are passed on as they are, so the caller can wrap gradlew in another command.
func NewCommand(factory command.Factory, name string, args []string, opts command.Opts) Command {
	collector := errorfinder.NewCollector()

	// os/exec gives the child one pipe and one copy goroutine as long as Stdout and Stderr compare equal.
	// Wrapping such a pair in two writers would hand the caller's single writer to two goroutines.
	if opts.Stdout != nil && interfaceEqual(opts.Stdout, opts.Stderr) {
		shared := io.MultiWriter(opts.Stdout, collector.StdoutWriter())
		opts.Stdout, opts.Stderr = shared, shared
	} else {
		opts.Stdout = collectInto(opts.Stdout, collector.StdoutWriter())
		opts.Stderr = collectInto(opts.Stderr, collector.StderrWriter())
	}

	return Command{
		command:   factory.Create(name, args, &opts),
		collector: collector,
	}
}

// PrintableCommandArgs ...
func (c Command) PrintableCommandArgs() string {
	return c.command.PrintableCommandArgs()
}

// Run runs the command. When it exits with a non-zero status, the returned error ends with the lines
// Gradle printed about the failure, or with the generic go-utils message when it printed none.
func (c Command) Run() error {
	err := c.command.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}

	return command.NewExitStatusError(c.command.PrintableCommandArgs(), exitErr, c.collector.Errors())
}

func collectInto(writer, collector io.Writer) io.Writer {
	if writer == nil {
		return collector
	}

	return io.MultiWriter(writer, collector)
}

// interfaceEqual compares two writers the way os/exec does, without panicking on an uncomparable type.
func interfaceEqual(a, b any) bool {
	defer func() { _ = recover() }()

	return a == b
}
