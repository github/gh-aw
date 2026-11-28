//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/require"
)

func applyModelAutoCodemodForTest(t *testing.T, content, filePath string) (string, bool, string, error) {
	t.Helper()
	doc, err := aw.Parse(content)
	require.NoError(t, err)
	codemod := getModelAutoToRouterCodemod()
	var updated string
	var applied bool
	var applyErr error
	stderr := testutil.CaptureStderr(t, func() {
		if filePath != "" {
			updated, applied, applyErr = codemod.ApplyWithContext(content, doc.Frontmatter, filePath)
		} else {
			updated, applied, applyErr = codemod.Apply(content, doc.Frontmatter)
		}
	})
	return updated, applied, stderr, applyErr
}

func TestModelAutoCodemod_RewritesEligibleAndIsIdempotent(t *testing.T) {
	cases := []struct {
		name           string
		input          string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "copilot top-level model: auto",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: auto
---
Do work.
`,
			wantContains:   []string{"engine: copilot", "model: router"},
			wantNotContain: []string{"model: auto", "model-provider:"},
		},
		{
			name: "pi top-level model: auto",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: pi
model: auto
---
Do work.
`,
			wantContains:   []string{"engine: pi", "model: router"},
			wantNotContain: []string{"model: auto", "model-provider:"},
		},
		{
			name: "claude string engine with copilot/auto expands engine and adds model-provider: github",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: claude
model: copilot/auto
---
Do work.
`,
			wantContains:   []string{"id: claude", "model-provider: github", "model: router"},
			wantNotContain: []string{"copilot/auto"},
		},
		{
			name: "claude map engine with copilot/auto preserves other fields and adds model-provider: github",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: claude
  bare: true
model: copilot/auto
---
Do work.
`,
			wantContains:   []string{"id: claude", "bare: true", "model-provider: github", "model: router"},
			wantNotContain: []string{"copilot/auto"},
		},
		{
			name: "codex string engine with copilot/auto expands engine and adds model-provider: github",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: codex
model: copilot/auto
---
Do work.
`,
			wantContains:   []string{"id: codex", "model-provider: github", "model: router"},
			wantNotContain: []string{"copilot/auto"},
		},
		{
			name: "claude with explicit model-provider: github and model: auto",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: claude
  model-provider: github
model: auto
---
Do work.
`,
			wantContains:   []string{"id: claude", "model-provider: github", "model: router"},
			wantNotContain: []string{"model: auto"},
		},
		{
			name: "engine.model: auto on copilot",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  model: auto
---
Do work.
`,
			wantContains:   []string{"id: copilot", "model: router"},
			wantNotContain: []string{"model: auto"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updated, applied, stderr, err := applyModelAutoCodemodForTest(t, tc.input, "")
			require.NoError(t, err)
			require.True(t, applied)
			require.Empty(t, stderr)
			for _, want := range tc.wantContains {
				require.Contains(t, updated, want)
			}
			for _, unwanted := range tc.wantNotContain {
				require.NotContains(t, updated, unwanted)
			}

			// Verify idempotency on a second pass.
			second, applied2, stderr2, err := applyModelAutoCodemodForTest(t, updated, "")
			require.NoError(t, err)
			require.False(t, applied2)
			require.Equal(t, updated, second)
			require.Empty(t, stderr2)

			// Verify the rewritten workflow compiles to equivalent routing as the original.
			origCompiler := workflow.NewCompiler()
			origCompiler.SetQuiet(true)
			origData, err := origCompiler.ParseWorkflowString(tc.input, "orig.md")
			require.NoError(t, err)
			fixedCompiler := workflow.NewCompiler()
			fixedCompiler.SetQuiet(true)
			fixedData, err := fixedCompiler.ParseWorkflowString(updated, "fixed.md")
			require.NoError(t, err)
			require.Equal(t, origData.EngineConfig.ID, fixedData.EngineConfig.ID)
			require.Equal(t, origData.EngineConfig.LLMProvider, fixedData.EngineConfig.LLMProvider)
			require.NotNil(t, origData.EngineConfig.ModelRouting)
			require.NotNil(t, fixedData.EngineConfig.ModelRouting)
			require.Equal(t, origData.EngineConfig.ModelRouting.Goal, fixedData.EngineConfig.ModelRouting.Goal)
			require.Equal(t, origData.EngineConfig.ModelRouting.Mode, fixedData.EngineConfig.ModelRouting.Mode)
		})
	}
}

func TestModelAutoCodemod_LeavesIneligibleUnchangedWithGuidance(t *testing.T) {
	cases := []struct {
		name           string
		input          string
		wantReasonPart string
	}{
		{
			name: "model-routing: false",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: auto
model-routing: false
---
Do work.
`,
			wantReasonPart: "model-routing is false",
		},
		{
			name: "fallback-models",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  fallback-models: [copilot/gpt-5.6-luna]
model: copilot/auto
---
Do work.
`,
			wantReasonPart: "fallback models",
		},
		{
			name: "model-catalog: false",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  model-catalog: false
model: auto
---
Do work.
`,
			wantReasonPart: "model-catalog is false",
		},
		{
			name: "stage-2 rollback via restrictive models.allowed on claude copilot/auto",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: claude
model: copilot/auto
models:
  allowed: [gpt-5.6-luna]
---
Do work.
`,
			wantReasonPart: "excluded by models.allowed",
		},
		{
			name: "native claude without model-provider: github",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
model: auto
---
Do work.
`,
			wantReasonPart: "not GitHub Copilot",
		},
		{
			name: "older AWF pin without fallback",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: auto
sandbox:
  agent:
    version: v0.29.20
---
Do work.
`,
			wantReasonPart: "v0.29.21",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updated, applied, stderr, err := applyModelAutoCodemodForTest(t, tc.input, "")
			require.NoError(t, err)
			require.False(t, applied)
			require.Equal(t, tc.input, updated)
			require.Contains(t, stderr, "left model:")
			require.Contains(t, stderr, tc.wantReasonPart)
			require.Contains(t, stderr, "pin a concrete model")
		})
	}
}

func TestModelAutoCodemod_UntouchedSilentCases(t *testing.T) {
	dir := t.TempDir()
	importedAliasPath := filepath.Join(dir, "shared", "aliases.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(importedAliasPath), 0o755))
	require.NoError(t, os.WriteFile(importedAliasPath, []byte("---\nmodels:\n  auto: [copilot/gpt-5.6-luna]\n---\n"), 0o600))
	mainWithImportPath := filepath.Join(dir, "main.md")

	cases := []struct {
		name     string
		input    string
		filePath string
	}{
		{
			name: "custom local models.auto alias",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: auto
models:
  auto: [copilot/gpt-5.6-luna]
---
Do work.
`,
		},
		{
			name: "custom imported models.auto alias",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: auto
imports:
  - shared/aliases.md
---
Do work.
`,
			filePath: mainWithImportPath,
		},
		{
			name: "parameterized auto?effort=high",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: auto?effort=high
---
Do work.
`,
		},
		{
			name: "shared workflow without on: trigger",
			input: `---
engine: copilot
model: copilot/auto
---
Shared fragment.
`,
		},
		{
			name: "shared workflow with import-safe on: block",
			input: `---
on:
  skip-bots:
    - dependabot
engine: copilot
model: auto
---
Shared fragment.
`,
		},
		{
			name: "omitted model",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
---
Do work.
`,
		},
		{
			name: "sub-agent auto model only",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: gpt-5.6-luna
---
Do work.

---
agent: helper
model: auto
---
Help.
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updated, applied, stderr, err := applyModelAutoCodemodForTest(t, tc.input, tc.filePath)
			require.NoError(t, err)
			require.False(t, applied)
			require.Equal(t, tc.input, updated)
			require.Empty(t, stderr)
		})
	}
}

func TestModelAutoCodemod_InheritedClaudeOrCodexWithCopilotAutoLeavesUnchangedWithGuidance(t *testing.T) {
	dir := t.TempDir()
	sharedClaudePath := filepath.Join(dir, "shared", "claude.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(sharedClaudePath), 0o755))
	require.NoError(t, os.WriteFile(sharedClaudePath, []byte("---\nengine: claude\n---\n"), 0o600))
	mainPath := filepath.Join(dir, "main.md")

	for _, tc := range []struct {
		name  string
		input string
	}{
		{
			name: "top-level model: copilot/auto with imported claude engine",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
imports:
  - shared/claude.md
model: copilot/auto
---
Do work.
`,
		},
		{
			name: "model-only engine map with imported claude engine",
			input: `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
imports:
  - shared/claude.md
engine:
  model: copilot/auto
---
Do work.
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, applied, stderr, err := applyModelAutoCodemodForTest(t, tc.input, mainPath)
			require.NoError(t, err)
			require.False(t, applied)
			require.Equal(t, tc.input, updated)
			require.Contains(t, stderr, "inherited from an import")
			require.Contains(t, stderr, "engine.model-provider: github")
		})
	}
}
