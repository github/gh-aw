//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func hasModelPricingResolver(compiler *workflow.Compiler) bool {
	// Keep this field name in sync with workflow.Compiler; this helper intentionally
	// inspects private state to preserve behavioral coverage without re-exporting API.
	return !reflect.ValueOf(compiler).Elem().FieldByName("modelPricingResolver").IsNil()
}

func TestCreateAndConfigureCompiler_RegistersModelPricingResolver(t *testing.T) {
	t.Parallel()
	compiler := createAndConfigureCompiler(CompileConfig{})
	if !hasModelPricingResolver(compiler) {
		t.Fatal("expected local model pricing resolver to be registered")
	}
}

func TestCreateAndConfigureCompilerInjectsCatalogPricing(t *testing.T) {
	t.Parallel()
	workflowPath := filepath.Join(t.TempDir(), "priced-model.md")
	err := os.WriteFile(workflowPath, []byte(`---
on:
  workflow_dispatch:
name: Priced Model
engine: codex
model: gpt-6.1-sol
---
Check model pricing.
`), 0o600)
	require.NoError(t, err)

	data, err := createAndConfigureCompiler(CompileConfig{}).ParseWorkflowFile(workflowPath)
	require.NoError(t, err)
	providers, ok := data.ModelCosts["providers"].(map[string]any)
	require.True(t, ok)
	openai, ok := providers["openai"].(map[string]any)
	require.True(t, ok)
	models, ok := openai["models"].(map[string]any)
	require.True(t, ok)
	entry, ok := models["gpt-6.1-sol"].(map[string]any)
	require.True(t, ok)
	cost, ok := entry["cost"].(map[string]string)
	require.True(t, ok)
	require.Equal(t, "2e-06", cost["input"])
	require.Equal(t, "1e-05", cost["output"])
}

// TestSetupRepositoryContext_ValidScheduleSeedLocksSlug verifies that when
// --schedule-seed contains a valid "owner/repo" slug, setupRepositoryContext
// sets it on the compiler AND locks it so per-file git-remote detection
// cannot overwrite it.
func TestSetupRepositoryContext_ValidScheduleSeedLocksSlug(t *testing.T) {
	t.Parallel()
	compiler := workflow.NewCompiler()
	config := CompileConfig{
		ScheduleSeed: "github/gh-aw",
	}

	setupRepositoryContext(compiler, config)

	if got := compiler.GetRepositorySlug(); got != "github/gh-aw" {
		t.Fatalf("expected slug github/gh-aw, got %q", got)
	}
	if !compiler.IsRepositorySlugLocked() {
		t.Fatal("slug should be locked after a valid --schedule-seed is applied")
	}

	// Simulate what compileWorkflowFile does: per-file remote slug should not win.
	compiler.SetRepositorySlugIfUnlocked("trask/gh-aw")
	if got := compiler.GetRepositorySlug(); got != "github/gh-aw" {
		t.Fatalf("per-file override should have been blocked; expected github/gh-aw, got %q", got)
	}
}

// TestSetupRepositoryContext_InvalidScheduleSeedDoesNotLock verifies that an
// invalid --schedule-seed value triggers a warning and falls back to git remote
// detection; the slug is NOT locked so per-file detection can still set it.
func TestSetupRepositoryContext_InvalidScheduleSeedDoesNotLock(t *testing.T) {
	t.Parallel()
	compiler := workflow.NewCompiler()
	config := CompileConfig{
		ScheduleSeed: "not-valid", // missing slash
	}

	setupRepositoryContext(compiler, config)

	if compiler.IsRepositorySlugLocked() {
		t.Fatal("slug should not be locked when --schedule-seed value is invalid")
	}
}

// TestSetupRepositoryContext_EmptyScheduleSeedDoesNotLock verifies that omitting
// --schedule-seed leaves the slug unlocked so per-file git-remote detection applies.
func TestSetupRepositoryContext_EmptyScheduleSeedDoesNotLock(t *testing.T) {
	t.Parallel()
	compiler := workflow.NewCompiler()
	config := CompileConfig{
		ScheduleSeed: "",
	}

	setupRepositoryContext(compiler, config)

	if compiler.IsRepositorySlugLocked() {
		t.Fatal("slug should not be locked when --schedule-seed is not provided")
	}
}

// TestSetupRepositoryContext_ScheduleSeedTakesPrecedenceOverPerFileRemote is an
// end-to-end regression guard: even after compileWorkflowFile calls
// SetRepositorySlugIfUnlocked, the slug remains the one from --schedule-seed.
func TestSetupRepositoryContext_ScheduleSeedTakesPrecedenceOverPerFileRemote(t *testing.T) {
	t.Parallel()
	compiler := workflow.NewCompiler()

	// Simulate setupRepositoryContext with a valid --schedule-seed.
	config := CompileConfig{ScheduleSeed: "upstream/repo"}
	setupRepositoryContext(compiler, config)

	// Simulate the per-file detection path in compileWorkflowFile.
	compiler.SetRepositorySlugIfUnlocked("fork/repo")

	if got := compiler.GetRepositorySlug(); got != "upstream/repo" {
		t.Fatalf("--schedule-seed should take precedence over per-file remote; expected upstream/repo, got %q", got)
	}
}
