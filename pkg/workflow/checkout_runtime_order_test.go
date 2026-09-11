//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// otlpTelemetryStepNames are compiler-injected observability steps. They are emitted
// for every workflow because the OTLP endpoint defaults to the enterprise
// GH_AW_DEFAULT_OTLP_ENDPOINT secret or variable / GH_AW_DEFAULT_OTLP_HEADERS
// secret pair, so they are not part of the checkout ordering contract exercised
// by these tests.
var otlpTelemetryStepNames = map[string]bool{
	"Mask OTLP telemetry headers":        true,
	"Mask OTLP custom attribute values":  true,
	"Check OTLP telemetry configuration": true,
}

// filterOTLPTelemetrySteps removes compiler-injected OTLP steps from a step name list.
func filterOTLPTelemetrySteps(names []string) []string {
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if otlpTelemetryStepNames[name] {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}

// TestCheckoutRuntimeOrderInCustomSteps verifies that when custom steps contain
// a checkout step, the temp directory is created first, then the checkout step
// runs, and runtime setup steps are inserted AFTER the checkout step. This ensures
// that the temp directory is available to all steps, checkout happens before
// runtime setup, and runtime tools are available to subsequent custom steps.
func TestCheckoutRuntimeOrderInCustomSteps(t *testing.T) {
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Checkout code
    uses: actions/checkout@v5
    with:
      persist-credentials: false
  - name: Use Node
    run: node --version
---

# Test workflow with checkout in custom steps
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "checkout-runtime-order-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflows directory
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write test workflow file
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	compiler.SetActionMode(ActionModeDev) // Use dev mode with local action paths
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read generated lock file
	lockPath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Extract the agent job section
	agentJobStart := strings.Index(lockStr, "  agent:")
	if agentJobStart == -1 {
		t.Fatal("Could not find agent job in compiled workflow")
	}

	// Find the next job (starts with "  " followed by a non-space character, e.g., "  activation:")
	// We need to skip the agent job content which has more indentation
	remainingContent := lockStr[agentJobStart+10:]
	nextJobStart := -1
	lines := strings.Split(remainingContent, "\n")
	for i, line := range lines {
		// A new job starts with exactly 2 spaces followed by a letter/number (not more spaces)
		if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '\t' {
			// Calculate the position in the original string
			nextJobStart = 0
			for j := range i {
				nextJobStart += len(lines[j]) + 1 // +1 for newline
			}
			break
		}
	}

	var agentJobSection string
	if nextJobStart == -1 {
		agentJobSection = lockStr[agentJobStart:]
	} else {
		agentJobSection = lockStr[agentJobStart : agentJobStart+10+nextJobStart]
	}

	// Debug: print first 1000 chars of agent job section
	sampleSize := min(1000, len(agentJobSection))
	t.Logf("Agent job section (first %d chars):\n%s", sampleSize, agentJobSection[:sampleSize])

	// Find all step names in order
	stepNames := []string{}
	stepLines := strings.SplitSeq(agentJobSection, "\n")
	for line := range stepLines {
		// Check if line contains "- name:" (with any amount of leading whitespace)
		if strings.Contains(line, "- name:") {
			// Extract the name part after "- name:"
			parts := strings.SplitN(line, "- name:", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				stepNames = append(stepNames, name)
			}
		}
	}

	stepNames = filterOTLPTelemetrySteps(stepNames)

	t.Logf("Found %d steps: %v", len(stepNames), stepNames)

	if len(stepNames) < 9 {
		t.Fatalf("Expected at least 9 steps, got %d: %v", len(stepNames), stepNames)
	}

	// Verify the order in dev mode (when local actions are used):
	// 1. First step should be "Checkout actions folder" (checkout local actions)
	// 2. Second step should be "Setup Scripts" (use the checked out action)
	// 3. Third step should be "Set runtime paths" (safe-outputs port, always injected)
	// 4. Fourth step should be "Create gh-aw temp directory" (before custom steps)
	// 5. Fifth step should be "Configure gh CLI for GitHub Enterprise" (GHE host setup)
	// 6. Sixth step should be "Download activation artifact" (moved before custom steps)
	// 7. Seventh step should be "Checkout code" (from custom steps - full checkout, no separate .github checkout needed)
	// 8. Eighth step should be "Setup Node.js" (runtime setup, inserted after checkout)
	// 9. Ninth step should be "Use Node" (from custom steps)
	// NOTE: The .github sparse checkout is skipped because custom steps contain a full checkout

	if stepNames[0] != "Checkout actions folder" {
		t.Errorf("First step should be 'Checkout actions folder', got '%s'", stepNames[0])
	}

	if stepNames[1] != "Setup Scripts" {
		t.Errorf("Second step should be 'Setup Scripts', got '%s'", stepNames[1])
	}

	if stepNames[2] != "Set runtime paths" {
		t.Errorf("Third step should be 'Set runtime paths', got '%s'", stepNames[2])
	}

	if stepNames[3] != "Create gh-aw temp directory" {
		t.Errorf("Fourth step should be 'Create gh-aw temp directory', got '%s'", stepNames[3])
	}

	if stepNames[4] != "Configure gh CLI for GitHub Enterprise" {
		t.Errorf("Fifth step should be 'Configure gh CLI for GitHub Enterprise', got '%s'", stepNames[4])
	}

	if stepNames[5] != "Download activation artifact" {
		t.Errorf("Sixth step should be 'Download activation artifact', got '%s'", stepNames[5])
	}

	if stepNames[6] != "Checkout code" {
		t.Errorf("Seventh step should be 'Checkout code', got '%s'", stepNames[6])
	}

	if stepNames[7] != "Setup Node.js" {
		t.Errorf("Eighth step should be 'Setup Node.js' (runtime setup after checkout), got '%s'", stepNames[7])
	}

	if stepNames[8] != "Use Node" {
		t.Errorf("Ninth step should be 'Use Node', got '%s'", stepNames[8])
	}

	// Verify that .github checkout is NOT present (redundant with full checkout in custom steps)
	for _, name := range stepNames {
		if name == "Checkout .github folder" {
			t.Error("Checkout .github folder should not be present when custom steps contain full repository checkout")
		}
	}

	// Additional check: verify correct ordering of key steps
	tempDirIndex := strings.Index(agentJobSection, "Create gh-aw temp directory")
	configureGHEIndex := strings.Index(agentJobSection, "Configure gh CLI for GitHub Enterprise")
	checkoutIndex := strings.Index(agentJobSection, "Checkout code")
	setupNodeIndex := strings.Index(agentJobSection, "Setup Node.js")

	if tempDirIndex == -1 {
		t.Fatal("Could not find 'Create gh-aw temp directory' step in agent job")
	}

	if configureGHEIndex == -1 {
		t.Fatal("Could not find 'Configure gh CLI for GitHub Enterprise' step in agent job")
	}

	if checkoutIndex == -1 {
		t.Fatal("Could not find 'Checkout code' step in agent job")
	}

	if setupNodeIndex == -1 {
		t.Fatal("Could not find 'Setup Node.js' step in agent job")
	}

	if tempDirIndex > configureGHEIndex {
		t.Error("Create gh-aw temp directory appears after Configure gh CLI for GitHub Enterprise, should be before")
	}

	if configureGHEIndex > checkoutIndex {
		t.Error("Configure gh CLI for GitHub Enterprise appears after Checkout code, should be before")
	}

	if setupNodeIndex < checkoutIndex {
		t.Error("Setup Node.js appears before Checkout code, should be after")
	}

	t.Logf("Step order is correct:")
	for i, name := range stepNames[:9] {
		t.Logf("  %d. %s", i+1, name)
	}
}

func TestCheckoutRuntimeOrderInCustomStepVariants(t *testing.T) {
	tests := []struct {
		name                 string
		customSteps          string
		orderedMarkers       []string
		wantSetupNodeNames   int
		wantSetupNodeActions int
	}{
		{
			name: "unnamed checkout with options as first custom step",
			customSteps: `  - uses: actions/checkout@v5
    with:
      fetch-depth: 0
      persist-credentials: false
  - name: Prepare context
    run: echo ready`,
			orderedMarkers: []string{
				"uses: actions/checkout@",
				"- name: Setup Node.js",
				"- name: Prepare context",
			},
			wantSetupNodeNames:   1,
			wantSetupNodeActions: 1,
		},
		{
			name: "named checkout with options as first custom step",
			customSteps: `  - name: Checkout repository
    uses: actions/checkout@v5
    with:
      fetch-depth: 0
      persist-credentials: false
  - name: Prepare context
    run: echo ready`,
			orderedMarkers: []string{
				"uses: actions/checkout@",
				"- name: Setup Node.js",
				"- name: Prepare context",
			},
			wantSetupNodeNames:   1,
			wantSetupNodeActions: 1,
		},
		{
			name: "checkout after deterministic step",
			customSteps: `  - name: Prepare context
    run: echo before
  - uses: actions/checkout@v5
    with:
      persist-credentials: false
  - name: After checkout
    run: echo after`,
			orderedMarkers: []string{
				"- name: Prepare context",
				"uses: actions/checkout@",
				"- name: Setup Node.js",
				"- name: After checkout",
			},
			wantSetupNodeNames:   1,
			wantSetupNodeActions: 1,
		},
		{
			name: "multiple checkout steps insert runtime once after first checkout",
			customSteps: `  - name: Prepare context
    run: echo before
  - uses: actions/checkout@v5
    with:
      persist-credentials: false
  - name: After first checkout
    run: echo middle
  - uses: actions/checkout@v5
    with:
      persist-credentials: false
  - name: After second checkout
    run: echo done`,
			orderedMarkers: []string{
				"- name: Prepare context",
				"uses: actions/checkout@",
				"- name: Setup Node.js",
				"- name: After first checkout",
				"uses: actions/checkout@",
				"- name: After second checkout",
			},
			wantSetupNodeNames:   1,
			wantSetupNodeActions: 1,
		},
		{
			name: "customized runtime setup action is preserved without duplication",
			customSteps: `  - uses: actions/checkout@v5
    with:
      persist-credentials: false
  - name: Setup Node from file
    uses: actions/setup-node@v5
    with:
      node-version-file: .nvmrc
  - name: Prepare context
    run: echo ready`,
			orderedMarkers: []string{
				"uses: actions/checkout@",
				"- name: Setup Node from file",
				"- name: Prepare context",
			},
			wantSetupNodeNames:   0,
			wantSetupNodeActions: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentJobSection := compileWorkflowAndGetAgentJobSection(t, tt.customSteps)
			assertOrderedMarkers(t, agentJobSection, tt.orderedMarkers)

			if got := strings.Count(agentJobSection, "- name: Setup Node.js"); got != tt.wantSetupNodeNames {
				t.Fatalf("expected %d generated Setup Node.js steps, got %d\n%s", tt.wantSetupNodeNames, got, agentJobSection)
			}
			if got := strings.Count(agentJobSection, "uses: actions/setup-node@"); got != tt.wantSetupNodeActions {
				t.Fatalf("expected %d setup-node actions, got %d\n%s", tt.wantSetupNodeActions, got, agentJobSection)
			}
		})
	}
}

