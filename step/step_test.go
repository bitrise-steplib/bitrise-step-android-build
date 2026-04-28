package step

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/reactnative/wrap"
	"github.com/bitrise-io/go-android/v2/gradle"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-steplib/bitrise-step-android-build/step/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func Test_GivenMatchingFiles_WhenGettingArtifacts_ThenArtifactsReturned(t *testing.T) {
	// Given
	step := createStep()
	startTime := time.Date(2021, 8, 18, 8, 0, 0, 0, time.UTC)
	appPathPattern := []string{"*/build/outputs/apk/*.apk", "*/build/outputs/bundle/*.aab"}
	gradleWrapper := new(mocks.MockGradleProjectWrapper)
	testArtifacts := []gradle.Artifact{
		{
			Path: "/bitrise/src/app/build/outputs/apk/my-app-debug.apk",
			Name: "my-app-debug.apk",
		},
	}
	gradleWrapper.On("FindArtifacts", startTime, appPathPattern[0], false).Return(testArtifacts, nil)
	gradleWrapper.On("FindArtifacts", startTime, appPathPattern[1], false).Return([]gradle.Artifact{}, nil)

	// When
	artifacts, err := step.getArtifacts(gradleWrapper, startTime, appPathPattern, false)

	// Then
	assert.NoError(t, err)
	assert.Equal(t, testArtifacts, artifacts)
	gradleWrapper.AssertCalled(t, "FindArtifacts", startTime, appPathPattern[0], false)
	gradleWrapper.AssertCalled(t, "FindArtifacts", startTime, appPathPattern[1], false)
}

func Test_GivenNoMatchingFiles_WhenGettingArtifacts_ThenRetryWithoutModTimeCheck(t *testing.T) {
	// Given
	step := createStep()
	startTime := time.Date(2021, 8, 18, 8, 0, 0, 0, time.UTC)
	appPathPattern := []string{"*/build/outputs/apk/*.apk", "*/build/outputs/bundle/*.aab"}
	gradleWrapper := new(mocks.MockGradleProjectWrapper)
	testArtifacts := []gradle.Artifact{
		{
			Path: "/bitrise/src/app/build/outputs/apk/my-app-debug.apk",
			Name: "my-app-debug.apk",
		},
	}
	gradleWrapper.On("FindArtifacts", startTime, mock.Anything, false).Return([]gradle.Artifact{}, nil)
	gradleWrapper.On("FindArtifacts", time.Time{}, appPathPattern[0], false).Return(testArtifacts, nil)
	gradleWrapper.On("FindArtifacts", time.Time{}, appPathPattern[1], false).Return([]gradle.Artifact{}, nil)

	// When
	artifacts, err := step.getArtifacts(gradleWrapper, startTime, appPathPattern, false)

	// Then
	assert.NoError(t, err)
	assert.Equal(t, testArtifacts, artifacts)
	gradleWrapper.AssertCalled(t, "FindArtifacts", startTime, appPathPattern[0], false)
	gradleWrapper.AssertCalled(t, "FindArtifacts", startTime, appPathPattern[1], false)
	gradleWrapper.AssertCalled(t, "FindArtifacts", time.Time{}, appPathPattern[0], false)
	gradleWrapper.AssertCalled(t, "FindArtifacts", time.Time{}, appPathPattern[1], false)
}

func createStep() AndroidBuild {
	envRepository := env.NewRepository()
	return AndroidBuild{
		inputParser: stepconf.NewInputParser(envRepository),
		logger:      log.NewLogger(),
		cmdFactory:  command.NewFactory(envRepository),
		detect: func(context.Context, log.Logger) wrap.Detection {
			return wrap.Detection{}
		},
	}
}

func Test_buildGradleCommand_NoWrapWhenCLIMissing(t *testing.T) {
	step := createStep()
	step.detect = func(context.Context, log.Logger) wrap.Detection {
		return wrap.Detection{}
	}

	cmd := step.buildGradleCommand(context.Background(), "/tmp/proj/gradlew", []string{"assembleDebug"}, &command.Opts{})

	assert.Equal(t, `/tmp/proj/gradlew "assembleDebug"`, cmd.PrintableCommandArgs())
}

func Test_buildGradleCommand_NoWrapWhenDetected_NotEnabled(t *testing.T) {
	// CLI present but RN cache not active → still no wrap.
	step := createStep()
	step.detect = func(context.Context, log.Logger) wrap.Detection {
		return wrap.Detection{CLIPath: "/usr/local/bin/bitrise-build-cache"}
	}

	cmd := step.buildGradleCommand(context.Background(), "/tmp/proj/gradlew", []string{"assembleDebug"}, &command.Opts{})

	assert.Equal(t, `/tmp/proj/gradlew "assembleDebug"`, cmd.PrintableCommandArgs())
}

func Test_buildGradleCommand_WrapsWhenRNCacheEnabled(t *testing.T) {
	cliPath := "/usr/local/bin/bitrise-build-cache"

	step := createStep()
	step.detect = func(context.Context, log.Logger) wrap.Detection {
		return wrap.Detection{CLIPath: cliPath, ReactNativeEnabled: true}
	}

	cmd := step.buildGradleCommand(context.Background(), "/tmp/proj/gradlew", []string{"assembleDebug", "--info"}, &command.Opts{})

	expected := fmt.Sprintf(`%s "react-native" "run" "--" "/tmp/proj/gradlew" "assembleDebug" "--info"`, cliPath)
	assert.Equal(t, expected, cmd.PrintableCommandArgs())
}

func Test_gradleTaskName(t *testing.T) {
	type args struct {
		appType string
		module  string
		variant string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "No module, no variant",
			args: args{
				appType: "apk",
				module:  "",
				variant: "",
			},
			want: "assemble",
		},
		{
			name: "App module, no variant",
			args: args{
				appType: "aab",
				module:  "app",
				variant: "",
			},
			want: ":app:bundle",
		},
		{
			name: "No module, debug variant",
			args: args{
				appType: "apk",
				module:  "",
				variant: "debug",
			},
			want: "assembleDebug",
		},
		{
			name: "App module, release variant",
			args: args{
				appType: "aab",
				module:  "app",
				variant: "release",
			},
			want: ":app:bundleRelease",
		},
		{
			name: "Nested module, flavor variant",
			args: args{
				appType: "apk",
				module:  "core:ui",
				variant: "demoRelease",
			},
			want: ":core:ui:assembleDemoRelease",
		},
		{
			name: "Module input starts with colon",
			args: args{
				appType: "aab",
				module:  ":core:ui",
				variant: "demoRelease",
			},
			want: ":core:ui:bundleDemoRelease",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gradleTaskName(tt.args.appType, tt.args.module, tt.args.variant)
			if err != nil {
				t.Errorf("Error: %v", err)
			}
			assert.Equalf(t, tt.want, got, "gradleTaskName(%v, %v, %v)", tt.args.appType, tt.args.module, tt.args.variant)
		})
	}
}
