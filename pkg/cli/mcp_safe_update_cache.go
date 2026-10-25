package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

// CollectLockFileManifests scans all *.lock.yml files in workflowsDir (defaults to
// ".github/workflows") and extracts their gh-aw-manifest headers.  The returned map
// is keyed by the lock-file path exactly as produced by the compiler so that lookups
// in the compiler's priorManifests map always match.
//
// This function must be called at MCP server startup, before any agent interaction,
// so that the returned manifests cannot be tampered with by the agent.
func CollectLockFileManifests(workflowsDir string) map[string]*workflow.GHAWManifest {
	if workflowsDir == "" {
		workflowsDir = constants.GetWorkflowDir()
	}

	result := make(map[string]*workflow.GHAWManifest)

	pattern := filepath.Join(workflowsDir, "*.lock.yml")
	lockFiles, err := filepath.Glob(pattern)
	if err != nil {
		mcpLog.Printf("Failed to glob lock files in %s: %v", workflowsDir, err)
		return result
	}

	for _, lockFile := range lockFiles {
		content, err := os.ReadFile(lockFile)
		if err != nil {
			mcpLog.Printf("Failed to read lock file %s: %v", lockFile, err)
			continue
		}

		manifest, err := workflow.ExtractGHAWManifestFromLockFile(string(content))
		if err != nil {
			mcpLog.Printf("Failed to extract manifest from %s: %v", lockFile, err)
			continue
		}

		// Store with the same path key the compiler will use (filepath.Clean of the lock file path).
		result[filepath.Clean(lockFile)] = manifest
		if manifest != nil {
			mcpLog.Printf("Cached manifest for %s: %d secret(s), %d action(s)", lockFile, len(manifest.Secrets), len(manifest.Actions))
		} else {
			mcpLog.Printf("Cached nil manifest for %s (no gh-aw-manifest header)", lockFile)
		}
	}

	mcpLog.Printf("Pre-cached %d lock-file manifest(s) at startup", len(result))
	return result
}

// WritePriorManifestFile serialises the manifest cache to a temporary JSON file and
// returns its path.  The caller is responsible for removing the file when done.
func WritePriorManifestFile(cache map[string]*workflow.GHAWManifest) (cachePath string, writeErr error) {
	mcpLog.Printf("Writing prior manifest cache to temp file: %d entries", len(cache))
	data, err := json.Marshal(cache)
	if err != nil {
		return "", fmt.Errorf("marshal manifest cache: %w", err)
	}

	f, err := os.CreateTemp("", "gh-aw-manifest-cache-*.json")
	if err != nil {
		return "", fmt.Errorf("create manifest cache file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			if writeErr == nil {
				writeErr = fmt.Errorf("close manifest cache file: %w", err)
			} else {
				mcpLog.Printf("Failed to close manifest cache file: %v", err)
			}
		}
		if writeErr != nil {
			cachePath = ""
			if err := os.Remove(f.Name()); err != nil {
				mcpLog.Printf("Failed to remove incomplete manifest cache file: %v", err)
			}
		}
	}()

	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("write manifest cache file: %w", err)
	}

	mcpLog.Printf("Prior manifest cache written to: %s (%d bytes)", f.Name(), len(data))
	return f.Name(), nil
}
