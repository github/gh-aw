//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestExplicitEmptyPermissionsInDevMode tests that explicit empty permissions ({})
// are correctly handled in dev mode - should be set to contents: read for local actions
func TestExplicitEmptyPermissionsInDevMode(t *testing.T) {
	tests := []struct {
		name                  string
		frontmatter           string
		actionMode            ActionMode
		expectedAgentPerms    string
		expectedTopLevelPerms string
		description           string
	}{
		{
			name: "explicit empty permissions in dev mode",
			frontmatter: `---
on: issues
engine: copilot
permissions: {}
---
# Test workflow
Test content`,
			actionMode:            ActionModeDev,
			expectedAgentPerms:    "permissions:\n      contents: read", // Dev mode needs contents: read for local actions
			expectedTopLevelPerms: "permissions: {}",                    // Top-level should stay empty
			description:           "Dev mode with explicit empty permissions should add contents: read to agent job for local actions",
		},
		{
			name: "explicit empty permissions in release mode",
			frontmatter: `---
on: issues
engine: copilot
permissions: {}
---
# Test workflow
Test content`,
			actionMode:            ActionModeRelease,
			expectedAgentPerms:    "permissions:\n      contents: read", // Agent job always gets contents: read
			expectedTopLevelPerms: "permissions: {}",
			description:           "Release mode with explicit empty permissions should add contents: read to agent job",
		},
		{
			name: "no permissions specified in dev mode",
			frontmatter: `---
on: issues
engine: copilot
---
# Test workflow
Test content`,
			actionMode:            ActionModeDev,
			expectedAgentPerms:    "permissions:\n      contents: read", // Dev mode needs contents: read for local actions
			expectedTopLevelPerms: "permissions: {}",                    // Top-level should always be empty
			description:           "Dev mode with no permissions should have empty top-level permissions, contents: read on agent job",
		},
		{
			name: "explicit read-all permissions in dev mode",
			frontmatter: `---
on: issues
engine: copilot
permissions: read-all
---
# Test workflow
Test content`,
			actionMode:            ActionModeDev,
			expectedAgentPerms:    "permissions: read-all", // Should stay read-all
			expectedTopLevelPerms: "permissions: {}",       // Top-level should always be empty
			description:           "Dev mode with read-all permissions should have empty top-level permissions, read-all on agent job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary test file
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Compile with specified action mode
			compiler := NewCompilerWithVersion("v1.0.0")
			compiler.actionMode = tt.actionMode

			err = compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Failed to compile: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			yamlBytes, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			yaml := string(yamlBytes)

			// Check top-level permissions
			if !strings.Contains(yaml, tt.expectedTopLevelPerms) {
				t.Errorf("%s\nExpected top-level permissions:\n%s\n\nBut got YAML:\n%s",
					tt.description, tt.expectedTopLevelPerms, yaml)
			}

			// Find agent job section and check its permissions
			lines := strings.Split(yaml, "\n")
			inAgentJob := false
			agentJobPerms := ""

			for i, line := range lines {
				// Look for agent job
				if strings.Contains(line, "agent:") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
					inAgentJob = true
					continue
				}

				// If we're in agent job, look for permissions
				if inAgentJob {
					// Check if we've moved to another job (new job starts with same indentation as "agent:")
					if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != "" {
						// Another job started
						break
					}

					// Look for permissions in agent job
					if strings.Contains(line, "permissions:") {
						// Capture this line and the next few lines for permissions
						agentJobPerms = line
						for j := i + 1; j < len(lines) && j < i+10; j++ {
							nextLine := lines[j]
							// Stop if we hit another job-level key or empty line
							if strings.HasPrefix(nextLine, "    ") && strings.TrimSpace(nextLine) != "" {
								agentJobPerms += "\n" + nextLine
							} else if strings.TrimSpace(nextLine) == "" {
								continue
							} else {
								break
							}
						}
						break
					}
				}
			}

			if agentJobPerms == "" {
				t.Errorf("%s\nCould not find permissions in agent job. Full YAML:\n%s",
					tt.description, yaml)
			} else {
				// Normalize whitespace for comparison by removing excess spaces
				normalizedPerms := strings.ReplaceAll(agentJobPerms, "      ", "")
				normalizedPerms = strings.ReplaceAll(normalizedPerms, "    ", "")
				normalizedExpected := strings.ReplaceAll(tt.expectedAgentPerms, "      ", "")
				normalizedExpected = strings.ReplaceAll(normalizedExpected, "    ", "")

				if !strings.Contains(normalizedPerms, normalizedExpected) {
					t.Errorf("%s\nExpected agent job permissions to contain:\n%s\n\nBut got:\n%s\n\nFull YAML:\n%s",
						tt.description, tt.expectedAgentPerms, agentJobPerms, yaml)
				}
			}
		})
	}
}
