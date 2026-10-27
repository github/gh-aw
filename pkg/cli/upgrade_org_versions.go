package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
)

func extractCompilerVersionsFromWorkflowsDir(directory string) ([]string, int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, 0, err
	}
	var versions []string
	unknown := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lock.yml") {
			continue
		}
		version, err := readCompilerVersionFromLockHeader(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, 0, fmt.Errorf("reading %s: %w", entry.Name(), err)
		}
		if version == "" {
			unknown++
		} else {
			versions = append(versions, version)
		}
	}
	slices.Sort(versions)
	return slices.Compact(versions), unknown, nil
}

// Read the comment header from the existing checkout, without per-file API calls
// or loading the generated workflow body.
func readCompilerVersionFromLockHeader(path string) (version string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return "", nil
		}
		comment := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if payload, found := strings.CutPrefix(comment, "gh-aw-metadata:"); found {
			payload = strings.TrimSpace(payload)
			if !strings.HasPrefix(payload, "{") {
				return "", errors.New("invalid gh-aw-metadata header: expected a JSON object; recompile with gh aw compile")
			}
			var metadata workflow.LockMetadata
			if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
				return "", fmt.Errorf("lock metadata JSON must be a valid JSON object; recompile with gh aw compile: %w", err)
			}
			return normalizeDisplayVersion(metadata.CompilerVersion), nil
		}
		if errors.Is(readErr, io.EOF) {
			return "", nil
		}
	}
}

func formatUpgradeCompilerVersions(preview orgRepoPreview) string {
	var versions []string
	for _, version := range preview.CurrentVersions {
		if normalized := normalizeDisplayVersion(version); normalized != "" {
			versions = append(versions, normalized)
		}
	}
	slices.Sort(versions)
	versions = slices.Compact(versions)
	if preview.UnknownVersionLocks > 0 {
		versions = append(versions, fmt.Sprintf("unknown (%d lock file(s))", preview.UnknownVersionLocks))
	}
	if len(versions) == 0 {
		return "unknown"
	}
	return strings.Join(versions, ", ")
}
