//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmissionOptIn(t *testing.T) {
	for _, repo := range []string{"", "false", "true"} {
		for _, workflow := range []string{"", "false", "true"} {
			t.Run("repo="+repo+"/workflow="+workflow, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755))
				if repo != "" {
					require.NoError(t, os.WriteFile(filepath.Join(root, RepoConfigFileName), []byte(`{"admission":`+repo+`}`), 0o600))
				}
				compiler := NewCompiler(WithNoEmit(true))
				compiler.gitRoot = root
				source := "---\non:\n  workflow_dispatch:\n"
				if workflow != "" {
					source += "  admission: " + workflow + "\n"
				}
				source += "---\nAdmission opt-in\n"
				data, err := compiler.ParseWorkflowString(source, filepath.Join(root, "workflow.md"))
				require.NoError(t, err)
				enabled := repo == "true" || workflow == "true"
				require.Equal(t, enabled, data.AdmissionEnabled)
				output, err := compiler.CompileToYAML(data, filepath.Join(root, "workflow.md"))
				require.NoError(t, err)
				var header strings.Builder
				require.NoError(t, compiler.generateManifestHeader(&header, data, nil, nil))
				manifest, err := ExtractGHAWManifestFromLockFile(header.String())
				require.NoError(t, err)
				require.NotNil(t, manifest)
				assert.Equal(t, enabled, manifest.Admission)
				manifestJSON, err := manifest.ToJSON()
				require.NoError(t, err)
				var manifestFields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(manifestJSON), &manifestFields))
				assert.Equal(t, strconv.FormatBool(enabled), string(manifestFields["admission"]))
				assert.Contains(t, manifestFields, "admission", "disabled admission must be recorded explicitly")
				roundtrip, err := ExtractGHAWManifestFromLockFile("# gh-aw-manifest: " + manifestJSON + "\n")
				require.NoError(t, err)
				assert.Equal(t, manifest, roundtrip)
				for _, name := range []string{"admission", "conclusion_admission"} {
					_, exists := compiler.jobManager.GetJob(name)
					assert.Equal(t, enabled, exists, name)
				}
				activation, exists := compiler.jobManager.GetJob("activation")
				require.True(t, exists)
				conclusion, exists := compiler.jobManager.GetJob("conclusion")
				require.True(t, exists)
				_, admissionOutput := activation.Outputs["admission-result"]
				assert.Equal(t, enabled, admissionOutput)
				_, compactOutput := conclusion.Outputs["admission_compact_session"]
				assert.Equal(t, enabled, compactOutput)
				assert.Equal(t, enabled, strings.Contains(strings.Join(conclusion.Steps, ""), "Prepare admission conclusion metadata"))
				if enabled {
					assert.Contains(t, activation.Needs, "admission")
					assert.Contains(t, activation.If, admissionJobResultCondition)
				} else {
					assert.NotContains(t, output, "needs.admission.")
					assert.NotContains(t, output, "needs.activation.outputs.admission-")
					assert.NotContains(t, activation.Needs, "admission")
					assert.NotContains(t, activation.Permissions, "id-token: write")
					assert.NotContains(t, conclusion.Permissions, "id-token: write")
				}
				var rendered map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(output), &rendered))
				require.IsType(t, map[string]any{}, rendered["on"])
				assert.NotContains(t, rendered["on"].(map[string]any), "admission")
			})
		}
	}
}

func TestAdmissionRejectsInvalidOptIn(t *testing.T) {
	for _, value := range []string{`"true"`, "1", "{}", "[]"} {
		t.Run(value, func(t *testing.T) {
			source := "---\non:\n  workflow_dispatch:\n  admission: " + value + "\n---\nTest\n"
			_, err := NewCompiler().ParseWorkflowString(source, filepath.Join(t.TempDir(), "workflow.md"))
			require.Error(t, err)
			assertInvalidAWJSONWarning(t, `{"admission":`+value+`}`)
		})
	}
}

