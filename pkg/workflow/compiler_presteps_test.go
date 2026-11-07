//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestPreStepsGeneration verifies that pre-steps are emitted before checkout and all
// other built-in steps in the agent job.
func TestPreStepsGeneration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-steps-test")

	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
pre-steps:
  - name: Mint short-lived token
    id: mint
    uses: some-org/token-minting-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
    with:
      scope: target-org/target-repo
steps:
  - name: Custom Setup Step
    run: echo "Custom setup"
post-steps:
  - name: Post AI Step
    run: echo "This runs after AI"
engine: claude
strict: false
---

# Test Pre-Steps Workflow

This workflow tests the pre-steps functionality.
`

	testFile := filepath.Join(tmpDir, "test-pre-steps.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with pre-steps: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-pre-steps.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify all three step types appear (check name value, not "- name:" prefix
	// since steps with an id field have id: first in the YAML output)
	if !strings.Contains(lockContent, "name: Mint short-lived token") {
		t.Error("Expected pre-step 'Mint short-lived token' to be in generated workflow")
	}
	if !strings.Contains(lockContent, "name: Custom Setup Step") {
		t.Error("Expected custom step 'Custom Setup Step' to be in generated workflow")
	}
	if !strings.Contains(lockContent, "name: Post AI Step") {
		t.Error("Expected post-step 'Post AI Step' to be in generated workflow")
	}

	// Pre-steps must appear before checkout, custom steps, and AI execution
	preStepIndex := indexInNonCommentLines(lockContent, "name: Mint short-lived token")
	checkoutIndex := indexInNonCommentLines(lockContent, "- name: Checkout repository")
	customStepIndex := indexInNonCommentLines(lockContent, "- name: Custom Setup Step")
	aiStepIndex := indexInNonCommentLines(lockContent, "- name: Execute Claude Code CLI")
	postStepIndex := indexInNonCommentLines(lockContent, "- name: Post AI Step")

	if preStepIndex == -1 {
		t.Fatal("Could not find pre-step in generated workflow")
	}
	if checkoutIndex == -1 {
		t.Fatal("Could not find checkout step in generated workflow")
	}
	if customStepIndex == -1 {
		t.Fatal("Could not find custom step in generated workflow")
	}
	if aiStepIndex == -1 {
		t.Fatal("Could not find AI execution step in generated workflow")
	}
	if postStepIndex == -1 {
		t.Fatal("Could not find post-step in generated workflow")
	}

	if preStepIndex >= checkoutIndex {
		t.Errorf("Pre-step (%d) should appear before checkout step (%d)", preStepIndex, checkoutIndex)
	}
	if preStepIndex >= customStepIndex {
		t.Errorf("Pre-step (%d) should appear before custom step (%d)", preStepIndex, customStepIndex)
	}
	if preStepIndex >= aiStepIndex {
		t.Errorf("Pre-step (%d) should appear before AI execution step (%d)", preStepIndex, aiStepIndex)
	}
	if postStepIndex <= aiStepIndex {
		t.Errorf("Post-step (%d) should appear after AI execution step (%d)", postStepIndex, aiStepIndex)
	}

	t.Logf("Step order verified: pre-step(%d) < checkout(%d) < custom(%d) < AI(%d) < post(%d)",
		preStepIndex, checkoutIndex, customStepIndex, aiStepIndex, postStepIndex)
}

// TestPreStepsTokenAvailableForCheckout verifies that a token minted in a pre-step
// can be referenced in checkout.token via a steps expression, avoiding the cross-job
// masked-value issue.
func TestPreStepsTokenAvailableForCheckout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-steps-token-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
  id-token: write
pre-steps:
  - name: Mint token
    id: mint
    uses: some-org/token-action@b1c2d3e4f5a6b1c2d3e4f5a6b1c2d3e4f5a6b1c2
    with:
      scope: target-org/target-repo
checkout:
  - repository: target-org/target-repo
    path: target
    token: ${{ steps.mint.outputs.token }}
    current: false
  - path: .
engine: claude
strict: false
---

Read a file from the checked-out repo.
`

	testFile := filepath.Join(tmpDir, "test-pre-steps-token.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-pre-steps-token.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// The minting step must appear in the agent job
	agentJobSection := extractJobSection(lockContent, "agent")
	if agentJobSection == "" {
		t.Fatal("Agent job section not found in generated workflow")
	}

	if !strings.Contains(agentJobSection, "name: Mint token") {
		t.Error("Expected pre-step 'Mint token' to be in the agent job")
	}

	// The token reference must appear in the checkout step
	if !strings.Contains(agentJobSection, "steps.mint.outputs.token") {
		t.Error("Expected steps.mint.outputs.token reference in agent job checkout step")
	}

	// The pre-step must appear before the checkout step
	mintIndex := indexInNonCommentLines(agentJobSection, "name: Mint token")
	checkoutIndex := indexInNonCommentLines(agentJobSection, "- name: Checkout target-org/target-repo into target")
	if mintIndex == -1 {
		t.Fatal("Could not find mint step in agent job")
	}
	if checkoutIndex == -1 {
		t.Fatal("Could not find cross-repo checkout step in agent job")
	}
	if mintIndex >= checkoutIndex {
		t.Errorf("Pre-step mint (%d) should appear before cross-repo checkout (%d)", mintIndex, checkoutIndex)
	}
}

