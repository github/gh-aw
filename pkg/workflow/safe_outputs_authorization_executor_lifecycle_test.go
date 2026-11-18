//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func authorizationExecutorSteps(t *testing.T, steps []string) []map[string]any {
	t.Helper()
	var parsed []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(steps, "")), &parsed))
	return parsed
}

func authorizationExecutorStepIndex(t *testing.T, steps []map[string]any, field, value string) int {
	t.Helper()
	for index, step := range steps {
		if step[field] == value {
			return index
		}
	}
	t.Fatalf("missing step with %s=%s", field, value)
	return -1
}

func TestAuthorizationCoverageSetupPreservesStagedReport(t *testing.T) {
	for _, mode := range []string{"unprotected", "live", "staged"} {
		t.Run(mode, func(t *testing.T) {
			coverage := map[string]any{}
			if mode != "unprotected" {
				coverage["authorization"] = authorizationTestAuthority("https://authority.example/a")
			}
			if mode == "staged" {
				coverage["staged"] = true
			}
			config := authorizationTestConfig(t, map[string]any{
				"create-issue": false, "upload-code-coverage": coverage,
			})
			compiler := NewCompiler()
			data := &WorkflowData{SafeOutputs: config}
			job, err := compiler.buildUploadCodeCoverageJob(data, "agent")
			require.NoError(t, err)
			steps := authorizationExecutorSteps(t, job.Steps)
			download := authorizationExecutorStepIndex(t, steps, "id", "download_upload_code_coverage_staging")
			verify := authorizationExecutorStepIndex(t, steps, "name", "Verify code coverage report")
			upload := authorizationExecutorStepIndex(t, steps, "id", "upload_code_coverage")
			require.Less(t, download, verify)
			if mode == "unprotected" {
				require.Equal(t, verify+1, upload)
				require.NotContains(t, strings.Join(job.Steps, ""), "authorize_upload_code_coverage")
				return
			}
			setup := authorizationExecutorSteps(t, compiler.buildSafeOutputsSetupSteps(data))
			require.NotEmpty(t, setup)
			require.Equal(t, setup, steps[:len(setup)])
			require.LessOrEqual(t, len(setup), download)
			gate := authorizationExecutorStepIndex(t, steps, "id", "authorize_upload_code_coverage")
			require.Equal(t, verify+1, gate)
			require.Equal(t, gate+1, upload, "authorization must stay at the publication boundary")
			require.Equal(t, "steps.authorize_upload_code_coverage.outputs.authorized == 'true'", steps[upload]["if"])
		})
	}
}

func TestAuthorizationGateInitializesSharedGlobals(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"create-issue": map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
	})
	steps, err := NewCompiler().buildAuthorizationGateStep(
		&WorkflowData{SafeOutputs: config}, "create_issue", "job", nil, nil, authorizationGateOptions{},
	)
	require.NoError(t, err)
	parsed := authorizationExecutorSteps(t, steps)
	require.Len(t, parsed, 1)
	script := parsed[0]["with"].(map[string]any)["script"].(string)
	require.Contains(t, script, "require(`${process.env.RUNNER_TEMP}/gh-aw/actions/setup_globals.cjs`)")
	require.Contains(t, script, "setupGlobals(core, github, context, exec, io, getOctokit);")
	require.Less(t, strings.Index(script, "setupGlobals(core,"), strings.Index(script, "safe_output_authorization_gate.cjs"))
}

func TestAuthorizationReusableLaunchDependenciesAndEvidence(t *testing.T) {
	config := authorizationTestConfig(t, map[string]any{
		"call-workflow": map[string]any{
			"workflows":     []any{"worker", "backup"},
			"authorization": authorizationTestAuthority("https://authority.example/a"),
		},
	})
	data := &WorkflowData{SafeOutputs: config, On: "\"on\":\n  workflow_call:\n"}
	require.NoError(t, resolveWorkflowSafeOutputAuthorizations(data))
	compiler := NewCompiler()
	_, err := compiler.buildCallWorkflowJobs(data, "")
	require.NoError(t, err)
	artifactNames := map[string]bool{}
	for _, workflow := range []string{"worker", "backup"} {
		job, exists := compiler.jobManager.GetJob("authorize-call-" + workflow)
		require.True(t, exists)
		require.Equal(t, []string{"safe_outputs", "activation", "agent"}, job.Needs)
		steps := authorizationExecutorSteps(t, job.Steps)
		download := authorizationExecutorStepIndex(t, steps, "id", "download-agent-output")
		require.Contains(t, steps[download]["with"].(map[string]any)["pattern"], "${{ needs.agent.outputs.artifact_prefix }}agent")
		gate := authorizationExecutorStepIndex(t, steps, "id", "authorize_call_workflow")
		upload := authorizationExecutorStepIndex(t, steps, "name", "Upload safe-output authorization evidence")
		require.Equal(t, gate+1, upload)
		require.Equal(t, "always()", steps[upload]["if"], "denied and failed decisions must also be retained")
		with := steps[upload]["with"].(map[string]any)
		require.Equal(t, "${{ runner.temp }}/gh-aw/safe-output-authorization.jsonl", with["path"])
		name := with["name"].(string)
		require.Contains(t, name, "${{ needs.agent.outputs.artifact_prefix }}")
		require.False(t, artifactNames[name], "each launch must upload a distinct artifact")
		artifactNames[name] = true
	}
}

func TestAuthorizationManifestCollectsEvidenceWithoutChangingLayout(t *testing.T) {
	for _, claims := range []bool{false, true} {
		for _, includeItems := range []bool{false, true} {
			config := authorizationTestConfig(t, map[string]any{
				"create-issue": map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")},
			})
			steps := authorizationExecutorSteps(t, buildSafeOutputManifestUploadStep(
				"", NewCompiler().getActionPin, claims, &WorkflowData{SafeOutputs: config}, includeItems,
			))
			collect := authorizationExecutorStepIndex(t, steps, "name", "Collect safe-output authorization evidence")
			upload := authorizationExecutorStepIndex(t, steps, "name", "Upload Safe Outputs Items")
			require.Equal(t, collect+1, upload)
			require.Equal(t, "always()", steps[collect]["if"])
			script := steps[collect]["with"].(map[string]any)["script"].(string)
			require.Contains(t, script, "const source = authorizationEvidencePath();")
			require.Contains(t, script, "if (fs.existsSync(source))")
			require.Contains(t, script, "fs.copyFileSync(source, '/tmp/gh-aw/safe-output-authorization.jsonl')")
			paths := steps[upload]["with"].(map[string]any)["path"].(string)
			require.Contains(t, paths, "/tmp/gh-aw/safe-output-authorization.jsonl\n")
			require.NotContains(t, paths, "runner.temp")
			require.NotContains(t, paths, "RUNNER_TEMP")
			for line := range strings.SplitSeq(strings.TrimSpace(paths), "\n") {
				require.True(t, strings.HasPrefix(line, "/tmp/gh-aw/"), "artifact roots must not change")
			}
			require.Equal(t, claims, strings.Contains(paths, "/claims/*/safe-output-authorization.jsonl"))
		}
	}
}
