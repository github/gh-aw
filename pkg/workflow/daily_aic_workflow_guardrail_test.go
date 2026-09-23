//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestResolveMaxDailyAIC(t *testing.T) {
	t.Run("prefers top-level literal value", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{"max-daily-ai-credits": 1234}, `"999"`)
		if got == nil || *got != "1234" {
			t.Fatalf("expected literal top-level value, got %v", got)
		}

	})

	t.Run("falls back to imported expression", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{}, `"${{ inputs.max-daily-ai-credits }}"`)
		if got == nil || *got != "${{ inputs.max-daily-ai-credits }}" {
			t.Fatalf("expected imported expression, got %v", got)
		}
	})

	t.Run("emits runtime expression when no frontmatter", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{}, "")
		wantExpr := "${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS || '5000' }}"
		if got == nil || *got != wantExpr {
			t.Fatalf("expected runtime expression %q, got %v", wantExpr, got)
		}
	})

	t.Run("frontmatter value takes precedence over runtime default expression", func(t *testing.T) {
		got := resolveMaxDailyAIC(map[string]any{"max-daily-ai-credits": 1234}, "")
		if got == nil || *got != "1234" {
			t.Fatalf("expected frontmatter value to override runtime default expression, got %v", got)
		}
	})

	t.Run("normalizes suffix strings", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{"max-daily-ai-credits": "100M"}, "")
		if got == nil || *got != "100000000" {
			t.Fatalf("expected normalized suffix string, got %v", got)
		}
	})

	t.Run("explicit disable skips guardrail", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{"max-daily-ai-credits": -1}, "")
		if got != nil {
			t.Fatalf("expected explicit disable to skip the guardrail, got %v", *got)
		}
	})

	// T-AIC-DG-007: Imported workflow max-daily-ai-credits used when no frontmatter value;
	// frontmatter takes precedence over imports (spec §9.3 (2)).
	t.Run("spec §9.3(2) / T-AIC-DG-007: imported config used when no frontmatter value", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{}, `"2000"`)
		if got == nil || *got != "2000" {
			t.Fatalf("spec §9.3(2): expected imported config value %q, got %v", "2000", got)
		}
	})

	t.Run("spec §9.3(2) / T-AIC-DG-007: frontmatter takes precedence over imported config", func(t *testing.T) {
		t.Parallel()
		got := resolveMaxDailyAIC(map[string]any{"max-daily-ai-credits": 9999}, `"2000"`)
		if got == nil || *got != "9999" {
			t.Fatalf("spec §9.3(2): expected frontmatter value to override imported config, got %v", got)
		}
	})
}

