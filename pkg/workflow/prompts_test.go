//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
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
	if !strings.Contains(lockStr, `GH_AW_CACHE_DIR: /tmp/gh-aw/cache-memory/`) {
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
	coldStartGuidance := "**On cold start** (`/tmp/gh-aw/cache-memory/function-namer-state.json` missing): treat this as expected initialization, not a failure. Do **not** call `report_missing_data` for a missing state file on first run or cold cache; run the Step 1 script as written, accept `LAST_INDEX=-1`, and continue."
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

func TestDailyFormalSpecVerifierDefinesConfiguredSafeOutputContract(t *testing.T) {
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
	requiredContract := "Draft the title and body locally first if needed, but emit exactly one final `create_pull_request` only after implementation and validation are complete."
	if !strings.Contains(workflow, requiredContract) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to require a single final create_pull_request after validation")
	}

	for _, expected := range []string{
		"cli-proxy: true",
		"safeoutputs create_pull_request '<JSON>'",
		"complete JSON object containing `title`, `body`, and `branch`",
		"If a direct safe-output tool is exposed instead, call it with the same arguments.",
		"Do **not** use `python3`, `node`, `jq`, or any other scripting runtime to construct or validate the pull request payload",
		"Never retry `create_pull_request` with empty, placeholder, or partial arguments.",
		`safeoutputs report_incomplete '{"reason":"<concrete blocker>"}'`,
		"Do not redirect it to a file.",
		"The generated test file is formatted and its focused Go tests passed against real implementation APIs.",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("Expected daily-formal-spec-verifier workflow to enforce its validated pull request contract with %q", expected)
		}
	}
	if strings.Contains(workflow, "Do **not** use `bash`, `cli-proxy`, or the `safeoutputs` CLI") {
		t.Fatal("Workflow must not forbid its configured safe-output transport")
	}

	reportIncompleteGuidance := "If the quality checks below cannot be met, emit `report_incomplete` through the same transport."
	if !strings.Contains(workflow, reportIncompleteGuidance) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to require report_incomplete fallback through the configured transport")
	}

	terminalSafeOutputGuidance := "**Before finishing, confirm you called exactly one terminal safe output:** `create_pull_request`, `report_incomplete`, or `noop`."
	if !strings.Contains(workflow, terminalSafeOutputGuidance) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to require exactly one terminal safe output before finishing")
	}
	if strings.Contains(workflow, "create_issue") {
		t.Fatal("Workflow must not request the unconfigured create_issue safe output")
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
	if strings.Contains(workflow, `- "sed *"`) {
		t.Fatal("Expected daily-formal-spec-verifier workflow to exclude unrestricted sed commands")
	}
	if strings.Contains(workflow, `- "bash .github/scripts/install_tla_runtime_test.sh"`) {
		t.Fatal("Expected the installer test to run outside the agent's shell allowlist")
	}
	if !strings.Contains(workflow, "run: bash .github/scripts/install_tla_runtime_test.sh") {
		t.Fatal("Expected the fixture-based installer test to run before agent execution")
	}

	lockContent, err := os.ReadFile(strings.TrimSuffix(workflowFile, ".md") + ".lock.yml")
	if err != nil {
		t.Fatalf("Failed to read compiled workflow lock: %v", err)
	}
	var compiled struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string
				Run  string
				Env  map[string]string
				With map[string]any
			}
		}
	}
	if err := yaml.Unmarshal(lockContent, &compiled); err != nil {
		t.Fatalf("Failed to parse compiled workflow lock: %v", err)
	}
	positions := map[string]int{}
	for index, step := range compiled.Jobs["agent"].Steps {
		positions[step.Name] = index
		if step.Name == "Checkout repository (gh-aw default)" && step.With["persist-credentials"] != false {
			t.Fatal("Expected the pre-agent installer checkout not to persist credentials")
		}
		if step.Name == "Validate fixture-based TLA+ installer" {
			if step.Run != "bash .github/scripts/install_tla_runtime_test.sh" || len(step.Env) != 0 {
				t.Fatal("Expected the pre-agent fixture test to run the fixed script without credential environment bindings")
			}
		}
	}
	installerIndex, ok := positions["Validate fixture-based TLA+ installer"]
	if !ok {
		t.Fatal("Expected fixture-based installer validation in the compiled agent job")
	}
	checkoutIndex, ok := positions["Checkout repository (gh-aw default)"]
	if !ok || checkoutIndex >= installerIndex {
		t.Fatal("Expected the fixture test to use the initial credential-free checkout")
	}
	for _, name := range []string{"Configure Git credentials", "Checkout PR branch", "Execute GitHub Copilot CLI"} {
		index, ok := positions[name]
		if !ok || index <= installerIndex {
			t.Fatalf("Expected fixture validation before %q so agent edits cannot change the executed test", name)
		}
	}
}

