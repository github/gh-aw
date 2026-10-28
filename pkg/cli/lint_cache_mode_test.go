//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheWriteOverrideCLI(t *testing.T) {
	for _, test := range []struct {
		name, trigger, root, job, want string
	}{
		{"low trust job", "issue_comment", "", "write-only", "jobs.check.cache-mode"},
		{"inherited workflow", "workflow_run", "write", "", "cache-mode"},
		{"capped job", "pull_request_target", "write", "read", ""},
		{"dispatch default", "repository_dispatch", "", "write", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "on: " + test.trigger + "\n"
			if test.root != "" {
				source += "cache-mode: " + test.root + "\n"
			}
			source += "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps: [{run: echo data}]\n"
			if test.job != "" {
				source += "    cache-mode: " + test.job + "\n"
			}
			file := filepath.Join(t.TempDir(), "cache.yml")
			require.NoError(t, os.WriteFile(file, []byte(source), 0600))
			cmd := NewLintCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--rules=cache-poisoning", "--json", file})
			require.NoError(t, cmd.Execute())
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.NewDecoder(&output).Decode(&diagnostics))
			if test.want == "" {
				assert.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, "cache-poisoning", diagnostics[0].RuleID)
				assert.Equal(t, workflowlint.Warning, diagnostics[0].Severity)
				assert.Equal(t, test.want, diagnostics[0].Path)
			}
		})
	}
}
