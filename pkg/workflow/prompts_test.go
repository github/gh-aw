//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
)

// ============================================================================
// Safe Outputs Prompt Tests
// ============================================================================

func TestSafeOutputsPromptText_FollowsXMLFormat(t *testing.T) {
	// This test is for the embedded prompt text which is no longer used
	// Skip it as we now generate the prompt dynamically
	t.Skip("Safe outputs prompt is now generated dynamically based on enabled tools")
}

// ============================================================================
// Cache Memory Prompt Tests
// ============================================================================

func TestCacheMemoryPromptIncludedWhenEnabled(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-cache-memory-prompt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with cache-memory enabled
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  cache-memory: true
---

# Test Workflow with Cache Memory

This is a test workflow with cache-memory enabled.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test 1: Verify unified prompt creation step is present
	if !strings.Contains(lockStr, "- name: Create prompt with built-in context") {
		t.Error("Expected 'Create prompt with built-in context' step in generated workflow")
	}

	// Test 2: Verify the template file reference and environment variables
	if !strings.Contains(lockStr, "cache_memory_prompt.md") {
		t.Error("Expected cache template file reference in generated workflow")
	}
	if !strings.Contains(lockStr, "GH_AW_CACHE_DIR: '/tmp/gh-aw/cache-memory/'") {
		t.Error("Expected GH_AW_CACHE_DIR environment variable in generated workflow")
	}
	if !strings.Contains(lockStr, "GH_AW_CACHE_DIR: process.env.GH_AW_CACHE_DIR") {
		t.Error("Expected GH_AW_CACHE_DIR in substitution step")
	}

	// Test 3: Verify the template file is rendered by the JavaScript action.
	if !strings.Contains(lockStr, "create_prompt.cjs") {
		t.Error("Expected JavaScript prompt renderer in generated workflow")
	}

	// Test 4: Verify the instruction mentions persistent cache
	if !strings.Contains(lockStr, "persist") {
		t.Error("Expected 'persist' reference in generated workflow")
	}

	t.Logf("Successfully verified cache memory instructions are included in generated workflow")
}

func TestCacheMemoryPromptNotIncludedWhenDisabled(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-no-cache-memory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow WITHOUT cache-memory
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  github:
---

# Test Workflow without Cache Memory

This is a test workflow without cache-memory.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test: Verify cache memory instructions are NOT included
	// Note: The "Create prompt with built-in context" step will still exist (for temp_folder etc.)
	// but the cache-specific content should not be there
	if strings.Contains(lockStr, "cache_memory_prompt.md") {
		t.Error("Did not expect cache template file reference in workflow without cache-memory")
	}

	if strings.Contains(lockStr, "/tmp/gh-aw/cache-memory/") {
		t.Error("Did not expect '/tmp/gh-aw/cache-memory/' reference in workflow without cache-memory")
	}

	t.Logf("Successfully verified cache memory instructions are NOT included when cache-memory is disabled")
}

func TestCacheMemoryPromptMultipleCaches(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-multi-cache-memory-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with multiple cache-memory entries
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  cache-memory:
    - id: default
      key: cache-1
    - id: session
      key: cache-2
---

# Test Workflow with Multiple Caches

This is a test workflow with multiple cache-memory entries.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test 1: Verify cache memory prompt step is created
	if !strings.Contains(lockStr, "- name: Create prompt with built-in context") {
		t.Error("Expected 'Create prompt with built-in context' step in generated workflow")
	}

	// Test 2: Verify multi-cache template file is referenced
	if !strings.Contains(lockStr, "cache_memory_prompt_multi.md") {
		t.Error("Expected 'cache_memory_prompt_multi.md' template file reference for multiple caches")
	}

	// Test 3: Verify both cache directories are mentioned in environment variables
	if !strings.Contains(lockStr, "/tmp/gh-aw/cache-memory/") {
		t.Error("Expected '/tmp/gh-aw/cache-memory/' reference for default cache")
	}

	if !strings.Contains(lockStr, "/tmp/gh-aw/cache-memory-session/") {
		t.Error("Expected '/tmp/gh-aw/cache-memory-session/' reference for session cache")
	}

	t.Logf("Successfully verified cache memory instructions handle multiple caches")
}

