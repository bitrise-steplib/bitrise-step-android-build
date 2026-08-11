package step

import (
	"testing"

	"github.com/bitrise-io/go-android/v2/gradle"
	"github.com/bitrise-io/go-android/v2/gradle/artifactmap"
	"github.com/stretchr/testify/assert"
)

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
	m, warnings := artifactmap.Build(apkFiles, aabFiles, mappingFiles)

	assert.Empty(t, warnings)
	assert.Equal(t, map[string]map[string]artifactmap.Entry{
		"app": {
			"demoRelease": {Mapping: "mapping.txt", AAB: []string{"app-demo-release.aab"}, APK: []string{}},
			"paidRelease": {Mapping: "mapping-20260805121530.txt", AAB: []string{"app-paid-release.aab"}, APK: []string{}},
		},
	}, m.Modules)
}
