//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilePiRejectsInvalidNestedConfiguration(t *testing.T) {
	for _, config := range []string{
		`{"model":{"maxTokenz":32000}}`,
		`{"model":{"api":"unsupported"}}`,
		`{"mcp":{"exposure":"unsupported"}}`,
		`{"mcp":{"toolExposure":{"github":"direct"}}}`,
	} {
		t.Run(config, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "pi-invalid.md")
			markdown := "---\non: workflow_dispatch\nengine:\n  id: pi\n  config: |\n    " + config + "\n---\nReport status.\n"
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			err := NewCompiler(WithVersion("dev")).CompileWorkflow(source)
			require.ErrorContains(t, err, "invalid nested configuration")
			_, statErr := os.Stat(filepath.Join(dir, "pi-invalid.lock.yml"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}