func TestDailyFunctionNamerColdStartHandling(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-function-namer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	coldStartGuidance := "**On cold start** (`/tmp/gh-aw/cache-memory/function-namer-state.json` missing): treat this as expected initialization, not a failure. Do **not** call `missing_data` for a missing state file on first run or cold cache; run the Step 1 script as written, accept `LAST_INDEX=-1`, and continue."
	if !strings.Contains(workflow, coldStartGuidance) {
		t.Fatal("Expected daily-function-namer workflow to include explicit cold-start guidance")
	}

	stepFiveGuidance := "If the state file was missing at the start of the run, initialize it from scratch here instead of reporting missing cache data."
	if !strings.Contains(workflow, stepFiveGuidance) {
		t.Fatal("Expected daily-function-namer workflow to initialize missing cold-start state instead of reporting missing data")
	}
}

func TestDailyFunctionNamerUsesConcreteClaudeModelsForExperiment(t *testing.T) {
	// daily-function-namer was migrated to the Pi engine (copilot/gpt-5.4). The
	// orphaned Claude experiments block was removed because Pi never consumed those
	// variants. Verify no experiments block is present to prevent future drift.
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-function-namer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	parsed, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}

	if _, ok := parsed.Frontmatter["experiments"]; ok {
		t.Fatal("daily-function-namer uses Pi engine and must not have an experiments block; remove orphaned experiment variants")
	}

	// Verify it uses the Pi engine
	engine, ok := parsed.Frontmatter["engine"].(map[string]any)
	if !ok {
		t.Fatal("Expected daily-function-namer to define engine")
	}
	if engine["id"] != "pi" {
		t.Fatalf("Expected daily-function-namer to use Pi engine, got %q", engine["id"])
	}
}

func TestGoLoggerDefinesSingleTerminalSafeOutputContract(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "go-logger.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	for _, keyword := range []string{
		"exactly one terminal outcome",
		"`create_pull_request`",
		"`noop`",
		"`report_incomplete`",
		"exactly once, as your final action",
		"Do not probe safe outputs",
		"Do not retry the call or switch to another terminal safe output",
	} {
		if !strings.Contains(workflow, keyword) {
			t.Fatalf("Expected go-logger workflow to include safe-output contract keyword %q", keyword)
		}
	}
}

func TestDailyCavemanOptimizerUsesConcreteClaudeModelsForExperiment(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-caveman-optimizer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	parsed, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}

	experiments, ok := parsed.Frontmatter["experiments"].(map[string]any)
	if !ok {
		t.Fatal("Expected daily-caveman-optimizer workflow to define experiments")
	}
	modelSize, ok := experiments["model_size"].(map[string]any)
	if !ok {
		t.Fatal("Expected daily-caveman-optimizer workflow to define experiments.model_size")
	}
	variants, ok := modelSize["variants"].([]any)
	if !ok {
		t.Fatal("Expected daily-caveman-optimizer workflow to define experiments.model_size.variants")
	}
	if len(variants) != 2 {
		t.Fatalf("Expected exactly 2 concrete Claude variants, got %#v", variants)
	}
	expected := map[any]bool{
		"claude-sonnet-5":  true,
		"claude-haiku-4.5": true,
	}
	for _, variant := range variants {
		if !expected[variant] {
			t.Fatalf("Expected concrete Claude variants [claude-sonnet-5, claude-haiku-4.5], got %#v", variants)
		}
	}
}

func TestDailyFormalSpecVerifierDefinesDirectSafeOutputContract(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-formal-spec-verifier.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	requiredContract := "Draft the title and body locally first if needed, but emit exactly one final `create_issue` safe output only after the full payload is complete."
	if !strings.Contains(workflow, requiredContract) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to require a single final create_issue safe output")
	}

	noShellGuidance := "Do **not** use `bash`, `cli-proxy`, or the `safeoutputs` CLI to create the issue or inspect the tool schema. Emit the safe output directly with `title` and `body` arguments."
	if !strings.Contains(workflow, noShellGuidance) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to forbid bash/CLI safe-output invocation")
	}

	reportIncompleteGuidance := "If the quality checks below cannot be met, emit `report_incomplete` directly as a safe output instead of `create_issue`."
	if !strings.Contains(workflow, reportIncompleteGuidance) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to require direct report_incomplete fallback")
	}

	terminalSafeOutputGuidance := "**Before finishing, confirm you called exactly one terminal safe output:** `create_issue`, `report_incomplete`, or `noop`."
	if !strings.Contains(workflow, terminalSafeOutputGuidance) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to require exactly one terminal safe output before finishing")
	}
}

func TestDailyFormalSpecVerifierAllowsReadOnlyFileInspection(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-formal-spec-verifier.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	if !strings.Contains(workflow, "edit: null") {
		t.Fatal("Expected daily-formal-spec-verifier workflow to enable built-in file inspection tools")
	}
	if !strings.Contains(workflow, `- "sed *"`) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to allow ranged sed reads")
	}
}

