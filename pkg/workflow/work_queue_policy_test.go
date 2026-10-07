package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueProposalValidationPreservesTemplatesAndNativePrincipalKeys(t *testing.T) {
	policy := workqueue.DefaultPolicy("${{ github.actor_id }}", "${{ github.repository }}")
	policy.Producers["1"] = policy.Producers["${{ github.actor_id }}"]
	before := policy.Pools["default"]
	validated := bindWorkQueuePolicyValidationTemplates(policy)
	require.Contains(t, policy.Producers, "${{ github.actor_id }}")
	require.Contains(t, policy.Producers, "1")
	require.NotContains(t, validated.Producers, "${{ github.actor_id }}")
	require.Contains(t, validated.Producers, "1")
	require.Contains(t, validated.Producers, "2")
	require.Equal(t, "${{ github.actor_id }}", policy.Pools["default"].Profiles["default"].Principal)
	require.Equal(t, "${{ github.repository }}", policy.Pools["default"].Profiles["default"].EffectScope)
	require.Equal(t, before.AllowedRepositories, policy.Pools["default"].AllowedRepositories)
	require.Equal(t, "2", validated.Pools["default"].Profiles["default"].Principal)
	require.Error(t, workqueue.ValidatePolicy(policy))
	require.NoError(t, workqueue.ValidatePolicy(validated))
}

func TestWorkQueueConfiguredProducerPrincipalsAreNativeDecimalIDs(t *testing.T) {
	configuration := func(principal string) *WorkflowData {
		return &WorkflowData{
			Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
			RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{
				"producers": map[string]any{principal: map[string]any{
					"pools": []string{"default"}, "priorities": []int{1, 2, 3, 4, 5}, "fairness-keys": []string{""},
				}},
				"worker-profiles": map[string]any{"default": map[string]any{
					"workflow": ".github/workflows/worker.lock.yml", "ref": strings.Repeat("a", 40),
					"principal": "888", "trust-domain": "team", "credential-scope": "repository",
					"effect-scope": "owner/repo", "max-claims-per-dispatch": 1,
				}},
			}},
		}
	}
	for _, principal := range []string{"compiler", "bot", "0", "01", "-1", "+1", "12.3", "${{ github.actor }}", "${{ github.actor_id }}", strings.Repeat("1", 257)} {
		t.Run(principal, func(t *testing.T) {
			err := validateWorkQueueConfiguration(configuration(principal))
			require.ErrorContains(t, err, "work-queue-policy.producers")
			require.ErrorContains(t, err, "stable positive decimal GitHub actor IDs")
			require.ErrorContains(t, err, "Example:")
		})
	}
	for _, principal := range []string{"1", "777", strings.Repeat("1", 129), strings.Repeat("1", 256)} {
		t.Run(principal, func(t *testing.T) {
			data := configuration(principal)
			require.NoError(t, validateWorkQueueConfiguration(data))
			require.Contains(t, data.WorkQueuePolicy.Policy.Producers, principal)
			require.NotContains(t, data.WorkQueuePolicy.Policy.Producers, "${{ github.actor_id }}")
			environment := strings.Join(workQueuePolicyEnvironment(data), "")
			require.Contains(t, environment, fmt.Sprintf(`\"%s\":`, principal))
			require.NotContains(t, environment, "compiler/validation")
			require.NotContains(t, environment, "github.actor_id")
		})
	}
}

func TestWorkQueueImplicitPolicyDoesNotAssertCallerDependentWorkerIdentity(t *testing.T) {
	for _, worker := range []bool{false, true} {
		data := &WorkflowData{
			Tools:          map[string]any{"work-queue": map[string]any{"worker": worker}},
			RawFrontmatter: map[string]any{},
		}
		require.NoError(t, validateWorkQueueConfiguration(data))
		environment := strings.Join(workQueuePolicyEnvironment(data), "")
		require.Contains(t, environment, `GH_AW_WORK_QUEUE_ENABLED: "true"`)
		require.NotContains(t, environment, "GH_AW_WORK_QUEUE_POLICY:")
		require.NotContains(t, environment, "github.actor_id")
	}
}

