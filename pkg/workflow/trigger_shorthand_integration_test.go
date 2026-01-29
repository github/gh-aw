//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTriggerShorthandIntegration(t *testing.T) {
	tests := []struct {
		name           string
		markdown       string
		wantTrigger    string
		wantNoCompile  bool
		wantErrContain string
	}{
		{
			name: "push trigger shorthand",
			markdown: `---
on: push
---
# Test Workflow
Test workflow for push trigger`,
			wantTrigger: `"on": push`,
		},
		{
			name: "push to branch shorthand",
			markdown: `---
on: push to main
---
# Test Workflow
Test workflow for push to branch`,
			wantTrigger: "branches:",
		},
		{
			name: "pull_request opened shorthand",
			markdown: `---
on: pull_request opened
---
# Test Workflow
Test workflow for pull request opened`,
			wantTrigger: "types:",
		},
		{
			name: "manual shorthand",
			markdown: `---
on: manual
---
# Test Workflow
Test workflow for manual dispatch`,
			wantTrigger: "workflow_dispatch:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for this test
			tmpDir := t.TempDir()
			mdFile := filepath.Join(tmpDir, "test.md")
			lockFile := filepath.Join(tmpDir, "test.lock.yml")

			// Write the markdown to a file
			if err := os.WriteFile(mdFile, []byte(tt.markdown), 0644); err != nil {
				t.Fatalf("Failed to write test markdown: %v", err)
			}

			c := NewCompiler()

			err := c.CompileWorkflow(mdFile)

			if tt.wantNoCompile {
				if err == nil {
					t.Errorf("Expected compilation to fail but it succeeded")
				}
				if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("Error message should contain %q but got: %s", tt.wantErrContain, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected compilation error: %v", err)
			}

			// Read the generated lock file
			yamlOutput, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			yamlStr := string(yamlOutput)

			if !strings.Contains(yamlStr, tt.wantTrigger) {
				t.Errorf("Compiled YAML should contain %q\nGot:\n%s", tt.wantTrigger, yamlStr)
			}

			if tt.wantTrigger == `"on": push` || tt.wantTrigger == `"on": pull_request` {
				// Simple triggers remain as-is, no workflow_dispatch added
				return
			}

			// Verify workflow_dispatch is added for most triggers
			if tt.wantTrigger != "workflow_dispatch:" && tt.name != "manual shorthand" {
				if !strings.Contains(yamlStr, "workflow_dispatch:") {
					t.Errorf("Compiled YAML should include workflow_dispatch\nGot:\n%s", yamlStr)
				}
			}
		})
	}
}

func TestTriggerShorthandWithFilters(t *testing.T) {
	tests := []struct {
		name        string
		markdown    string
		wantContain []string
	}{
		{
			name: "push to main with branch filter",
			markdown: `---
on: push to main
---
# Test Workflow
Test`,
			wantContain: []string{
				"push:",
				"branches:",
				"- main",
			},
		},
		{
			name: "push tags with pattern",
			markdown: `---
on: push tags v*
---
# Test Workflow
Test`,
			wantContain: []string{
				"push:",
				"tags:",
				"- v*",
			},
		},
		{
			name: "pull_request affecting path",
			markdown: `---
on: pull_request affecting src/**.go
---
# Test Workflow
Test`,
			wantContain: []string{
				"pull_request:",
				"paths:",
				"- src/**.go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for this test
			tmpDir := t.TempDir()
			mdFile := filepath.Join(tmpDir, "test.md")
			lockFile := filepath.Join(tmpDir, "test.lock.yml")

			// Write the markdown to a file
			if err := os.WriteFile(mdFile, []byte(tt.markdown), 0644); err != nil {
				t.Fatalf("Failed to write test markdown: %v", err)
			}

			c := NewCompiler()

			err := c.CompileWorkflow(mdFile)
			if err != nil {
				t.Fatalf("Unexpected compilation error: %v", err)
			}

			// Read the generated lock file
			yamlOutput, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			yamlStr := string(yamlOutput)

			for _, want := range tt.wantContain {
				if !strings.Contains(yamlStr, want) {
					t.Errorf("Compiled YAML should contain %q\nGot:\n%s", want, yamlStr)
				}
			}
		})
	}
}

func TestTriggerShorthandBackwardCompatibility(t *testing.T) {
	// Test that existing trigger formats still work
	tests := []struct {
		name     string
		markdown string
	}{
		{
			name: "slash command shorthand",
			markdown: `---
on: /test
---
# Test
Test`,
		},
		{
			name: "label trigger shorthand",
			markdown: `---
on: issue labeled bug
---
# Test
Test`,
		},
		{
			name: "traditional YAML format with object",
			markdown: `---
on:
  push:
    branches: [main]
  pull_request:
    types: [opened]
---
# Test
Test`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for this test
			tmpDir := t.TempDir()
			mdFile := filepath.Join(tmpDir, "test.md")

			// Write the markdown to a file
			if err := os.WriteFile(mdFile, []byte(tt.markdown), 0644); err != nil {
				t.Fatalf("Failed to write test markdown: %v", err)
			}

			c := NewCompiler()

			err := c.CompileWorkflow(mdFile)
			if err != nil {
				t.Errorf("Backward compatibility broken: %v", err)
			}
		})
	}
}