func TestDailyFormalSpecVerifierHasToolBudgetAwareness(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-formal-spec-verifier.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	if !strings.Contains(workflow, "If you are approaching the tool call limit, emit a partial result immediately rather than continuing to gather more data.") {
		t.Fatal("Expected daily-formal-spec-verifier workflow to contain tool budget awareness instruction")
	}
	if !strings.Contains(workflow, "report_incomplete") {
		t.Fatal("Expected daily-formal-spec-verifier workflow to mention report_incomplete as budget-exhaustion fallback")
	}
}

func TestLayoutSpecMaintainerHasToolBudgetAwareness(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "layout-spec-maintainer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	for _, expected := range []string{
		"If you are approaching the tool call limit, emit a partial result immediately rather than continuing to gather more data.",
		"run at most **50 lock files total**",
		"Early emission",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("Expected layout-spec-maintainer workflow to contain %q", expected)
		}
	}
}

func TestDailyAgentOfTheDayBlogWriterHasGitDenialMitigationAllowlist(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-agent-of-the-day-blog-writer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	// The workflow uses bash: ["*"] to allow all bash commands (including git),
	// which covers the git denial mitigation pattern without needing an explicit allowlist.
	if !strings.Contains(workflow, `bash: ["*"]`) {
		t.Fatalf("Expected Daily Agent of the Day Blog Writer workflow to allow all bash commands via bash: [\"*\"]")
	}
}

func TestLayoutSpecMaintainerHasGitDenialMitigationAllowlist(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "layout-spec-maintainer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	for _, expected := range []string{
		"  - cd * && git status",
		"  - cd * && git checkout -b *",
		"  - git -C * checkout -b *",
		"  - cd * && git add * && git diff --cached --stat",
		"  - cd * && git add * && git status",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("Expected Layout Specification Maintainer workflow to contain %q", expected)
		}
	}
}

func TestDailySPDDSpecPlannerAllowsReadOnlyFileInspection(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-spdd-spec-planner.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	if !strings.Contains(workflow, "edit: null") {
		t.Fatal("Expected daily-spdd-spec-planner workflow to enable built-in file inspection tools")
	}
	if !strings.Contains(workflow, `- "sed *"`) {
		t.Fatal("Expected daily-spdd-spec-planner workflow to allow ranged sed reads")
	}
	if !strings.Contains(workflow, `"cat specs/**/*.md"`) {
		t.Fatal("Expected daily-spdd-spec-planner workflow to allow reading spec files in subdirectories")
	}
}

func TestDailyCacheStrategyAnalyzerUsesCodexCompatibleModels(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-cache-strategy-analyzer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	parsed, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}

	if got := parsed.Frontmatter["model"]; got != "openai/gpt-5.3-codex" {
		t.Fatalf("Expected Codex-compatible base model, got %#v", got)
	}

	experiments, ok := parsed.Frontmatter["experiments"].(map[string]any)
	if !ok {
		t.Fatal("Expected daily-cache-strategy-analyzer workflow to define experiments")
	}
	modelSize, ok := experiments["model_size"].(map[string]any)
	if !ok {
		t.Fatal("Expected daily-cache-strategy-analyzer workflow to define experiments.model_size")
	}
	variants, ok := modelSize["variants"].([]any)
	if !ok {
		t.Fatal("Expected daily-cache-strategy-analyzer workflow to define experiments.model_size.variants")
	}
	if len(variants) != 2 {
		t.Fatalf("Expected exactly 2 codex-compatible variants, got %#v", variants)
	}
	want := map[string]bool{
		"gpt-5.3-codex":       true,
		"gpt-5.3-codex-spark": true,
	}
	got := make(map[string]bool, len(variants))
	for _, v := range variants {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("Expected all variants to be strings, got %T in %#v", v, variants)
		}
		if !want[s] {
			t.Fatalf("Unexpected variant %q; want exactly [gpt-5.3-codex, gpt-5.3-codex-spark], got %#v", s, variants)
		}
		got[s] = true
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("Missing expected variant %q; got %#v", k, variants)
		}
	}
}

