package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCooldown(t *testing.T) {
	tests := []struct {
		name        string
		value       any
		want        time.Duration
		errContains string
	}{
		{name: "missing"},
		{name: "minimum", value: "5m", want: 5 * time.Minute},
		{name: "fractional seconds", value: "5m0.9s", want: 5*time.Minute + 900*time.Millisecond},
		{name: "composite", value: "1h30m", want: 90 * time.Minute},
		{name: "below minimum", value: "299s", errContains: "must be at least 5m"},
		{name: "invalid", value: "one hour", errContains: "invalid duration"},
		{name: "expression", value: "${{ inputs.cooldown }}", errContains: "expressions are not supported"},
		{name: "wrong type", value: 300, errContains: "expected a duration string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{"on": map[string]any{"workflow_dispatch": nil}}
			if tt.value != nil {
				frontmatter["on"].(map[string]any)["cooldown"] = tt.value
			}

			got, err := extractCooldown(frontmatter)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCooldownCompilation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "cooldown")
	workflowPath := filepath.Join(tmpDir, "cooldown.md")
	content := `---
on:
  workflow_dispatch:
  cooldown: 1h30m
engine: claude
---
Run after the cooldown.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockBytes, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockContent := string(lockBytes)

	assert.Contains(t, lockContent, "Check workflow cooldown")
	assert.Contains(t, lockContent, `GH_AW_COOLDOWN_SECONDS: "5400"`)
	assert.Contains(t, lockContent, "require(path.join(actionsDir, 'check_cooldown.cjs'))")
	assert.Contains(t, lockContent, "actions: read")
	assert.Contains(t, lockContent, "steps.check_cooldown.outputs.cooldown_ok == 'true'")
	assert.Contains(t, lockContent, "# cooldown: 1h30m # Cooldown processed as run history check in pre-activation job")
}

func TestCooldownCompilationWithScheduleDoesNotRequireMembership(t *testing.T) {
	tmpDir := testutil.TempDir(t, "cooldown-schedule")
	workflowPath := filepath.Join(tmpDir, "cooldown-schedule.md")
	content := `---
on:
  schedule:
    - cron: "*/30 * * * *"
  cooldown: 6h
engine: claude
---
Run after the cooldown.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockBytes, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lockContent := string(lockBytes)

	assert.Contains(t, lockContent, "Check workflow cooldown")
	assert.Contains(t, lockContent, `GH_AW_COOLDOWN_SECONDS: "21600"`)
	assert.Contains(t, lockContent, "activated: ${{ steps.check_cooldown.outputs.cooldown_ok == 'true' }}")
	assert.NotContains(t, lockContent, "check_membership")
}

func TestCooldownCompilationRoundsSecondsUp(t *testing.T) {
	tmpDir := testutil.TempDir(t, "cooldown")
	workflowPath := filepath.Join(tmpDir, "cooldown.md")
	content := `---
on:
  workflow_dispatch:
  cooldown: 5m0.9s
engine: claude
---
Run after the cooldown.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockBytes, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)

	assert.Contains(t, string(lockBytes), `GH_AW_COOLDOWN_SECONDS: "301"`)
}
