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
	references := []struct {
		name       string
		expression string
	}{
		{"dotted", "${{ secrets.LATE_STEP_SECRET }}"},
		{"lowercase dotted", "${{ secrets.late_step_secret }}"},
		{"mixed case context", "${{ SeCrEtS.Late_Step_Secret }}"},
		{"bracket", "${{ secrets['LATE_STEP_SECRET'] }}"},
		{"lowercase bracket", "${{ secrets['late_step_secret'] }}"},
		{"spaced bracket", "${{ secrets [ 'Late_Step_Secret' ] }}"},
		{"grouped", "${{ (secrets['late_step_secret']) }}"},
		{"grouped index", "${{ secrets[('late_step_secret')] }}"},
		{"fallback", "${{ secrets.late_step_secret || secrets['LATE_STEP_SECRET'] }}"},
		{"YAML quoted fallback", "'[${{ secrets.LATE_STEP_SECRET || ''fallback'' }}]'"},
		{"YAML quoted bracket", "'[${{ secrets[''late_step_secret''] }}]'"},
		{"YAML quoted expression literal", "'${{ ''secrets.FAKE_KEY'' }} ${{ secrets.LATE_STEP_SECRET }}'"},
	}
	for _, job := range []string{"agent", "safe_outputs"} {
		for _, field := range []string{"setup-steps", "pre-steps"} {
			for _, reference := range references {
				t.Run(job+"/"+field+"/"+reference.name, func(t *testing.T) {
					source := fmt.Sprintf("---\non: workflow_dispatch\nengine: copilot\npermissions: {contents: read}\nsafe-outputs:\n  push-to-pull-request-branch:\n  threat-detection: false\njobs:\n  %s:\n    %s:\n      - name: Late credential binding\n        run: echo configured\n        env:\n          TOOL_CREDENTIAL: %s\n---\nPublic prompt.\n", job, field, reference.expression)
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
						assert.Equal(t, 1, strings.Count(step.Env["GH_AW_SECRET_NAMES"], "LATE_STEP_SECRET"))
						assert.Equal(t, "${{ secrets.LATE_STEP_SECRET }}", step.Env["SECRET_LATE_STEP_SECRET"])
						assert.NotContains(t, step.Env, "SECRET_late_step_secret")
						assert.NotContains(t, step.Env, "SECRET_FAKE_KEY")
					}
					assert.Equal(t, 2, found, "both producer and final redaction must receive the late-only secret")
				})
			}
		}
	}
}

func TestProtectedFileDiagnosticsRequireProductionRedactionBindings(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentStep := appendEnvVarLine([]string{"      - name: Agent step", "        env:"},
		"EXTRA", "[${{ secrets.AGENT_STEP_SECRET || 'fallback' }}]")
	require.NoError(t, compiler.jobManager.AddJob(&Job{
		Name:  string(constants.AgentJobName),
		Env:   map[string]string{"OPAQUE": "[${{ secrets[('agent_opaque_secret')] || 'fallback' }}]"},
		Steps: []string{strings.Join(agentStep, "\n") + "\n"},
	}))
	data := &WorkflowData{
		Name: "Protected filename diagnostics",
		Env:  "env:\n  PRIVATE: '[${{ secrets.GLOBAL_OPAQUE_SECRET || ''fallback'' }}]'\n",
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
		Env:         "env:\n  PRIVATE: '[${{ secrets[(''global_opaque_secret'')] || ''fallback'' }}]'\n",
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

func TestProtectedFileDiagnosticsSecretReviewInventory(t *testing.T) {
	for _, value := range []string{
		"'[${{ secrets.RESTRICTED || ''fallback'' }}]'",
		"${{ secrets[('restricted')] }}",
	} {
		t.Run(value, func(t *testing.T) {
			source := "---\non: workflow_dispatch\nengine: copilot\npermissions: {contents: read}\n" +
				"jobs:\n  inventory_only:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo configured\n        env:\n          TOKEN: " + value +
				"\n---\nPublic prompt.\n"
			document, err := aw.Parse(source)
			require.NoError(t, err)
			path := filepath.Join(t.TempDir(), "review.md")
			compiler := NewCompiler(WithNoEmit(true))
			data, err := compiler.ParseWorkflowDocument(document, path)
			require.NoError(t, err)
			content, secrets, _, err := compiler.generateYAML(data, path)
			require.NoError(t, err)
			assert.NotContains(t, content, "SECRET_RESTRICTED:", "inventory must discover the original binding, not a redactor's synthetic reference")
			assert.Contains(t, secrets, "RESTRICTED", "the generated body inventory must include the credential")
			err = EnforceSafeUpdate(SafeUpdateOptions{
				Manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
				SecretNames: secrets,
			})
			require.ErrorContains(t, err, "RESTRICTED")

			compiler = NewCompiler(WithNoEmit(true))
			data, err = compiler.ParseWorkflowDocument(document, path)
			require.NoError(t, err)
			compiler.SetPriorManifests(map[string]*GHAWManifest{
				strings.TrimSuffix(path, ".md") + ".lock.yml": {Version: currentGHAWManifestVersion},
			})
			require.NoError(t, compiler.CompileWorkflowData(data, path))
			assert.Contains(t, strings.Join(compiler.GetSafeUpdateWarnings(), "\n"), "RESTRICTED",
				"the compile lifecycle must surface the newly discovered secret for review")
		})
	}
}
