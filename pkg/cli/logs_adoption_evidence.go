package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Collection inventories preserve distinct invocation artifacts. Ordinary logs
// retain artifact-named directories alongside their flattened root copies.
func logsAdoptionEvidenceDirs(runDir string, names []string) ([]string, error) {
	dirs, inventoried, err := readEpisodeAllArtifactDirs(runDir)
	if err != nil || inventoried {
		return dirs, err
	}
	entries, err := os.ReadDir(runDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && artifactMatchesFilter(entry.Name(), names) {
			dirs = append(dirs, filepath.Join(runDir, entry.Name()))
		}
	}
	return dirs, nil
}

func logsAdoptionEnrichmentDir(manifestDir, runDir string) (string, error) {
	dirs, err := logsAdoptionEvidenceDirs(runDir, []string{"agent-output-fallback", "agent"})
	if err != nil {
		return "", err
	}
	name := logsAdoptionArtifactName(manifestDir)
	if manifestDir == runDir {
		name = "safe-outputs-items"
	}
	prefix, matched := strings.CutSuffix(name, "safe-outputs-items")
	if !matched {
		return "", nil
	}
	for _, suffix := range []string{"agent-output-fallback", "agent"} {
		for _, dir := range dirs {
			if logsAdoptionArtifactName(dir) == prefix+suffix {
				return dir, nil
			}
		}
	}
	if manifestDir == runDir || name == "safe-outputs-items" {
		return runDir, nil // Historical flattened agent output.
	}
	return "", nil
}

func logsAdoptionArtifactName(dir string) string {
	name, err := os.ReadFile(filepath.Join(dir, ".episode-artifact-name"))
	if err == nil {
		return string(name)
	}
	return filepath.Base(dir)
}
