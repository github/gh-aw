//go:build integration

package workflow

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type ledgerCompilationStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type ledgerCompilationJob struct {
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Needs       any               `yaml:"needs"`
	Concurrency struct {
		Group            string `yaml:"group"`
		Queue            string `yaml:"queue"`
		CancelInProgress bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Steps []ledgerCompilationStep `yaml:"steps"`
}

func TestCommentMemoryLedgerWorkflowCompilationIntegration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ledger   string
		types    map[string]string
		comments bool
		imported bool
		staged   bool
		queue    bool
	}{
		{
			name:   "comment-backed notes",
			ledger: "  ledger:\n    storage: comment-memory\n    type: notes\n",
			types:  map[string]string{"default": "notes"}, comments: true, queue: true,
		},
		{
			name:     "mixed imported Git and comment ledgers",
			ledger:   "  ledger:\n    thread:\n      storage: comment-memory\n      type: notes\n",
			types:    map[string]string{"archive": "counter", "thread": "notes"},
			comments: true, imported: true, queue: true,
		},
		{
			name:   "staged comment ledger with queue disabled",
			ledger: "  ledger:\n    storage: comment-memory\n    type: map\n",
			types:  map[string]string{"default": "map"}, comments: true, staged: true,
		},
		{
			name:   "default Git storage",
			ledger: "  ledger:\n    type: counter\n",
			types:  map[string]string{"default": "counter"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "comment-ledger-compilation-")
			source := "---\non: workflow_dispatch\nengine: copilot\n"
			if tc.imported {
				shared := "---\ntools:\n  ledger:\n    archive:\n      type: counter\n---\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0o600))
				source += "imports:\n  - shared.md\n"
			}
			if tc.staged {
				source += "features:\n  group-concurrency-queue: false\nsafe-outputs:\n  staged: true\n"
			}
			source += "tools:\n"
			if tc.comments {
				source += `  comment-memory:
    target: 42
    target-repo: octo/state
    allowed-repos: [octo/state]
    github-token: ${{ secrets.MEMORY_TOKEN }}
`
			}
			source += tc.ledger + "---\nRead prior ledger state and record findings.\n"
			filename := filepath.Join(dir, "ledger.md")
			require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
			compiler := NewCompiler(WithVersion("integration"))
			compiler.SetApprove(true)
			require.NoError(t, compiler.CompileWorkflow(filename))
			content, err := os.ReadFile(stringutil.MarkdownToLockFile(filename))
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]ledgerCompilationJob `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			for _, name := range []string{"agent", "safe_outputs", "push_ledger_changes"} {
				require.Contains(t, compiled.Jobs, name)
			}
			agent := compiled.Jobs["agent"]
			safeOutputs := compiled.Jobs["safe_outputs"]
			persistence := compiled.Jobs["push_ledger_changes"]
			projectionIndex := ledgerCompilationStepIndex(t, agent, "ledger_projections")
			projection := agent.Steps[projectionIndex]
			push := persistence.Steps[ledgerCompilationStepIndex(t, persistence, "push_ledger_changes")]
			require.Subset(t, persistence.Needs, []string{"agent", "activation", "safe_outputs"})

			encoded := projection.Env["GH_AW_LEDGER_CONFIG_BASE64"]
			require.NotEmpty(t, encoded)
			require.Equal(t, encoded, push.Env["GH_AW_LEDGER_CONFIG_BASE64"])
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			var definitions []map[string]any
			require.NoError(t, json.Unmarshal(decoded, &definitions))
			require.Len(t, definitions, len(tc.types))
			seenTypes := make(map[string]string)
			for _, definition := range definitions {
				name, ok := definition["name"].(string)
				require.True(t, ok)
				require.Contains(t, tc.types, name)
				require.NotContains(t, seenTypes, name)
				kind, ok := definition["type"].(string)
				require.True(t, ok)
				seenTypes[name] = kind
				if tc.comments && name != "archive" {
					require.Equal(t, "comment-memory", definition["storage"])
					require.Empty(t, definition["branch_name"])
				} else {
					require.Contains(t, []any{nil, "git"}, definition["storage"])
					require.Equal(t, "ledgers/"+name, definition["branch_name"])
				}
				require.Contains(t, string(content), "/tmp/gh-aw/ledgers/"+name+"/ledger.db")
			}
			require.Equal(t, tc.types, seenTypes)

			handler := safeOutputs.Steps[ledgerCompilationStepIndex(t, safeOutputs, "process_safe_outputs")]
			var handlers struct {
				LedgerAppend struct {
					Ledgers []struct {
						Name string `json:"name"`
						Type string `json:"type"`
					} `json:"ledgers"`
				} `json:"ledger_append"`
			}
			require.NoError(t, json.Unmarshal([]byte(handler.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &handlers))
			require.Len(t, handlers.LedgerAppend.Ledgers, len(tc.types))
			handlerTypes := make(map[string]string)
			for _, definition := range handlers.LedgerAppend.Ledgers {
				handlerTypes[definition.Name] = definition.Type
			}
			require.Equal(t, tc.types, handlerTypes)
			upload := safeOutputs.Steps[ledgerCompilationStepIndex(t, safeOutputs, "Upload validated ledger transactions")]
			download := persistence.Steps[ledgerCompilationStepIndex(t, persistence, "Download validated ledger transactions")]
			require.NotEmpty(t, upload.With["name"])
			require.Equal(t, upload.With["name"], download.With["name"])
			require.Equal(t, upload.With["path"], push.Env["GH_AW_LEDGER_TRANSACTIONS"])

			wantPermissions := map[string]string{"contents": "write"}
			if tc.comments {
				wantPermissions["issues"] = "write"
				wantPermissions["pull-requests"] = "write"
				if !tc.imported {
					wantPermissions["contents"] = "read"
				}
				require.Contains(t, persistence.Concurrency.Group, "'octo/state', '42'")
				require.False(t, persistence.Concurrency.CancelInProgress)
				if tc.queue {
					require.Equal(t, "max", persistence.Concurrency.Queue)
				} else {
					require.Empty(t, persistence.Concurrency.Queue)
				}
				configIndex := ledgerCompilationStepIndex(t, agent, "Write comment-memory configuration")
				prepareIndex := ledgerCompilationStepIndex(t, agent, "Prepare comment memory files")
				require.Less(t, configIndex, projectionIndex)
				require.Less(t, projectionIndex, prepareIndex)
				config := agent.Steps[configIndex].Env["GH_AW_EARLY_COMMENT_MEMORY_CONFIG"]
				restoredConfig := persistence.Steps[ledgerCompilationStepIndex(t, persistence, "Write comment-memory configuration")]
				require.Equal(t, config, restoredConfig.Env["GH_AW_EARLY_COMMENT_MEMORY_CONFIG"])
				var memoryConfig map[string]map[string]any
				require.NoError(t, json.Unmarshal([]byte(config), &memoryConfig))
				require.Equal(t, "42", memoryConfig["comment_memory"]["target"])
				require.Equal(t, "octo/state", memoryConfig["comment_memory"]["target-repo"])
				require.Equal(t, []any{"octo/state"}, memoryConfig["comment_memory"]["allowed_repos"])
				require.NotContains(t, memoryConfig["comment_memory"], "github-token")
			} else {
				require.Empty(t, persistence.Concurrency.Group)
			}
			require.Equal(t, wantPermissions, persistence.Permissions)
			for _, step := range []ledgerCompilationStep{projection, push} {
				require.Equal(t, "${{ github.token }}", step.Env["GH_TOKEN"])
				require.Contains(t, []string{"", "${{ github.token }}"}, step.With["github-token"])
				if tc.comments {
					require.Equal(t, "${{ secrets.MEMORY_TOKEN }}", step.Env["GH_AW_COMMENT_MEMORY_TOKEN"])
					require.Equal(t, "unknown", step.Env["GH_AW_COMMENT_MEMORY_TOKEN_SOURCE"])
				} else {
					require.NotContains(t, step.Env, "GH_AW_COMMENT_MEMORY_TOKEN")
					require.NotContains(t, step.Env, "GH_AW_COMMENT_MEMORY_TOKEN_SOURCE")
				}
			}
			if tc.staged {
				require.Equal(t, "true", push.Env["GH_AW_SAFE_OUTPUTS_STAGED"])
			} else {
				require.NotContains(t, push.Env, "GH_AW_SAFE_OUTPUTS_STAGED")
			}
			require.NotContains(t, agent.Env, "GH_AW_COMMENT_MEMORY_TOKEN")
		})
	}
}

func ledgerCompilationStepIndex(t *testing.T, job ledgerCompilationJob, name string) int {
	t.Helper()
	var matches []int
	for index, step := range job.Steps {
		if step.ID == name || step.Name == name {
			matches = append(matches, index)
		}
	}
	require.Len(t, matches, 1, "expected exactly one step matching %q", name)
	return matches[0]
}
