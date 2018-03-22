package gradle

import (
	"path/filepath"

	"github.com/bitrise-io/go-utils/command"
)

// Artifact ...
type Artifact struct {
	Path string
	Name string
}

// Export ...
func (artifact Artifact) Export(destination string) error {
	return command.CopyFile(artifact.Path, filepath.Join(destination, artifact.Name))
}
