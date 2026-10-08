//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDryRunFeatures(t *testing.T) {
	for _, tt := range []struct {
		name     string
		features map[string]any
		failure  string
	}{
		{"none", nil, ""},
		{"ordinary feature", map[string]any{"gh-aw-detection": false}, ""},
		{"disabled", map[string]any{"dangerously-disable-sandbox-agent": false}, ""},
		{"empty string", map[string]any{"dangerously-example": ""}, ""},
		{"enabled", map[string]any{"dangerously-disable-sandbox-agent": true}, "features.dangerously-disable-sandbox-agent"},
		{"string value", map[string]any{"dangerously-example": "enabled"}, "features.dangerously-example"},
		{"case insensitive", map[string]any{"Dangerously-example": true}, "features.Dangerously-example"},
		{"deterministic order", map[string]any{"dangerously-z": true, "dangerously-a": true}, "features.dangerously-a, features.dangerously-z"},
		{"mixed case lexical order", map[string]any{"dangerously-a": true, "Dangerously-z": true, "dangerously-z": true}, "features.Dangerously-z, features.dangerously-a, features.dangerously-z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDryRunFeatures(tt.features)
			if tt.failure == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.failure)
			}
		})
	}
}

func TestDryRunRefusesAuthoredDangerousFeatures(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "frontmatter", true: "import"}[imported], func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "workflow.md")
			lock := filepath.Join(dir, "workflow.lock.yml")
			config := "features:\n  dangerously-example: true\n"
			if imported {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\n"+config+"---\nShared instructions.\n"), 0o600))
				config = "imports:\n  - shared.md\n"
			}
			require.NoError(t, os.WriteFile(source, []byte("---\nstrict: false\non: workflow_dispatch\nengine: agy\n"+config+"---\nSay hello.\n"), 0o600))
			require.NoError(t, os.WriteFile(lock, []byte("existing lock\n"), 0o600))
			compiler := NewCompiler()
			compiler.SetDryRun(true)
			require.ErrorContains(t, compiler.CompileWorkflow(source), "features.dangerously-example")
			content, err := os.ReadFile(lock)
			require.NoError(t, err)
			assert.Equal(t, "existing lock\n", string(content))
			compiler.SetDryRun(false)
			require.NoError(t, compiler.CompileWorkflow(source))
		})
	}
}
