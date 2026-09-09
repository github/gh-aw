//go:build integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUseSamplesReplacesAgentStep verifies that compiling with
// SetUseSamples(true) replaces the engine `Execute coding agent` step
// with the deterministic `Replay safe-outputs samples` step driven by
// apply_samples.cjs.
func TestUseSamplesReplacesAgentStep(t *testing.T) {
	const md = `---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
safe-outputs:
  create-issue:
    samples:
      - title: "Deterministic test issue"
        body: "Issue body emitted by gh-aw samples replay."
---

Trivial workflow whose only job is to be compiled with --use-samples.
`

	tmpFile, err := os.CreateTemp("", "use-samples-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	t.Run("Default Mode", func(t *testing.T) {
		compiler := NewCompiler()
		if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
		defer os.Remove(lockPath)
		b, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		lockContent := string(b)
		if strings.Contains(lockContent, "Replay safe-outputs samples") {
			t.Error("Did not expect samples replay step in default mode")
		}
		if strings.Contains(lockContent, "apply_samples.cjs") {
			t.Error("Did not expect apply_samples driver in default mode")
		}
	})

	t.Run("Use Samples Mode", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetUseSamples(true)
		if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("ParseWorkflowFile failed: %v", err)
		}
		if !workflowData.UseSamples {
			t.Fatal("Expected workflowData.UseSamples to be true after SetUseSamples(true)")
		}
		lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
		defer os.Remove(lockPath)
		b, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		lockContent := string(b)
		if !strings.Contains(lockContent, "Replay safe-outputs samples (deterministic)") {
			t.Error("Expected `Replay safe-outputs samples (deterministic)` step in lock file")
		}
		if !strings.Contains(lockContent, "apply_samples.cjs") {
			t.Error("Expected lock file to invoke apply_samples.cjs driver")
		}
		if !strings.Contains(lockContent, "node \"${RUNNER_TEMP}/gh-aw/actions/apply_samples.cjs\"") {
			t.Error("Expected apply_samples.cjs invocation to use RUNNER_TEMP shell variable")
		}
		if !strings.Contains(lockContent, "GH_AW_SAMPLES:") {
			t.Error("Expected GH_AW_SAMPLES env var in lock file")
		}
		// GITHUB_TOKEN is required so apply_samples.cjs can resolve the PR head
		// ref via the REST API for issue_comment / slash-command triggers.
		if !strings.Contains(lockContent, "GITHUB_TOKEN: ${{ github.token }}") {
			t.Error("Expected GITHUB_TOKEN env var in samples replay step")
		}
		if !strings.Contains(lockContent, `"tool":"create_issue"`) {
			t.Error("Expected JSON-encoded create_issue tool entry in lock file")
		}
		if !strings.Contains(lockContent, "Deterministic test issue") {
			t.Error("Expected sample title in lock file")
		}
		if !strings.Contains(lockContent, "id: agentic_execution") {
			t.Error("Expected id: agentic_execution on the replay step")
		}
		// Threat detection must be force-disabled under --use-samples so the
		// deterministic replay isn't perturbed by an LLM-backed detection job.
		if strings.Contains(lockContent, "\n  detection:\n") {
			t.Error("Expected no `detection:` job under --use-samples")
		}
	})
}

// TestFeaturesSamplesOptInReplacesAgentStep verifies that a workflow
// declaring `features: { samples: true }` in its own frontmatter compiles
// into samples-mode output under a plain `gh aw compile`, without needing
// the hidden `--use-samples` flag / SetUseSamples(true).
func TestFeaturesSamplesOptInReplacesAgentStep(t *testing.T) {
	const md = `---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
features:
  samples: true
safe-outputs:
  create-issue:
    samples:
      - title: "Deterministic test issue"
        body: "Issue body emitted by gh-aw samples replay."
---

Trivial workflow that opts into samples replay via features.samples.
`

	tmpFile, err := os.CreateTemp("", "features-samples-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseWorkflowFile failed: %v", err)
	}
	if !workflowData.UseSamples {
		t.Fatal("Expected workflowData.UseSamples to be true from features.samples: true, without SetUseSamples")
	}

	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lockContent := string(b)
	if !strings.Contains(lockContent, "Replay safe-outputs samples (deterministic)") {
		t.Error("Expected `Replay safe-outputs samples (deterministic)` step in lock file")
	}
	if !strings.Contains(lockContent, "apply_samples.cjs") {
		t.Error("Expected lock file to invoke apply_samples.cjs driver")
	}
	// Threat detection must be force-disabled when features.samples is set, mirroring
	// the --use-samples behaviour.
	if strings.Contains(lockContent, "\n  detection:\n") {
		t.Error("Expected no `detection:` job with features.samples: true")
	}
	// features.samples is documented as an internal/hidden compiler knob; it must not
	// leak into GH_AW_INFO_FEATURES, which is runtime-visible metadata.
	if strings.Contains(lockContent, "GH_AW_INFO_FEATURES") && strings.Contains(lockContent, `"samples"`) {
		t.Error("Expected GH_AW_INFO_FEATURES to omit the internal `samples` feature flag")
	}
}

