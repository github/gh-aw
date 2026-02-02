//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCustomJobsWithFullStepSpecification(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  string
		expectedYAML []string // Strings that should be present in the compiled workflow
		shouldError  bool
	}{
		{
			name: "job with full step properties",
			frontmatter: `---
on: workflow_dispatch
permissions:
  contents: read
jobs:
  test-job:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 1
          persist-credentials: false
      - name: Run command
        id: run-cmd
        run: echo "test"
        env:
          NODE_ENV: test
        continue-on-error: false
        shell: bash
        working-directory: ./src
      - name: Conditional step
        if: github.ref == 'refs/heads/main'
        run: echo "main only"
---

# Test workflow
`,
			expectedYAML: []string{
				"test-job:",
				"runs-on: ubuntu-latest",
				"- name: Checkout",
				"uses: actions/checkout@",
				"with:",
				"fetch-depth: 1",
				"persist-credentials: false",
				"- name: Run command",
				"id: run-cmd",
				"run: echo \"test\"",
				"env:",
				"NODE_ENV: test",
				"continue-on-error: false",
				"shell: bash",
				"working-directory: ./src",
				"- name: Conditional step",
				"if: github.ref == 'refs/heads/main'",
			},
			shouldError: false,
		},
		{
			name: "job with uses action step",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
---

# Build workflow
`,
			expectedYAML: []string{
				"build:",
				"- uses: actions/setup-node@",
				"with:",
				"node-version:", // Both "20" and '20' are valid YAML
				"cache:",        // Both "npm" and 'npm' are valid YAML
			},
			shouldError: false,
		},
		{
			name: "job with run step",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
---

# Test workflow
`,
			expectedYAML: []string{
				"test:",
				"- run: npm test",
			},
			shouldError: false,
		},
		{
			name: "job with if condition on step",
			frontmatter: `---
on: pull_request
permissions:
  contents: read
jobs:
  conditional:
    runs-on: ubuntu-latest
    steps:
      - name: Always run
        run: echo "always"
      - name: Conditional
        if: github.event_name == 'pull_request'
        run: echo "PR only"
---

# Conditional workflow
`,
			expectedYAML: []string{
				"conditional:",
				"- name: Always run",
				"run: echo \"always\"",
				"- name: Conditional",
				"if: github.event_name == 'pull_request'",
				"run: echo \"PR only\"",
			},
			shouldError: false,
		},
		{
			name: "job with outputs",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: ${{ steps.version.outputs.version }}
      tag: ${{ steps.version.outputs.tag }}
    steps:
      - name: Get version
        id: version
        run: |
          echo "version=1.0.0" >> $GITHUB_OUTPUT
          echo "tag=v1.0.0" >> $GITHUB_OUTPUT
---

# Build workflow
`,
			expectedYAML: []string{
				"build:",
				"outputs:",
				"tag: ${{ steps.version.outputs.tag }}",
				"version: ${{ steps.version.outputs.version }}",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary workflow file
			tmpDir := testutil.TempDir(t, "test-*")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(testFile)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected compilation error: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			yamlBytes, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			yamlContent := string(yamlBytes)

			// Check for expected strings in the YAML
			for _, expected := range tt.expectedYAML {
				if !strings.Contains(yamlContent, expected) {
					t.Errorf("Expected YAML to contain %q\nGot:\n%s", expected, yamlContent)
				}
			}
		})
	}
}

func TestStepValidation(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		shouldError bool
		errorMsg    string
	}{
		{
			name: "step without uses or run should be invalid",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  invalid:
    runs-on: ubuntu-latest
    steps:
      - name: Invalid step
        id: test
---

# Invalid workflow
`,
			// Schema validation correctly rejects steps without 'uses' or 'run'
			shouldError: true,
			errorMsg:    "oneOf",
		},
		{
			name: "step with both uses and run should be invalid",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  invalid:
    runs-on: ubuntu-latest
    steps:
      - name: Invalid step
        uses: actions/checkout@v4
        run: echo "test"
---

# Invalid workflow
`,
			// Schema validation correctly rejects steps with both 'uses' AND 'run'
			shouldError: true,
			errorMsg:    "oneOf",
		},
		{
			name: "step with only uses is valid",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  valid:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
---

# Valid workflow
`,
			shouldError: false,
		},
		{
			name: "step with only run is valid",
			frontmatter: `---
on: push
permissions:
  contents: read
jobs:
  valid:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
---

# Valid workflow
`,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary workflow file
			tmpDir := testutil.TempDir(t, "test-*")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(testFile)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