func TestAdmissionOutputGatingCompilesWithLint(t *testing.T) {
	t.Parallel()
	for _, admission := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			for _, variant := range []string{"admission-only", "detection-and-evals"} {
				t.Run("admission="+strconv.FormatBool(admission)+"/dry-run="+strconv.FormatBool(dryRun)+"/"+variant, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					path := filepath.Join(dir, "admission.md")
					source := "---\non:\n  workflow_dispatch:\n  admission: " + strconv.FormatBool(admission) + "\ntools:\n  github: false\n"
					if variant != "admission-only" {
						source += "safe-outputs:\n  create-issue:\n  threat-detection: true\nevals:\n  - id: completed\n    question: Did the workflow complete?\n"
					}
					source += "---\nAdmission output gating\n"
					require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
					compiler := NewCompiler(WithLint(true))
					compiler.gitRoot = dir
					compiler.SetReadOnly(true)
					compiler.SetSkipValidation(true)
					compiler.SetDryRun(dryRun)
					require.NoError(t, compiler.CompileWorkflow(path))
					require.Zero(t, compiler.GetWarningCount())
					lock, err := os.ReadFile(filepath.Join(dir, "admission.lock.yml"))
					require.NoError(t, err)
					require.Equal(t, admission, strings.Contains(string(lock), "needs.activation.outputs.admission-"))

					for _, name := range []string{"admission", "conclusion_admission"} {
						_, exists := compiler.jobManager.GetJob(name)
						assert.Equal(t, admission, exists, name)
					}
					activation, exists := compiler.jobManager.GetJob("activation")
					require.True(t, exists)
					conclusion, exists := compiler.jobManager.GetJob("conclusion")
					require.True(t, exists)
					assert.Contains(t, conclusion.Needs, "activation")
					conclusionSteps := strings.Join(conclusion.Steps, "")
					for env, output := range map[string]string{
						"GH_AW_ADMISSION_RESULT":                          "admission-result",
						"GH_AW_ADMISSION_MAX_AI_CREDITS":                  "admission-max-ai-credits",
						"GH_AW_ADMISSION_MAX_AI_CREDITS_THREAT_DETECTION": "admission-max-ai-credits-threat-detection",
						"GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS":            "admission-max-ai-credits-evals",
					} {
						if admission {
							assert.Equal(t, "${{ needs.admission.outputs."+strings.TrimPrefix(output, "admission-")+" }}", activation.Outputs[output])
						} else {
							assert.NotContains(t, activation.Outputs, output)
						}
						assert.Equal(t, admission, strings.Contains(conclusionSteps, env+": ${{ needs.activation.outputs."+output+" }}"), env)
					}
					assert.Equal(t, admission, strings.Contains(conclusionSteps, "Prepare admission conclusion metadata"))
					assert.Equal(t, admission, conclusion.Outputs["admission_compact_session"] != "")

					jobs := []string{"agent"}
					if variant != "admission-only" {
						jobs = append(jobs, "detection", "evals")
					}
					for _, name := range jobs {
						job, exists := compiler.jobManager.GetJob(name)
						require.True(t, exists, name)
						steps := strings.Join(job.Steps, "")
						assert.Equal(t, admission && !dryRun, strings.Contains(steps, "GH_AW_ADMISSION_RESULT: ${{ needs.activation.outputs.admission-result }}"), name)
						assert.Equal(t, admission && !dryRun, strings.Contains(steps, "GH_AW_ADMISSION_MAX_AI_CREDITS: ${{ needs.activation.outputs.admission-max-ai-credits"), name)
					}
				})
			}
		}
	}
}