func TestWorkQueueExplicitPolicyKeepsDistinctImmutableSenderAndWorkerPrincipals(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
		RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{
			"producers": map[string]any{"777": map[string]any{
				"pools": []string{"default"}, "priorities": []int{1, 2, 3, 4, 5}, "fairness-keys": []string{""},
			}},
			"worker-profiles": map[string]any{"default": map[string]any{
				"workflow": ".github/workflows/worker.lock.yml", "ref": strings.Repeat("a", 40),
				"principal": "888", "trust-domain": "team", "credential-scope": "repository",
				"effect-scope": "owner/repo", "max-claims-per-dispatch": 1,
			}},
		}},
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.Contains(t, data.WorkQueuePolicy.Policy.Producers, "777")
	require.NotContains(t, data.WorkQueuePolicy.Policy.Producers, "888")
	require.Equal(t, "888", data.WorkQueuePolicy.Policy.Pools["default"].Profiles["default"].Principal)
	environment := strings.Join(workQueuePolicyEnvironment(data), "")
	require.Contains(t, environment, "GH_AW_WORK_QUEUE_POLICY:")
	require.Contains(t, environment, `\"principal\":\"888\"`)
	require.NotContains(t, environment, "github.actor_id")
	require.NotContains(t, environment, "github.actor }}")
}

func TestWorkQueuePartialProfileProposalCannotPinCallerRelativeProducer(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
		RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{
			"worker-profiles": map[string]any{"default": map[string]any{
				"workflow": ".github/workflows/worker.lock.yml", "ref": strings.Repeat("a", 40),
				"principal": "22", "trust-domain": "team", "credential-scope": "repository", "effect-scope": "owner/repo",
			}},
		}},
	}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "explicit producers table")
	require.NotContains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_POLICY:")
	data.RawFrontmatter["work-queue-policy"].(map[string]any)["producers"] = workQueueTestProducerRules()
	require.NoError(t, validateWorkQueueConfiguration(data))
	environment := strings.Join(workQueuePolicyEnvironment(data), "")
	require.Contains(t, environment, `\"777\":`)
	require.Contains(t, environment, `\"principal\":\"22\"`)
	require.NotContains(t, environment, "github.actor_id")
}

func workQueueTestProducerRules() map[string]any {
	return map[string]any{"777": map[string]any{
		"pools": []string{"default"}, "priorities": []int{1, 2, 3, 4, 5}, "fairness-keys": []string{""},
	}}
}

func TestWorkQueuePolicyProposalCannotInferLaunchPrincipalFromDispatcher(t *testing.T) {
	for _, block := range []map[string]any{
		{"mode": "weighted-priority"},
		{"class-weights": []int{8, 4, 2, 1, 1}},
		{"dependencies": map[string]any{"max-observation-age": "60s"}},
	} {
		for _, worker := range []bool{false, true} {
			data := &WorkflowData{
				Tools:          map[string]any{"work-queue": map[string]any{"worker": worker}},
				RawFrontmatter: map[string]any{"work-queue-policy": block},
			}
			if !worker {
				data.SafeOutputs = &SafeOutputsConfig{DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}}}
			}
			err := validateWorkQueueConfiguration(data)
			require.ErrorContains(t, err, "approved dispatch credential's numeric GitHub principal")
			require.ErrorContains(t, err, "Example:")
			require.NotContains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_POLICY:")
		}
	}
	for _, block := range []map[string]any{{}, {"dependencies": map[string]any{"read-credentials": map[string]any{"foreign/design": "${{ secrets.DESIGN_READ_TOKEN }}"}}}} {
		data := &WorkflowData{
			Tools:          map[string]any{"work-queue": map[string]any{"worker": true}},
			RawFrontmatter: map[string]any{"work-queue-policy": block},
		}
		require.NoError(t, validateWorkQueueConfiguration(data))
		require.NotContains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_POLICY:")
	}
}

