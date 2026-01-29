//go:build !integration

package campaign

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverWorkflows(t *testing.T) {
	tests := []struct {
		name          string
		workflows     map[string]string // filename -> content
		goals         []string
		expectedCount int
		expectedIDs   []string
	}{
		{
			name: "discover security workflow",
			workflows: map[string]string{
				"security-scanner.md": `---
name: Security Scanner
description: Scan for vulnerabilities
---
# Security Scanner
Scan repositories for security vulnerabilities`,
			},
			goals:         []string{"security"},
			expectedCount: 1,
			expectedIDs:   []string{"security-scanner"},
		},
		{
			name: "discover multiple matching workflows",
			workflows: map[string]string{
				"dependency-updater.md": `---
name: Dependency Updater
description: Update npm packages
---
# Dependency Updater`,
				"package-scanner.md": `---
name: Package Scanner
description: Scan for outdated dependencies
---
# Package Scanner`,
			},
			goals:         []string{"dependencies"},
			expectedCount: 2,
			expectedIDs:   []string{"dependency-updater", "package-scanner"},
		},
		{
			name: "skip campaign files",
			workflows: map[string]string{
				"my-campaign.campaign.md": `---
name: My Campaign
---
# Campaign`,
				"security-scanner.md": `---
name: Security Scanner
description: Scan for vulnerabilities
---
# Scanner`,
			},
			goals:         []string{"security"},
			expectedCount: 1,
			expectedIDs:   []string{"security-scanner"},
		},
		{
			name: "no matching workflows",
			workflows: map[string]string{
				"random-workflow.md": `---
name: Random Workflow
description: Does something random
---
# Random`,
			},
			goals:         []string{"security"},
			expectedCount: 0,
			expectedIDs:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()
			workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowsDir, 0755))

			// Create workflow files
			for filename, content := range tt.workflows {
				filePath := filepath.Join(workflowsDir, filename)
				require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))
			}

			// Discover workflows
			matches, err := DiscoverWorkflows(tmpDir, tt.goals)
			require.NoError(t, err)

			// Verify results
			assert.Len(t, matches, tt.expectedCount, "Expected %d matches, got %d", tt.expectedCount, len(matches))

			// Verify IDs
			actualIDs := make([]string, len(matches))
			for i, match := range matches {
				actualIDs[i] = match.ID
			}

			if tt.expectedCount > 0 {
				for _, expectedID := range tt.expectedIDs {
					assert.Contains(t, actualIDs, expectedID, "Expected workflow ID %s not found", expectedID)
				}
			}
		})
	}
}

func TestMatchWorkflow(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		goals           []string
		expectedMatch   bool
		expectedScore   int
		minKeywordCount int
	}{
		{
			name: "security workflow matches security goal",
			content: `---
name: Security Scanner
description: Scan for security vulnerabilities
---
# Security Scanner`,
			goals:           []string{"security"},
			expectedMatch:   true,
			expectedScore:   20, // "security" and "vulnerabilities"
			minKeywordCount: 2,
		},
		{
			name: "dependency workflow matches dependency goal",
			content: `---
name: Dependency Updater
description: Update npm dependencies and packages
---
# Updater`,
			goals:           []string{"dependencies"},
			expectedMatch:   true,
			expectedScore:   20,
			minKeywordCount: 2,
		},
		{
			name: "no match for unrelated workflow",
			content: `---
name: Random Workflow
description: Does something random
---
# Random`,
			goals:         []string{"security"},
			expectedMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpFile := filepath.Join(t.TempDir(), "test.md")
			require.NoError(t, os.WriteFile(tmpFile, []byte(tt.content), 0644))

			// Match workflow
			match, err := matchWorkflow(tmpFile, tt.goals)
			require.NoError(t, err)

			if tt.expectedMatch {
				require.NotNil(t, match, "Expected a match but got nil")
				assert.GreaterOrEqual(t, match.Score, tt.expectedScore, "Expected score >= %d, got %d", tt.expectedScore, match.Score)
				assert.GreaterOrEqual(t, len(match.Keywords), tt.minKeywordCount, "Expected at least %d keywords", tt.minKeywordCount)
			} else {
				assert.Nil(t, match, "Expected no match but got one")
			}
		})
	}
}

func TestSortWorkflowMatches(t *testing.T) {
	matches := []WorkflowMatch{
		{ID: "low", Score: 10},
		{ID: "high", Score: 50},
		{ID: "medium", Score: 30},
	}

	sortWorkflowMatches(matches)

	assert.Equal(t, "high", matches[0].ID)
	assert.Equal(t, "medium", matches[1].ID)
	assert.Equal(t, "low", matches[2].ID)
}
