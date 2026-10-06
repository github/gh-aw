package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestValidatePostStepsSafeOutputs(t *testing.T) {
	tests := []struct {
		name, script, want string
	}{
		{"gh issue create", `gh issue create --title "test"`, "safe-outputs.create-issue"},
		{"gh issue create in pipeline", `echo title | gh issue create --body-file -`, "safe-outputs.create-issue"},
		{"gh issue create with repo flag", `gh -R owner/repo issue create --title test --body body`, "safe-outputs.create-issue"},
		{"gh api post", `gh api -X POST /repos/octo/repo/issues -f title=test`, "safe-outputs.create-issue"},
		{"gh api implicit post", `gh api repos/octo/repo/issues -f title=test`, "safe-outputs.create-issue"},
		{"gh api input before endpoint", `gh api --input payload.json -X POST repos/octo/repo/issues`, "safe-outputs.create-issue"},
		{"gh api continued command", "gh api repos/octo/repo/issues \\\n  -X POST -f title=test", "safe-outputs.create-issue"},
		{"gh api repository expression", `gh api "repos/${{ github.repository }}/issues" -f title=test`, "safe-outputs.create-issue"},
		{"gh api repository variable", `gh api "repos/$GITHUB_REPOSITORY/issues" -f title=test`, "safe-outputs.create-issue"},
		{"curl post", `curl -X POST https://api.github.com/repos/octo/repo/issues`, "safe-outputs.create-issue"},
		{"curl implicit post", `curl --data '{"title":"test"}' https://api.github.com/repos/octo/repo/issues`, "safe-outputs.create-issue"},
		{"curl json post", `curl --json '{"title":"test"}' https://api.github.com/repos/octo/repo/issues`, "safe-outputs.create-issue"},
		{"curl explicit POST overrides get", `curl -G --data-urlencode state=open -X POST https://api.github.com/repos/octo/repo/issues`, "safe-outputs.create-issue"},
		{"curl continued post", "curl -X POST \\\n  https://api.github.com/repos/octo/repo/issues", "safe-outputs.create-issue"},
		{"octokit create", `await github.rest.issues.create({owner, repo, title})`, "safe-outputs.create-issue"},
		{"github request post", `await github.request('POST /repos/{owner}/{repo}/issues', {owner, repo, title})`, "safe-outputs.create-issue"},
		{"octokit request post", `await octokit.request('POST /repos/{owner}/{repo}/issues', {owner, repo, title})`, "safe-outputs.create-issue"},
		{"agent output", `jq '.items' /tmp/gh-aw/agent_output.json`, "safe outputs"},
		{"agent output env", `cat "$GH_AW_AGENT_OUTPUT"`, "safe outputs"},
		{"safe outputs output file", `cat /tmp/gh-aw/safeoutputs/output.json`, "safe outputs"},
		{"safe outputs MCP storage", `cat "$RUNNER_TEMP/gh-aw/safeoutputs/outputs.jsonl"`, "safe outputs"},
		{"collected safe outputs", `cat /tmp/gh-aw/safeoutputs.jsonl`, "safe outputs"},
		{"safe outputs runtime path", `cat "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}"`, "safe outputs"},
		{"read issues", `gh issue list --limit 10`, ""},
		{"read issues with repo flag", `gh -R owner/repo issue list --limit 10`, ""},
		{"read issues api", `gh api /repos/octo/repo/issues`, ""},
		{"read issues api with fields", `gh api -X GET /repos/octo/repo/issues -f state=open`, ""},
		{"curl GET with data", `curl -G --data-urlencode state=open https://api.github.com/repos/octo/repo/issues`, ""},
		{"curl explicit GET with data", `curl -X GET --data '{"state":"open"}' https://api.github.com/repos/octo/repo/issues`, ""},
		{"curl explicit GET overrides JSON", `curl --json '{"state":"open"}' -X GET https://api.github.com/repos/octo/repo/issues`, ""},
		{"curl POST to unrelated route after read", `gh api /repos/octo/repo/issues; curl -X POST https://example.com/other`, ""},
		{"read on previous line before unrelated curl", "gh api /repos/octo/repo/issues\ncurl -X POST https://example.com/other", ""},
		{"comment on issue", `gh api -X POST /repos/octo/repo/issues/12/comments`, ""},
		{"github request GET", `await github.request('GET /repos/{owner}/{repo}/issues', {owner, repo})`, ""},
		{"github request issue comment", `await octokit.request('POST /repos/{owner}/{repo}/issues/{issue_number}/comments', {owner, repo})`, ""},
		{"unrelated output", `cat /tmp/gh-aw/cache-memory/output.json`, ""},
		{"cleanup", `rm -rf /tmp/temp-report`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePostStepsSafeOutputs([]any{map[string]any{"name": "Example", "run": tt.script}})
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.want)
				require.ErrorContains(t, err, `post-steps step 1 ("Example")`)
			}
		})
	}
}