// TestCheckoutFirstWhenNoCustomSteps verifies that when there are no custom steps,
// the automatic checkout is added first.
func TestCheckoutFirstWhenNoCustomSteps(t *testing.T) {
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test workflow without custom steps

Run node --version to check the Node.js version.
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "checkout-first-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflows directory
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write test workflow file
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	compiler.SetActionMode(ActionModeDev) // Use dev mode with local action paths
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read generated lock file
	lockPath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Extract the agent job section
	agentJobStart := strings.Index(lockStr, "  agent:")
	if agentJobStart == -1 {
		t.Fatal("Could not find agent job in compiled workflow")
	}

	// Find the next job (starts with "  " followed by a non-space character, e.g., "  activation:")
	// We need to skip the agent job content which has more indentation
	remainingContent := lockStr[agentJobStart+10:]
	nextJobStart := -1
	lines := strings.Split(remainingContent, "\n")
	for i, line := range lines {
		// A new job starts with exactly 2 spaces followed by a letter/number (not more spaces)
		if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '\t' {
			// Calculate the position in the original string
			nextJobStart = 0
			for j := range i {
				nextJobStart += len(lines[j]) + 1 // +1 for newline
			}
			break
		}
	}

	var agentJobSection string
	if nextJobStart == -1 {
		agentJobSection = lockStr[agentJobStart:]
	} else {
		agentJobSection = lockStr[agentJobStart : agentJobStart+10+nextJobStart]
	}

	// Find all step names in order
	stepNames := []string{}
	stepLines := strings.SplitSeq(agentJobSection, "\n")
	for line := range stepLines {
		// Check if line contains "- name:" (with any amount of leading whitespace)
		if strings.Contains(line, "- name:") {
			// Extract the name part after "- name:"
			parts := strings.SplitN(line, "- name:", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				stepNames = append(stepNames, name)
			}
		}
	}

	stepNames = filterOTLPTelemetrySteps(stepNames)

	if len(stepNames) < 4 {
		t.Fatalf("Expected at least 4 steps, got %d: %v", len(stepNames), stepNames)
	}

	// Verify the order in dev mode:
	// 1. First step should be "Checkout actions folder" (checkout local actions)
	// 2. Second step should be "Setup Scripts" (use the checked out action)
	// 3. Third step should be "Set runtime paths" (safe-outputs port, always injected)
	// 4. Fourth step should be "Checkout repository" (automatic full checkout - no separate .github checkout needed)
	// NOTE: The .github sparse checkout is skipped when full repository checkout is performed

	if stepNames[0] != "Checkout actions folder" {
		t.Errorf("First step should be 'Checkout actions folder', got '%s'", stepNames[0])
	}

	if stepNames[1] != "Setup Scripts" {
		t.Errorf("Second step should be 'Setup Scripts', got '%s'", stepNames[1])
	}

	if stepNames[2] != "Set runtime paths" {
		t.Errorf("Third step should be 'Set runtime paths', got '%s'", stepNames[2])
	}

	if stepNames[3] != "Checkout repository" {
		t.Errorf("Fourth step should be 'Checkout repository', got '%s'", stepNames[3])
	}

	// Verify that .github checkout is NOT present (redundant with full checkout)
	for _, name := range stepNames {
		if name == "Checkout .github folder" {
			t.Error("Checkout .github folder should not be present when full repository checkout is performed")
		}
	}

	t.Logf("Step order is correct:")
	t.Logf("  1. %s", stepNames[0])
	t.Logf("  2. %s", stepNames[1])
	t.Logf("  3. %s", stepNames[2])
	t.Logf("  4. %s", stepNames[3])
}

