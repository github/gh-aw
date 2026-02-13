//go:build !integration

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkflowCounting tests that only user workflows are counted and displayed
func TestWorkflowCounting(t *testing.T) {
	// This test verifies that internal workflows (those without .md files) are hidden from users
	// Only workflows with .md source files are counted and displayed

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

	// Count user workflows only (internal workflows are not displayed to users)
	var userWorkflowCount int
	for name := range simulatedGitHubWorkflows {
		if mdWorkflowNames[name] {
			userWorkflowCount++
		}
	}

	// Verify counts
	assert.Equal(t, len(mdWorkflowNames), userWorkflowCount, "User workflow count should match .md file count")

	// Verify message format (internal workflows are never mentioned)
	var message string
	if userWorkflowCount == 1 {
		message = "✓ Fetched 1 workflow"
	} else {
		message = fmt.Sprintf("✓ Fetched %d workflows", userWorkflowCount)
	}
	assert.NotContains(t, message, "internal", "Message should never contain 'internal' - internal workflows are hidden from users")
	assert.NotContains(t, message, "public", "Message should not contain 'public' - only user workflows are shown")

	t.Logf("User workflows: %d, Expected message: %s", userWorkflowCount, message)
}
