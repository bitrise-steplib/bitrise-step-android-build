package buildcache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/log"
	"github.com/stretchr/testify/assert"
)

func installStub(t *testing.T, dir, script string) string {
	t.Helper()
	path := filepath.Join(dir, "bitrise-build-cache")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}

	return path
}

func TestDetect_NoCLIOnPath(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	got := Detect(context.Background(), log.NewLogger())

	assert.Equal(t, Detection{}, got)
}

func TestDetect_Enabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}

	dir := t.TempDir()
	stubPath := installStub(t, dir, `#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--feature=react-native" ] && [ "$3" = "--quiet" ]; then
  exit 0
fi
echo "unexpected args: $*" >&2
exit 127
`)
	t.Setenv("PATH", dir)

	got := Detect(context.Background(), log.NewLogger())

	assert.True(t, got.ReactNativeEnabled)
	assert.Equal(t, stubPath, got.CLIPath)
}

func TestDetect_Disabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}

	dir := t.TempDir()
	stubPath := installStub(t, dir, `#!/bin/sh
if [ "$1" = "status" ]; then
  exit 1
fi
echo "unexpected args: $*" >&2
exit 127
`)
	t.Setenv("PATH", dir)

	got := Detect(context.Background(), log.NewLogger())

	assert.False(t, got.ReactNativeEnabled)
	assert.Equal(t, stubPath, got.CLIPath)
}

func TestDetect_StatusFailsUnexpectedly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}

	dir := t.TempDir()
	stubPath := installStub(t, dir, `#!/bin/sh
# neither 0 (enabled) nor 1 (disabled) → probe failure
if [ "$1" = "status" ]; then
  exit 7
fi
echo "unexpected args: $*" >&2
exit 127
`)
	t.Setenv("PATH", dir)

	got := Detect(context.Background(), log.NewLogger())

	assert.False(t, got.ReactNativeEnabled)
	assert.Equal(t, stubPath, got.CLIPath)
}

// TestDetect_OldCLIWithoutStatusSubcommand simulates a user running an older
// CLI binary that predates the `status` subcommand. Cobra exits 1 on unknown
// command, which queryRNEnabled reads as "disabled" — correct safe fallback,
// but worth asserting since it's every user's state during the release window
// between CLI merge and step rollout.
func TestDetect_OldCLIWithoutStatusSubcommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}

	dir := t.TempDir()
	stubPath := installStub(t, dir, `#!/bin/sh
# Cobra's "unknown command" exit code is 1.
if [ "$1" = "status" ]; then
  exit 1
fi
echo "unexpected args: $*" >&2
exit 127
`)
	t.Setenv("PATH", dir)

	got := Detect(context.Background(), log.NewLogger())

	assert.False(t, got.ReactNativeEnabled)
	assert.Equal(t, stubPath, got.CLIPath)
}

// TestDetect_StatusTimeout asserts that a hung CLI does not hang the step.
// This is the most important invariant of this module.
func TestDetect_StatusTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}
	if testing.Short() {
		t.Skip("timeout test is slow")
	}

	dir := t.TempDir()
	// /bin/sleep absolute path — tests set PATH to stub dir only, so external
	// sleep wouldn't be resolvable otherwise.
	stubPath := installStub(t, dir, `#!/bin/sh
if [ "$1" = "status" ]; then
  /bin/sleep 30
  exit 0
fi
echo "unexpected args: $*" >&2
exit 127
`)
	t.Setenv("PATH", dir)

	started := time.Now()
	got := Detect(context.Background(), log.NewLogger())
	elapsed := time.Since(started)

	assert.False(t, got.ReactNativeEnabled)
	assert.Equal(t, stubPath, got.CLIPath)
	// Allow generous headroom over statusTimeout for CI jitter, but prove we
	// didn't wait on the full sleep.
	assert.Less(t, elapsed, 15*time.Second, "Detect must return within statusTimeout + slack")
}

func TestDetect_OptOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub isn't portable to windows")
	}

	dir := t.TempDir()
	// Stub would report "enabled" if invoked — opt-out env var must short-circuit
	// before the subprocess spawn.
	installStub(t, dir, `#!/bin/sh
if [ "$1" = "status" ]; then
  exit 0
fi
echo "unexpected args: $*" >&2
exit 127
`)
	t.Setenv("PATH", dir)
	t.Setenv(OptOutEnv, "0")

	got := Detect(context.Background(), log.NewLogger())

	assert.Equal(t, Detection{}, got)
}