func TestWorkQueueReconciliationPropagatesPreviewWithoutGlobalAuthority(t *testing.T) {
	for _, value := range []TemplatableBool{"true", "${{ inputs.preview }}"} {
		data := &WorkflowData{
			Tools:       map[string]any{"work-queue": map[string]any{"worker": true}},
			SafeOutputs: &SafeOutputsConfig{Staged: &value},
		}
		require.NoError(t, validateWorkQueueConfiguration(data))
		step := strings.Join(NewCompiler().buildWorkQueueClaimReconciliationStep(data), "")
		require.Contains(t, step, "GH_AW_SAFE_OUTPUTS_STAGED:")
		require.Contains(t, step, "requireAssignment: true")
		require.NotContains(t, step, "claim_authorized")
	}
	literalFalse := TemplatableBool("false")
	for _, value := range []*TemplatableBool{nil, &literalFalse} {
		data := &WorkflowData{
			Tools:       map[string]any{"work-queue": map[string]any{"worker": true}},
			SafeOutputs: &SafeOutputsConfig{Staged: value},
		}
		require.NoError(t, validateWorkQueueConfiguration(data))
		compiler := NewCompiler()
		controls, err := compiler.buildWorkQueueControlProcessingStep(data)
		require.NoError(t, err)
		for _, step := range [][]string{compiler.buildWorkQueueClaimReconciliationStep(data), controls} {
			require.NotContains(t, strings.Join(step, ""), "GH_AW_SAFE_OUTPUTS_STAGED:", "unset/literal false must not shadow an inherited global preview")
		}
		compiler.trialMode = true
		controls, err = compiler.buildWorkQueueControlProcessingStep(data)
		require.NoError(t, err)
		for _, step := range [][]string{compiler.buildWorkQueueClaimReconciliationStep(data), controls} {
			text := strings.Join(step, "")
			require.Contains(t, text, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`, "global trial mode must reach both reconciliation and controls")
			require.NotContains(t, text, "claim_authorized")
		}
	}
}

func TestWorkQueueMandatoryPolicy(t *testing.T) {
	ordinary := &WorkflowData{Tools: map[string]any{}, RawFrontmatter: map[string]any{}}
	require.NoError(t, validateWorkQueueConfiguration(ordinary))
	require.Nil(t, ordinary.WorkQueuePolicy)
	worker := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, RawFrontmatter: map[string]any{}}
	require.NoError(t, validateWorkQueueConfiguration(worker))
	require.True(t, workQueueRequiresAssignment(worker))
	require.Equal(t, "weighted-priority", worker.WorkQueuePolicy.Policy.Mode)
	require.Equal(t, 1, worker.WorkQueuePolicy.Policy.Pools["default"].Profiles["default"].MaxClaims)
	require.Equal(t, 1, worker.WorkQueuePolicy.Policy.AccountingWeights[""])
	require.Equal(t, "${{ github.actor_id }}", worker.WorkQueuePolicy.Policy.Pools["default"].Profiles["default"].Principal)
	for _, value := range []any{false, map[string]any{"storage": "issues"}, map[string]any{"worker": true, "require-assignment": false}} {
		require.Error(t, validateWorkQueueConfiguration(&WorkflowData{Tools: map[string]any{"work-queue": value}}))
	}

	for _, mode := range []any{false, "disabled", "fifo", ""} {
		require.Error(t, validateWorkQueueConfiguration(&WorkflowData{
			Tools: map[string]any{"work-queue": true}, RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{"mode": mode}},
		}))
	}
}

func TestWorkQueueObserverPreservesOrdinaryOutputsAndProtectedRuntimeRoles(t *testing.T) {
	observer := &WorkflowData{
		Tools:          map[string]any{"work-queue": true},
		SafeOutputs:    &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}, Steps: []any{map[string]any{"run": "echo ordinary"}}},
		RawFrontmatter: map[string]any{},
	}
	require.NoError(t, validateWorkQueueConfiguration(observer))
	compiler := NewCompiler()
	require.NoError(t, compiler.validateWorkQueueTargets(observer, "observer.md"))
	require.Equal(t, "observer", workQueueRuntimeRole(observer))
	require.Empty(t, compiler.buildWorkQueueClaimReconciliationStep(observer))
	controlSteps, err := compiler.buildWorkQueueControlProcessingStep(observer)
	require.NoError(t, err)
	require.Empty(t, controlSteps)
	permissions, _ := safeOutputsJobPermissions(observer)
	require.NotContains(t, permissions.RenderToYAML(), "contents: write")
	require.NotContains(t, permissions.RenderToYAML(), "actions: write")
	steps, err := compiler.buildSafeOutputsUserProvidedSteps(observer)
	require.NoError(t, err)
	require.Contains(t, strings.Join(steps, ""), "echo ordinary")
	require.Contains(t, strings.Join(workQueuePolicyEnvironment(observer), ""), `GH_AW_WORK_QUEUE_ROLE: "observer"`)
	require.NotContains(t, strings.Join(workQueuePolicyEnvironment(observer), ""), "GH_AW_WORK_QUEUE_POLICY:")
	for _, env := range []map[string]string{compiler.buildMainJobEnv(observer), compiler.buildJobLevelSafeOutputEnvVars(observer, "observer")} {
		require.Equal(t, `"observer"`, env["GH_AW_WORK_QUEUE_ROLE"])
		require.NotContains(t, env, "GH_AW_WORK_QUEUE_INTENT_ORIGIN")
	}
	worker := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	require.Equal(t, `"worker"`, compiler.buildMainJobEnv(worker)["GH_AW_WORK_QUEUE_ROLE"])
	require.Equal(t, `"${{ needs.activation.outputs.work_queue_origin }}"`, compiler.buildMainJobEnv(worker)["GH_AW_WORK_QUEUE_INTENT_ORIGIN"])
	require.Equal(t, `"worker"`, compiler.buildJobLevelSafeOutputEnvVars(worker, "worker")["GH_AW_WORK_QUEUE_ROLE"])
	require.Equal(t, `"${{ needs.agent.outputs.work_queue_origin }}"`, compiler.buildJobLevelSafeOutputEnvVars(worker, "worker")["GH_AW_WORK_QUEUE_INTENT_ORIGIN"])
	require.Equal(t, "${{ steps.work_queue_intent_origin.outputs.work_queue_origin }}", compiler.buildMainJobOutputs(worker)["work_queue_origin"])
	require.NotContains(t, compiler.buildMainJobOutputs(observer), "work_queue_origin")
	for _, role := range []*WorkflowData{observer, worker} {
		permissions, err := compiler.buildMainJobPermissions(role)
		require.NoError(t, err)
		if role == worker {
			require.Contains(t, permissions, "actions: read")
		} else {
			require.NotContains(t, permissions, "actions: read")
		}
		var originStep strings.Builder
		compiler.generateWorkQueueIntentOriginStep(&originStep, role)
		if role == worker {
			require.Contains(t, originStep.String(), "id: work_queue_intent_origin")
			require.Contains(t, originStep.String(), "capture_work_queue_intent_origin.cjs")
		} else {
			require.Empty(t, originStep.String())
		}
	}
}

func TestWorkQueueNativePrincipalIsNotMutableLogin(t *testing.T) {
	for _, principal := range []string{"bot", "0", "01", "12.3", "${{ github.actor }}"} {
		data := &WorkflowData{Tools: map[string]any{"work-queue": true}, RawFrontmatter: map[string]any{
			"work-queue-policy": map[string]any{"worker-profiles": map[string]any{"default": map[string]any{
				"workflow": ".github/workflows/worker.lock.yml", "ref": strings.Repeat("a", 40),
				"principal": principal, "trust-domain": "team", "credential-scope": "repository",
				"effect-scope": "owner/repo", "max-claims-per-dispatch": 1,
			}}},
		}}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "GitHub actor ID")
	}
}

func TestWorkQueueWorkerRunNameUsesImmutableDispatchCorrelation(t *testing.T) {
	data := &WorkflowData{
		Tools:          map[string]any{"work-queue": map[string]any{"worker": true}},
		RawFrontmatter: map[string]any{"on": map[string]any{"workflow_dispatch": map[string]any{}, "push": map[string]any{}}},
		RunName:        `run-name: "Existing generated default"`,
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.Equal(t, workQueueWorkerRunName, data.RunName)
	require.Contains(t, data.RunName, "github.event_name == 'workflow_dispatch' && inputs.work_queue_assignment != ''")
	require.Contains(t, data.RunName, "format('gh-aw work-queue {0}', fromJSON(inputs.work_queue_assignment).dispatch_id)")
	data.RawFrontmatter["run-name"] = "arbitrary correlation"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "worker run-name is reserved")
	ordinary := &WorkflowData{Tools: map[string]any{}, RunName: `run-name: "Ordinary"`}
	require.NoError(t, validateWorkQueueConfiguration(ordinary))
	require.Equal(t, `run-name: "Ordinary"`, ordinary.RunName)
	dispatcher := &WorkflowData{Tools: map[string]any{"work-queue": true}, RunName: `run-name: "Dispatcher"`}
	require.NoError(t, validateWorkQueueConfiguration(dispatcher))
	require.Equal(t, `run-name: "Dispatcher"`, dispatcher.RunName)
}

func TestWorkQueueControlsRunAfterCompletionWithBoundedCompilerConfiguration(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: &SafeOutputsConfig{}}
	require.NoError(t, validateWorkQueueConfiguration(data))
	compiler := NewCompiler()
	controlSteps, err := compiler.buildWorkQueueControlProcessingStep(data)
	require.NoError(t, err)
	step := strings.Join(controlSteps, "")
	require.Contains(t, step, "GH_AW_WORK_QUEUE_CONTROL_CONFIG")
	require.Contains(t, step, "JSON.parse(process.env.GH_AW_WORK_QUEUE_CONTROL_CONFIG)")
	require.Contains(t, step, "work_queue_control_adapter.cjs")
	require.Contains(t, step, "requireAssignment: true")
	require.NotContains(t, step, "authorized")
	require.Equal(t, 1, strings.Count(step, "const { setupGlobals }"))
	require.Equal(t, 1, strings.Count(step, "setupGlobals(core, github, context, exec, io, getOctokit);"))
	steps, err := compiler.buildSafeOutputsSetupAndDownloadSteps(data, "")
	require.NoError(t, err)
	all := strings.Join(steps, "")
	require.Less(t, strings.Index(all, "work_queue_claim_reconciliation"), strings.Index(all, "work_queue_controls"))
	require.Contains(t, all, "GH_AW_WORK_QUEUE_FINISH_INTENT:")
	dispatcher := &WorkflowData{Tools: map[string]any{"work-queue": true}, SafeOutputs: &SafeOutputsConfig{DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}}}}
	require.NoError(t, validateWorkQueueConfiguration(dispatcher))
	controlSteps, err = compiler.buildWorkQueueControlProcessingStep(dispatcher)
	require.NoError(t, err)
	require.NotContains(t, strings.Join(controlSteps, ""), "requireAssignment:")
}

func TestWorkQueueForeignReadsBindSeparateEnvironmentCredentials(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, RawFrontmatter: map[string]any{
		"work-queue-policy": map[string]any{"dependencies": map[string]any{
			"read-credentials": map[string]any{
				"foreign/design": "${{ secrets.DESIGN_READ_TOKEN }}",
				"foreign/code":   "${{ secrets.CODE_READ_TOKEN }}",
			},
		}},
	}}
	require.NoError(t, validateWorkQueueConfiguration(data))
	env := workQueuePolicyEnvironment(data)
	all := strings.Join(env, "")
	require.NotContains(t, all, "GH_AW_WORK_QUEUE_POLICY:")
	require.NotContains(t, all, "github.actor_id")
	require.Contains(t, all, "GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0: ${{ secrets.CODE_READ_TOKEN }}")
	require.Contains(t, all, "GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_1: ${{ secrets.DESIGN_READ_TOKEN }}")
	for _, line := range env {
		if strings.Contains(line, "GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS:") {
			require.Contains(t, line, `\"foreign/code\":\"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_0\"`)
			require.Contains(t, line, `\"foreign/design\":\"GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_1\"`)
			require.NotContains(t, line, "secrets.")
		}
	}
	readCredentials := data.RawFrontmatter["work-queue-policy"].(map[string]any)["dependencies"].(map[string]any)["read-credentials"].(map[string]any)
	readCredentials["foreign/design"] = "${{ secrets.DESIGN_READ_TOKEN || github.token }}"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "separate secrets expression")
	readCredentials["foreign/design"] = "${{ secrets.DESIGN_READ_TOKEN }}"
	readCredentials["FOREIGN/design"] = "${{ secrets.OTHER_READ_TOKEN }}"
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "conflicting case-insensitive")
	for i := range 65 {
		readCredentials[fmt.Sprintf("foreign/repo%d", i)] = "${{ secrets.OTHER_READ_TOKEN }}"
	}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "at most 64")
}

