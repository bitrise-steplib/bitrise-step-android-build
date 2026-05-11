package main

import (
	"os"

	"github.com/bitrise-io/go-steputils/v2/export"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
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
		logger.Errorf("Process config: %s", err.Error())
		return 1
	}

	result, err := androidBuild.Run(config)
	if err != nil {
		logger.Errorf("Run: %s", err.Error())
		return 1
	}

	if err := androidBuild.Export(result, config.DeployDir); err != nil {
		logger.Errorf("Export outputs: %s", err.Error())
		return 1
	}

	return 0
}