func TestAdmissionRuntimeBudgetSelection(t *testing.T) {
	for _, tc := range []struct {
		name, result, allowance, expected string
		component, invalid                bool
		local                             int64
		evals                             bool
	}{
		{"approved overrides local", "admitted", "111", "111", false, false, 777, false},
		{"approved higher allowance", "admitted", "999", "999", false, false, 777, false},
		{"approved overrides unlimited", "admitted", "111", "111", false, false, -1, false},
		{"disabled retains local", "", "", "777", false, false, 777, false},
		{"disabled retains unlimited", "", "", "-1", false, false, -1, false},
		{"ignored ignores stale allowance", "ignored", "111", "777", false, false, 777, false},
		{"optional absent retains component", "admitted", "", "777", true, false, 777, false},
		{"primary absent blocks", "admitted", "", "", false, true, 777, false},
		{"invalid allowance blocks", "admitted", "12oops", "", false, true, 777, false},
		{"overflow blocks", "admitted", "2147483648", "", false, true, 777, false},
		{"shell payload blocks", "admitted", "$(echo unsafe)", "", false, true, 777, false},
		{"detection selects allowance", "admitted", "111", "111", true, false, 777, false},
		{"evals selects allowance", "admitted", "999", "999", false, false, 777, true},
		{"evals missing optional retains policy", "admitted", "", "777", false, false, 777, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{MaxAICredits: tc.local}, IsDetectionRun: tc.component, IsEvalsRun: tc.evals}
			script, config := buildMaxAICreditsExport(AWFCommandConfig{WorkflowData: data}, `{"apiProxy":{"maxRuns":1,"maxAiCredits":777}}`)
			assert.Equal(t, 1, strings.Count(config, `"maxAiCredits"`))
			assert.Contains(t, config, `"maxAiCredits":${GH_AW_MAX_AI_CREDITS}`)
			resultFile := filepath.Join(t.TempDir(), "budget")
			cmd := exec.Command("bash", "-c", script+"\nprintf '%s' "+buildAWFConfigPrintfArg(config, true)+" > \"$BUDGET_RESULT\"")
			cmd.Env = append(os.Environ(), "GH_AW_ADMISSION_RESULT="+tc.result, "GH_AW_ADMISSION_MAX_AI_CREDITS="+tc.allowance, "GH_AW_MAX_AI_CREDITS=222", "BUDGET_RESULT="+resultFile)
			output, err := cmd.CombinedOutput()
			if tc.invalid {
				require.Error(t, err)
				assert.Contains(t, string(output), "ERR_ADMISSION")
				return
			}
			require.NoError(t, err, "%s", output)
			value, err := os.ReadFile(resultFile)
			require.NoError(t, err)
			assert.JSONEq(t, `{"apiProxy":{"maxRuns":1,"maxAiCredits":`+tc.expected+`}}`, string(value))
			for _, withheld := range []string{"777", "111", "999", "222"} {
				assert.NotContains(t, string(output), withheld)
			}
		})
	}
}

func TestAdmissionComponentBudgetOutputs(t *testing.T) {
	for _, tc := range []struct {
		name, output     string
		detection, evals bool
	}{
		{"agent", "admission-max-ai-credits", false, false},
		{"detection", "admission-max-ai-credits-threat-detection", true, false},
		{"evals", "admission-max-ai-credits-evals", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			applyDefaultMaxAICreditsEnvToMap(env, &WorkflowData{AdmissionEnabled: true, EngineConfig: &EngineConfig{MaxAICredits: 777}, IsDetectionRun: tc.detection, IsEvalsRun: tc.evals})
			assert.Equal(t, "${{ needs.activation.outputs."+tc.output+" }}", env["GH_AW_ADMISSION_MAX_AI_CREDITS"])
			assert.Equal(t, "${{ needs.activation.outputs.admission-result }}", env["GH_AW_ADMISSION_RESULT"])
			for _, disabled := range []*WorkflowData{
				{EngineConfig: &EngineConfig{MaxAICredits: 777}, IsDetectionRun: tc.detection, IsEvalsRun: tc.evals},
				{AdmissionEnabled: true, DryRun: true, IsDetectionRun: tc.detection, IsEvalsRun: tc.evals},
			} {
				env := map[string]string{}
				applyDefaultMaxAICreditsEnvToMap(env, disabled)
				assert.NotContains(t, env, "GH_AW_ADMISSION_RESULT")
				assert.NotContains(t, env, "GH_AW_ADMISSION_MAX_AI_CREDITS")
			}
		})
	}
}

func TestAdmissionSessionOutputs(t *testing.T) {
	for _, daily := range []bool{false, true} {
		step := strings.Join(buildUsageActivityGenerationStep(true, daily, false, func(action string) string { return action }), "")
		for env, output := range map[string]string{
			"GH_AW_ADMISSION_RESULT":                          "admission-result",
			"GH_AW_ADMISSION_MAX_AI_CREDITS":                  "admission-max-ai-credits",
			"GH_AW_ADMISSION_MAX_AI_CREDITS_THREAT_DETECTION": "admission-max-ai-credits-threat-detection",
			"GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS":            "admission-max-ai-credits-evals",
		} {
			assert.Contains(t, step, env+": ${{ needs.activation.outputs."+output+" }}")
		}
	}
}

