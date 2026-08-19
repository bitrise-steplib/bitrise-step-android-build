package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-android/v2/gradle"
	"github.com/bitrise-io/go-android/v2/gradle/artifactmap"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_freeDeployName_SameSecondCollision covers the case that used to lose a
// file: two variants' mapping.txt copied into the flat deploy dir inside one
// second. The timestamp suffix alone repeats, so the second copy overwrote the
// first and the artifact map paired both variants with the survivor.
func Test_freeDeployName_SameSecondCollision(t *testing.T) {
	deployDir := t.TempDir()
	build := AndroidBuild{logger: log.NewLogger(), pathChecker: pathutil.NewPathChecker()}

	// nothing there yet: the artifact keeps its own name
	first, err := build.freeDeployName(deployDir, "mapping.txt")
	require.NoError(t, err)
	assert.Equal(t, "mapping.txt", first)
	require.NoError(t, os.WriteFile(filepath.Join(deployDir, first), []byte("demoRelease"), 0600))

	second, err := build.freeDeployName(deployDir, "mapping.txt")
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
	require.NoError(t, os.WriteFile(filepath.Join(deployDir, second), []byte("paidRelease"), 0600))

	// the third export lands in the same second as the second one
	third, err := build.freeDeployName(deployDir, "mapping.txt")
	require.NoError(t, err)
	assert.NotEqual(t, second, third, "a same-second collision must not reuse the taken name")
	require.NoError(t, os.WriteFile(filepath.Join(deployDir, third), []byte("fullRelease"), 0600))

	// each variant's content survives under its own name
	for name, want := range map[string]string{first: "demoRelease", second: "paidRelease", third: "fullRelease"} {
		content, err := os.ReadFile(filepath.Join(deployDir, name))
		require.NoError(t, err)
		assert.Equal(t, want, string(content), "%s should still hold its own variant's file", name)
	}
}

func Test_deployPaths(t *testing.T) {
	exported := []exportedArtifact{
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/apk/demo/release/a.apk"}, deployPath: "/deploy/a.apk"},
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/apk/paid/release/b.apk"}, deployPath: "/deploy/b.apk"},
	}

	assert.Equal(t, []string{"/deploy/a.apk", "/deploy/b.apk"}, deployPaths(exported))
	assert.Empty(t, deployPaths(nil))
}

func Test_artifactMapFiles_SelectedTypeDeterminesAppList(t *testing.T) {
	apps := []exportedArtifact{
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/apk/demo/release/app-demo-release.apk"}, deployPath: "/deploy/app-demo-release.apk"},
	}
	mappings := []exportedArtifact{
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/mapping/demoRelease/mapping.txt"}, deployPath: "/deploy/mapping.txt"},
	}

	apkFiles, aabFiles, mappingFiles := artifactMapFiles(apkAppType, apps, mappings)
	assert.Len(t, apkFiles, 1)
	assert.Empty(t, aabFiles)
	assert.Len(t, mappingFiles, 1)
	assert.Equal(t, artifactmap.File{DeployPath: "/deploy/app-demo-release.apk", SourcePath: "/src/app/build/outputs/apk/demo/release/app-demo-release.apk"}, apkFiles[0])

	apkFiles, aabFiles, _ = artifactMapFiles(aabAppType, apps, mappings)
	assert.Empty(t, apkFiles)
	assert.Len(t, aabFiles, 1)
}

// Test_artifactMap_PairsVariants locks the step-level expectation: artifacts
// exported the way Export() exports them (deploy path + gradle source path)
// group into a map that pairs each variant's app file with that variant's
// mapping file, even after a collision-avoidance rename in the deploy dir.
func Test_artifactMap_PairsVariants(t *testing.T) {
	apps := []exportedArtifact{
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/bundle/demoRelease/app-demo-release.aab"}, deployPath: "/deploy/app-demo-release.aab"},
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/bundle/paidRelease/app-paid-release.aab"}, deployPath: "/deploy/app-paid-release.aab"},
	}
	mappings := []exportedArtifact{
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/mapping/demoRelease/mapping.txt"}, deployPath: "/deploy/mapping.txt"},
		{artifact: gradle.Artifact{Path: "/src/app/build/outputs/mapping/paidRelease/mapping.txt"}, deployPath: "/deploy/mapping-20260805121530.txt"},
	}

	apkFiles, aabFiles, mappingFiles := artifactMapFiles(aabAppType, apps, mappings)
	m, warnings := artifactmap.Build(apkFiles, aabFiles, nil, mappingFiles)

	assert.Empty(t, warnings)
	assert.Equal(t, map[string]map[string]artifactmap.Entry{
		"app": {
			"demoRelease": {Mapping: "mapping.txt", AAB: []string{"app-demo-release.aab"}, APK: []string{}},
			"paidRelease": {Mapping: "mapping-20260805121530.txt", AAB: []string{"app-paid-release.aab"}, APK: []string{}},
		},
	}, m.Modules)
}
