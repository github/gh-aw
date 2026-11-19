//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestAuthorizationCompilerRejectsMutableDependencies(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	for _, test := range []struct {
		name, config, wantError string
	}{
		{"unresolved action", "steps:\n  - uses: unknown/action@unresolved-ref\n", "all action references to be pinned"},
		{"unresolved skill", "skills:\n  - unknown/skills@unresolved-ref\n", "full commit SHA"},
		{"default branch skill", "skills:\n  - unknown/skills@\n", "full commit SHA"},
		{"pinned skill", "skills:\n  - unknown/skills@" + sha + "\n", ""},
		{"local skill", "skills:\n  - .github/skills/local\n", ""},
		{"remote action metadata", "safe-outputs:\n  actions:\n    notify:\n      uses: unknown/action@" + sha + "\n", "remote metadata fetching is disabled"},
		{"inline action metadata", "safe-outputs:\n  actions:\n    notify:\n      uses: unknown/action@" + sha + "\n      inputs: {}\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, constants.WorkflowsDir, "test.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(source), constants.DirPermOwnerOnly))
			require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\npermissions:\n  contents: read\n"+test.config+"---\n# Test\nDo nothing.\n"), constants.FilePermSensitive))
			// No external programs are available, even if a future regression
			// accidentally attempts metadata resolution.
			t.Setenv("PATH", root)
			data, generated, err := NewAuthorizationVerificationCompiler(root).CompileAuthorizationSnapshot(source)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Nil(t, data)
				require.Empty(t, generated)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, generated)
			}
		})
	}
}

func TestAuthorizationCompilerRemoteMetadataRequiresInlineInputs(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	root := t.TempDir()
	compiler := NewAuthorizationVerificationCompiler(root)
	compiler.actionCache.SetInputs("unknown/action", sha, map[string]*ActionYAMLInput{"payload": {}})
	config := &SafeOutputActionConfig{Uses: "unknown/action@" + sha}
	data := &WorkflowData{
		ActionCache: compiler.actionCache, ActionResolver: compiler.actionResolver,
		SafeOutputs: &SafeOutputsConfig{Actions: map[string]*SafeOutputActionConfig{"notify": config}},
	}
	t.Setenv("PATH", root)
	err := validateAuthorizationActionMetadata(data)
	require.ErrorContains(t, err, "remote metadata fetching is disabled")
	require.Nil(t, config.Inputs, "candidate-owned cached metadata is not a reproducible input")
}