func TestAdmissionComponentExecutionWiring(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "pi"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(engine+"/dry-run="+strconv.FormatBool(dryRun), func(t *testing.T) {
				data := &WorkflowData{
					AdmissionEnabled: true,
					DryRun:           dryRun,
					AI:               engine,
					EngineConfig:     &EngineConfig{ID: engine, MaxAICredits: 777},
					SafeOutputs:      &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
				}
				compiler := NewCompiler()
				evals := strings.Join(compiler.buildEvalsEngineSteps(data), "")
				assert.Equal(t, !dryRun, strings.Contains(evals, "GH_AW_ADMISSION_MAX_AI_CREDITS: ${{ needs.activation.outputs.admission-max-ai-credits-evals }}"))
				assert.Equal(t, !dryRun, strings.Contains(evals, "GH_AW_ADMISSION_RESULT: ${{ needs.activation.outputs.admission-result }}"))
				assert.NotContains(t, evals, "needs.activation.outputs.admission-max-ai-credits-threat-detection")
				assert.Contains(t, evals, `GH_AW_MAX_AI_CREDITS="$GH_AW_ADMISSION_MAX_AI_CREDITS"`)
				for _, external := range []bool{false, true} {
					data.Features = map[string]any{string(constants.GHAWDetectionFeatureFlag): external}
					detection := strings.Join(compiler.buildDetectionJobSteps(data), "")
					assert.Equal(t, !dryRun, strings.Contains(detection, "GH_AW_ADMISSION_MAX_AI_CREDITS: ${{ needs.activation.outputs.admission-max-ai-credits-threat-detection }}"))
					assert.Equal(t, !dryRun, strings.Contains(detection, "GH_AW_ADMISSION_RESULT: ${{ needs.activation.outputs.admission-result }}"))
					assert.NotContains(t, detection, "needs.activation.outputs.admission-max-ai-credits-evals")
					assert.Contains(t, detection, `GH_AW_MAX_AI_CREDITS="$GH_AW_ADMISSION_MAX_AI_CREDITS"`)
				}
			})
		}
	}
}