func TestDailyAICWorkflowGuardrailInCompiledWorkflow(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-ai-credits-workflow-guardrail-*")
	workflowFile := filepath.Join(testDir, "daily-guardrail.md")

	workflow := `---
on:
  workflow_dispatch:
  stale-check: false
max-daily-ai-credits: 100_000_000
safe-outputs:
  add-comment:
    max: 1
---

Guardrail test workflow`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)
	activationStart := strings.Index(lockStr, "\n  activation:\n")
	if activationStart == -1 {
		t.Fatal("expected compiled workflow to include an activation job")
	}
	activationSection := lockStr[activationStart:]
	if nextJob := strings.Index(activationSection, "\n  agent:\n"); nextJob != -1 {
		activationSection = activationSection[:nextJob]
	}

	if !strings.Contains(lockStr, "id: daily-ai-credits-workflow-guardrail") {
		t.Fatal("expected activation job to include the daily AI Credits guardrail step")
	}
	if !strings.Contains(lockStr, "if: ${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}") {
		t.Fatal("expected frontmatter-configured guardrail step to use env-based runtime gating")
	}
	if !strings.Contains(lockStr, "check_daily_aic_workflow_guardrail.cjs") {
		t.Fatal("expected activation job to call check_daily_aic_workflow_guardrail.cjs")
	}
	if !strings.Contains(lockStr, `GH_AW_MAX_DAILY_AI_CREDITS: "100000000"`) {
		t.Fatal("expected activation job env to include normalized guardrail threshold")
	}
	if !strings.Contains(lockStr, `GH_AW_MAX_AI_CREDITS: ${{ vars.GH_AW_DEFAULT_MAX_AI_CREDITS || '1000' }}`) {
		t.Fatal("expected activation job env to include the per-run AI Credits fallback")
	}
	if !strings.Contains(lockStr, "daily_ai_credits_exceeded: ${{ steps.daily-ai-credits-workflow-guardrail.outputs.daily_ai_credits_exceeded == 'true' }}") {
		t.Fatal("expected activation job to expose daily_ai_credits_exceeded output")
	}
	if !strings.Contains(lockStr, "daily_ai_credits_guardrail_status: ${{ steps.daily-ai-credits-workflow-guardrail.outputs.daily_ai_credits_guardrail_status || '' }}") {
		t.Fatal("expected activation job to expose daily_ai_credits_guardrail_status output for structural vs transient failure distinction")
	}
	if !strings.Contains(lockStr, "daily_ai_credits_guardrail_error: ${{ steps.daily-ai-credits-workflow-guardrail.outputs.daily_ai_credits_guardrail_error || '' }}") {
		t.Fatal("expected activation job to expose the daily AI Credits guardrail error")
	}
	if !strings.Contains(lockStr, "daily_ai_credits_total: ${{ steps.daily-ai-credits-workflow-guardrail.outputs.daily_ai_credits_total || '' }}") {
		t.Fatal("expected activation job to expose the aggregated AI Credits total output")
	}
	if strings.Contains(lockStr, "daily_ai_credits_issue_url") {
		t.Fatal("expected activation job to avoid surfacing a separate daily AI Credits issue URL")
	}
	if !strings.Contains(lockStr, "if: needs.activation.outputs.daily_ai_credits_exceeded != 'true'") {
		t.Fatal("expected the agent job to be skipped when the daily AI Credits guardrail is exceeded")
	}
	if !strings.Contains(lockStr, "GH_AW_DAILY_AI_CREDITS_EXCEEDED: ${{ needs.activation.outputs.daily_ai_credits_exceeded }}") {
		t.Fatal("expected the conclusion job to receive the daily AI Credits guardrail output")
	}
	if !strings.Contains(lockStr, "GH_AW_DAILY_AI_CREDITS_GUARDRAIL_STATUS: ${{ needs.activation.outputs.daily_ai_credits_guardrail_status }}") {
		t.Fatal("expected the conclusion job to receive the daily AI Credits guardrail status")
	}
	if !strings.Contains(lockStr, "GH_AW_DAILY_AI_CREDITS_GUARDRAIL_ERROR: ${{ needs.activation.outputs.daily_ai_credits_guardrail_error }}") {
		t.Fatal("expected the conclusion job to receive the daily AI Credits guardrail error")
	}
	if !strings.Contains(lockStr, "needs.activation.outputs.daily_ai_credits_exceeded == 'true'") {
		t.Fatal("expected the conclusion job condition to allow activation guardrail failures through")
	}
	if !strings.Contains(lockStr, "needs.activation.outputs.daily_ai_credits_guardrail_status == 'structural_error'") ||
		!strings.Contains(lockStr, "needs.activation.outputs.daily_ai_credits_guardrail_status == 'transient_error'") {
		t.Fatal("expected the conclusion job condition to report daily AI Credits accounting failures")
	}
	if !strings.Contains(activationSection, "actions: read") {
		t.Fatal("expected activation permissions to include actions: read for workflow run inspection")
	}
	if strings.Contains(activationSection, "issues: write") {
		t.Fatal("expected activation permissions to avoid issues: write for the daily AI Credits guardrail")
	}
	if !strings.Contains(activationSection, "safe-output-artifact-client: ${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}") {
		t.Fatal("expected frontmatter-configured guardrail to gate artifact client installation dynamically")
	}
	if !strings.Contains(activationSection, "restore_aic_scan_cache.cjs") {
		t.Fatal("expected activation job to restore verified scan observations")
	}
	if !strings.Contains(activationSection, "id: restore-daily-aic-cache-fallback") {
		t.Fatal("expected activation job to include the artifact-based AIC cache fallback step")
	}
	if strings.Contains(activationSection, "id: detect-daily-aic-cache-miss") {
		t.Fatal("expected activation job to not include a separate bash cache-miss detection step (check is now in JS)")
	}
	wantFallbackIf := "if: " + maxDailyAICreditsConfiguredIfExpr
	if !strings.Contains(activationSection, wantFallbackIf) {
		t.Fatalf("expected artifact fallback step to use the standard AIC guard if: condition, want %q", wantFallbackIf)
	}
	if strings.Contains(activationSection, "cache-matched-key") || strings.Contains(lockStr, "write_daily_aic_usage_cache.cjs") {
		t.Fatal("scan observations must not depend on a prefix cache or conclusion-only records")
	}
	if !strings.Contains(activationSection, "name: aic-usage-scan-v2") ||
		!strings.Contains(activationSection, "path: /tmp/gh-aw/agentic-workflow-usage-scan-v2.jsonl") {
		t.Fatal("expected activation to publish its complete set of resolved observations")
	}
	restoreStart := strings.Index(activationSection, "name: Restore daily AIC scan observations")
	scanStart := strings.Index(activationSection, "name: Check daily workflow token guardrail")
	publishStart := strings.Index(activationSection, "name: Publish daily AIC scan observations")
	if restoreStart < 0 || scanStart <= restoreStart || publishStart <= scanStart {
		t.Fatal("expected restore, scan, then publication in the same activation job")
	}
	if strings.Contains(activationSection[restoreStart:scanStart], "continue-on-error: true") {
		t.Fatal("restore API failure must stop activation before another scan")
	}
}

