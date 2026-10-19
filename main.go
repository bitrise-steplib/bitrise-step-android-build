package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-steputils/v2/export"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/errorutil"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-steplib/bitrise-step-android-build/step"
)

func main() {
	os.Exit(run())
}

func run() int {
	envRepository := env.NewRepository()
	inputParser := stepconf.NewInputParser(envRepository)
	logger := log.NewLogger()
	cmdFactory := command.NewFactory(envRepository)
	exporter := export.NewExporter(cmdFactory, fileutil.NewFileManager())
	androidBuild := step.NewAndroidBuild(inputParser, logger, cmdFactory, exporter)

	config, err := androidBuild.ProcessConfig()
	if err != nil {
		logFailure(logger, "Process config", err)
		return 1
	}

	result, err := androidBuild.Run(config)
	if err != nil {
		logFailure(logger, "Run", err)
		return 1
	}

	if err := androidBuild.Export(result, config.DeployDir); err != nil {
		logFailure(logger, "Export outputs", err)
		return 1
	}

	return 0
}

// logFailure prints the error as the layers that led to it, with the reason the failing tool gave as
// the last one. The message is an argument, never the format string: tool output can contain a %.
func logFailure(logger log.Logger, phase string, err error) {
	logger.Errorf("%s", errorutil.FormattedError(fmt.Errorf("%s: %w", phase, err)))
}
