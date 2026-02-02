//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

// shouldSkipFirewallWorkflow returns true if the workflow filename contains ".firewall"
// and the firewall feature is not enabled. This helper should be used in tests that
// iterate over actual workflow files in .github/workflows to skip firewall workflows
// when the GH_AW_FEATURES environment variable doesn't include "firewall".
func shouldSkipFirewallWorkflow(workflowName string) bool {
	return strings.Contains(workflowName, ".firewall") && !isFeatureEnabled(constants.FeatureFlag("firewall"), nil)
}

func TestNormalizeWorkflowName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain name",
			input:    "weekly-research",
			expected: "weekly-research",
		},
		{
			name:     "name with .md extension",
			input:    "weekly-research.md",
			expected: "weekly-research",
		},
		{
			name:     "name with .lock.yml extension",
			input:    "weekly-research.lock.yml",
			expected: "weekly-research",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "name with multiple dots",
			input:    "daily-test.coverage.md",
			expected: "daily-test.coverage",
		},
		{
			name:     "name ending with partial extension",
			input:    "workflow.lock",
			expected: "workflow.lock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringutil.NormalizeWorkflowName(tt.input)
			if result != tt.expected {
				t.Errorf("stringutil.NormalizeWorkflowName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestResolveWorkflowName(t *testing.T) {
	// Create a temporary directory with workflow files
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create sample workflow files
	testWorkflows := map[string]string{
		"weekly-research": "Weekly Research",
		"daily-plan":      "Daily Plan",
		"issue-triage":    "Issue Triage",
	}
	for workflowID, workflowName := range testWorkflows {
		mdFile := filepath.Join(workflowsDir, workflowID+".md")
		lockFile := filepath.Join(workflowsDir, workflowID+".lock.yml")

		err = os.WriteFile(mdFile, []byte("# "+workflowID+"\nSome content"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		lockContent := "name: \"" + workflowName + "\"\non: push\n"
		err = os.WriteFile(lockFile, []byte(lockContent), 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Change to the temp directory
	t.Chdir(tempDir)

	tests := []struct {
		name                 string
		workflowInput        string
		expectedWorkflowName string
		expectError          bool
	}{
		{
			name:                 "valid workflow ID",
			workflowInput:        "weekly-research",
			expectedWorkflowName: "Weekly Research",
			expectError:          false,
		},
		{
			name:                 "valid workflow ID with .md extension",
			workflowInput:        "daily-plan.md",
			expectedWorkflowName: "Daily Plan",
			expectError:          false,
		},
		{
			name:                 "valid workflow ID with .lock.yml extension",
			workflowInput:        "issue-triage.lock.yml",
			expectedWorkflowName: "Issue Triage",
			expectError:          false,
		},
		{
			name:                 "empty workflow ID",
			workflowInput:        "",
			expectedWorkflowName: "",
			expectError:          false,
		},
		{
			name:          "non-existent workflow ID",
			workflowInput: "non-existent",
			expectError:   true,
		},
		{
			name:          "non-existent workflow ID with extension",
			workflowInput: "non-existent.md",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveWorkflowName(tt.workflowInput)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for workflow input %q, but got none", tt.workflowInput)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for workflow input %q: %v", tt.workflowInput, err)
				}
				if result != tt.expectedWorkflowName {
					t.Errorf("Expected workflow name %q, got %q", tt.expectedWorkflowName, result)
				}
			}
		})
	}
}

func TestResolveWorkflowName_MissingLockFile(t *testing.T) {
	// Create a temporary directory with workflow files
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create only the .md file, but not the .lock.yml file
	mdFile := filepath.Join(workflowsDir, "incomplete-workflow.md")
	err = os.WriteFile(mdFile, []byte("# Incomplete Workflow\nSome content"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	t.Chdir(tempDir)

	// Test that it returns an error when lock file is missing
	_, err = ResolveWorkflowName("incomplete-workflow")
	if err == nil {
		t.Error("Expected error when lock file is missing, but got none")
	}
	if err != nil && !contains(err.Error(), "Run 'gh aw compile'") {
		t.Errorf("Expected error to mention compilation, got: %v", err)
	}
}

func TestResolveWorkflowName_InvalidYAML(t *testing.T) {
	// Create a temporary directory with workflow files
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create workflow with invalid YAML
	mdFile := filepath.Join(workflowsDir, "invalid-yaml.md")
	lockFile := filepath.Join(workflowsDir, "invalid-yaml.lock.yml")

	err = os.WriteFile(mdFile, []byte("# Invalid YAML\nSome content"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create lock file with invalid YAML
	err = os.WriteFile(lockFile, []byte("invalid: yaml: content: ["), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	t.Chdir(tempDir)

	// Test that it returns an error when YAML is invalid
	_, err = ResolveWorkflowName("invalid-yaml")
	if err == nil {
		t.Error("Expected error when YAML is invalid, but got none")
	}
	if err != nil && !contains(err.Error(), "failed to parse YAML") {
		t.Errorf("Expected error to mention YAML parsing, got: %v", err)
	}
}

func TestResolveWorkflowName_MissingNameField(t *testing.T) {
	// Create a temporary directory with workflow files
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create workflow with valid YAML but missing name field
	mdFile := filepath.Join(workflowsDir, "no-name.md")
	lockFile := filepath.Join(workflowsDir, "no-name.lock.yml")

	err = os.WriteFile(mdFile, []byte("# No Name\nSome content"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create lock file with valid YAML but no name field
	err = os.WriteFile(lockFile, []byte("on: push\njobs: {}\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	t.Chdir(tempDir)

	// Test that it returns an error when name field is missing
	_, err = ResolveWorkflowName("no-name")
	if err == nil {
		t.Error("Expected error when name field is missing, but got none")
	}
	if err != nil && !contains(err.Error(), "workflow name not found") {
		t.Errorf("Expected error to mention missing workflow name, got: %v", err)
	}
}

func TestResolveWorkflowName_ExistingAgenticWorkflow(t *testing.T) {
	// Get current working directory
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal("Cannot determine current directory")
	}

	// The test is run from the project root where go.mod is located
	// Check if we are already in the right place by looking for go.mod and .github/workflows
	projectRoot := currentDir

	// If we're in a subdirectory (like pkg/workflow), go up to find the project root
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(projectRoot, constants.GetWorkflowDir())); err == nil {
				break // Found project root
			}
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			t.Skipf("Cannot find project root with go.mod and .github/workflows")
		}
		projectRoot = parent
	}

	// Change to project root if needed
	if projectRoot != currentDir {
		err = os.Chdir(projectRoot)
		if err != nil {
			t.Skipf("Cannot change to project root: %v", err)
		}
		defer func() {
			if err := os.Chdir(currentDir); err != nil {
				t.Errorf("Failed to restore working directory: %v", err)
			}
		}()
	}

	// Test with known existing workflows - we'll read the actual name from lock files
	knownWorkflows := []string{"weekly-research", "daily-plan", "issue-triage"}

	for _, workflow := range knownWorkflows {
		t.Run("existing_"+workflow, func(t *testing.T) {
			workflowsDir := ".github/workflows"
			mdFile := filepath.Join(workflowsDir, workflow+".md")
			lockFile := filepath.Join(workflowsDir, workflow+".lock.yml")

			// Skip .firewall workflows unless the firewall feature is enabled
			if shouldSkipFirewallWorkflow(workflow) {
				t.Skipf("Skipping firewall workflow %s (feature not enabled)", workflow)
			}

			// Check if both files exist
			if _, err := os.Stat(mdFile); err != nil {
				t.Skipf("Workflow %s.md not found, skipping", workflow)
			}
			if _, err := os.Stat(lockFile); err != nil {
				t.Skipf("Workflow %s.lock.yml not found, skipping", workflow)
			}

			// Test resolving the workflow
			result, err := ResolveWorkflowName(workflow)
			if err != nil {
				t.Errorf("Error resolving existing workflow %s: %v", workflow, err)
			}

			// The result should be the actual workflow name from the YAML, not the filename
			if result == "" {
				t.Errorf("Expected non-empty workflow name for %s", workflow)
			}
			// Since we don't know the exact content of the real lock files,
			// just verify we get a non-empty string that's different from the filename
			if result == workflow+".lock.yml" {
				t.Errorf("Expected workflow name from YAML, but got filename %s", result)
			}

			// Test with different input formats - should all return the same workflow name
			result2, err := ResolveWorkflowName(workflow + ".md")
			if err != nil {
				t.Errorf("Error resolving workflow %s.md: %v", workflow, err)
			}
			if result2 != result {
				t.Errorf("Expected %s for input %s.md, got %s", result, workflow, result2)
			}

			result3, err := ResolveWorkflowName(workflow + ".lock.yml")
			if err != nil {
				t.Errorf("Error resolving workflow %s.lock.yml: %v", workflow, err)
			}
			if result3 != result {
				t.Errorf("Expected %s for input %s.lock.yml, got %s", result, workflow, result3)
			}
		})
	}
}

func TestShouldSkipFirewallWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		workflowName string
		featureValue string
		shouldSkip   bool
	}{
		{
			name:         "regular workflow without firewall feature",
			workflowName: "weekly-research",
			featureValue: "",
			shouldSkip:   false,
		},
		{
			name:         "firewall workflow without firewall feature",
			workflowName: "dev.firewall",
			featureValue: "",
			shouldSkip:   true,
		},
		{
			name:         "firewall workflow with firewall feature enabled",
			workflowName: "dev.firewall",
			featureValue: "firewall",
			shouldSkip:   false,
		},
		{
			name:         "firewall workflow with multiple features including firewall",
			workflowName: "test.firewall.workflow",
			featureValue: "feature1,firewall,feature2",
			shouldSkip:   false,
		},
		{
			name:         "regular workflow with firewall feature enabled",
			workflowName: "daily-plan",
			featureValue: "firewall",
			shouldSkip:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the environment variable
			if tt.featureValue != "" {
				t.Setenv("GH_AW_FEATURES", tt.featureValue)
			}

			result := shouldSkipFirewallWorkflow(tt.workflowName)
			if result != tt.shouldSkip {
				t.Errorf("shouldSkipFirewallWorkflow(%q) with GH_AW_FEATURES=%q = %v, expected %v",
					tt.workflowName, tt.featureValue, result, tt.shouldSkip)
			}
		})
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && s[len(s)-len(substr):] == substr) ||
		(len(s) > len(substr) && s[:len(substr)] == substr) ||
		(len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