func TestDailyAICExecutionEvidenceSurroundsPreAgentFailure(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-aic-pre-agent-failure-*")
	workflowFile := filepath.Join(testDir, "daily-aic-pre-agent-failure.md")
	workflow := `---
on:
  workflow_dispatch:
steps:
  - name: Fail before agent execution
    run: exit 1
safe-outputs:
  add-comment:
    max: 1
---

Pre-agent failure accounting test`
	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}
	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowFile))
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)
	initialize := strings.Index(lockStr, "name: Initialize agent execution evidence")
	failure := strings.Index(lockStr, "name: Fail before agent execution")
	execution := strings.Index(lockStr, "id: agentic_execution")
	started := -1
	if execution >= 0 {
		started = strings.Index(lockStr[execution:], `"state":"started"`)
	}
	if initialize < 0 || failure <= initialize || execution <= failure || started < 0 {
		t.Fatalf("expected execution evidence to prove a setup failure occurred before agent execution")
	}
	if !strings.Contains(lockStr, "/tmp/gh-aw/agent_execution.json") {
		t.Fatal("expected the agent artifact to include execution evidence")
	}
}

func TestDailyAICGuardrailDynamicGate(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-effective-workflow-no-guardrail-*")
	workflowFile := filepath.Join(testDir, "no-daily-guardrail.md")

	workflow := `---
on:
  workflow_dispatch:
  stale-check: false
safe-outputs:
  add-comment:
    max: 1
---

No daily guardrail`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)
	if !strings.Contains(lockStr, "id: daily-ai-credits-workflow-guardrail") {
		t.Fatal("expected activation job to emit the daily AI Credits guardrail step even when threshold is unset")
	}
	if !strings.Contains(lockStr, "if: ${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}") {
		t.Fatal("expected emitted daily AI Credits guardrail step to be dynamically skipped when threshold is unset")
	}
	if !strings.Contains(lockStr, "daily_ai_credits_exceeded") {
		t.Fatal("expected workflows to continue wiring daily AI Credits outputs when guardrail step is emitted")
	}
	if !strings.Contains(lockStr, "safe-output-artifact-client: ${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}") {
		t.Fatal("expected emitted guardrail to gate artifact client installation dynamically")
	}
}

func TestDailyAICWorkflowGuardrailConfiguredViaEnvVar(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-effective-workflow-env-guardrail-*")
	workflowFile := filepath.Join(testDir, "daily-guardrail-env.md")

	workflow := `---
on:
  workflow_dispatch:
  stale-check: false
env:
  GH_AW_MAX_DAILY_AI_CREDITS: "5000000"
safe-outputs:
  add-comment:
    max: 1
---

Daily guardrail via env var`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	if !strings.Contains(lockStr, "id: daily-ai-credits-workflow-guardrail") {
		t.Fatal("expected activation job to include the daily AI Credits guardrail step when env var is configured")
	}
	if !strings.Contains(lockStr, "if: ${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}") {
		t.Fatal("expected daily AI Credits guardrail step to gate execution on GH_AW_MAX_DAILY_AI_CREDITS")
	}
	if !strings.Contains(lockStr, "safe-output-artifact-client: ${{ env.GH_AW_MAX_DAILY_AI_CREDITS != '' }}") {
		t.Fatal("expected setup step to conditionally install artifact client when daily AI Credits guardrail is env-configured")
	}
}

