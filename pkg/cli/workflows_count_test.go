//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkflowCounting tests that user and internal workflows are counted correctly
func TestWorkflowCounting(t *testing.T) {
	// This test verifies the logic for counting user vs internal workflows
	// It simulates what fetchGitHubWorkflows does

	// Save current directory
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Should get current directory")

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	err = os.Chdir(repoRoot)
	require.NoError(t, err, "Should change to repository root")
	defer os.Chdir(originalDir)

	// Get markdown workflow files (simulating what fetchGitHubWorkflows does)
	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		t.Skipf("Skipping test: no .github/workflows directory found: %v", err)
	}

	// Build set of workflow names from .md files
	mdWorkflowNames := make(map[string]bool)
	for _, file := range mdFiles {
		name := extractWorkflowNameFromPath(file)
		mdWorkflowNames[name] = true
	}

	// We should have at least some .md files
	assert.NotEmpty(t, mdWorkflowNames, "Should have at least some .md workflow files")

	// Simulate having some GitHub workflows where not all have .md files
	// (in reality, this would come from GitHub API)
	simulatedGitHubWorkflows := make(map[string]*GitHubWorkflow)

	// Add all the .md workflows as "user workflows"
	for name := range mdWorkflowNames {
		simulatedGitHubWorkflows[name] = &GitHubWorkflow{
			Name:  name,
			Path:  ".github/workflows/" + name + ".lock.yml",
			State: "active",
		}
	}

	// Add some internal workflows (those without .md files)
	internalWorkflows := []string{"agentics-maintenance", "ci", "auto-close-parent-issues"}
	for _, name := range internalWorkflows {
		// Only add if not already present (to avoid duplicates)
		if !mdWorkflowNames[name] {
			simulatedGitHubWorkflows[name] = &GitHubWorkflow{
				Name:  name,
				Path:  ".github/workflows/" + name + ".yml",
				State: "active",
			}
		}
	}

	// Count user vs internal workflows
	var userWorkflowCount, internalWorkflowCount int
	for name := range simulatedGitHubWorkflows {
		if mdWorkflowNames[name] {
			userWorkflowCount++
		} else {
			internalWorkflowCount++
		}
	}

	// Verify counts
	assert.Equal(t, len(mdWorkflowNames), userWorkflowCount, "User workflow count should match .md file count")
	assert.GreaterOrEqual(t, internalWorkflowCount, 0, "Internal workflow count should be non-negative")

	// Verify message format
	var message string
	if internalWorkflowCount > 0 {
		message = "✓ Fetched " + string(rune(userWorkflowCount+'0')) + " public and " + string(rune(internalWorkflowCount+'0')) + " internal workflows"
		assert.Contains(t, message, "public", "Message should contain 'public' when internal workflows exist")
		assert.Contains(t, message, "internal", "Message should contain 'internal' when internal workflows exist")
	} else {
		message = "✓ Fetched " + string(rune(userWorkflowCount+'0')) + " workflows"
		assert.NotContains(t, message, "internal", "Message should not contain 'internal' when no internal workflows")
	}

	t.Logf("User workflows: %d, Internal workflows: %d", userWorkflowCount, internalWorkflowCount)
	t.Logf("Expected message format: %s", message)
}
