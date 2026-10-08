//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCollectSecretReferences(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "Single secret reference",
			yaml: `env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`,
			expected: []string{"GITHUB_TOKEN"},
		},
		{
			name: "Multiple secret references",
			yaml: `env:
  GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
  API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
  TAVILY_KEY: ${{ secrets.TAVILY_API_KEY }}`,
			expected: []string{"ANTHROPIC_API_KEY", "COPILOT_GITHUB_TOKEN", "TAVILY_API_KEY"},
		},
		{
			name: "Secret references with OR fallback",
			yaml: `env:
  TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}`,
			expected: []string{"GH_AW_GITHUB_MCP_SERVER_TOKEN", "GH_AW_GITHUB_TOKEN", "GITHUB_TOKEN"},
		},
		{
			name: "Duplicate secret references",
			yaml: `env:
  TOKEN1: ${{ secrets.API_KEY }}
  TOKEN2: ${{ secrets.API_KEY }}
  TOKEN3: ${{ secrets.API_KEY }}`,
			expected: []string{"API_KEY"},
		},
		{
			name: "No secret references",
			yaml: `env:
  FOO: bar
  BAZ: qux`,
			expected: []string{},
		},
		{
			name: "Mixed case - only uppercase secrets",
			yaml: `env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  api_key: ${{ secrets.api_key }}`,
			expected: []string{"GITHUB_TOKEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CollectSecretReferences(tt.yaml)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expected), len(result))
				t.Logf("Expected: %v", tt.expected)
				t.Logf("Got: %v", result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Expected secret[%d] = %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

func TestEscapeSingleQuoteBackslash(t *testing.T) {
	got := escapeSingleQuoteBackslash(`owner's\workflow`)
	if got != `owner\'s\\workflow` {
		t.Fatalf("escapeSingleQuoteBackslash() = %q", got)
	}
}

func TestCollectActionReferences(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "Single external action",
			yaml: `
      steps:
        - name: Checkout
          uses: actions/checkout@abc123 # v4`,
			expected: []string{"actions/checkout@abc123 # v4"},
		},
		{
			name: "Multiple external actions deduplicated",
			yaml: `
      steps:
        - uses: actions/checkout@abc123 # v4
        - uses: actions/checkout@abc123 # v4
        - uses: actions/setup-node@def456 # v4`,
			expected: []string{"actions/checkout@abc123 # v4", "actions/setup-node@def456 # v4"},
		},
		{
			name: "Local actions are excluded",
			yaml: `
      steps:
        - uses: ./actions/setup
        - uses: ./.github/workflows/worker.lock.yml
        - uses: actions/checkout@abc123 # v4`,
			expected: []string{"actions/checkout@abc123 # v4"},
		},
		{
			name: "Action without inline tag",
			yaml: `
      steps:
        - uses: actions/checkout@abc123`,
			expected: []string{"actions/checkout@abc123"},
		},
		{
			name: "No external actions",
			yaml: `
      steps:
        - uses: ./actions/setup
        - run: echo hello`,
			expected: []string{},
		},
		{
			name: "Only local actions",
			yaml: `
      steps:
        - uses: ./actions/setup
        - uses: ./.github/workflows/other.lock.yml`,
			expected: []string{},
		},
		{
			name: "Mixed local and external",
			yaml: `
      steps:
        - uses: ./actions/setup
        - uses: actions/github-script@sha1 # v8
        - uses: ./.github/workflows/worker.lock.yml
        - uses: actions/upload-artifact@sha2 # v4`,
			expected: []string{"actions/github-script@sha1 # v8", "actions/upload-artifact@sha2 # v4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CollectActionReferences(tt.yaml)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d actions, got %d", len(tt.expected), len(result))
				t.Logf("Expected: %v", tt.expected)
				t.Logf("Got: %v", result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Expected action[%d] = %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestLockFileHeaderOrder(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := testutil.TempDir(t, "lock-header-order-test")

	// Create a test workflow file
	testWorkflow := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test Header Order

Test workflow for verifying lock file header order.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testWorkflow), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)
	lines := strings.Split(lockStr, "\n")

	// Verify gh-aw-metadata is the FIRST line
	if len(lines) == 0 {
		t.Fatal("Lock file is empty")
	}
	if !strings.HasPrefix(lines[0], "# gh-aw-metadata:") {
		t.Errorf("Expected first line to be '# gh-aw-metadata: ...', got: %q", lines[0])
	}

	// Verify the header contains secrets and actions sections
	if !strings.Contains(lockStr, "# Secrets used:") {
		t.Error("Expected '# Secrets used:' section in lock file header")
	}
	if !strings.Contains(lockStr, "# Custom actions used:") {
		t.Error("Expected '# Custom actions used:' section in lock file header")
	}

	// Verify ordering: metadata first, then logo/disclaimer, then secrets/actions, then workflow body
	metadataPos := strings.Index(lockStr, "# gh-aw-metadata:")
	logoPos := strings.Index(lockStr, "# This file was automatically generated")
	secretsPos := strings.Index(lockStr, "# Secrets used:")
	actionsPos := strings.Index(lockStr, "# Custom actions used:")
	workflowPos := strings.Index(lockStr, "name: \"")

	if metadataPos > logoPos {
		t.Error("gh-aw-metadata should appear before the disclaimer logo")
	}
	if logoPos > secretsPos {
		t.Error("Disclaimer should appear before secrets list")
	}
	if secretsPos > actionsPos {
		t.Error("Secrets list should appear before actions list")
	}
	if actionsPos > workflowPos {
		t.Error("Actions list should appear before workflow body (name:)")
	}
}

func TestSecretRedactionStepGeneration(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := testutil.TempDir(t, "secret-redaction-test")

	// Create a test workflow file
	testWorkflow := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test Workflow

Test workflow for secret redaction.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testWorkflow), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify the redaction step is present (copilot engine has declared output files)
	if !strings.Contains(lockStr, "Redact secrets in logs") {
		t.Error("Expected redaction step in generated workflow")
	}

	// Verify the environment variable is set
	if !strings.Contains(lockStr, "GH_AW_SECRET_NAMES") {
		t.Error("Expected GH_AW_SECRET_NAMES environment variable")
	}

	// Verify secret environment variables are passed (both new and legacy names)
	if !strings.Contains(lockStr, "SECRET_COPILOT_GITHUB_TOKEN") {
		t.Error("Expected SECRET_COPILOT_GITHUB_TOKEN environment variable")
	}

	// Verify the redaction step uses actions/github-script
	if !strings.Contains(lockStr, "uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3") {
		t.Error("Expected redaction step to use actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3")
	}

	// Verify the redaction step runs with if: always()
	redactionStepIdx := strings.Index(lockStr, "Redact secrets in logs")
	if redactionStepIdx == -1 {
		t.Fatal("Redaction step not found")
	}

	// Check that if: always() appears near the redaction step
	redactionSection := lockStr[redactionStepIdx:min(redactionStepIdx+500, len(lockStr))]
	if !strings.Contains(redactionSection, "if: always()") {
		t.Error("Expected redaction step to have 'if: always()' condition")
	}
}

// parseGeneratedSteps parses generated step YAML into a list of step mappings so
// tests can assert on the structure GitHub Actions will see rather than on the
// raw text. The generated fragment is indented as list items under "steps:".
func parseGeneratedSteps(t *testing.T, generated string) []map[string]any {
	t.Helper()

	var parsed struct {
		Steps []map[string]any `yaml:"steps"`
	}
	if err := yaml.Unmarshal([]byte("steps:\n"+generated), &parsed); err != nil {
		t.Fatalf("Generated steps are not valid YAML: %v\n%s", err, generated)
	}
	return parsed.Steps
}

func TestSecretRedactionRunsWithoutWorkflowSecrets(t *testing.T) {
	var builder strings.Builder
	compiler := NewCompiler()

	compiler.generateSecretRedactionStep(&builder, "", &WorkflowData{})

	output := builder.String()
	if !strings.Contains(output, "const { main } = require('${{ runner.temp }}/gh-aw/actions/redact_secrets.cjs');") {
		t.Error("Expected redaction script to run without workflow secret references")
	}
	if strings.Contains(output, "No secrets to redact") {
		t.Error("Did not expect a no-op redaction step without workflow secret references")
	}

	steps := parseGeneratedSteps(t, output)
	if len(steps) != 1 {
		t.Fatalf("Expected exactly 1 generated step, got %d", len(steps))
	}
	// A bare "env:" would parse as null and fail workflow schema validation,
	// so the key must be omitted entirely when there are no secrets to pass.
	if env, ok := steps[0]["env"]; ok {
		t.Errorf("Did not expect an env key without workflow secret references, got %#v", env)
	}
}

func TestSecretRedactionEmitsEnvMappingWithWorkflowSecrets(t *testing.T) {
	var builder strings.Builder
	compiler := NewCompiler()

	compiler.generateSecretRedactionStep(&builder, "${{ secrets.MY_TOKEN }}", &WorkflowData{})

	steps := parseGeneratedSteps(t, builder.String())
	if len(steps) != 1 {
		t.Fatalf("Expected exactly 1 generated step, got %d", len(steps))
	}
	env, ok := steps[0]["env"].(map[string]any)
	if !ok {
		t.Fatalf("Expected env to be a mapping, got %#v", steps[0]["env"])
	}
	if env["GH_AW_SECRET_NAMES"] != "MY_TOKEN" {
		t.Errorf("Expected GH_AW_SECRET_NAMES to list the secret, got %#v", env["GH_AW_SECRET_NAMES"])
	}
	if _, ok := env["SECRET_MY_TOKEN"]; !ok {
		t.Errorf("Expected the secret value to be passed as SECRET_MY_TOKEN, got %#v", env)
	}
}

func TestSecretRedactionPreparesFilesOnlyForCustomMasking(t *testing.T) {
	tests := []struct {
		name    string
		masking *SecretMaskingConfig
		prepare bool
	}{
		{name: "no custom masking"},
		{name: "empty custom masking", masking: &SecretMaskingConfig{}},
		{
			name:    "configured custom masking",
			masking: &SecretMaskingConfig{Steps: []map[string]any{{"name": "Custom masking", "run": "echo redacting"}}},
			prepare: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			compiler := NewCompiler()
			compiler.generateSecretRedactionStep(&builder, "", &WorkflowData{SecretMasking: tt.masking})
			steps := parseGeneratedSteps(t, builder.String())
			with := steps[0]["with"].(map[string]any)
			script := with["script"].(string)
			if got := strings.Contains(script, "await main({ prepareForCustomMasking: true });"); got != tt.prepare {
				t.Errorf("Expected custom masking preparation %v, got script:\n%s", tt.prepare, script)
			}
			if !tt.prepare && !strings.Contains(script, "await main();") {
				t.Errorf("Expected default redaction without custom masking, got script:\n%s", script)
			}
			if tt.prepare && len(steps) != 2 {
				t.Errorf("Expected built-in redaction and custom hook, got %d steps", len(steps))
			}
		})
	}
}
