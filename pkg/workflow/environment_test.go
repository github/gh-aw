//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestEnvironmentSupport(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		expected    string
	}{
		{
			name: "simple environment name",
			frontmatter: `---
on:
  issues:
    types: [opened]
environment: production
---

# Test Workflow

This is a test.`,
			expected: "environment: production",
		},
		{
			name: "environment object with name and URL",
			frontmatter: `---
on:
  issues:
    types: [opened]
environment:
  name: staging
  url: https://staging.example.com
---

# Test Workflow

This is a test.`,
			expected: `environment:
      name: staging
      url: https://staging.example.com`,
		},
		{
			name: "environment with expressions",
			frontmatter: `---
on:
  workflow_dispatch:
    inputs:
      environment:
        description: 'Target environment'
        required: true
environment:
  name: ${{ github.event.inputs.environment }}
  url: ${{ steps.deploy.outputs.url }}
---

# Test Workflow

This is a test.`,
			expected: `environment:
      name: ${{ github.event.inputs.environment }}
      url: ${{ steps.deploy.outputs.url }}`,
		},
		{
			name: "no environment specified",
			frontmatter: `---
on:
  issues:
    types: [opened]
---

# Test Workflow

This is a test.`,
			expected: "", // No environment section should be present
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary workflow file
			tmpDir, err := os.MkdirTemp("", "workflow-environment-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			workflowFile := tmpDir + "/test.md"
			err = os.WriteFile(workflowFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Parse the workflow
			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(workflowFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Check if environment is correctly extracted
			if tt.expected == "" {
				if workflowData.Environment != "" {
					t.Errorf("Expected no environment, but got: %s", workflowData.Environment)
				}
			} else {
				if !strings.Contains(workflowData.Environment, strings.TrimSpace(strings.Split(tt.expected, "\n")[0])) {
					t.Errorf("Expected environment to contain '%s', but got: %s", tt.expected, workflowData.Environment)
				}
			}

			// Generate YAML and check if environment appears in the main job
			yamlContent, err := compiler.generateYAML(workflowData, workflowFile)
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			if tt.expected == "" {
				// Should not contain environment section
				if strings.Contains(yamlContent, "environment:") {
					t.Errorf("Expected no environment in YAML, but found environment section")
				}
			} else {
				// Should contain environment section in the main job
				lines := strings.Split(yamlContent, "\n")
				inMainJob := false
				foundEnvironment := false

				for i, line := range lines {
					if strings.Contains(line, string(constants.AgentJobName)+":") {
						inMainJob = true
						continue
					}
					if inMainJob && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && line != "  "+string(constants.AgentJobName)+":" {
						// Found next job, stop looking
						break
					}
					if inMainJob && strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "environment:") {
						foundEnvironment = true
						// For complex environment objects, check the next few lines too
						if strings.Contains(tt.expected, "name:") {
							nextLines := []string{line}
							for j := i + 1; j < len(lines) && j < i+5; j++ {
								if strings.HasPrefix(lines[j], "      ") || strings.TrimSpace(lines[j]) == "" {
									nextLines = append(nextLines, lines[j])
								} else {
									break
								}
							}
							combinedLines := strings.Join(nextLines, "\n")
							if !strings.Contains(combinedLines, "name:") {
								t.Errorf("Expected environment object with name, but didn't find it in: %s", combinedLines)
							}
						}
						break
					}
				}

				if !foundEnvironment {
					t.Errorf("Expected environment section in main job, but not found in YAML:\n%s", yamlContent)
				}
			}
		})
	}
}

func TestEnvironmentIndentation(t *testing.T) {
	frontmatter := `---
on:
  issues:
    types: [opened]
environment:
  name: production
  url: https://prod.example.com
---

# Test Workflow

This is a test.`

	// Create temporary workflow file
	tmpDir, err := os.MkdirTemp("", "workflow-environment-indent-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workflowFile := tmpDir + "/test.md"
	err = os.WriteFile(workflowFile, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Parse and generate YAML
	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	yamlContent, err := compiler.generateYAML(workflowData, workflowFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Check that environment is properly indented within the job
	expectedIndentedEnvironment := `    environment:
      name: production
      url: https://prod.example.com`

	if !strings.Contains(yamlContent, expectedIndentedEnvironment) {
		t.Errorf("Expected properly indented environment section, but got:\n%s", yamlContent)
	}
}
