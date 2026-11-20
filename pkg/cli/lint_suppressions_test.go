//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintSuppressionCLI(t *testing.T) {
	for _, extension := range []string{".yml", ".md"} {
		for _, invalid := range []bool{false, true} {
			t.Run(extension+"/"+strconv.FormatBool(invalid), func(t *testing.T) {
				rule := "template-injection"
				if invalid {
					rule = "typo-rule"
				}
				source := "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n" +
					"      # gh-aw: ignore[" + rule + "] -- Reviewed false positive\n" +
					"      - run: echo '${{ github.event.comment.body }}'\n" +
					"      - run: echo '${{ github.event.issue.title }}'\n"
				if extension == ".md" {
					source = "---\n" + source + "---\n\nPrompt.\n"
				}
				file := filepath.Join(t.TempDir(), "workflow"+extension)
				require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
				command := NewLintCommand()
				var output bytes.Buffer
				command.SetOut(&output)
				command.SetErr(&output)
				command.SetArgs([]string{"--rules=template-injection", "--json", file})
				err := command.Execute()
				var diagnostics []workflowLintDiagnostic
				require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
				if invalid {
					require.ErrorContains(t, err, "invalid lint suppression")
					assert.Len(t, diagnostics, 2)
				} else {
					require.ErrorContains(t, err, "workflow lint found 1 error")
					require.Len(t, diagnostics, 1)
					assert.Equal(t, "jobs.check.steps[1].run", diagnostics[0].Path)
				}
			})
		}
	}
}