func TestAdmissionRuntimeWiring(t *testing.T) {
	t.Setenv("GH_AW_DEFAULT_ADMISSION_URL", "https://compiler-process.invalid/api/admission")
	t.Setenv("GH_AW_ADMISSION_WORKFLOW_HASH", "compiler-process-hash")
	dir := testutil.TempDir(t, "admission-runtime-*")
	file := filepath.Join(dir, "admission.md")
	require.NoError(t, os.WriteFile(file, []byte("---\non:\n  workflow_dispatch:\n  admission: true\n  stale-check: false\nsafe-outputs:\n  add-comment:\n    max: 1\n---\nAdmission runtime\n"), 0o644))
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(file))
	content, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
	require.NoError(t, err)
	yaml := string(content)
	for _, fragment := range []string{
		"id: admission", "id-token: write", "activation_admission.cjs",
		"vars.GH_AW_DEFAULT_ADMISSION_URL != ''",
		"GH_AW_DEFAULT_ADMISSION_URL: ${{ vars.GH_AW_DEFAULT_ADMISSION_URL }}",
		"GH_AW_DEFAULT_ADMISSION_AUDIENCE: ${{ vars.GH_AW_DEFAULT_ADMISSION_AUDIENCE }}",
		"GH_AW_DEFAULT_ADMISSION_ON_OUTAGE: ${{ vars.GH_AW_DEFAULT_ADMISSION_ON_OUTAGE }}",
		"GH_AW_INFO_WORKFLOW_ID: ${{ job.workflow_ref }}",
		"GITHUB_ACTOR: ${{ github.actor }}", "GITHUB_TRIGGERING_ACTOR: ${{ github.triggering_actor }}",
	} {
		assert.Contains(t, yaml, fragment)
	}

	assert.NotContains(t, yaml, "compiler-process.invalid")
	assert.NotContains(t, yaml, "compiler-process-hash")
	hashPattern := regexp.MustCompile(`GH_AW_ADMISSION_WORKFLOW_HASH: "([0-9a-f]{64}):([0-9a-f]{64})"`)
	hashes := hashPattern.FindStringSubmatch(yaml)
	require.Len(t, hashes, 3)
	assert.Contains(t, yaml, `"frontmatter_hash":"`+hashes[1]+`"`)
	assert.Contains(t, yaml, `"body_hash":"`+hashes[2]+`"`)
	assert.Contains(t, yaml, strings.Join([]string{
		"            await require(require('node:path').join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'activation_admission.cjs')).main(core, github, context, exec, io, getOctokit);",
	}, "\n"))
	assert.Contains(t, yaml, admissionDailyAICFallbackIfExpr)
	admission, exists := compiler.jobManager.GetJob("admission")
	require.True(t, exists)
	assert.Contains(t, admission.If, "vars.GH_AW_DEFAULT_ADMISSION_URL != ''")
	assert.Contains(t, admission.Permissions, "id-token: write")
	assert.NotContains(t, strings.Join(admission.Steps, ""), "create-github-app-token")
	assert.NotContains(t, strings.Join(admission.Steps, ""), "daily-ai-credits-workflow-guardrail")
	assert.NotContains(t, admission.If, "MAX_DAILY_AI_CREDITS")
	activation, exists := compiler.jobManager.GetJob("activation")
	require.True(t, exists)
	assert.Contains(t, activation.Needs, "admission")
	assert.Contains(t, activation.If, "!cancelled()")
	assert.Contains(t, activation.If, admissionJobResultCondition)
	assert.NotContains(t, activation.Permissions, "id-token: write")
	assert.Equal(t, "${{ needs.admission.outputs.result }}", activation.Env["GH_AW_ADMISSION_RESULT"])
	assert.NotContains(t, strings.Join(activation.Steps, ""), "activation_admission.cjs")
	assert.Contains(t, strings.Join(activation.Steps, ""), "daily-ai-credits-workflow-guardrail")
	conclusion, exists := compiler.jobManager.GetJob("conclusion")
	require.True(t, exists)
	assert.NotContains(t, conclusion.Permissions, "id-token: write")
	conclusionSteps := strings.Join(conclusion.Steps, "")
	assert.NotContains(t, conclusionSteps, "conclusion_admission.cjs")
	assert.Contains(t, conclusionSteps, "Prepare admission conclusion metadata")
	assert.Contains(t, conclusionSteps, "readCompactUnifiedSession('/tmp/gh-aw/usage/aw_session.jsonl')")
	assert.NotContains(t, conclusionSteps, "path.join(process.env.RUNNER_TEMP, 'gh-aw', 'usage'")
	assert.Equal(t, "${{ steps.admission-conclusion-metadata.outputs.compact_session }}", conclusion.Outputs["admission_compact_session"])
	assert.Equal(t, "${{ steps.upload-usage-artifact.outputs.artifact-id }}", conclusion.Outputs["usage_artifact_id"])
	assert.Equal(t, "${{ steps.upload-usage-artifact-retry.outputs.artifact-id }}", conclusion.Outputs["usage_artifact_retry_id"])
	assert.Less(t, strings.Index(conclusionSteps, "Generate usage activity summary and unified session"), strings.Index(conclusionSteps, "Prepare admission conclusion metadata"))
	assert.Less(t, strings.Index(conclusionSteps, "Prepare admission conclusion metadata"), strings.Index(conclusionSteps, "Upload usage artifact"))
	assert.Greater(t, strings.Index(conclusionSteps, "Retry upload usage artifact"), strings.Index(conclusionSteps, "Upload usage artifact"))
	conclusionAdmission, exists := compiler.jobManager.GetJob(string(constants.ConclusionAdmissionJobName))
	require.True(t, exists)
	assert.Equal(t, []string{"conclusion"}, conclusionAdmission.Needs)
	assert.Equal(t, "always() && needs.conclusion.result != 'skipped' && vars.GH_AW_DEFAULT_ADMISSION_URL != ''", conclusionAdmission.If)
	assert.Contains(t, conclusionAdmission.Permissions, "id-token: write")
	assert.Contains(t, conclusionAdmission.Permissions, "contents: read")
	conclusionAdmissionSteps := strings.Join(conclusionAdmission.Steps, "")
	assert.Contains(t, conclusionAdmissionSteps, "conclusion_admission.cjs")
	assert.Contains(t, conclusionAdmissionSteps, "GH_AW_ADMISSION_COMPACT_SESSION: ${{ needs.conclusion.outputs.admission_compact_session }}")
	assert.Contains(t, conclusionAdmissionSteps, "GH_AW_USAGE_ARTIFACT_ID: ${{ needs.conclusion.outputs.usage_artifact_id }}")
	assert.Contains(t, conclusionAdmissionSteps, "GH_AW_USAGE_ARTIFACT_RETRY_ID: ${{ needs.conclusion.outputs.usage_artifact_retry_id }}")
	assert.Contains(t, conclusionAdmissionSteps, "GH_AW_ADMISSION_WORKFLOW_HASH: \""+hashes[1]+":"+hashes[2]+"\"")
	assert.NotContains(t, conclusionAdmissionSteps, "safe_outputs")
	for name, job := range compiler.jobManager.jobs {
		if name == "admission" {
			continue
		}
		if name == "activation" {
			assert.Equal(t, "${{ needs.admission.outputs.max-ai-credits }}", job.Outputs["admission-max-ai-credits"])
		}
		encoded, err := json.Marshal(job)
		require.NoError(t, err)
		for _, fragment := range []string{"steps.admission.outputs", "admission-aic", "admission_budget.cjs"} {
			assert.NotContains(t, string(encoded), fragment, "job %s", name)
		}
		if name != string(constants.ConclusionAdmissionJobName) {
			assert.NotContains(t, string(encoded), "GH_AW_ADMISSION_WORKFLOW_HASH", "job %s", name)
		}
	}

	require.NoError(t, os.WriteFile(file, []byte("---\non:\n  workflow_dispatch:\n  admission: true\n  stale-check: false\nsafe-outputs:\n  add-comment:\n    max: 1\n---\nChanged admission prompt\n"), 0o644))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	changed, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
	require.NoError(t, err)
	changedHashes := hashPattern.FindStringSubmatch(string(changed))
	require.Len(t, changedHashes, 3)
	assert.Equal(t, hashes[1], changedHashes[1])
	assert.NotEqual(t, hashes[2], changedHashes[2], "Prompt changes must invalidate workflow-content caches")
}

