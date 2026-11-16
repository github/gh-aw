//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerLintUntrustedRolesResolvedDetection(t *testing.T) {
	t.Parallel()
	const header = "---\non:\n  issues:\n    types: [opened]\n  roles: all\nengine: copilot\npermissions:\n  contents: read\n"
	const reporters = "safe-outputs:\n  report-missing-tool: false\n  report-missing-data: false\n  report-incomplete: false\n  noop: false\n"
	for _, tt := range []struct {
		name, source, shared, wantErr string
		useSamples                    bool
	}{
		{
			name:    "authored expression",
			source:  "safe-outputs:\n  add-comment: {}\n  threat-detection: '${{ false }}'\n",
			wantErr: "safe-outputs.threat-detection is a runtime expression",
		},
		{
			name:    "imported expression",
			source:  "imports:\n  - shared/outputs.md\n",
			shared:  "---\nsafe-outputs:\n  add-comment: {}\n  threat-detection: '${{ false }}'\n---\n",
			wantErr: "an imported safe-outputs configuration gates threat detection",
		},
		{
			name:    "imported custom publisher",
			source:  "imports:\n  - shared/outputs.md\n" + reporters,
			shared:  "---\nsafe-outputs:\n  threat-detection:\n    engine: false\n  jobs:\n    publish:\n      description: Publish\n      runs-on: ubuntu-latest\n      steps:\n        - run: echo publish\n---\n",
			wantErr: "an imported safe-outputs configuration disables threat detection",
		},
		{
			name:   "samples feature",
			source: "features:\n  samples: true\nsafe-outputs:\n  add-comment:\n    samples:\n      - body: hi\n  threat-detection: true\n",
		},
		{
			name:       "use-samples flag",
			source:     "safe-outputs:\n  add-comment:\n    samples:\n      - body: hi\n  threat-detection: true\n",
			useSamples: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.shared != "" {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "shared"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "shared", "outputs.md"), []byte(tt.shared), 0o600))
			}
			path := filepath.Join(dir, "test.md")
			require.NoError(t, os.WriteFile(path, []byte(header+tt.source+"---\nTriage the issue.\n"), 0o600))
			compiler := NewCompiler(WithLint(true), WithNoEmit(true))
			compiler.gitRoot = dir
			compiler.SetReadOnly(true)
			compiler.SetSkipValidation(true)
			compiler.SetUseSamples(tt.useSamples)
			err := compiler.CompileWorkflow(path)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "[untrusted-roles-detection]")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