func TestDailyModelResolutionUsesCopilotEngineWithMiniSubAgentModel(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-model-resolution.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)

	// Verify the top-level engine is copilot (not codex) so gpt-5.4-mini is accepted
	if strings.Contains(workflow, "id: codex") {
		t.Fatal("Expected daily-model-resolution workflow to use copilot engine, not codex (gpt-5.4-mini is not a valid Codex model)")
	}
	if !strings.Contains(workflow, "id: copilot") {
		t.Fatal("Expected daily-model-resolution workflow to use copilot engine")
	}

	agentStart := strings.Index(workflow, "## agent: `run-analyzer`")
	if agentStart == -1 {
		t.Fatal("Expected daily-model-resolution workflow to define the run-analyzer sub-agent")
	}
	agentBlock := workflow[agentStart:]
	if !strings.Contains(agentBlock, "\nmodel: gpt-5.4-mini\n") {
		t.Fatal("Expected daily-model-resolution run-analyzer sub-agent to use explicit model gpt-5.4-mini")
	}
}

func TestDailyGoTestParallelizerUsesCodexCompatibleModel(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-go-test-parallelizer.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	if !strings.Contains(workflow, "id: codex") {
		t.Fatal("Expected daily-go-test-parallelizer workflow to use the Codex engine")
	}
	if !strings.Contains(workflow, "model: copilot/gpt-5.3-codex") {
		t.Fatal("Expected daily-go-test-parallelizer workflow to use a Codex-compatible Copilot model")
	}
}

func TestDailyCLIPerformanceUsesCodexCompatibleModel(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-cli-performance.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	if !strings.Contains(workflow, "engine:\n  id: codex\n") {
		t.Fatal("Expected daily-cli-performance workflow to use the Codex engine")
	}
	if !strings.Contains(workflow, "\nmodel: openai/gpt-5.3-codex\n") {
		t.Fatal("Expected daily-cli-performance workflow to use a Codex-compatible OpenAI model")
	}
}