func compileWorkflowAndGetAgentJobSection(t *testing.T, customSteps string) string {
	t.Helper()

	workflowContent := `---
on: workflow_dispatch
engine: copilot
runs-on: self-hosted
permissions:
  contents: read
  issues: read
  pull-requests: read
steps:
` + customSteps + `
---

# Test workflow with custom checkout steps
`

	tempDir, err := os.MkdirTemp("", "checkout-runtime-order-variant")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	compiler.SetActionMode(ActionModeDev)
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockPath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	return extractAgentJobSection(t, string(lockContent))
}

func extractAgentJobSection(t *testing.T, lockStr string) string {
	t.Helper()

	agentJobStart := strings.Index(lockStr, "  agent:")
	if agentJobStart == -1 {
		t.Fatal("Could not find agent job in compiled workflow")
	}

	remainingContent := lockStr[agentJobStart+10:]
	nextJobStart := -1
	lines := strings.Split(remainingContent, "\n")
	for i, line := range lines {
		if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '\t' {
			nextJobStart = 0
			for j := range i {
				nextJobStart += len(lines[j]) + 1
			}
			break
		}
	}

	if nextJobStart == -1 {
		return lockStr[agentJobStart:]
	}

	return lockStr[agentJobStart : agentJobStart+10+nextJobStart]
}

func assertOrderedMarkers(t *testing.T, content string, markers []string) {
	t.Helper()

	cursor := 0
	for _, marker := range markers {
		idx := strings.Index(content[cursor:], marker)
		if idx == -1 {
			t.Fatalf("marker %q not found after offset %d\n%s", marker, cursor, content)
		}
		cursor += idx + len(marker)
	}
}
