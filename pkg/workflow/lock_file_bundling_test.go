//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// TestLockFilesHaveNoBundledRequires verifies that all compiled lock.yml files
// have properly bundled JavaScript code with no local require() statements.
// This ensures that code in GitHub Script steps is self-contained and doesn't
// reference external .cjs files that won't be available at runtime.
func TestLockFilesHaveNoBundledRequires(t *testing.T) {
	// Find the repository root
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repository root: %v", err)
	}

	workflowsDir := filepath.Join(repoRoot, ".github", "workflows")

	// Find all .lock.yml files
	lockFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	if err != nil {
		t.Fatalf("Failed to find lock.yml files: %v", err)
	}

	if len(lockFiles) == 0 {
		t.Skip("No lock.yml files found to test")
	}

	t.Logf("Found %d lock.yml files to check", len(lockFiles))

	// Pattern to match local requires like require("./file.cjs") or require('../file.cjs')
	localRequirePattern := regexp.MustCompile(`require\s*\(\s*["']\.{1,2}/[^"']+\.cjs["']\s*\)`)

	var failedFiles []string
	var totalScriptSteps int

	for _, lockFile := range lockFiles {
		relPath, _ := filepath.Rel(repoRoot, lockFile)
		t.Logf("Checking %s", relPath)

		content, err := os.ReadFile(lockFile)
		if err != nil {
			t.Errorf("Failed to read %s: %v", relPath, err)
			continue
		}

		// Parse YAML
		var workflow map[string]any
		if err := yaml.Unmarshal(content, &workflow); err != nil {
			t.Errorf("Failed to parse YAML in %s: %v", relPath, err)
			continue
		}

		// Navigate to jobs
		jobs, ok := workflow["jobs"].(map[string]any)
		if !ok {
			// No jobs, skip this file
			continue
		}

		// Check each job
		for jobName, jobData := range jobs {
			jobMap, ok := jobData.(map[string]any)
			if !ok {
				continue
			}

			// Get steps
			steps, ok := jobMap["steps"].([]any)
			if !ok {
				continue
			}

			// Check each step
			for stepIdx, stepData := range steps {
				stepMap, ok := stepData.(map[string]any)
				if !ok {
					continue
				}

				// Check if this is a github-script step
				uses, ok := stepMap["uses"].(string)
				if !ok || !strings.Contains(uses, "actions/github-script@") {
					continue
				}

				totalScriptSteps++

				// Get the with.script content
				withMap, ok := stepMap["with"].(map[string]any)
				if !ok {
					continue
				}

				script, ok := withMap["script"].(string)
				if !ok {
					continue
				}

				// Check for local requires
				matches := localRequirePattern.FindAllString(script, -1)
				if len(matches) > 0 {
					stepName := "unnamed"
					if name, ok := stepMap["name"].(string); ok {
						stepName = name
					}

					t.Errorf("Found unbundled local requires in %s (job: %s, step: %s, index: %d):",
						relPath, jobName, stepName, stepIdx)
					for _, match := range matches {
						t.Errorf("  - %s", match)
					}

					// Add to failed files if not already present
					alreadyFailed := false
					for _, f := range failedFiles {
						if f == relPath {
							alreadyFailed = true
							break
						}
					}
					if !alreadyFailed {
						failedFiles = append(failedFiles, relPath)
					}
				}
			}
		}
	}

	t.Logf("Checked %d lock.yml files with %d github-script steps", len(lockFiles), totalScriptSteps)

	if len(failedFiles) > 0 {
		t.Errorf("\nFound %d lock.yml file(s) with unbundled local requires:", len(failedFiles))
		for _, file := range failedFiles {
			t.Errorf("  - %s", file)
		}
		t.Error("\nThese files should have bundled JavaScript code without local require() statements.")
		t.Error("The bundler should inline all local .cjs dependencies into the script.")
		t.Error("Run 'make recompile' to regenerate the lock files with proper bundling.")
	}
}

// findRepoRoot finds the repository root by looking for go.mod
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
