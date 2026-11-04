//go:build !integration

package workflow

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectedFileDiagnosticsCaptureLateJobStepSecrets(t *testing.T) {
	for _, job := range []string{"agent", "safe_outputs"} {
		for _, field := range []string{"setup-steps", "pre-steps"} {
			t.Run(job+"/"+field, func(t *testing.T) {
				source := fmt.Sprintf("---\non: workflow_dispatch\nengine: copilot\npermissions: {contents: read}\nsafe-outputs:\n  push-to-pull-request-branch:\n  threat-detection: false\njobs:\n  %s:\n    %s:\n      - name: Late credential binding\n        run: echo configured\n        env:\n          TOOL_CREDENTIAL: ${{ secrets.LATE_STEP_SECRET }}\n---\nPublic prompt.\n", job, field)
				document, err := aw.Parse(source)
				require.NoError(t, err)
				compiler := NewCompiler(WithNoEmit(true))
				data, err := compiler.ParseWorkflowDocument(document, filepath.Join(t.TempDir(), "protected.md"))
				require.NoError(t, err)
				content, err := compiler.CompileToYAML(data, "protected.md")
				require.NoError(t, err)
				var workflow struct {
					Jobs map[string]struct {
						Steps []struct {
							ID  string            `yaml:"id"`
							Env map[string]string `yaml:"env"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal([]byte(content), &workflow))
				found := 0
				for _, step := range workflow.Jobs["safe_outputs"].Steps {
					if step.ID != "process_safe_outputs" && step.ID != "redact_secrets" {
						continue
					}
					found++
					assert.Contains(t, step.Env["GH_AW_SECRET_NAMES"], "LATE_STEP_SECRET")
					assert.Equal(t, "${{ secrets.LATE_STEP_SECRET }}", step.Env["SECRET_LATE_STEP_SECRET"])
				}
				assert.Equal(t, 2, found, "both producer and final redaction must receive the late-only secret")
			})
		}
	}
}

func TestProtectedFileDiagnosticsRequireProductionRedactionBindings(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{
		Name:  string(constants.AgentJobName),
		Env:   map[string]string{"OPAQUE": "${{ secrets.AGENT_OPAQUE_SECRET }}"},
		Steps: []string{"      - name: Agent step\n        env:\n          EXTRA: ${{ secrets.AGENT_STEP_SECRET }}\n"},
	}))
	data := &WorkflowData{
		Name: "Protected filename diagnostics",
		Env:  "env:\n  PRIVATE: ${{ secrets.GLOBAL_OPAQUE_SECRET }}\n",
		SafeOutputs: &SafeOutputsConfig{
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			GitHubToken:             "${{ secrets.PUBLICATION_TOKEN }}",
		},
		Tools: map[string]any{},
	}
	steps, err := compiler.buildHandlerManagerStep(data)
	require.NoError(t, err)
	var parsed []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(steps, "")), &parsed))
	var producer map[string]any
	for _, step := range parsed {
		if step["id"] == "process_safe_outputs" {
			producer = step
		}
	}
	require.NotNil(t, producer)
	env := producer["env"].(map[string]any)
	for _, name := range []string{"GLOBAL_OPAQUE_SECRET", "AGENT_OPAQUE_SECRET", "AGENT_STEP_SECRET", "PUBLICATION_TOKEN"} {
		assert.Contains(t, env["GH_AW_SECRET_NAMES"], name)
		assert.Equal(t, "${{ secrets."+name+" }}", env["SECRET_"+name])
	}
	assert.NotContains(t, env["GH_AW_SECRET_NAMES"], "UNREFERENCED_SECRET")
}

func TestProtectedFileDiagnosticsUploadsFailClosedWithoutQueueOrMerge(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		Env:         "env:\n  PRIVATE: ${{ secrets.GLOBAL_OPAQUE_SECRET }}\n",
		SafeOutputs: &SafeOutputsConfig{PushToPullRequestBranch: &PushToPullRequestBranchConfig{}},
	}
	content := strings.Join(compiler.appendFinalSafeOutputSteps(data, []string{"      - name: Process Safe Outputs\n"}, ""), "")
	var steps []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(content), &steps))
	var redaction, upload map[string]any
	for _, step := range steps {
		if step["id"] == "redact_secrets" {
			redaction = step
		}
		if step["name"] == "Upload Safe Outputs Items" {
			upload = step
		}
	}
	require.NotNil(t, redaction)
	require.NotNil(t, upload)
	assert.Equal(t, "${{ secrets.GLOBAL_OPAQUE_SECRET }}", redaction["env"].(map[string]any)["SECRET_GLOBAL_OPAQUE_SECRET"])
	assert.Contains(t, upload["if"], "steps.redact_secrets.outcome == 'success'")
	assert.Contains(t, upload["if"], "steps.validate_recovery_envelopes.outcome == 'success'")
	assert.NotContains(t, content, "/claims/")
	assert.Contains(t, upload["with"].(map[string]any)["path"], "safe-output-outcomes.jsonl")
	assert.Less(t, strings.Index(content, "id: redact_secrets"), strings.Index(content, "name: Upload Safe Outputs Items"))
}