func TestDailyAICGuardrailNegativeValueRejected(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-effective-workflow-explicit-disable-*")
	workflowFile := filepath.Join(testDir, "daily-guardrail-explicit-disable.md")

	// -2 is below the minimum of -1 (the explicit disable sentinel) and must be rejected.
	workflow := `---
on:
  workflow_dispatch:
  stale-check: false
max-daily-ai-credits: -2
safe-outputs:
  add-comment:
    max: 1
---

Invalid negative daily guardrail value`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowFile)
	if err == nil {
		t.Fatal("expected compile to fail for invalid negative max-daily-ai-credits")
	}
	// Schema validation or frontmatter validation may produce the error; either
	// correctly rejects values below -1.
	if !strings.Contains(err.Error(), "must be -1") && !strings.Contains(err.Error(), "minimum") {
		t.Fatalf("expected validation error rejecting -2, got: %v", err)
	}
}

func TestDailyAICObjectFormMissingValueRejected(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-aic-missing-value-*")
	workflowFile := filepath.Join(testDir, "daily-aic-missing-value.md")

	// Object form without a 'value' key must be rejected with a clear error.
	workflow := `---
on:
  workflow_dispatch:
  stale-check: false
max-daily-ai-credits:
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
safe-outputs:
  add-comment:
    max: 1
---

Object form without value key`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowFile)
	if err == nil {
		t.Fatal("expected compile to fail for object form missing 'value' key")
	}
	if !strings.Contains(err.Error(), "value") {
		t.Fatalf("expected error to mention 'value' field, got: %v", err)
	}
}

func TestMaxDailyAICObjectForm(t *testing.T) {
	t.Run("object form value is used as threshold", func(t *testing.T) {
		got := resolveMaxDailyAIC(map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
			},
		}, "")
		if got == nil || *got != "5000" {
			t.Fatalf("expected object form value to be used as threshold, got %v", got)
		}
	})

	t.Run("object form with value -1 is treated as disabled", func(t *testing.T) {
		got := resolveMaxDailyAIC(map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": -1,
			},
		}, "")
		if got != nil {
			t.Fatalf("expected nil (disabled) for object form value -1, got %v", got)
		}
	})

	t.Run("object form github-app is extracted", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
				"github-app": map[string]any{
					"client-id":   "${{ vars.APP_ID }}",
					"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
				},
			},
		}
		app := extractMaxDailyAICGitHubApp(frontmatter)
		if app == nil {
			t.Fatal("expected github-app to be extracted from object form")
		}
		if app.AppID != "${{ vars.APP_ID }}" {
			t.Fatalf("unexpected AppID: %s", app.AppID)
		}
		if app.PrivateKey != "${{ secrets.APP_PRIVATE_KEY }}" {
			t.Fatalf("unexpected PrivateKey: %s", app.PrivateKey)
		}
	})

	t.Run("object form continue-on-error is extracted", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value":             5000,
				"continue-on-error": true,
			},
		}
		if !resolveMaxDailyAICContinueOnError(frontmatter, "") {
			t.Fatal("expected continue-on-error to be enabled")
		}
	})

	t.Run("object form repo-memory backend is extracted", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value":   5000,
				"backend": "repo-memory",
			},
		}
		if got := resolveMaxDailyAICBackend(frontmatter, ""); got != "repo-memory" {
			t.Fatalf("expected repo-memory backend, got %q", got)
		}
	})

	t.Run("continue-on-error defaults to false", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
			},
		}

		if resolveMaxDailyAICContinueOnError(frontmatter, "") {
			t.Fatal("expected continue-on-error to be disabled by default")
		}
	})

	t.Run("continue-on-error falls back to imported config when main frontmatter omits it", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
			},
		}
		importedJSON := `{"value":5000,"continue-on-error":true}`
		if !resolveMaxDailyAICContinueOnError(frontmatter, importedJSON) {
			t.Fatal("expected continue-on-error to be resolved from imported config")
		}
	})

	t.Run("continue-on-error main frontmatter takes precedence over imported config", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value":             5000,
				"continue-on-error": false,
			},
		}
		importedJSON := `{"value":5000,"continue-on-error":true}`
		if resolveMaxDailyAICContinueOnError(frontmatter, importedJSON) {
			t.Fatal("expected main frontmatter continue-on-error:false to take precedence over imported config")
		}
	})

	t.Run("object form github-app with ignore-if-missing is preserved", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
				"github-app": map[string]any{
					"client-id":         "${{ vars.APP_ID }}",
					"private-key":       "${{ secrets.APP_KEY }}",
					"ignore-if-missing": true,
				},
			},
		}

		app := extractMaxDailyAICGitHubApp(frontmatter)
		if app == nil {
			t.Fatal("expected non-nil app when ignore-if-missing is set")
		}
		if !app.IgnoreIfMissing {
			t.Fatal("expected IgnoreIfMissing to be true")
		}
	})

	t.Run("object form github-app with ignore-if-missing and empty credentials is preserved", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
				"github-app": map[string]any{
					"ignore-if-missing": true,
				},
			},
		}
		app := extractMaxDailyAICGitHubApp(frontmatter)
		if app == nil {
			t.Fatal("expected non-nil app when ignore-if-missing is set, even with empty credentials")
		}
		if !app.IgnoreIfMissing {
			t.Fatal("expected IgnoreIfMissing to be true")
		}
	})

	t.Run("scalar form returns nil github-app", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": 5000,
		}
		app := extractMaxDailyAICGitHubApp(frontmatter)
		if app != nil {
			t.Fatalf("expected nil github-app for scalar form, got %+v", app)
		}
	})

	t.Run("object form without github-app returns nil", func(t *testing.T) {
		frontmatter := map[string]any{
			"max-daily-ai-credits": map[string]any{
				"value": 5000,
			},
		}
		app := extractMaxDailyAICGitHubApp(frontmatter)
		if app != nil {
			t.Fatalf("expected nil github-app when not specified, got %+v", app)
		}
	})
}

