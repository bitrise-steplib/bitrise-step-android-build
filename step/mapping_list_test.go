package step

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_alignedMappingList(t *testing.T) {
	const (
		demoAAB     = "/bitrise/src/app/build/outputs/bundle/demoRelease/app-demo-release.aab"
		prodAAB     = "/bitrise/src/app/build/outputs/bundle/prodRelease/app-prod-release.aab"
		debugAAB    = "/bitrise/src/app/build/outputs/bundle/debug/app-debug.aab"
		demoMapping = "/bitrise/src/app/build/outputs/mapping/demoRelease/mapping.txt"
		prodMapping = "/bitrise/src/app/build/outputs/mapping/prodRelease/mapping.txt"
	)

	t.Run("pairs each app with its variant's mapping", func(t *testing.T) {
		apps := []exportedArtifact{
			{deployPath: "/deploy/app-demo-release.aab", sourcePath: demoAAB},
			{deployPath: "/deploy/app-prod-release.aab", sourcePath: prodAAB},
		}
		mappings := []exportedArtifact{
			{deployPath: "/deploy/mapping.txt", sourcePath: demoMapping},
			{deployPath: "/deploy/mapping-2.txt", sourcePath: prodMapping},
		}

		list, ok := alignedMappingList(apps, mappings)

		assert.True(t, ok)
		assert.Equal(t, []string{"/deploy/mapping.txt", "/deploy/mapping-2.txt"}, list)
	})

	t.Run("empty placeholder keeps positions when a variant has no mapping", func(t *testing.T) {
		apps := []exportedArtifact{
			{deployPath: "/deploy/app-demo-release.aab", sourcePath: demoAAB},
			{deployPath: "/deploy/app-debug.aab", sourcePath: debugAAB},
			{deployPath: "/deploy/app-prod-release.aab", sourcePath: prodAAB},
		}
		mappings := []exportedArtifact{
			{deployPath: "/deploy/mapping.txt", sourcePath: demoMapping},
			{deployPath: "/deploy/mapping-2.txt", sourcePath: prodMapping},
		}

		list, ok := alignedMappingList(apps, mappings)

		assert.True(t, ok)
		assert.Equal(t, []string{"/deploy/mapping.txt", "", "/deploy/mapping-2.txt"}, list)
	})

	t.Run("no mapping matched any app variant", func(t *testing.T) {
		apps := []exportedArtifact{
			{deployPath: "/deploy/app-debug.aab", sourcePath: debugAAB},
		}
		mappings := []exportedArtifact{
			{deployPath: "/deploy/mapping.txt", sourcePath: demoMapping},
		}

		_, ok := alignedMappingList(apps, mappings)

		assert.False(t, ok)
	})

	t.Run("no mappings at all", func(t *testing.T) {
		apps := []exportedArtifact{
			{deployPath: "/deploy/app-demo-release.aab", sourcePath: demoAAB},
		}

		_, ok := alignedMappingList(apps, nil)

		assert.False(t, ok)
	})
}