func TestValidatePostStepsSafeOutputsRejectsArtifactFileInputs(t *testing.T) {
	tests := []struct {
		name string
		path any
		want string
	}{
		{"agent output file", "/tmp/gh-aw/agent_output.json", "safe outputs"},
		{"safe-output root", "/tmp/gh-aw/", "narrower file path"},
		{"safe-output file in array", []any{"/tmp/gh-aw/agent/", "/tmp/gh-aw/safeoutputs.jsonl"}, "safe outputs"},
		{"permitted artifact directory", "/tmp/gh-aw/agent/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := []any{map[string]any{
				"name": "Upload", "uses": "actions/upload-artifact@v4",
				"with": map[string]any{"path": tt.path},
			}}
			err := validatePostStepsSafeOutputs(steps)
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.want)
			}
		})
	}
}

func TestValidatePostStepsSafeOutputsRejectsSafeOutputCachePaths(t *testing.T) {
	steps := []any{map[string]any{
		"name": "Cache", "uses": "actions/cache@v4",
		"with": map[string]any{"path": "/tmp/gh-aw/agent_output.json"},
	}}
	require.ErrorContains(t, validatePostStepsSafeOutputs(steps), "safe outputs")
}

func TestValidatePostStepsSafeOutputsRejectsSecretEnv(t *testing.T) {
	steps := []any{map[string]any{
		"name": "Expose secret", "run": "echo done",
		"env": map[string]any{"TOKEN": "${{ secrets.API_TOKEN }}"},
	}}
	require.ErrorContains(t, validatePostStepsSafeOutputs(steps), "secrets are not allowed in post-steps")
}

func TestValidatePostStepsSafeOutputsScriptInput(t *testing.T) {
	steps := []any{map[string]any{
		"name": "Create issue", "uses": "actions/github-script@v7",
		"with": map[string]any{"script": "await github.rest.issues.create({owner, repo, title})"},
	}}
	require.ErrorContains(t, validatePostStepsSafeOutputs(steps), "safe-outputs.create-issue")
}

func TestProcessAndMergePostStepsRejectsImportedSafeOutputAccess(t *testing.T) {
	imported, err := yaml.Marshal([]any{map[string]any{
		"name": "Read output", "run": "cat /tmp/gh-aw/agent_output.json",
	}})
	require.NoError(t, err)
	c := NewCompiler()
	data := &WorkflowData{}
	err = c.processAndMergePostSteps(
		map[string]any{"post-steps": []any{map[string]any{"name": "Cleanup", "run": "echo done"}}},
		data,
		&parser.ImportsResult{MergedPostSteps: string(imported)},
	)
	require.ErrorContains(t, err, `post-steps step 2 ("Read output")`)
	require.ErrorContains(t, err, "safe-outputs")
	require.Empty(t, data.PostSteps)
}

func TestProcessAndMergePostStepsRejectsMainIssueCreation(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{}
	err := c.processAndMergePostSteps(
		map[string]any{"post-steps": []any{map[string]any{"name": "Publish", "run": "gh issue create --title bug"}}},
		data,
		&parser.ImportsResult{},
	)
	require.ErrorContains(t, err, "safe-outputs.create-issue")
	require.ErrorContains(t, err, "Publish")
}

func TestCompileWorkflowRejectsPostStepSafeOutputAccess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "workflow.md")
	content := `---
on: workflow_dispatch
engine: copilot
post-steps:
  - name: Inspect safe outputs
    run: cat /tmp/gh-aw/agent_output.json
---
# Check results
`
	require.NoError(t, os.WriteFile(file, []byte(content), 0600))
	err := NewCompiler().CompileWorkflow(file)
	require.ErrorContains(t, err, "post-steps step 1")
	require.ErrorContains(t, err, "safe-outputs")
}