func TestMaxDailyAICRepoMemoryBackendCompiledWorkflow(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-aic-repo-memory-*")
	workflowFile := filepath.Join(testDir, "daily-aic-repo-memory.md")

	workflow := `---
on:
  workflow_dispatch:
max-daily-ai-credits:
  value: 10000
  backend: repo-memory
tools:
  repo-memory: true
---

Daily AIC guardrail with repo-memory ledger`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowFile))
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)
	if !strings.Contains(lockStr, "GH_AW_MAX_DAILY_AI_CREDITS_BACKEND: \"repo-memory\"") {
		t.Fatal("expected activation guardrail to receive repo-memory backend env")
	}
	if !strings.Contains(lockStr, "GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC: ${{ vars.GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC || 'false' }}") {
		t.Fatal("expected repo-memory backend to require explicit insecure opt-in")
	}
	if !strings.Contains(lockStr, "Clone daily AIC repo-memory ledger (default)") {
		t.Fatal("expected activation to clone repo-memory ledger")
	}
	if strings.Contains(lockStr, "Restore daily AIC scan observations") || strings.Contains(lockStr, "Publish daily AIC scan observations") {
		t.Fatal("expected repo-memory backend to skip artifact-backed scan cache restore/publish steps")
	}
	appendIdx := strings.Index(lockStr, "Append daily AIC repo-memory ledger")
	pushIdx := strings.Index(lockStr, "Push repo-memory changes (default)")
	if appendIdx < 0 || pushIdx < 0 || appendIdx > pushIdx {
		t.Fatal("expected trusted daily AIC repo-memory ledger append before repo-memory push")
	}
	if !strings.Contains(lockStr, "Reset untrusted daily AIC repo-memory ledger artifact") {
		t.Fatal("expected untrusted agent-supplied repo-memory ledger files to be removed before append")
	}
	cloneIdx := strings.Index(lockStr, "Clone daily AIC repo-memory ledger for hydration")
	resetIdx := strings.Index(lockStr, "Reset untrusted daily AIC repo-memory ledger artifact")
	hydrateIdx := strings.Index(lockStr, "Hydrate daily AIC repo-memory ledger from trusted branch")
	if cloneIdx < 0 || resetIdx < 0 || hydrateIdx < 0 || cloneIdx >= resetIdx || resetIdx >= hydrateIdx || hydrateIdx >= appendIdx {
		t.Fatal("expected the ledger to be hydrated from a trusted branch clone (in order: clone, reset, hydrate, append) so accumulated history is not discarded")
	}
	if !strings.Contains(lockStr, "cp -a \"$GH_AW_DAILY_AIC_LEDGER_SOURCE_DIR/daily-aic-ledger/.\" \"$GH_AW_DAILY_AIC_REPO_MEMORY_DIR/daily-aic-ledger/\"") {
		t.Fatal("expected the hydration step to copy the trusted branch snapshot's ledger into the reset artifact directory")
	}
}