func TestDailySquidImageScanUsesFixedLogQueries(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "daily-squid-image-scan.md")
	content, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}

	workflow := string(content)
	frontmatterParts := strings.SplitN(workflow, "---\n", 3)
	if len(frontmatterParts) != 3 {
		t.Fatal("Expected daily-squid-image-scan workflow to contain frontmatter")
	}
	frontmatter := frontmatterParts[1]
	if !strings.Contains(frontmatter, "  bash: []") {
		t.Fatal("Expected daily-squid-image-scan workflow to disable agent bash commands")
	}
	if !strings.Contains(frontmatter, "  edit: false") {
		t.Fatal("Expected daily-squid-image-scan workflow to explicitly disable agent file editing")
	}
	if strings.Contains(workflow, "grep") {
		t.Fatal("Expected scan-log queries to use fixed Bash builtins rather than model-invocable grep")
	}

	lockFile := filepath.Join(repoRoot, ".github", "workflows", "daily-squid-image-scan.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow lock: %v", err)
	}
	if strings.Contains(string(lockContent), "--allow-tool shell(grep)") || strings.Contains(string(lockContent), `"shell(grep)"`) {
		t.Fatal("Expected compiled daily-squid-image-scan lock to exclude agent grep permissions")
	}
	var compiled struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string
				Env  map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(lockContent, &compiled); err != nil {
		t.Fatalf("Failed to parse compiled workflow lock: %v", err)
	}
	summaryIndex, executionIndex := -1, -1
	for index, step := range compiled.Jobs["agent"].Steps {
		if step.Name == "Index scan logs with fixed queries" {
			summaryIndex = index
		}
		if step.Name != "Execute GitHub Copilot CLI" {
			continue
		}
		executionIndex = index
		var serverArgs []string
		if err := json.Unmarshal([]byte(step.Env["GH_AW_COPILOT_SDK_SERVER_ARGS"]), &serverArgs); err != nil {
			t.Fatalf("Failed to parse compiled SDK server arguments: %v", err)
		}
		var serverAllowedTools []string
		for index, arg := range serverArgs {
			if arg == "--allow-all-tools" || arg == "--allow-all" {
				t.Fatalf("Expected compiled SDK server to exclude unrestricted tool permissions, got %q", arg)
			}
			if arg == "--allow-tool" && index+1 < len(serverArgs) {
				tool := serverArgs[index+1]
				if tool == "write" || tool == "*" {
					t.Fatalf("Expected compiled SDK server to exclude agent write permissions, got %q", tool)
				}
				serverAllowedTools = append(serverAllowedTools, tool)
			}
		}
		for _, tool := range []string{"github", "safeoutputs"} {
			if !slices.Contains(serverAllowedTools, tool) {
				t.Fatalf("Expected compiled SDK server to preserve %q permissions", tool)
			}
		}
		var toolConfig struct {
			Capabilities            map[string]bool `json:"capabilities"`
			ExplicitlyDisabledTools []string        `json:"explicitlyDisabledTools"`
			Permissions             struct {
				AllowedTools []string `json:"allowedTools"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(step.Env["GH_AW_COPILOT_SDK_TOOL_CONFIG"]), &toolConfig); err != nil {
			t.Fatalf("Failed to parse compiled SDK tool permissions: %v", err)
		}
		bashEnabled, configured := toolConfig.Capabilities["bash"]
		if !configured || bashEnabled || !slices.Contains(toolConfig.ExplicitlyDisabledTools, "bash") {
			t.Fatal("Expected compiled SDK permissions to explicitly disable agent bash")
		}
		editEnabled, configured := toolConfig.Capabilities["edit"]
		if !configured || editEnabled || !slices.Contains(toolConfig.ExplicitlyDisabledTools, "edit") {
			t.Fatal("Expected compiled SDK permissions to explicitly disable agent file editing")
		}
		for _, tool := range toolConfig.Permissions.AllowedTools {
			if tool == "*" || tool == "bash" || tool == "write" || strings.HasPrefix(tool, "shell") {
				t.Fatalf("Expected no shell or write permissions that could alter scan evidence, got %q", tool)
			}
		}
		for _, tool := range []string{"github", "read", "safeoutputs"} {
			if !slices.Contains(toolConfig.Permissions.AllowedTools, tool) {
				t.Fatalf("Expected compiled SDK permissions to preserve %q access", tool)
			}
		}
	}
	if summaryIndex < 0 || executionIndex <= summaryIndex {
		t.Fatal("Expected fixed log summaries to run before agent execution")
	}

	for _, expected := range []string{
		`marker_pattern='Running \((syft|grype|grant)\)|syft scanned'`,
		`finding_pattern='^[^[:space:]]+:1:1: (error|warning|info): \[[A-Za-z]+\]'`,
		`if [[ "$line" =~ $marker_pattern ]]`,
		`if [[ "$line" =~ $finding_pattern ]]`,
		`finding_counts[match_index]=$((finding_counts[match_index] + 1))`,
		`markers="$output_dir/${scan}-markers.txt"`,
		`counts="$output_dir/compile-finding-counts.txt"`,
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("Expected daily-squid-image-scan workflow to contain fixed scan-log query %q", expected)
		}
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
	for _, expected := range []string{
		"**Tool budget awareness**: Reserve time for focused validation and delivery.",
		"If the quality bar or validation cannot be completed, emit `report_incomplete`; never publish a partial or unvalidated test suite.",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("Expected daily-formal-spec-verifier workflow to enforce tool-budget validation and fallback with %q", expected)
		}
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

func TestBlogWorkflowsPrepareCommittedSafeOutputBranches(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}

	for _, name := range []string{"daily-agent-of-the-day-blog-writer", "daily-ecosystem-explorer"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", name+".md"))
			if err != nil {
				t.Fatalf("Failed to read workflow file: %v", err)
			}
			workflow := string(content)
			for _, expected := range []string{
				`bash: ["*"]`,
				"cli-proxy: true",
				"`create_pull_request` packages committed changes",
				"`git checkout -b ",
				"${{ github.run_id }}`",
				"`git add -- <blog-post-path>`",
				"`git diff --cached --name-only`",
				"`git commit -m ",
				"`branch` equal to the verified local feature branch",
				"If preparation fails or unrelated staged files exist, emit `report_incomplete`",
				"Do not run `git push`, `git pull`, `git fetch`, any `git remote *`",
			} {
				if !strings.Contains(workflow, expected) {
					t.Errorf("Expected local-only publication preparation contract %q", expected)
				}
			}
			for _, forbidden := range []string{
				"Do not stage, commit, or switch branches yourself",
				"Do not run git write commands yourself",
				"Do not run `git add`, `git commit`, `git checkout`",
				"It will package the edited docs files into the PR for you",
			} {
				if strings.Contains(workflow, forbidden) {
					t.Errorf("Workflow contradicts committed-branch safe-output contract with %q", forbidden)
				}
			}
		})
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

	if got, _ := parsed.Frontmatter["model"].(string); !isCodexCompatibleWorkflowModel(got) {
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

	// Verify the top-level engine is copilot (not codex) so the Copilot mini alias is accepted
	if strings.Contains(workflow, "id: codex") {
		t.Fatal("Expected daily-model-resolution workflow to use copilot engine, not codex (the run-analyzer sub-agent uses the Copilot mini alias)")
	}
	if !strings.Contains(workflow, "id: copilot") {
		t.Fatal("Expected daily-model-resolution workflow to use copilot engine")
	}

	agentStart := strings.Index(workflow, "## agent: `run-analyzer`")
	if agentStart == -1 {
		t.Fatal("Expected daily-model-resolution workflow to define the run-analyzer sub-agent")
	}
	agentBlock := workflow[agentStart:]
	if !strings.Contains(agentBlock, "\nmodel: mini\n") {
		t.Fatal("Expected daily-model-resolution run-analyzer sub-agent to use the mini alias")
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
	parsed, err := parser.ExtractFrontmatterFromContent(workflow)
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}
	if model, _ := parsed.Frontmatter["model"].(string); !isCodexCompatibleWorkflowModel(model) {
		t.Fatalf("Expected daily-go-test-parallelizer workflow to use a Codex-compatible model, got %q", model)
	}
	if !strings.Contains(workflow, "Before stopping, call exactly one terminal safe-output tool") {
		t.Fatal("Expected daily-go-test-parallelizer workflow to require a terminal safe output")
	}
}

