package step

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bitrise-io/go-android/gradle"
	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/log"
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
	}
}

func Test_buildGradleCommand_NoWrapWhenCLIMissing(t *testing.T) {
	// PATH pointing at empty dir → no CLI found → no wrap.
	t.Setenv("PATH", t.TempDir())

	step := createStep()
	cmd := step.buildGradleCommand("/tmp/proj/gradlew", []string{"assembleDebug"}, &command.Opts{})

	printed := cmd.PrintableCommandArgs()
	assert.Contains(t, printed, "gradlew")
	assert.NotContains(t, printed, "bitrise-build-cache")
}

func Test_buildGradleCommand_WrapsWhenRNCacheEnabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "bitrise-build-cache")
	err := os.WriteFile(stub, []byte(`#!/bin/sh
# --version → exit 0; status --feature=react-native --quiet → exit 0 (enabled)
[ "$1" = "--version" ] && exit 0
[ "$1" = "status" ] && exit 0
exit 99
`), 0o755)
	assert.NoError(t, err)
	t.Setenv("PATH", dir)

	step := createStep()
	cmd := step.buildGradleCommand("/tmp/proj/gradlew", []string{"assembleDebug"}, &command.Opts{})

	printed := cmd.PrintableCommandArgs()
	assert.Contains(t, printed, "bitrise-build-cache")
	assert.Contains(t, printed, "react-native")
	assert.Contains(t, printed, "run")
	assert.Contains(t, printed, "gradlew")
	assert.True(t, strings.Contains(printed, "assembleDebug"), "gradle args preserved after wrap")
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
