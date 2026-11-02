//go:build !integration

package workflow

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const gitLabFixture = "../connectors/testdata/providers/gitlab/"

func gitLabConsumerSource(t *testing.T, selection string) string {
	t.Helper()
	source, err := os.ReadFile(gitLabFixture + "workflow.md")
	require.NoError(t, err)
	start := strings.Index(string(source), "tools:\n")
	require.NotEqual(t, -1, start)
	end := strings.Index(string(source)[start:], "---\n")
	require.NotEqual(t, -1, end)
	return string(source[:start]) + selection + string(source[start+end:])
}

func parseGitLabConsumer(t *testing.T, source string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "gitlab.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithVersion("v0.0.0-1-gabcdef0"))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	data, err := compiler.ParseWorkflowFile(path)
	_, statErr := os.Stat(filepath.Join(root, ".github", "aw"))
	require.True(t, os.IsNotExist(statErr), "consumer must require only the compiler-embedded declaration")
	return compiler, data, path, err
}

func TestGitLabConnectorIndependentNativeSelections(t *testing.T) {
	for _, test := range []struct {
		name, selection string
		read, write     bool
	}{
		{"import-only", "", false, false},
		{"mr-only", "tools:\n  gitlab: [get-merge-request]\n", true, false},
		{"pipeline-only", "tools:\n  gitlab: [get-pipeline]\n", true, false},
		{"note-only", "safe-outputs:\n  gitlab-add-merge-request-note:\n", false, true},
		{"both", "tools:\n  gitlab: [get-merge-request, get-pipeline]\nsafe-outputs:\n  gitlab-add-merge-request-note:\n", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, data, path, err := parseGitLabConsumer(t, gitLabConsumerSource(t, test.selection))
			require.NoError(t, err)
			if !test.read && !test.write {
				require.Empty(t, data.Connectors)
			} else {
				config := data.Connectors["gitlab"]
				require.NotNil(t, config)
				require.Nil(t, config.Context)
				require.Equal(t, test.read, config.Agent != nil)
				require.Equal(t, test.write, config.SafeOutputs != nil)
				if test.read {
					require.Equal(t, []string{"APPROVED_GITLAB_READER"}, config.Agent.Secrets())
					require.Equal(t, []string{"https://gitlab.com"}, config.Agent.Origins())
					for _, name := range config.Agent.ToolNames() {
						require.True(t, strings.HasPrefix(name, "gitlab_"))
					}
				}
				if test.write {
					require.Equal(t, []string{"APPROVED_GITLAB_WRITER"}, config.SafeOutputs.Secrets())
					require.Equal(t, []string{"add-merge-request-note"}, config.SafeOutputs.SafeOutputNames())
				}
			}
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			var document map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(output), &document))
			jobs := document["jobs"].(map[string]any)
			agent, err := json.Marshal(jobs["agent"])
			require.NoError(t, err)
			effects, err := json.Marshal(jobs["safe_outputs"])
			require.NoError(t, err)
			require.Equal(t, test.read, strings.Contains(string(agent), "${{ secrets.APPROVED_GITLAB_READER }}"))
			require.NotContains(t, string(agent), "${{ secrets.APPROVED_GITLAB_WRITER }}")
			require.Equal(t, test.write, strings.Contains(string(effects), "${{ secrets.APPROVED_GITLAB_WRITER }}"))
			require.NotContains(t, string(effects), "${{ secrets.APPROVED_GITLAB_READER }}")
			require.NotContains(t, output, "${{ secrets.GITLAB_READ_TOKEN }}")
			require.NotContains(t, output, "${{ secrets.GITLAB_NOTE_TOKEN }}")
		})
	}
}