// TestFeaturesSamplesFromImportEnablesReplay verifies that `features: { samples: true }`
// declared only in an imported shared workflow still activates samples replay for the
// importing workflow: WorkflowData.UseSamples must be true, threat detection must be
// force-disabled, and the compiled lock file must contain the deterministic replay step
// instead of invoking the agent.
func TestFeaturesSamplesFromImportEnablesReplay(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "features-samples-import-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sharedDir := filepath.Join(tmpDir, ".github", "workflows", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	sharedPath := filepath.Join(sharedDir, "samples-config.md")
	sharedContent := `---
features:
  samples: true
---

# Shared Samples Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	mainPath := filepath.Join(tmpDir, ".github", "workflows", "main.md")
	mainContent := `---
on: workflow_dispatch
permissions: read-all
engine:
  id: claude
imports:
  - shared/samples-config.md
safe-outputs:
  create-issue:
    samples:
      - title: "Deterministic test issue"
        body: "Issue body emitted by gh-aw samples replay."
---

# Main Workflow

Test that features.samples: true from an import enables samples replay.
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main workflow file: %v", err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mainPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	workflowData, err := compiler.ParseWorkflowFile(mainPath)
	if err != nil {
		t.Fatalf("ParseWorkflowFile failed: %v", err)
	}
	if !workflowData.UseSamples {
		t.Fatal("Expected workflowData.UseSamples to be true from imported features.samples: true")
	}
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.ThreatDetection != nil {
		t.Fatal("Expected threat-detection to be force-disabled when samples replay is enabled via imports")
	}

	lockPath := strings.TrimSuffix(mainPath, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	if !strings.Contains(lockStr, "Replay safe-outputs samples (deterministic)") {
		t.Error("Expected `Replay safe-outputs samples (deterministic)` step in lock file")
	}
	if !strings.Contains(lockStr, "apply_samples.cjs") {
		t.Error("Expected lock file to invoke apply_samples.cjs driver")
	}
	if strings.Contains(lockStr, "\n  detection:\n") {
		t.Error("Expected no `detection:` job when samples replay is enabled via imports")
	}
}

// TestUseSamplesCreatePullRequestWithPatch is the end-to-end smoke test for
// the create-pull-request + patch sidecar flow. It compiles a workflow whose
// only safe-output is `create-pull-request` with a `samples` entry carrying
// a `patch` sidecar, then inspects the generated lock.yml to verify that:
//
//  1. The agentic step is replaced by the deterministic replay step
//  2. GH_AW_SAMPLES contains a JSON-encoded create_pull_request entry
//  3. The patch is partitioned into `sidecars`, NOT into `arguments`
//     (the MCP server's create_pull_request handler must NOT receive `patch`
//     as a tool argument — it derives the diff from the working tree)
//  4. The branch name and other PR fields land in `arguments`
//  5. The actual diff payload is preserved verbatim in the lock file
//     (so the driver can `git apply` it at replay time)
//  6. No `detection:` job is emitted
func TestUseSamplesCreatePullRequestWithPatch(t *testing.T) {
	const patch = "diff --git a/sample.txt b/sample.txt\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ b/sample.txt\n@@ -0,0 +1 @@\n+hello from gh-aw samples\n"

	md := `---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
safe-outputs:
  create-pull-request:
    samples:
      - title: "Sample PR from gh-aw"
        body: "PR body emitted by samples replay."
        branch: "feat/gh-aw-sample-pr"
        patch: |
` + indentBlock(patch, "          ") + `---

Trivial workflow exercising create-pull-request via --use-samples.
`

	tmpFile, err := os.CreateTemp("", "use-samples-cpr-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock := string(b)

	// 1. Agentic step replaced
	if !strings.Contains(lock, "Replay safe-outputs samples (deterministic)") {
		t.Error("Expected `Replay safe-outputs samples (deterministic)` step in lock file")
	}
	if !strings.Contains(lock, "apply_samples.cjs") {
		t.Error("Expected lock file to invoke apply_samples.cjs driver")
	}

	// 2. GH_AW_SAMPLES contains a create_pull_request entry
	if !strings.Contains(lock, "GH_AW_SAMPLES:") {
		t.Fatal("Expected GH_AW_SAMPLES env var in lock file")
	}
	if !strings.Contains(lock, `"tool":"create_pull_request"`) {
		t.Error("Expected JSON-encoded create_pull_request tool entry in lock file")
	}

	// Extract the GH_AW_SAMPLES JSON block from the YAML for structural assertions.
	samplesJSON := extractGHAWSamplesJSON(t, lock)
	var entries []map[string]any
	if err := json.Unmarshal([]byte(samplesJSON), &entries); err != nil {
		t.Fatalf("failed to parse GH_AW_SAMPLES JSON: %v\nRaw:\n%s", err, samplesJSON)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one sample entry, got %d", len(entries))
	}
	entry := entries[0]

	// 3. Patch is in sidecars, NOT in arguments
	args, _ := entry["arguments"].(map[string]any)
	sidecars, _ := entry["sidecars"].(map[string]any)
	if args == nil {
		t.Fatal("expected entry.arguments to be an object")
	}
	if _, hasPatchInArgs := args["patch"]; hasPatchInArgs {
		t.Error("patch must be stripped from arguments — MCP create_pull_request handler must not receive it")
	}
	if sidecars == nil {
		t.Fatal("expected entry.sidecars to be present (patch should land here)")
	}
	gotPatch, _ := sidecars["patch"].(string)
	if gotPatch == "" {
		t.Fatal("expected sidecars.patch to be a non-empty string")
	}

	// 4. PR fields preserved in arguments
	if args["title"] != "Sample PR from gh-aw" {
		t.Errorf("arguments.title = %q, want %q", args["title"], "Sample PR from gh-aw")
	}
	if args["body"] != "PR body emitted by samples replay." {
		t.Errorf("arguments.body = %q, want %q", args["body"], "PR body emitted by samples replay.")
	}
	if args["branch"] != "feat/gh-aw-sample-pr" {
		t.Errorf("arguments.branch = %q, want %q", args["branch"], "feat/gh-aw-sample-pr")
	}

	// 5. Patch payload preserved verbatim
	if !strings.Contains(gotPatch, "diff --git a/sample.txt b/sample.txt") {
		t.Errorf("sidecars.patch missing diff header; got: %q", gotPatch)
	}
	if !strings.Contains(gotPatch, "+hello from gh-aw samples") {
		t.Errorf("sidecars.patch missing payload line; got: %q", gotPatch)
	}

	// 6. No detection job
	if strings.Contains(lock, "\n  detection:\n") {
		t.Error("Expected no `detection:` job under --use-samples")
	}
}

// indentBlock prefixes every line of s with prefix. Used to embed a multi-line
// patch under a YAML block scalar in the test fixture.
func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}

// extractGHAWSamplesJSON pulls the literal block scalar value of GH_AW_SAMPLES
// out of the compiled YAML and returns the unindented JSON text. This avoids
// pulling in a full YAML parser for what is a tightly-controlled emit format.
func extractGHAWSamplesJSON(t *testing.T, lock string) string {
	t.Helper()
	const marker = "GH_AW_SAMPLES: |\n"
	start := strings.Index(lock, marker)
	if start < 0 {
		t.Fatalf("could not find %q in lock file", marker)
	}
	start += len(marker)
	// Determine indentation from the first content line.
	rest := lock[start:]
	firstNL := strings.Index(rest, "\n")
	if firstNL < 0 {
		t.Fatal("malformed GH_AW_SAMPLES block: no newline after first line")
	}
	firstLine := rest[:firstNL]
	indent := firstLine[:len(firstLine)-len(strings.TrimLeft(firstLine, " "))]
	if indent == "" {
		t.Fatal("malformed GH_AW_SAMPLES block: expected indented content")
	}
	// Collect lines until we hit one that no longer starts with the same indent
	// (i.e. the next YAML key like GH_AW_AGENT_STDIO_LOG).
	var out strings.Builder
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(line, indent) {
			break
		}
		out.WriteString(strings.TrimPrefix(line, indent))
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

// TestUseSamplesEmitsEmptyArrayWhenNoSamplesConfigured guards against a
// regression where compiling with --use-samples but no `samples:` entries on
// any enabled handler caused json.Marshal of a nil Go slice to emit the
// literal string "null" into GH_AW_SAMPLES, which the driver rightly
// rejected with `GH_AW_SAMPLES must be a JSON array`. The compiler must
// emit "[]" instead so the driver can exit cleanly with `no samples to
// replay`.
func TestUseSamplesEmitsEmptyArrayWhenNoSamplesConfigured(t *testing.T) {
	// Workflow opts into --use-samples and configures safe-outputs but has
	// no `samples:` entries on the create-issue handler.
	const md = `---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
safe-outputs:
  create-issue:
    title-prefix: "[no-samples] "
---

Workflow with safe-outputs but no samples — should still compile and
emit a valid empty-array GH_AW_SAMPLES under --use-samples.
`

	tmpFile, err := os.CreateTemp("", "use-samples-empty-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock := string(b)

	// Must still emit the replay step.
	if !strings.Contains(lock, "Replay safe-outputs samples (deterministic)") {
		t.Fatal("Expected replay step in lock file even with no samples configured")
	}

	samplesJSON := extractGHAWSamplesJSON(t, lock)
	if samplesJSON == "null" {
		t.Fatalf("GH_AW_SAMPLES must not be the literal `null` (driver would reject it); got %q", samplesJSON)
	}
	if samplesJSON != "[]" {
		t.Fatalf("GH_AW_SAMPLES = %q, want %q", samplesJSON, "[]")
	}
}

// TestUseSamplesPreservesRuntimeExpressionsInLockFile verifies that a sample
// value containing a `${{ ... }}` GitHub Actions expression compiles cleanly
// AND lands verbatim in the GH_AW_SAMPLES env value of the lock file, so that
// GitHub Actions substitutes the real value on the runner before
// apply_samples.cjs reads it.
//
// Regression for https://github.com/github/gh-aw/issues/37532.
func TestUseSamplesPreservesRuntimeExpressionsInLockFile(t *testing.T) {
	const md = `---
on:
  workflow_dispatch:
    inputs:
      issue_number:
        description: 'Issue number'
        required: true
        type: number
permissions: read-all
engine:
  id: claude
safe-outputs:
  add-labels:
    samples:
      - item_number: ${{ github.event.inputs.issue_number }}
        labels: ["copilot-safe-output-label-test"]
---

Runtime-templated sample for workflow_dispatch-driven testing.
`

	tmpFile, err := os.CreateTemp("", "use-samples-runtime-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock := string(b)

	samplesJSON := extractGHAWSamplesJSON(t, lock)
	assert.Contains(t, samplesJSON, "${{ github.event.inputs.issue_number }}", "GH_AW_SAMPLES should preserve live runtime expression for substitution")
	// The marshalled payload must still be valid JSON (the expression sits
	// inside a JSON string, so no escaping concerns at compile time).
	var parsed []any
	err = json.Unmarshal([]byte(samplesJSON), &parsed)
	require.NoError(t, err, "GH_AW_SAMPLES should remain valid JSON at compile time")
	require.NotEmpty(t, parsed, "GH_AW_SAMPLES should include at least one sample entry")
}

// TestUseSamplesEmitsPerRepoTokenMap verifies that a workflow with multiple
// `checkout:` entries — each carrying its own `github-token:` — produces a
// `GH_AW_REPO_TOKENS` env var whose JSON map binds every repo slug to the
// matching token expression. apply_samples.cjs uses that map to pick the
// right token when it calls the GitHub REST API to resolve a PR head ref
// for a cross-repo sample (issue: a single hard-coded `github.token`
// would fail for repos that require a PAT or App-token).
func TestUseSamplesEmitsPerRepoTokenMap(t *testing.T) {
	const md = `---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
checkout:
  - fetch-depth: 0
    path: ./automation
  - repository: github/github
    token: ${{ secrets.CRITICAL_PATH_GITHUB_GITHUB_PAT }}
    path: ./github
safe-outputs:
  create-issue:
    samples:
      - title: "Cross-repo sample"
        body: "Body for cross-repo issue."
---

Multi-checkout workflow exercising the per-repo token map.
`
	tmpFile, err := os.CreateTemp("", "use-samples-tokens-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock := string(b)

	require.Contains(t, lock, "Replay safe-outputs samples (deterministic)")
	require.Contains(t, lock, "GH_AW_REPO_TOKENS: |", "expected GH_AW_REPO_TOKENS env var in lock file")

	tokensJSON := extractGHAWRepoTokensJSON(t, lock)
	var tokens map[string]string
	require.NoError(t, json.Unmarshal([]byte(tokensJSON), &tokens), "GH_AW_REPO_TOKENS must be valid JSON")
	assert.Equal(t, "${{ secrets.CRITICAL_PATH_GITHUB_GITHUB_PAT }}", tokens["github/github"], "cross-repo entry must map to its declared token")
	// The default-checkout entry has no explicit token and must NOT appear in
	// the map (GITHUB_TOKEN handles that case in apply_samples.cjs).
	_, hasDefaultRepo := tokens["${{ github.repository }}"]
	assert.False(t, hasDefaultRepo, "default checkout without a github-token must not appear in the map; got: %v", tokens)
	// GITHUB_TOKEN fallback must still be emitted alongside the map.
	assert.Contains(t, lock, "GITHUB_TOKEN: ${{ github.token }}")
}

// TestUseSamplesOmitsRepoTokenMapWhenNoCustomAuth verifies that workflows
// whose `checkout:` entries declare no explicit token (i.e. rely on the
// runner's default GITHUB_TOKEN) do NOT emit a GH_AW_REPO_TOKENS env var.
// This keeps the generated YAML noise-free for the common case.
func TestUseSamplesOmitsRepoTokenMapWhenNoCustomAuth(t *testing.T) {
	const md = `---
on:
  workflow_dispatch:
permissions: read-all
engine:
  id: claude
safe-outputs:
  create-issue:
    samples:
      - title: "No custom auth"
        body: "Body for the default-auth sample issue, long enough to satisfy schema."
---

Default-auth workflow — should not need the per-repo token map.
`
	tmpFile, err := os.CreateTemp("", "use-samples-no-tokens-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	require.NoError(t, compiler.CompileWorkflow(tmpFile.Name()))
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	lock := string(b)

	assert.Contains(t, lock, "Replay safe-outputs samples (deterministic)")
	assert.NotContains(t, lock, "GH_AW_REPO_TOKENS", "GH_AW_REPO_TOKENS must be omitted when no checkout supplies a custom token")
}

// extractGHAWRepoTokensJSON pulls the literal block scalar value of
// TestUseSamplesForwardsWorkflowInputEnvVars verifies that when a safe-outputs
// field references a workflow_dispatch input (e.g. `target: ${{ inputs.pull_request_number }}`),
// the compiler forwards the corresponding GH_AW_INPUT_* environment variable
// to the "Replay safe-outputs samples (deterministic)" step so that
// safe_outputs_config.cjs can resolve the ${GH_AW_INPUT_…} placeholder written
// into config.json. Regression test for https://github.com/github/gh-aw/issues/54810.
func TestUseSamplesForwardsWorkflowInputEnvVars(t *testing.T) {
	const md = `---
on:
  workflow_dispatch:
    inputs:
      branch_name:
        required: true
        type: string
      pull_request_number:
        required: true
        type: number
permissions: read-all
engine:
  id: claude
safe-outputs:
  push-to-pull-request-branch:
    branch: ${{ inputs.branch_name }}
    target: ${{ inputs.pull_request_number }}
    samples:
      - message: "sample push"
        patch: |
          diff --git a/sample.txt b/sample.txt
          new file mode 100644
          --- /dev/null
          +++ b/sample.txt
          @@ -0,0 +1 @@
          +sample
---

Workflow whose safe-outputs config references workflow_dispatch inputs.
`
	tmpFile, err := os.CreateTemp("", "use-samples-inputs-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock := string(b)

	require.Contains(t, lock, "Replay safe-outputs samples (deterministic)")

	// Locate the replay step's env block and verify it forwards the
	// GH_AW_INPUT_PULL_REQUEST_NUMBER var referenced by `target:`.
	replayIdx := strings.Index(lock, "Replay safe-outputs samples (deterministic)")
	require.GreaterOrEqual(t, replayIdx, 0)
	nextStepIdx := strings.Index(lock[replayIdx+1:], "\n      - name:")
	var replayStep string
	if nextStepIdx < 0 {
		replayStep = lock[replayIdx:]
	} else {
		replayStep = lock[replayIdx : replayIdx+1+nextStepIdx]
	}

	assert.Contains(t, replayStep, "GH_AW_INPUT_PULL_REQUEST_NUMBER: ${{ inputs.pull_request_number }}",
		"replay step must forward GH_AW_INPUT_PULL_REQUEST_NUMBER so safe_outputs_config.cjs can resolve the placeholder")
}

// GH_AW_REPO_TOKENS out of the compiled YAML and returns the unindented JSON
// text. Mirrors extractGHAWSamplesJSON.
func extractGHAWRepoTokensJSON(t *testing.T, lock string) string {
	t.Helper()
	const marker = "GH_AW_REPO_TOKENS: |\n"
	start := strings.Index(lock, marker)
	if start < 0 {
		t.Fatalf("could not find %q in lock file", marker)
	}
	start += len(marker)
	rest := lock[start:]
	firstNL := strings.Index(rest, "\n")
	if firstNL < 0 {
		t.Fatal("malformed GH_AW_REPO_TOKENS block: no newline after first line")
	}
	firstLine := rest[:firstNL]
	indent := firstLine[:len(firstLine)-len(strings.TrimLeft(firstLine, " "))]
	if indent == "" {
		t.Fatal("malformed GH_AW_REPO_TOKENS block: expected indented content")
	}
	var out strings.Builder
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(line, indent) {
			break
		}
		out.WriteString(strings.TrimPrefix(line, indent))
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

// TestUseSamplesReplaysReplaceLabel is a regression test for
// https://github.com/github/gh-aw/issues/54811: a `replace-label` safe output
// configured with `samples:` must validate at compile time and be collected
// into GH_AW_SAMPLES as a `replace_label` MCP `tools/call` so that
// `--use-samples` suites actually perform the label transition instead of
// silently replaying nothing.
func TestUseSamplesReplaysReplaceLabel(t *testing.T) {
	const md = `---
on:
  issues:
    types: [opened]
permissions: read-all
engine:
  id: claude
safe-outputs:
  replace-label:
    allowed-transitions:
      - from: old
        to: new
    samples:
      - item_number: "${{ github.event.issue.number }}"
        label_to_remove: old
        label_to_add: new
---

Replace-label workflow replayed deterministically from samples.
`

	tmpFile, err := os.CreateTemp("", "use-samples-replace-label-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(md); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetUseSamples(true)
	if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	lockPath := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
	defer os.Remove(lockPath)
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lock := string(b)

	samplesJSON := extractGHAWSamplesJSON(t, lock)
	if !strings.Contains(samplesJSON, `"tool":"replace_label"`) {
		t.Fatalf("GH_AW_SAMPLES should contain a replace_label entry; got %s", samplesJSON)
	}
	if !strings.Contains(samplesJSON, "${{ github.event.issue.number }}") {
		t.Fatalf("GH_AW_SAMPLES should preserve the runtime expression; got %s", samplesJSON)
	}
}

// TestSafeOutputsMissingSamples verifies the diagnostic helper that backs the
// compile-time warning emitted when samples replay is enabled but an enabled
// safe output declares no samples — the failure mode reported in
// https://github.com/github/gh-aw/issues/54811 where a sampled run succeeded
// with an empty GH_AW_SAMPLES and left the fixture untouched.
func TestSafeOutputsMissingSamples(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		if got := safeOutputsMissingSamples(nil); got != nil {
			t.Fatalf("expected nil for nil config, got %v", got)
		}
	})

	t.Run("enabled handler without samples is reported", func(t *testing.T) {
		config := &SafeOutputsConfig{ReplaceLabel: &ReplaceLabelConfig{}}
		got := safeOutputsMissingSamples(config)
		if len(got) != 1 || got[0] != "replace-label" {
			t.Fatalf("expected [replace-label], got %v", got)
		}
	})

	t.Run("handler with samples is not reported", func(t *testing.T) {
		config := &SafeOutputsConfig{ReplaceLabel: &ReplaceLabelConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Samples: []map[string]any{{"item_number": 1, "label_to_remove": "old", "label_to_add": "new"}},
			},
		}}
		if got := safeOutputsMissingSamples(config); len(got) != 0 {
			t.Fatalf("expected no missing samples, got %v", got)
		}
	})
}