func TestAdmissionConclusionWorkQueueParticipant(t *testing.T) {
	data := &WorkflowData{
		AdmissionEnabled: true,
		Tools:            map[string]any{"work-queue": map[string]any{"worker": true}},
		SafeOutputs:      &SafeOutputsConfig{},
	}
	job, err := NewCompiler().buildConclusionJob(data, "agent", []string{"safe_outputs"})
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.NotContains(t, job.Permissions, "id-token: write")
	steps := strings.Join(job.Steps, "")
	for _, name := range []string{"Generate usage activity summary and unified session", "Prepare admission conclusion metadata", "Upload usage artifact"} {
		assert.Equal(t, 1, strings.Count(steps, "name: "+name+"\n"))
	}
	assert.NotContains(t, steps, "Report admission conclusion")
	assert.Less(t, strings.Index(steps, "reconcile_work_queue_run.cjs"), strings.Index(steps, "Generate usage activity summary and unified session"))
	assert.Less(t, strings.Index(steps, "Generate usage activity summary and unified session"), strings.Index(steps, "Prepare admission conclusion metadata"))
	assert.Greater(t, strings.Index(steps, "Retry upload usage artifact"), strings.Index(steps, "Upload usage artifact"))
}

func TestAdmissionConclusionUploadOrdering(t *testing.T) {
	for _, worker := range []bool{false, true} {
		data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}
		if worker {
			data.Tools = map[string]any{"work-queue": map[string]any{"worker": true}}
		}
		job, err := NewCompiler().buildConclusionJob(data, "agent", nil)
		require.NoError(t, err)
		require.NotNil(t, job)
		steps := strings.Join(job.Steps, "")
		previous := -1
		for _, name := range []string{
			"Generate usage activity summary and unified session",
			"Upload usage artifact",
			"Wait before retrying usage artifact upload",
			"Retry upload usage artifact",
		} {
			index := strings.Index(steps, "      - name: "+name+"\n")
			require.Greater(t, index, previous, "%s must follow preceding publication steps", name)
			previous = index
		}
		assert.NotContains(t, steps, "Report admission conclusion")
	}
}

