package main

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-steplib/bitrise-step-android-build/step"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("Step run failed: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	androidBuild := createAndroidBuild()
	config, err := androidBuild.ProcessConfig()
	if err != nil {
		return err
	}

	result, err := androidBuild.Run(config)
	if err != nil {
		return err
	}

	if err := androidBuild.Export(result, config.DeployDir); err != nil {
		return err
	}

	androidBuild.CollectCache(config)

	return nil
}

func createAndroidBuild() *step.AndroidBuild {
	stepInputParser := step.NewInputParser()
	logger := log.NewLogger(false)
	cmdFactory := command.NewFactory(env.NewRepository())
	return step.NewAndroidBuild(stepInputParser, logger, cmdFactory)
}