func TestCodexWorkflowsUseCodexModels(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	err = filepath.WalkDir(workflowDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parser.ExtractFrontmatterFromContent(string(content))
		if err != nil {
			return err
		}

		engineID := ""
		switch engine := parsed.Frontmatter["engine"].(type) {
		case string:
			engineID = engine
		case map[string]any:
			engineID, _ = engine["id"].(string)
		}
		if engineID != "codex" {
			return nil
		}

		model, _ := parsed.Frontmatter["model"].(string)
		if !strings.Contains(strings.ToLower(model), "codex") {
			t.Errorf("%s uses Codex with non-Codex model %q", path, model)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to inspect workflows: %v", err)
	}
}

// ============================================================================
// Playwright Prompt Tests
// ============================================================================

func TestPlaywrightPromptIncludedWhenEnabled(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-playwright-prompt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with playwright tool enabled
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  playwright:
---

# Test Workflow with Playwright

This is a test workflow with playwright enabled.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test 1: Verify playwright prompt step is created
	if !strings.Contains(lockStr, "- name: Create prompt with built-in context") {
		t.Error("Expected 'Create prompt with built-in context' step in generated workflow")
	}

	// Test 2: Verify the renderer configuration includes the playwright prompt file.
	if !strings.Contains(lockStr, `\"file\":\"playwright_prompt.md\"`) {
		t.Error("Expected playwright prompt file in renderer configuration")
	}

	t.Logf("Successfully verified playwright output directory instructions are included in generated workflow")
}

func TestPlaywrightPromptNotIncludedWhenDisabled(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-no-playwright-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow WITHOUT playwright tool
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: codex
tools:
  github:
---

# Test Workflow without Playwright

This is a test workflow without playwright.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test: Verify playwright instructions are NOT included
	// Note: The "Create prompt with built-in context" step will still exist (for temp_folder etc.)
	// but the playwright-specific content should not be there
	if strings.Contains(lockStr, "Playwright Output Directory") {
		t.Error("Did not expect 'Playwright Output Directory' header in workflow without playwright")
	}

	if strings.Contains(lockStr, "playwright_prompt.md") {
		t.Error("Did not expect 'playwright_prompt.md' reference in workflow without playwright")
	}

	t.Logf("Successfully verified playwright output directory instructions are NOT included when playwright is disabled")
}

func TestPlaywrightPromptOrderAfterTempFolder(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-playwright-order-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with playwright
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  playwright:
---

# Test Workflow

This is a test workflow to verify playwright instructions come after temp folder.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Find positions of temp folder and playwright instructions
	// Both are now in the same unified step, so we check their content order
	tempFolderPos := strings.Index(lockStr, "temp_folder_prompt.md")
	playwrightPos := strings.Index(lockStr, "playwright_prompt.md")

	// Test: Verify playwright instructions come after temp folder instructions
	if tempFolderPos == -1 {
		t.Error("Expected temporary folder instructions in generated workflow")
	}

	if playwrightPos == -1 {
		t.Error("Expected playwright output directory instructions in generated workflow")
	}

	if tempFolderPos != -1 && playwrightPos != -1 && playwrightPos <= tempFolderPos {
		t.Errorf("Expected playwright instructions to come after temp folder instructions, but found at positions TempFolder=%d, Playwright=%d", tempFolderPos, playwrightPos)
	}

	t.Logf("Successfully verified playwright instructions come after temp folder instructions in generated workflow")
}

func TestPlaywrightAWFPolicyPromptIncludedForCLIModeWithFirewall(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gh-aw-playwright-awf-prompt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  playwright:
    mode: cli
sandbox:
  agent:
    id: awf
---

# Test Workflow with Playwright CLI and firewall

This is a test workflow with playwright CLI mode and the firewall enabled.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	if !strings.Contains(lockStr, `\"file\":\"playwright_awf_prompt.md\"`) {
		t.Error("Expected playwright AWF policy prompt file in renderer configuration when CLI mode and firewall are both enabled")
	}
}

func TestPlaywrightAWFPolicyPromptNotIncludedWithoutFirewall(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gh-aw-playwright-awf-no-firewall-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
tools:
  playwright:
    mode: cli
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
---

# Test Workflow with Playwright CLI, firewall disabled

This is a test workflow with playwright CLI mode but the firewall disabled.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	if strings.Contains(lockStr, "playwright_awf_prompt.md") {
		t.Error("Did not expect playwright AWF policy prompt file when firewall is disabled")
	}
}

// ============================================================================
// PR Context Prompt Tests
// ============================================================================

func TestPRContextPromptIncludedForIssueComment(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-pr-context-prompt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with issue_comment trigger
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on:
  issue_comment:
    types: [created]
permissions:
  contents: read
engine: claude
---

# Test Workflow with Issue Comment

This is a test workflow with issue_comment trigger.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test 1: Verify PR context prompt step is created
	if !strings.Contains(lockStr, "- name: Create prompt with built-in context") {
		t.Error("Expected 'Create prompt with built-in context' step in generated workflow")
	}

	// Test 2: Verify the renderer configuration includes the PR context prompt file.
	if !strings.Contains(lockStr, `\"file\":\"pr_context_prompt.md\"`) {
		t.Error("Expected PR context prompt file in renderer configuration")
	}

	t.Logf("Successfully verified PR context instructions are included for issue_comment trigger")
}

func TestPRContextPromptIncludedForCommand(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-pr-context-command-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with command trigger
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on:
  command:
    name: mybot
permissions:
  contents: read
engine: claude
---

# Test Workflow with Command

This is a test workflow with command trigger.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test: Verify PR context prompt step is created for command triggers
	if !strings.Contains(lockStr, "- name: Create prompt with built-in context") {
		t.Error("Expected 'Create prompt with built-in context' step in workflow with command trigger")
	}

	t.Logf("Successfully verified PR context instructions are included for command trigger")
}

func TestPRContextPromptNotIncludedForPush(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-no-pr-context-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with push trigger (no comment triggers)
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
permissions:
  contents: read
engine: claude
---

# Test Workflow without Comment Triggers

This is a test workflow with push trigger only.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test: Verify PR context prompt content is NOT included for push triggers
	// Note: The "Create prompt with built-in context" step will still exist (for temp_folder etc.)
	// but the PR-specific content should not be there
	if strings.Contains(lockStr, "pr_context_prompt.md") {
		t.Error("Did not expect 'pr_context_prompt.md' reference for push trigger")
	}

	t.Logf("Successfully verified PR context instructions are NOT included for push trigger")
}

func TestPRContextPromptNotIncludedWithoutCheckout(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "gh-aw-pr-no-checkout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test workflow with comment trigger but no checkout (no contents permission)
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on:
  issue_comment:
    types: [created]
permissions:
  issues: read
engine: claude
---

# Test Workflow without Contents Permission

This is a test workflow without contents read permission.
`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
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
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test: Verify PR context prompt content is NOT created without contents permission
	// Note: The "Create prompt with built-in context" step will still exist (for temp_folder etc.)
	// but the PR-specific content should not be there
	if strings.Contains(lockStr, "pr_context_prompt.md") {
		t.Error("Did not expect 'pr_context_prompt.md' reference without contents read permission")
	}

	t.Logf("Successfully verified PR context instructions are NOT included without contents permission")
}