func TestWorkQueueProfileBoundsAndImmutableRevision(t *testing.T) {
	for _, max := range []int{0, 1, 16, 17, -1} {
		profile := map[string]any{
			"workflow": ".github/workflows/worker.lock.yml", "ref": "0123456789012345678901234567890123456789",
			"principal": "123", "trust-domain": "team", "credential-scope": "repository", "effect-scope": "owner/repo",
			"max-claims-per-dispatch": max,
		}
		data := &WorkflowData{
			Tools:          map[string]any{"work-queue": true},
			RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{"worker-profiles": map[string]any{"default": profile}}},
		}
		err := validateWorkQueueConfiguration(data)
		if max < 1 || max > 16 {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, max, data.WorkQueuePolicy.Policy.Pools["default"].Profiles["default"].MaxClaims)
			profile["ref"] = strings.Repeat("a", 64)
			require.NoError(t, validateWorkQueueConfiguration(data))
			profile["ref"] = strings.Repeat("A", 64)
			require.Error(t, validateWorkQueueConfiguration(data))
		}
		profile["ref"] = "refs/heads/main"
		require.Error(t, validateWorkQueueConfiguration(data))
	}
}

func TestWorkQueueExplicitProfilesReplaceSynthesizedDefaults(t *testing.T) {
	profile := func(principal string) map[string]any {
		return map[string]any{
			"workflow": ".github/workflows/worker.lock.yml", "ref": strings.Repeat("a", 40),
			"principal": principal, "trust-domain": "trusted", "credential-scope": "repository", "effect-scope": "owner/repo",
		}
	}
	profiles := map[string]any{"zeta": profile("888"), "alpha": profile("777")}
	data := &WorkflowData{
		Tools:          map[string]any{"work-queue": map[string]any{"worker": true}},
		RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{"worker-profiles": profiles, "producers": workQueueTestProducerRules()}},
	}
	require.NoError(t, validateWorkQueueConfiguration(data))
	pool := data.WorkQueuePolicy.Policy.Pools["default"]
	require.Len(t, pool.Profiles, 2)
	require.NotContains(t, pool.Profiles, "default")
	require.Equal(t, "alpha", pool.DefaultProfile)
	require.Equal(t, "777", pool.Profiles["alpha"].Principal)
	require.Equal(t, "888", pool.Profiles["zeta"].Principal)
	require.Equal(t, 1, pool.Profiles["alpha"].MaxClaims)
	profiles["default"] = profile("999")
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.Equal(t, "default", data.WorkQueuePolicy.Policy.Pools["default"].DefaultProfile)
	data.RawFrontmatter["work-queue-policy"] = map[string]any{"worker-profiles": map[string]any{}}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "at least one approved profile")
}

