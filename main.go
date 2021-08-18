package main

import (
	"os"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-steplib/bitrise-step-android-build/step"
)

func main() {
	if err := run(); err != nil {
		log.Errorf("Step run failed: %s", err)
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

	if err := androidBuild.Export(result, config); err != nil {
		return err
	}

	androidBuild.CollectCache(config)

	return nil
}

func createAndroidBuild() *step.AndroidBuild {
	stepInputParser := step.NewInputParser()
	return step.NewAndroidBuild(stepInputParser)
}
