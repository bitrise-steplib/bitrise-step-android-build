// Package buildcache detects whether the Bitrise Build Cache CLI is installed
// on this machine and whether the React Native build cache has been activated.
// The step uses the result to decide whether to wrap its gradle invocation in
// `bitrise-build-cache react-native run --`.
package buildcache

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/log"
)

const (
	cliBinary = "bitrise-build-cache"

	statusTimeout = 5 * time.Second

	// OptOutEnv, when set to "0", skips detection entirely and forces the step
	// to behave as if the CLI were absent. Killswitch for operators if the
	// wrapper ever ships a regression.
	OptOutEnv = "BITRISE_BUILD_CACHE_RN_WRAP"
)

// Detection describes the CLI's reachability and RN-cache activation state on
// this machine. A zero-value Detection means "no wrapping should happen" —
// either because the CLI is absent, unhealthy, or RN cache isn't activated.
type Detection struct {
	CLIPath            string
	ReactNativeEnabled bool
}

// Detect probes the CLI on PATH and queries RN-enablement. Any failure
// degrades to a zero-value Detection with a warn log — this function must
// never cause the step to fail.
func Detect(ctx context.Context, logger log.Logger) Detection {
	if os.Getenv(OptOutEnv) == "0" {
		return Detection{}
	}

	path, err := exec.LookPath(cliBinary)
	if err != nil {
		return Detection{}
	}

	enabled, err := queryRNEnabled(ctx, path)
	if err != nil {
		logger.Warnf("Bitrise Build Cache status probe failed (%s). Skipping RN cache wrap.", err)

		return Detection{CLIPath: path}
	}

	return Detection{
		CLIPath:            path,
		ReactNativeEnabled: enabled,
	}
}

// queryRNEnabled calls `<cli> status --feature=react-native --quiet`. Exit 0
// means enabled, exit 1 means disabled. Any other outcome is a probe failure.
func queryRNEnabled(ctx context.Context, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "status", "--feature=react-native", "--quiet")
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
	}

	return false, err
}