func TestWorkQueuePolicySharedIdentityFixtures(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/identity-validation.json")
	require.NoError(t, err)
	var fixture struct {
		Version              int `json:"version"`
		IdentityMaxUTF8Bytes int `json:"identity_max_utf8_bytes"`
		DecimalMaxDigits     int `json:"decimal_max_digits"`
		Identity             []struct {
			Name              string `json:"name"`
			Text              string `json:"text"`
			Repeat            int    `json:"repeat"`
			ExpectedUTF8Bytes int    `json:"expected_utf8_bytes"`
			Valid             bool   `json:"valid"`
		} `json:"identity"`
		Decimal []struct {
			Name   string `json:"name"`
			Digits int    `json:"digits"`
			Valid  bool   `json:"valid"`
		} `json:"decimal"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.Equal(t, 3, fixture.Version)
	require.Equal(t, 256, fixture.IdentityMaxUTF8Bytes)
	require.Equal(t, 256, fixture.DecimalMaxDigits)
	validate := func(principal, profileName, trustDomain string) error {
		profile := map[string]any{
			"workflow": ".github/workflows/worker.lock.yml", "ref": strings.Repeat("a", 40),
			"principal": principal, "trust-domain": trustDomain, "credential-scope": "repository", "effect-scope": "owner/repo",
		}
		workflow := &WorkflowData{
			Tools:          map[string]any{"work-queue": map[string]any{"worker": true}},
			RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{"worker-profiles": map[string]any{profileName: profile}, "producers": workQueueTestProducerRules()}},
		}
		return validateWorkQueueConfiguration(workflow)
	}
	for _, entry := range fixture.Decimal {
		t.Run(entry.Name, func(t *testing.T) {
			err := validate(strings.Repeat("1", entry.Digits), "default", "trusted")
			if entry.Valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	for _, entry := range fixture.Identity {
		id := strings.Repeat(entry.Text, entry.Repeat)
		require.Len(t, id, entry.ExpectedUTF8Bytes, entry.Name)
		for _, field := range []string{"profile", "trust-domain"} {
			t.Run(field+"/"+entry.Name, func(t *testing.T) {
				profile, trust := "default", "trusted"
				if field == "profile" {
					profile = id
				} else {
					trust = id
				}
				err := validate("123", profile, trust)
				if entry.Valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestWorkQueueForeignReadsRequireSeparateCredential(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{"work-queue": true},
		RawFrontmatter: map[string]any{"work-queue-policy": map[string]any{
			"dependencies": map[string]any{"repositories": []any{"other/repo"}, "max-observation-age": "60s"},
		}},
	}
	require.ErrorContains(t, validateWorkQueueConfiguration(data), "separately bound")
	dependencies := data.RawFrontmatter["work-queue-policy"].(map[string]any)["dependencies"].(map[string]any)
	dependencies["read-credentials"] = map[string]any{"other/repo": "${{ secrets.DEPENDENCY_READ }}"}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.Equal(t, "${{ secrets.DEPENDENCY_READ }}", data.WorkQueuePolicy.ForeignReadCredentials["other/repo"])
	require.EqualValues(t, 60000, data.WorkQueuePolicy.Policy.Pools["default"].MaxObservationAgeMS)
}

func TestWorkQueueRejectsUnscopedDelegation(t *testing.T) {
	for _, safe := range []*SafeOutputsConfig{
		{Steps: []any{map[string]any{"run": "echo unsafe"}}},
		{Jobs: map[string]*SafeJobConfig{"custom": {}}},
		{Actions: map[string]*SafeOutputActionConfig{"custom": {}}},
		{Scripts: map[string]*SafeScriptConfig{"custom": {}}},
		{CallWorkflow: &CallWorkflowConfig{}},
		{DispatchRepository: &DispatchRepositoryConfig{}},
		{UploadCodeCoverage: &UploadCodeCoverageConfig{}},
		{CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{}},
	} {
		data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: safe}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim delivery adapter")
	}
}

func TestWorkQueueRejectsExternalHandlerTransports(t *testing.T) {
	for _, safe := range []*SafeOutputsConfig{
		{CreateWorkItems: &CreateWorkItemConfig{}},
		{UploadWorkItemAttachments: &UploadWorkItemAttachmentConfig{}},
	} {
		data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}, SafeOutputs: safe}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim target and delivery adapter")
	}
}

func TestWorkQueueRejectsPersistentStandaloneWrites(t *testing.T) {
	for _, data := range []*WorkflowData{
		{LedgerConfig: &LedgerToolConfig{}},
		{RepoMemoryConfig: &RepoMemoryConfig{}},
		{DriveMemoryConfig: &DriveMemoryConfig{}},
	} {
		data.Tools = map[string]any{"work-queue": map[string]any{"worker": true}}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim delivery adapter")
	}
}

func TestWorkQueuePolicyDictionaryKeysAreNotRewritten(t *testing.T) {
	actual := normalizeWorkQueuePolicyKeys(map[string]any{
		"accounting-weights": map[string]any{"project-a": 2},
		"pools":              map[string]any{"pool-a": map[string]any{"default-profile": "profile-a", "profiles": map[string]any{"profile-a": map[string]any{"max-claims-per-dispatch": 3}}}},
	}).(map[string]any)
	require.Contains(t, actual["accounting_weights"], "project-a")
	pool := actual["pools"].(map[string]any)["pool-a"].(map[string]any)
	require.Equal(t, "profile-a", pool["default_profile"])
	require.Equal(t, 3, pool["profiles"].(map[string]any)["profile-a"].(map[string]any)["max_claims"])
}

func TestWorkQueuePolicyProposalUsesCanonicalLimitsAndEntitlements(t *testing.T) {
	for _, block := range []map[string]any{
		{"limits": map[string]any{"operations": 0}},
		{"limits": map[string]any{"assignment-bytes": 49 * 1024}},
		{"outstanding": map[string]any{"claims": 0}},
		{"outstanding": map[string]any{"dispatches": 0}},
		{"outstanding": map[string]any{"per-account-claims": 0}},
		{"producers": map[string]any{"bot": map[string]any{"pools": []string{"foreign"}, "priorities": []int{3}, "fairness-keys": []string{""}}}},
		{"producers": map[string]any{"bot": map[string]any{"pools": []string{"default"}, "priorities": []int{0}, "fairness-keys": []string{""}}}},
		{"limits": map[string]any{"unknown": 1}},
	} {
		data := &WorkflowData{Tools: map[string]any{"work-queue": true}, RawFrontmatter: map[string]any{"work-queue-policy": block}}
		require.Error(t, validateWorkQueueConfiguration(data), "%v", block)
	}
}
