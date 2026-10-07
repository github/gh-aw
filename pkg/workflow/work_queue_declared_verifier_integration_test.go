//go:build integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueDeclaredVerifierActualCompilation(t *testing.T) {
	directory := filepath.Join(testutil.TempDir(t, "declared-verifier-emission-"), ".github", "workflows")
	require.NoError(t, os.MkdirAll(directory, 0o700))
	filename := filepath.Join(directory, "worker.md")
	source := `---
on: workflow_dispatch
engine: claude
tools:
  work-queue:
    worker: true
safe-outputs:
  claim-adapters:
    custom:
      mode: prepared
      effect-type: github_rest
      target-repo: owner/repo
      verifier-id: CheckCreated.v1
      field-map: {name: check_name}
      expected: {head_sha: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
      request: {method: POST, route: "/repos/{owner}/{repo}/check-runs", permission: checks}
      verifier:
        route: "/repos/{owner}/{repo}/check-runs/{receipt_id}"
        resource-kind: check_run
        fields: {name: name, head_sha: head_sha}
    issue:
      mode: prepared
      effect-type: update_issue
      target-repo: owner/repo
      verifier-id: IssueUpdated.v1
      field-map: {item_number: number, title: title}
  jobs:
    custom:
      steps:
        - run: node prepare.cjs
    issue:
      steps:
        - run: node prepare.cjs
---
Prepare the assigned check.
`
	require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
	compiler := NewCompiler(WithVersion("integration"))
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(filename))
	content, err := os.ReadFile(filepath.Join(directory, "worker.lock.yml"))
	require.NoError(t, err)
	var document struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string            `yaml:"id"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &document))
	var registry struct {
		Adapters map[string]*WorkQueueClaimAdapter `json:"claim_adapters"`
	}
	for _, step := range document.Jobs[string(constants.SafeOutputsJobName)].Steps {
		if step.ID == "process_safe_outputs" {
			require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &registry))
		}
	}
	require.Contains(t, registry.Adapters, "custom")
	require.Equal(t, "CheckCreated.v1", registry.Adapters["custom"].VerifierID)
	require.Equal(t, map[string]string{"name": "check_name"}, registry.Adapters["custom"].FieldMap)
	require.Equal(t, map[string]any{"head_sha": strings.Repeat("a", 40)}, registry.Adapters["custom"].Expected)
	require.Equal(t, "IssueUpdated.v1", registry.Adapters["issue"].VerifierID)
	require.Equal(t, map[string]string{"item_number": "number", "title": "title"}, registry.Adapters["issue"].FieldMap)
}
