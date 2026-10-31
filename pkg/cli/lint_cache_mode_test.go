//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheWriteOverrideCLI(t *testing.T) {
	for _, test := range []struct {
		name, trigger, root, job, want string
	}{
		{"low trust job", "issue_comment", "", "write-only", "jobs.check.cache-mode"},
		{"issues job", "issues", "", "write-only", "jobs.check.cache-mode"},
		{"discussion job", "discussion", "", "write", "jobs.check.cache-mode"},
		{"discussion comment job", "discussion_comment", "", "write-only", "jobs.check.cache-mode"},
		{"mixed issues caller", "{push: {}, issues: {}}", "write", "", "cache-mode"},
		{"inherited workflow", "workflow_run", "write", "", "cache-mode"},
		{"reusable caller grants no writes", "workflow_call", "", "write-only", "jobs.check.cache-mode"},
		{"reusable caller default", "workflow_call", "", "", ""},
		{"reusable caller read", "workflow_call", "", "read", ""},
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

func TestLintCacheModeLiteralsCLI(t *testing.T) {
	for _, scope := range []string{"workflow", "job"} {
		for _, test := range []struct {
			name, mode string
			valid      bool
		}{
			{"absent", "", true},
			{"read", "read", true},
			{"write", "write", true},
			{"write only", "write-only", true},
			{"none", "none", true},
			{"unsupported", "read-only", false},
			{"empty string", `""`, false},
			{"empty scalar", "\n", false},
			{"null", "null", false},
			{"number", "42", false},
			{"boolean", "true", false},
			{"sequence", "[read]", false},
			{"mapping", "{mode: read}", false},
			{"expression", `"${{ 'read' }}"`, false},
			{"leading whitespace", `" read"`, false},
			{"trailing whitespace", `"read "`, false},
			{"uppercase", "READ", false},
		} {
			t.Run(scope+"/"+test.name, func(t *testing.T) {
				source := "on: push\npermissions: {contents: read}\n"
				if scope == "workflow" && test.mode != "" {
					source += "cache-mode: " + test.mode + "\n"
				}
				source += "jobs:\n  check:\n    runs-on: ubuntu-latest\n"
				if scope == "job" && test.mode != "" {
					source += "    cache-mode: " + test.mode + "\n"
				}
				source += "    steps: [{run: echo data}]\n"
				dir := actionlintTestDir(t)
				// Docker actionlint runs as guest, so its non-sensitive fixture must be readable.
				require.NoError(t, os.Chmod(dir, constants.DirPermPublic))
				file := filepath.Join(dir, "cache.yml")
				require.NoError(t, os.WriteFile(file, []byte(source), constants.FilePermPublic))
				for _, args := range [][]string{{file}, {"--rules=cache-poisoning", file}} {
					cmd := NewLintCommand()
					var output bytes.Buffer
					cmd.SetOut(&output)
					cmd.SetErr(&output)
					cmd.SetArgs(args)
					err := cmd.Execute()
					if test.valid {
						require.NoError(t, err, output.String())
					} else {
						require.Error(t, err, output.String())
						if test.mode == "read-only" || test.mode == `""` {
							path := "cache-mode"
							if scope == "job" {
								path = "jobs.check.cache-mode"
							}
							require.ErrorContains(t, err, "cache-poisoning")
							require.ErrorContains(t, err, path)
							require.ErrorContains(t, err, "requires read, write, write-only, or none")
						}
					}
				}
			})
		}
	}
}