// TestPreStepsOnly verifies that a workflow with only pre-steps (no custom steps or post-steps)
// compiles correctly.
func TestPreStepsOnly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-steps-only-test")

	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
pre-steps:
  - name: Only Pre Step
    run: echo "This runs before checkout"
engine: claude
strict: false
---

# Test Pre-Steps Only Workflow

This workflow tests pre-steps without custom steps or post-steps.
`

	testFile := filepath.Join(tmpDir, "test-pre-steps-only.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with pre-steps only: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-pre-steps-only.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	if !strings.Contains(lockContent, "- name: Only Pre Step") {
		t.Error("Expected pre-step 'Only Pre Step' to be in generated workflow")
	}

	// Default checkout must still be present and after the pre-step
	preStepIndex := indexInNonCommentLines(lockContent, "- name: Only Pre Step")
	checkoutIndex := indexInNonCommentLines(lockContent, "- name: Checkout repository")
	aiStepIndex := indexInNonCommentLines(lockContent, "- name: Execute Claude Code CLI")

	if preStepIndex == -1 {
		t.Fatal("Could not find pre-step in generated workflow")
	}
	if checkoutIndex == -1 {
		t.Error("Expected default checkout step to still be present")
	}
	if aiStepIndex == -1 {
		t.Fatal("Could not find AI execution step in generated workflow")
	}

	if checkoutIndex != -1 && preStepIndex >= checkoutIndex {
		t.Errorf("Pre-step (%d) should appear before checkout step (%d)", preStepIndex, checkoutIndex)
	}
	if preStepIndex >= aiStepIndex {
		t.Errorf("Pre-step (%d) should appear before AI execution step (%d)", preStepIndex, aiStepIndex)
	}
}

// TestCommentMemoryBeforeCustomSteps verifies that the comment-memory preparation steps
// (activation artifact download, config write, and prepare comment memory files) are
// emitted BEFORE any user custom steps: block. This ensures deterministic steps can
// read prior comment-memory state without requiring an LLM turn.
func TestCommentMemoryBeforeCustomSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "comment-memory-order-test")

	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
safe-outputs:
  add-comment:
    max: 1
tools:
  comment-memory: true
steps:
  - name: Custom Step
    run: echo "I should run after comment-memory is ready"
engine: claude
strict: false
---

# Test Comment Memory Ordering

This workflow tests that comment-memory files are prepared before custom steps run.
`

	testFile := filepath.Join(tmpDir, "test-comment-memory-order.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-comment-memory-order.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)
	agentSection := extractJobSection(lockContent, "agent")
	if agentSection == "" {
		t.Fatal("Agent job section not found in generated workflow")
	}

	// All three comment-memory-related steps must be present
	if !strings.Contains(agentSection, "name: Download activation artifact") {
		t.Error("Expected 'Download activation artifact' step in agent job")
	}
	if !strings.Contains(agentSection, "name: Write comment-memory configuration") {
		t.Error("Expected 'Write comment-memory configuration' step in agent job")
	}
	if !strings.Contains(agentSection, "create_files.cjs") || !strings.Contains(agentSection, "renderFiles(") {
		t.Error("Expected comment-memory configuration to use the owner-only file writer")
	}
	if strings.Contains(agentSection, `printf '%s\n' "$GH_AW_EARLY_COMMENT_MEMORY_CONFIG"`) {
		t.Error("Comment-memory configuration must not be written through a shell redirect")
	}
	if !strings.Contains(agentSection, "name: Prepare comment memory files") {
		t.Error("Expected 'Prepare comment memory files' step in agent job")
	}
	if !strings.Contains(agentSection, "name: Custom Step") {
		t.Error("Expected 'Custom Step' in agent job")
	}

	// Verify ordering: all comment-memory steps before the custom step
	downloadIdx := indexInNonCommentLines(agentSection, "- name: Download activation artifact")
	configIdx := indexInNonCommentLines(agentSection, "- name: Write comment-memory configuration")
	prepareIdx := indexInNonCommentLines(agentSection, "- name: Prepare comment memory files")
	customIdx := indexInNonCommentLines(agentSection, "- name: Custom Step")

	if downloadIdx == -1 {
		t.Fatal("Could not find 'Download activation artifact' step")
	}
	if configIdx == -1 {
		t.Fatal("Could not find 'Write comment-memory configuration' step")
	}
	if prepareIdx == -1 {
		t.Fatal("Could not find 'Prepare comment memory files' step")
	}
	if customIdx == -1 {
		t.Fatal("Could not find 'Custom Step'")
	}

	if downloadIdx >= customIdx {
		t.Errorf("Activation artifact download (%d) must come before custom step (%d)", downloadIdx, customIdx)
	}
	if configIdx >= customIdx {
		t.Errorf("Comment-memory config write (%d) must come before custom step (%d)", configIdx, customIdx)
	}
	if prepareIdx >= customIdx {
		t.Errorf("Comment-memory prepare (%d) must come before custom step (%d)", prepareIdx, customIdx)
	}

	// Verify intra-group ordering: download < config < prepare
	if downloadIdx >= configIdx {
		t.Errorf("Activation artifact download (%d) must come before config write (%d)", downloadIdx, configIdx)
	}
	if configIdx >= prepareIdx {
		t.Errorf("Config write (%d) must come before prepare comment memory files (%d)", configIdx, prepareIdx)
	}

	t.Logf("Step order verified: download(%d) < config(%d) < prepare(%d) < custom(%d)",
		downloadIdx, configIdx, prepareIdx, customIdx)
}