func TestGitLabConnectorNativeConsumerRejections(t *testing.T) {
	for _, selection := range []string{
		"tools:\n  gitlab: [add-merge-request-note]\n",
		"tools:\n  gitlab: [gitlab_get_pipeline]\n",
		"tools:\n  gitlab: [list-projects]\n",
		"safe-outputs:\n  gitlab-get-pipeline:\n",
		"safe-outputs:\n  gitlab-merge-request:\n",
		"safe-outputs:\n  gitlab-retry-pipeline:\n",
		"safe-outputs:\n  gitlab-add-merge-request-note: {target: '*'}\n",
		"context:\n  gitlab: {}\n",
	} {
		t.Run(strings.ReplaceAll(selection, "\n", " "), func(t *testing.T) {
			_, _, _, err := parseGitLabConsumer(t, gitLabConsumerSource(t, selection))
			require.Error(t, err)
		})
	}
	const read = "tools:\n  gitlab: [get-merge-request, get-pipeline]\n"
	for _, test := range []struct{ old, new string }{
		{`projects: ["42"]`, `projects: []`},
		{`merge-requests: ["7"]`, `merge-requests: []`},
		{`pipelines: ["900"]`, `pipelines: []`},
		{`projects: ["42"]`, `projects: ["*"]`},
		{"api-origin: https://gitlab.com", "api-origin: http://gitlab.com"},
		{"api-origin: https://gitlab.com", "api-origin: https://gitlab.com/api/v4"},
		{"api-origin: https://gitlab.com", "api-origin: https://user:password@gitlab.com"},
		{"reader-secret: APPROVED_GITLAB_READER", "reader-secret: '${{ secrets.VALUE }}'"},
	} {
		_, _, _, err := parseGitLabConsumer(t, strings.Replace(gitLabConsumerSource(t, read), test.old, test.new, 1))
		require.Error(t, err, test.new)
	}
	for _, event := range []string{"gitlab-merge-request-opened", "gitlab-pipeline-updated"} {
		source := strings.Replace(gitLabConsumerSource(t, ""), "on: workflow_dispatch", "on: "+event, 1)
		_, _, _, err := parseGitLabConsumer(t, source)
		require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
	}
	source, err := os.ReadFile(gitLabFixture + "workflow-ingress-unavailable.md")
	require.NoError(t, err)
	_, _, _, err = parseGitLabConsumer(t, string(source))
	require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
}

func TestGitLabConnectorEmptyGrantsAndDocumentedExamples(t *testing.T) {
	for _, selection := range []string{"", "tools:\n  gitlab: [get-merge-request]\n", "safe-outputs:\n  gitlab-add-merge-request-note:\n"} {
		source := "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\nimports: ['@builtin:connectors/gitlab.md']\n" + selection + "---\nNo implicit grants.\n"
		_, data, _, err := parseGitLabConsumer(t, source)
		if selection == "" {
			require.NoError(t, err)
			require.Empty(t, data.Connectors)
		} else {
			require.ErrorContains(t, err, "resource allowlist")
		}

	}
	source, err := os.ReadFile("../../.github/aw/connectors/gitlab.md")
	require.NoError(t, err)
	_, body, ok := strings.Cut(string(source), "<!--\n")
	require.True(t, ok)
	body = strings.TrimSuffix(strings.TrimSpace(body), "-->")
	var documentation struct {
		Examples []struct {
			ID     string `xml:"id,attr"`
			YAML   string `xml:"yaml"`
			Prompt string `xml:"prompt"`
		} `xml:"examples>example"`
	}
	require.NoError(t, xml.Unmarshal([]byte(body), &documentation))
	require.Len(t, documentation.Examples, 2)
	for _, example := range documentation.Examples {
		t.Run(example.ID, func(t *testing.T) {
			compiler, data, path, err := parseGitLabConsumer(t, "---\n"+strings.TrimSpace(example.YAML)+"\n---\n"+example.Prompt)
			require.NoError(t, err)
			_, err = compiler.CompileToYAML(data, path)
			require.NoError(t, err)
		})
	}
}

func TestGitLabConnectorExplicitSelfManagedOrigin(t *testing.T) {
	source := strings.Replace(gitLabConsumerSource(t, "tools:\n  gitlab: [get-pipeline]\n"),
		"api-origin: https://gitlab.com", "api-origin: https://gitlab.example.invalid", 1)
	_, data, _, err := parseGitLabConsumer(t, source)
	require.NoError(t, err)
	require.Equal(t, []string{"https://gitlab.example.invalid"}, data.Connectors["gitlab"].Agent.Origins())
}