func TestConclusionAdmissionRequiresValidWorkflowHashes(t *testing.T) {
	for _, tc := range []struct {
		name            string
		frontmatterHash string
		bodyHash        string
	}{
		{name: "missing frontmatter hash", bodyHash: strings.Repeat("b", 64)},
		{name: "short body hash", frontmatterHash: strings.Repeat("a", 64), bodyHash: strings.Repeat("b", 63)},
		{name: "uppercase hash", frontmatterHash: strings.Repeat("A", 64), bodyHash: strings.Repeat("b", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCompiler().buildConclusionAdmissionJob(&WorkflowData{
				Name:            "test",
				FrontmatterHash: tc.frontmatterHash,
				BodyHash:        tc.bodyHash,
			})
			require.EqualError(t, err, "admission requires 64-character lowercase SHA-256 workflow hashes")
		})
	}
}

func TestAdmissionActivationDependencyConditions(t *testing.T) {
	for _, condition := range []string{"", "github.event_name == 'push'", "always()"} {
		t.Run(condition, func(t *testing.T) {
			compiler := NewCompiler()
			activation := &Job{If: condition, Needs: []string{"pre_activation", "select"}}
			admission, err := compiler.buildAdmissionJob(&WorkflowData{
				Name:            "test",
				FrontmatterHash: strings.Repeat("a", 64),
				BodyHash:        strings.Repeat("b", 64),
			}, activation)
			require.NoError(t, err)
			assert.Equal(t, []string{"pre_activation", "select"}, admission.Needs)
			assert.Contains(t, admission.If, "vars.GH_AW_DEFAULT_ADMISSION_URL != ''")
			compiler.connectActivationAdmission(activation)
			assert.Equal(t, []string{"pre_activation", "select", "admission"}, activation.Needs)
			assert.Contains(t, activation.If, "!cancelled()")
			assert.Contains(t, activation.If, admissionJobResultCondition)
			if condition != "always()" {
				assert.Contains(t, activation.If, "needs.pre_activation.result == 'success'")
				assert.Contains(t, activation.If, "needs.select.result == 'success'")
			}
		})
	}
}

func TestAdmissionJobCannotBeCustomized(t *testing.T) {
	for _, name := range []string{"admission", "conclusion_admission"} {
		for _, config := range []map[string]any{
			{}, {"if": "always()"}, {"needs": "agent"}, {"env": map[string]any{"UNTRUSTED": "value"}},
			{"permissions": map[string]any{"contents": "write"}}, {"continue-on-error": true},
			{"setup-steps": []any{map[string]any{"run": "echo unsafe"}}},
			{"pre-steps": []any{map[string]any{"run": "echo unsafe"}}},
			{"steps": []any{map[string]any{"run": "echo unsafe"}}},
		} {
			data := &WorkflowData{Jobs: map[string]any{name: config}}
			require.ErrorContains(t, NewCompiler().applyBuiltinJobPreSteps(data), "jobs."+name+" is compiler-owned")
			require.ErrorContains(t, NewCompiler().applyBuiltinJobAugmentations(data), "jobs."+name+" is compiler-owned")
		}
	}

}

func TestAdmissionSkipSafeDownstreamConditions(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager.jobs = map[string]*Job{
		"admission":  {Name: "admission", If: "vars.GH_AW_DEFAULT_ADMISSION_URL != ''"},
		"activation": {Name: "activation", Needs: []string{"admission"}},
		"agent":      {Name: "agent", Needs: []string{"activation", "custom"}, If: "needs.activation.outputs.daily_ai_credits_exceeded != 'true'"},
		"custom":     {Name: "custom"},
		"detection":  {Name: "detection", Needs: []string{"agent"}, If: "needs.agent.outputs.output != ''"},
		"conclusion": {Name: "conclusion", Needs: []string{"agent"}, If: "always()"},
	}
	compiler.ensureAdmissionSkipSafeConditions()
	assert.Equal(t, "vars.GH_AW_DEFAULT_ADMISSION_URL != ''", compiler.jobManager.jobs["admission"].If)
	assert.Contains(t, compiler.jobManager.jobs["activation"].If, admissionJobResultCondition)
	assert.NotContains(t, compiler.jobManager.jobs["activation"].If, "&& needs.admission.result == 'success'")
	for _, name := range []string{"activation", "agent", "detection"} {
		assert.Contains(t, compiler.jobManager.jobs[name].If, "!cancelled()")
	}
	agentCondition := compiler.jobManager.jobs["agent"].If
	assert.Contains(t, agentCondition, "needs.activation.result == 'success'")
	assert.Contains(t, agentCondition, "needs.custom.result == 'success'")
	assert.Contains(t, agentCondition, "needs.activation.outputs.daily_ai_credits_exceeded != 'true'")
	assert.Contains(t, compiler.jobManager.jobs["detection"].If, "needs.agent.result == 'success'")
	assert.Empty(t, compiler.jobManager.jobs["custom"].If)
	assert.Equal(t, "always()", compiler.jobManager.jobs["conclusion"].If)
	compiler.ensureAdmissionSkipSafeConditions()
	assert.Equal(t, agentCondition, compiler.jobManager.jobs["agent"].If)
}