func TestDailyGoTestParallelizerProvisionsDisposableGoCaches(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "daily-go-test-parallelizer.md"))
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}
	parsed, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}
	engine, ok := parsed.Frontmatter["engine"].(map[string]any)
	if !ok {
		t.Fatal("Expected an engine configuration")
	}
	env, ok := engine["env"].(map[string]any)
	if !ok {
		t.Fatal("Expected explicit Go cache environment variables")
	}
	for name, want := range map[string]string{
		"GOCACHE":    "/tmp/gh-aw/agent/go-build-cache",
		"GOMODCACHE": "/tmp/gh-aw/agent/go-mod-cache",
	} {
		if got := env[name]; got != want {
			t.Errorf("Expected %s=%q, got %v", name, want, got)
		}
	}
	steps, ok := parsed.Frontmatter["steps"].([]any)
	if !ok {
		t.Fatal("Expected a step to provision disposable Go caches")
	}
	for _, step := range steps {
		if step, ok := step.(map[string]any); ok && step["run"] == "mkdir -p /tmp/gh-aw/agent/go-build-cache /tmp/gh-aw/agent/go-mod-cache" {
			return
		}
	}
	t.Fatal("Expected both configured Go caches to be provisioned before the agent runs")
}

func TestABTestingAdvisorProvisionsCompilerResources(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("Failed to find repo root: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ab-testing-advisor.md"))
	if err != nil {
		t.Fatalf("Failed to read workflow file: %v", err)
	}
	workflow := string(content)
	parsed, err := parser.ExtractFrontmatterFromContent(workflow)
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}
	engine, ok := parsed.Frontmatter["engine"].(map[string]any)
	if !ok {
		t.Fatal("Expected an engine configuration")
	}
	env, ok := engine["env"].(map[string]any)
	if !ok {
		t.Fatal("Expected explicit Go cache environment variables")
	}
	for name, want := range map[string]string{
		"GOCACHE":    "/tmp/gh-aw/agent/go-build-cache",
		"GOMODCACHE": "/tmp/gh-aw/agent/go-mod-cache",
	} {
		if got := env[name]; got != want {
			t.Errorf("Expected %s=%q, got %v", name, want, got)
		}
	}
	steps, ok := parsed.Frontmatter["steps"].([]any)
	if !ok {
		t.Fatal("Expected a step to provision disposable Go caches")
	}
	provisioned := false
	for _, step := range steps {
		if step, ok := step.(map[string]any); ok && step["run"] == "mkdir -p /tmp/gh-aw/agent/go-build-cache /tmp/gh-aw/agent/go-mod-cache" {
			provisioned = true
		}
	}
	if !provisioned {
		t.Error("Expected both configured Go caches to be provisioned before the agent runs")
	}
	network, ok := parsed.Frontmatter["network"].(map[string]any)
	if !ok {
		t.Fatal("Expected a network configuration")
	}
	allowed, ok := network["allowed"].([]any)
	if !ok || !slices.Contains(allowed, any("go")) {
		t.Error("Expected Go module downloads to be allowed through the firewall")
	}
	for _, required := range []string{
		"Before discovery, run `make build`",
		"`./gh-aw version`",
		"`./gh-aw compile`",
		"do not read pre-existing caches or inspect the process environment",
		"do not install an alternative compiler, bypass validation, or publish an unvalidated campaign",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("Expected compiler bootstrap guidance %q", required)
		}
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
	parsed, err := parser.ExtractFrontmatterFromContent(workflow)
	if err != nil {
		t.Fatalf("Failed to parse workflow frontmatter: %v", err)
	}
	if model, _ := parsed.Frontmatter["model"].(string); !isCodexCompatibleWorkflowModel(model) {
		t.Fatalf("Expected daily-cli-performance workflow to use a Codex-compatible model, got %q", model)
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
		if !isCodexCompatibleWorkflowModel(model) {
			t.Errorf("%s uses Codex with non-Codex model %q", path, model)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to inspect workflows: %v", err)
	}
}

func isCodexCompatibleWorkflowModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	model, _, _ = strings.Cut(model, "?")
	// Empty selects Codex's native default; both auto spellings are inference pickers.
	return model == "" || codexModelID(model) == "auto" || isCodexCompatibleModel(model)
}

func TestIsCodexCompatibleWorkflowModel(t *testing.T) {
	for _, test := range []struct {
		model string
		want  bool
	}{
		{model: "", want: true},
		{model: "auto", want: true},
		{model: "copilot/auto", want: true},
		{model: "copilot/gpt-5.3-codex", want: true},
		{model: "gpt-6.1-sol", want: true},
		{model: " Copilot/GPT-6.1-SOL?effort=high ", want: true},
		{model: "openai/gpt-6.1-sol", want: true},
		{model: "copilot/gpt-6.1-sol", want: true},
		{model: "openai/gpt-6-sol", want: true},
		{model: "copilot/gpt-6-sol", want: true},
		{model: "openai/gpt-6-luna", want: true},
		{model: "copilot/gpt-6-luna", want: true},
		{model: "openai/gpt-6-astra", want: true},
		{model: "copilot/gpt-6-astra", want: true},
		{model: "openai/gpt-5.6-sol", want: true},
		{model: "copilot/gpt-5.6-sol", want: true},
		{model: "openai/gpt-5.6-terra", want: true},
		{model: "copilot/gpt-5.6-terra", want: true},
		{model: "openai/gpt-5.6-luna", want: true},
		{model: "copilot/gpt-5.6-luna", want: true},
		{model: "openai/gpt-4o", want: false},
		{model: "copilot/mai-code-1.1-flash", want: false},
		{model: "copilot/gpt-6-unknown", want: false},
		{model: "openai/gpt-6-unknown", want: false},
		{model: "anthropic/claude-sonnet-5", want: false},
	} {
		t.Run(test.model, func(t *testing.T) {
			if got := isCodexCompatibleWorkflowModel(test.model); got != test.want {
				t.Errorf("isCodexCompatibleWorkflowModel(%q) = %t, want %t", test.model, got, test.want)
			}
		})
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