func TestMaxDailyAICRepoMemoryBackendRequiresRepoMemory(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-aic-repo-memory-missing-*")
	workflowFile := filepath.Join(testDir, "daily-aic-repo-memory-missing.md")

	workflow := `---
on:
  workflow_dispatch:
max-daily-ai-credits:
  value: 10000
  backend: repo-memory
---

Daily AIC guardrail without repo-memory`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowFile)
	if err == nil {
		t.Fatal("expected compile to fail when repo-memory backend is configured without tools.repo-memory")
	}
	if !strings.Contains(err.Error(), "requires tools.repo-memory") {
		t.Fatalf("expected repo-memory requirement error, got: %v", err)
	}
}

func TestMaxDailyAICContinueOnErrorCompiledWorkflow(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-aic-continue-on-error-*")
	workflowFile := filepath.Join(testDir, "daily-aic-continue-on-error.md")

	workflow := `---
on:
  workflow_dispatch:
max-daily-ai-credits:
  value: 10000
  continue-on-error: true
---

Daily AIC guardrail with warning-only unknown accounting`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowFile))
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)
	stepStart := strings.Index(lockStr, "id: daily-ai-credits-workflow-guardrail")
	if stepStart < 0 {
		t.Fatal("expected compiled workflow to include the daily AI Credits guardrail step")
	}
	stepEnd := strings.Index(lockStr[stepStart:], "\n      - name:")
	if stepEnd < 0 {
		t.Fatal("expected another step after the daily AI Credits guardrail step")
	}
	guardrailStep := lockStr[stepStart : stepStart+stepEnd]
	if !strings.Contains(guardrailStep, "continue-on-error: true") {
		t.Fatal("expected unknown daily AI Credits accounting to be warning-only")
	}
}

func TestMaxDailyAICWithGitHubAppCompiledWorkflow(t *testing.T) {
	testDir := testutil.TempDir(t, "daily-aic-github-app-*")
	workflowFile := filepath.Join(testDir, "daily-aic-github-app.md")

	workflow := `---
on:
  workflow_dispatch:
  stale-check: false
max-daily-ai-credits:
  value: 10000
  github-app:
    client-id: ${{ vars.AIC_APP_CLIENT_ID }}
    private-key: ${{ secrets.AIC_APP_PRIVATE_KEY }}
safe-outputs:
  add-comment:
    max: 1
---

Daily AIC guardrail with dedicated GitHub App`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0o644); err != nil {
		t.Fatalf("failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	if !strings.Contains(lockStr, "id: "+dailyAICAppTokenStepID) {
		t.Fatal("expected compiled workflow to include the daily AIC GitHub App token mint step")
	}
	// The mint step must be gated on the guardrail env var so it is skipped in workflows
	// where the guardrail is not active at runtime.
	if !strings.Contains(lockStr, "if: "+maxDailyAICreditsConfiguredIfExpr) {
		t.Fatalf("expected mint step to be gated on guardrail env var %s", maxDailyAICreditsConfiguredIfExpr)
	}
	if !strings.Contains(lockStr, "${{ vars.AIC_APP_CLIENT_ID }}") {
		t.Fatal("expected daily AIC token step to include the configured client-id")
	}
	if !strings.Contains(lockStr, "${{ secrets.AIC_APP_PRIVATE_KEY }}") {
		t.Fatal("expected daily AIC token step to include the configured private-key")
	}
	if !strings.Contains(lockStr, "permission-actions: read") {
		t.Fatal("expected daily AIC token step to request actions: read permission")
	}
	aicTokenRef := fmt.Sprintf("${{ steps.%s.outputs.token }}", dailyAICAppTokenStepID)
	if !strings.Contains(lockStr, aicTokenRef) {
		t.Fatalf("expected guardrail steps to use the dedicated AIC app token %s", aicTokenRef)
	}
	if !strings.Contains(lockStr, `GH_AW_MAX_DAILY_AI_CREDITS: "10000"`) {
		t.Fatal("expected activation job env to include the guardrail threshold from the object form")
	}
}

func TestDailyAICRepoMemorySelectionAndValidation(t *testing.T) {
	t.Parallel()

	data := &WorkflowData{
		MaxDailyAICBackend: maxDailyAICBackendRepoMemory,
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
			{ID: "first"},
			{ID: "second"},
		}},
	}
	entry, ok := dailyAICRepoMemoryEntry(data)
	if !ok || entry.ID != "first" {
		t.Fatalf("expected first configured repo-memory entry, got %+v", entry)
	}

	data.RepoMemoryConfig = nil
	if err := validateMaxDailyAICFrontmatter(data); err == nil || !strings.Contains(err.Error(), "requires tools.repo-memory") {
		t.Fatalf("expected imported repo-memory backend to require repo-memory, got %v", err)
	}
}
