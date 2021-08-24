package main

import (
	"os"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-steplib/bitrise-step-android-build/step"
)

func main() {
	os.Exit(run())
}

func run() int {
	inputParser := stepconf.NewDefaultEnvParser()
	logger := log.NewLogger(false)
	cmdFactory := command.NewFactory(env.NewRepository())
	androidBuild := step.NewAndroidBuild(inputParser, logger, cmdFactory)

	config, err := androidBuild.ProcessConfig()
	if err != nil {
		logger.Errorf(err.Error())
		return 1
	}

	result, err := androidBuild.Run(config)
	if err != nil {
		logger.Errorf(err.Error())
		return 1
	}

	if err := androidBuild.Export(result, config.DeployDir); err != nil {
		logger.Errorf(err.Error())
		return 1
	}

	androidBuild.CollectCache(config)

	return 0
}
