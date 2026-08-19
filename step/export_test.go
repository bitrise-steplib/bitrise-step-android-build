package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/stretchr/testify/assert"
)

func Test_freeDeployName_SameSecondCollision(t *testing.T) {
	deployDir := t.TempDir()
	build := AndroidBuild{logger: log.NewLogger(), pathChecker: pathutil.NewPathChecker()}

	// nothing there yet: the artifact keeps its own name
	first, err := build.freeDeployName(deployDir, "mapping.txt")
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "mapping.txt", first)
	assert.NoError(t, os.WriteFile(filepath.Join(deployDir, first), []byte("demoRelease"), 0600))

	second, err := build.freeDeployName(deployDir, "mapping.txt")
	assert.NoError(t, err)
	assert.NotEqual(t, first, second)
	assert.NoError(t, os.WriteFile(filepath.Join(deployDir, second), []byte("paidRelease"), 0600))

	// the third export lands in the same second as the second one
	third, err := build.freeDeployName(deployDir, "mapping.txt")
	assert.NoError(t, err)
	assert.NotEqual(t, second, third, "a same-second collision must not reuse the taken name")
	assert.NoError(t, os.WriteFile(filepath.Join(deployDir, third), []byte("fullRelease"), 0600))

	// each variant's content survives under its own name
	for name, want := range map[string]string{first: "demoRelease", second: "paidRelease", third: "fullRelease"} {
		content, err := os.ReadFile(filepath.Join(deployDir, name))
		assert.NoError(t, err)
		assert.Equal(t, want, string(content), "%s should still hold its own variant's file", name)
	}
}