func TestAdmissionRejectsFrontmatterHooks(t *testing.T) {
	for _, name := range []string{"admission", "conclusion_admission"} {
		for _, hook := range []string{"setup-steps", "pre-steps", "steps"} {
			t.Run(name+"/"+hook, func(t *testing.T) {
				file := filepath.Join(t.TempDir(), "workflow.md")
				source := "---\non: workflow_dispatch\njobs:\n  " + name + ":\n    " + hook + ":\n      - run: echo author-hook\n---\nTest\n"
				require.NoError(t, os.WriteFile(file, []byte(source), 0o644))
				require.ErrorContains(t, NewCompiler().CompileWorkflow(file), "jobs."+name+" is compiler-owned")
			})
		}
	}
}

func TestAdmissionRejectsCodeGenerationSwitch(t *testing.T) {
	file := filepath.Join(testutil.TempDir(t, "admission-unsupported-gate-*"), "workflow.md")
	for _, value := range []string{"false", "true"} {
		t.Run(value, func(t *testing.T) {
			source := "---\non: workflow_dispatch\nadmission-gate: " + value + "\n---\nTest workflow\n"
			require.NoError(t, os.WriteFile(file, []byte(source), 0o644))
			err := NewCompiler().CompileWorkflow(file)
			require.ErrorContains(t, err, "admission-gate", "only on.admission is a supported workflow opt-in")
		})
	}
}

func TestAdmissionRepositoryReusableCallerPermissions(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/*.yml")
	require.NoError(t, err)
	callers := 0
	for _, file := range files {
		if strings.HasSuffix(file, ".lock.yml") {
			continue
		}
		workflow, err := readWorkflowYAML(file)
		require.NoError(t, err)
		jobs, ok := workflow["jobs"].(map[string]any)
		require.True(t, ok, file)
		for name, value := range jobs {
			job, ok := value.(map[string]any)
			require.True(t, ok, "%s/%s", file, name)
			uses, _ := job["uses"].(string)
			if !strings.HasPrefix(uses, "./.github/workflows/") || !strings.HasSuffix(uses, ".lock.yml") {
				continue
			}
			perms := NewPermissionsParserFromValue(job["permissions"]).ToPermissions()
			level, exists := perms.Get(PermissionIdToken)
			assert.True(t, exists, "%s/%s must permit the callee's admission OIDC", file, name)
			assert.Equal(t, PermissionWrite, level, "%s/%s", file, name)
			callers++
		}
	}
	require.NotZero(t, callers, "must cover the repository's hand-authored reusable callers")
}

func TestAdmissionReusableCallerPermissionsWithStaleWorkerLock(t *testing.T) {
	t.Setenv("GH_AW_WORKFLOWS_DIR", "")
	dir := filepath.Join(testutil.TempDir(t, "admission-reusable-*"), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	worker := filepath.Join(dir, "worker.md")
	require.NoError(t, os.WriteFile(worker, []byte("---\non:\n  workflow_call: {}\n---\nWorker\n"), 0o644))
	for _, staleLock := range []bool{false, true} {
		if staleLock {
			require.NoError(t, os.WriteFile(stringutil.MarkdownToLockFile(worker), []byte("on:\n  workflow_call: {}\njobs:\n  activation:\n    permissions:\n      contents: read\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo old-worker\n"), 0o644))
		}
		imported, err := extractCallWorkflowPermissionImport("worker", filepath.Join(dir, "caller.md"))
		require.NoError(t, err)
		require.NotNil(t, imported)
		assert.Contains(t, imported.permissions.RenderToYAML(), "id-token: write")
	}
}
