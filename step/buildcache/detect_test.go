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

// stubSpec describes one branch of the shell-script stub. The script matches
// argv against `match` (a literal shell test expression) and exits with
// `exitCode`. Any argv that doesn't match a spec prints to stderr and exits
// 127 — the stub never silently returns a non-0/1 code, so accidental new
// subprocess calls fail loudly instead of being absorbed as "probe failure".
type stubSpec struct {
	match    string // e.g. `"$1" = "status"`
	exitCode int
	sleep    int // seconds, 0 = no sleep
}

func installStub(t *testing.T, dir string, specs []stubSpec) string {
	t.Helper()

	script := "#!/bin/sh\n"
	for _, s := range specs {
		if s.sleep > 0 {
			// Use absolute path — tests constrain PATH to the stub dir, so
			// `sleep` from /bin wouldn't be resolvable otherwise.
			script += "if [ " + s.match + " ]; then\n"
			script += "  /bin/sleep " + itoa(s.sleep) + "\n"
			script += "  exit " + itoa(s.exitCode) + "\n"
			script += "fi\n"
			continue
		}
		script += "if [ " + s.match + " ]; then exit " + itoa(s.exitCode) + "; fi\n"
	}
	script += `echo "unexpected args: $*" >&2` + "\n"
	script += "exit 127\n"

	path := filepath.Join(dir, "bitrise-build-cache")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}

	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}

	return string(b)
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
	stubPath := installStub(t, dir, []stubSpec{
		{match: `"$1" = "status" -a "$2" = "--feature=react-native" -a "$3" = "--quiet"`, exitCode: 0},
	})
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
	stubPath := installStub(t, dir, []stubSpec{
		{match: `"$1" = "status"`, exitCode: 1},
	})
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
	stubPath := installStub(t, dir, []stubSpec{
		// neither 0 (enabled) nor 1 (disabled) → probe failure
		{match: `"$1" = "status"`, exitCode: 7},
	})
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
	stubPath := installStub(t, dir, []stubSpec{
		// cobra's "unknown command" exit code is 1
		{match: `"$1" = "status"`, exitCode: 1},
	})
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
	stubPath := installStub(t, dir, []stubSpec{
		// sleep longer than statusTimeout so ctx cancel fires
		{match: `"$1" = "status"`, exitCode: 0, sleep: 30},
	})
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
	installStub(t, dir, []stubSpec{
		{match: `"$1" = "status"`, exitCode: 0},
	})
	t.Setenv("PATH", dir)
	t.Setenv(OptOutEnv, "0")

	got := Detect(context.Background(), log.NewLogger())

	assert.Equal(t, Detection{}, got)
}
