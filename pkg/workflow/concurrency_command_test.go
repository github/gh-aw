//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestCommandConcurrencyCompilation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		on      string
		mixedPR bool
	}{
		{name: "default command events", on: "  slash_command: test-bot"},
		{name: "PR comment alias", on: "  slash_command:\n    name: test-bot\n    events: [pull_request_comment]"},
		{name: "PR review comment", on: "  slash_command:\n    name: test-bot\n    events: [pull_request_review_comment]"},
		{name: "PR review", on: "  slash_command: test-bot\n  pull_request_review:\n    types: [submitted]"},
		{name: "PR target", on: "  slash_command: test-bot\n  pull_request_target:\n    types: [opened]"},
		{name: "centralized command", on: "  slash_command:\n    name: test-bot\n    strategy: centralized"},
		{name: "centralized command and PR", on: "  slash_command:\n    name: test-bot\n    strategy: centralized\n  pull_request:\n    types: [opened, synchronize]", mixedPR: true},
		{name: "label command only", on: "  label_command: test-bot"},
		{name: "label command and PR", on: "  label_command: test-bot\n  pull_request:\n    types: [labeled, unlabeled]", mixedPR: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(testutil.TempDir(t, "command-concurrency"), "workflow.md")
			content := "---\non:\n" + tt.on + "\nengine: copilot\ncheckout: false\n---\nTest command concurrency.\n"
			require.NoError(t, os.WriteFile(path, []byte(content), 0644))
			require.NoError(t, NewCompiler().CompileWorkflow(path))
			compiled, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			require.NoError(t, NewCompiler().validateGitHubActionsSchema(string(compiled)))
			var workflow struct {
				Concurrency struct {
					Group  string `yaml:"group"`
					Cancel any    `yaml:"cancel-in-progress"`
					Queue  string `yaml:"queue"`
				} `yaml:"concurrency"`
			}

			require.NoError(t, yaml.Unmarshal(compiled, &workflow))
			if tt.mixedPR {
				require.Equal(t, "${{ github.event_name == 'pull_request' }}", workflow.Concurrency.Cancel)
				require.Contains(t, workflow.Concurrency.Group, "${{ github.event_name == 'pull_request' && 'pull_request' || 'command' }}")
				require.Empty(t, workflow.Concurrency.Queue)
			} else {
				require.Nil(t, workflow.Concurrency.Cancel)
				require.Equal(t, "max", workflow.Concurrency.Queue)
				require.NotContains(t, workflow.Concurrency.Group, "github.event_name")
			}
		})
	}
}

func TestConcurrencyQueueSchemaValidation(t *testing.T) {
	for _, queue := range []string{"single", "max", "${{ github.event_name == 'pull_request' && 'single' || 'max' }}", "invalid"} {
		content := "on: workflow_dispatch\nconcurrency:\n  group: test\n  queue: " + queue + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"
		err := NewCompiler().validateGitHubActionsSchema(content)
		if queue == "invalid" || strings.HasPrefix(queue, "${{") {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}
