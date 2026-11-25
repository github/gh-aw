//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgramCheckoutValidation(t *testing.T) {
	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{name: "default checkout"},
		{name: "explicit checkout", extra: "checkout:\n  ref: main\n"},
		{name: "disabled checkout", extra: "checkout: false\n", want: "requires the compiler-managed workflow repository checkout"},
		{name: "contents none", extra: "permissions:\n  contents: none\n", want: "requires the compiler-managed workflow repository checkout"},
		{name: "manual checkout", extra: "steps:\n  - uses: actions/checkout@v4\n", want: "requires the compiler-managed workflow repository checkout"},
		{name: "conditional manual checkout", extra: "steps:\n  - if: false\n    uses: actions/checkout@v4\n", want: "requires the compiler-managed workflow repository checkout"},
		{name: "sparse checkout", extra: "checkout:\n  sparse-checkout: src\n", want: "does not support sparse checkout"},
		{name: "wiki checkout", extra: "checkout:\n  wiki: true\n", want: "not a wiki checkout"},
		{name: "additional checkout root", extra: "checkout:\n  repository: example/other\n  path: .\n", want: "additional checkouts are not supported"},
		{name: "additional checkout overlaps source", extra: "checkout:\n  repository: example/other\n  path: .github\n", want: "additional checkouts are not supported"},
		{name: "additional checkout separate", extra: "checkout:\n  repository: example/other\n  path: target\n", want: "additional checkouts are not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\nstrict: false\n" + tt.extra + "---\n"
			_, err := compileProgramWorkflows(t, map[string]string{"w.md": source, "p.ts": "export default () => {}"}, "w")
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProgramCheckoutRejectsSymlinkTargetOverlap(t *testing.T) {
	dir := t.TempDir()
	workflows := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor", "p.ts"), []byte("export default () => {}"), 0o600))
	require.NoError(t, os.Symlink("../../vendor/p.ts", filepath.Join(workflows, "p.ts")))
	source := "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\nstrict: false\ncheckout:\n  repository: example/other\n  path: vendor\n---\n"
	markdownPath := filepath.Join(workflows, "w.md")
	require.NoError(t, os.WriteFile(markdownPath, []byte(source), 0o600))
	err := NewCompiler().CompileWorkflow(markdownPath)
	require.ErrorContains(t, err, "additional checkout")
}

func TestProgramCheckoutTriggerValidation(t *testing.T) {
	tests := []struct {
		name     string
		trigger  string
		checkout string
		want     string
	}{
		{name: "reusable", trigger: "workflow_call", want: "does not support workflow_call"},
		{name: "mixed triggers", trigger: "\n  workflow_call:\n  workflow_dispatch:", want: "does not support workflow_call"},
		{name: "pull request target", trigger: "pull_request_target", want: "requires the compiler-managed workflow repository checkout"},
		{name: "pull request target explicit checkout", trigger: "pull_request_target", checkout: "checkout:\n  ref: main\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "---\non: " + tt.trigger + "\nengine: none\nprogram: p.ts\nstrict: false\n" + tt.checkout + "---\n"
			_, err := compileProgramWorkflows(t, map[string]string{"w.md": source, "p.ts": "export default () => {}"}, "w")
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProgramCheckoutWarningOnCompile(t *testing.T) {
	source := "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\nstrict: false\ncheckout:\n  ref: ${{ github.head_ref }}\n---\n"
	output := captureStderrOutput(t, func() {
		_, err := compileProgramWorkflows(t, map[string]string{"w.md": source, "p.ts": "export default () => {}"}, "w")
		require.NoError(t, err)
	})
	assert.Contains(t, output, `uses checkout.ref "${{ github.head_ref }}"`)
	assert.Contains(t, output, "Programs, imported")
}