func TestCommentMemoryTemplatableMaxCompiles(t *testing.T) {
	tmpDir := testutil.TempDir(t, "comment-memory-input-test")
	testContent := `---
on:
  workflow_dispatch:
    inputs:
      max:
        description: Maximum comment-memory entries
        required: false
        type: number
permissions:
  contents: read
  issues: read
tools:
  comment-memory:
    max: ${{ inputs.max }}
engine: claude
strict: false
---

# Test Comment Memory Input
`
	testFile := filepath.Join(tmpDir, "test-comment-memory-input.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCompiler().CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with templatable comment-memory max: %v", err)
	}
	lockFile := filepath.Join(tmpDir, "test-comment-memory-input.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockContent := string(content)
	if !strings.Contains(lockContent, "GH_AW_EARLY_COMMENT_MEMORY_CONFIG:") {
		t.Fatal("Expected early comment-memory config to be passed through an environment variable")
	}
	if !strings.Contains(lockContent, "${{ inputs.max }}") {
		t.Fatal("Expected the comment-memory max input expression to be preserved")
	}
	if !strings.Contains(lockContent, "create_files.cjs") || !strings.Contains(lockContent, "renderFiles(") {
		t.Fatal("Expected the early config to use the owner-only file writer")
	}
	if strings.Contains(lockContent, `printf '%s\n' "$GH_AW_EARLY_COMMENT_MEMORY_CONFIG"`) {
		t.Fatal("Expected the early config not to use a shell redirect")
	}
	if strings.Contains(lockContent, "<< 'COMMENT_MEMORY_CONFIG") {
		t.Fatal("Expected the early config to avoid a quoted heredoc")
	}
}

// TestActivationArtifactWithoutCommentMemory verifies that the activation artifact download
// step is always emitted even when comment-memory is not configured, and that the
// comment-memory config/prepare steps are not emitted in that case.
func TestActivationArtifactWithoutCommentMemory(t *testing.T) {
	tmpDir := testutil.TempDir(t, "no-comment-memory-test")

	testContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: claude
strict: false
---

# Test Without Comment Memory

This workflow has no comment-memory configured.
`

	testFile := filepath.Join(tmpDir, "test-no-comment-memory.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-no-comment-memory.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)
	agentSection := extractJobSection(lockContent, "agent")
	if agentSection == "" {
		t.Fatal("Agent job section not found in generated workflow")
	}

	// Activation artifact download must always be present
	if !strings.Contains(agentSection, "name: Download activation artifact") {
		t.Error("Expected 'Download activation artifact' step even without comment-memory")
	}

	// Comment-memory-specific steps must NOT be present
	if strings.Contains(agentSection, "name: Write comment-memory configuration") {
		t.Error("Did not expect 'Write comment-memory configuration' step when comment-memory is not configured")
	}
	if strings.Contains(agentSection, "name: Prepare comment memory files") {
		t.Error("Did not expect 'Prepare comment memory files' step when comment-memory is not configured")
	}
}

func TestPreStepsSecretsValidation(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	frontmatter := map[string]any{
		"pre-steps": []any{
			map[string]any{
				"name": "Use secret in pre-step",
				"run":  "echo ${{ secrets.MY_SECRET }}",
			},
		},
	}

	err := compiler.validateStepsSecrets(frontmatter)
	if err == nil {
		t.Error("Expected strict-mode error for secrets in pre-steps but got nil")
	}
	if !strings.Contains(err.Error(), "pre-steps") {
		t.Errorf("Expected error to mention 'pre-steps', got: %v", err)
	}
}
