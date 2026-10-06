package workflow

import (
	"fmt"
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
	_, err := workqueue.Genesis(workqueue.Actor{
		Role: "administrator", Principal: "1", Repository: "compiler/validation",
	}, validated, "compiler-policy-validation", "compiler-proposal", 1)
	require.NoError(t, err)
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
		Tools: map[string]any{"work-queue": true},
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
	step := strings.Join(compiler.buildWorkQueueControlProcessingStep(data), "")
	require.Contains(t, step, "GH_AW_WORK_QUEUE_CONTROL_CONFIG")
	require.Contains(t, step, "JSON.parse(process.env.GH_AW_WORK_QUEUE_CONTROL_CONFIG)")
	require.Contains(t, step, "work_queue_control_adapter.cjs")
	require.Contains(t, step, "requireAssignment: true")
	require.NotContains(t, step, "authorized")
	steps, err := compiler.buildSafeOutputsSetupAndDownloadSteps(data, "")
	require.NoError(t, err)
	all := strings.Join(steps, "")
	require.Less(t, strings.Index(all, "work_queue_claim_reconciliation"), strings.Index(all, "work_queue_controls"))
	require.Contains(t, all, "GH_AW_WORK_QUEUE_FINISH_INTENT:")
	dispatcher := &WorkflowData{Tools: map[string]any{"work-queue": true}}
	require.NoError(t, validateWorkQueueConfiguration(dispatcher))
	require.NotContains(t, strings.Join(compiler.buildWorkQueueControlProcessingStep(dispatcher), ""), "requireAssignment:")
}

func TestWorkQueueForeignReadsBindSeparateEnvironmentCredentials(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"work-queue": true}, RawFrontmatter: map[string]any{
		"work-queue-policy": map[string]any{"dependencies": map[string]any{
			"repositories": []any{"foreign/design", "foreign/code"},
			"read-credentials": map[string]any{
				"foreign/design": "${{ secrets.DESIGN_READ_TOKEN }}",
				"foreign/code":   "${{ secrets.CODE_READ_TOKEN }}",
			},
		}},
	}}
	require.NoError(t, validateWorkQueueConfiguration(data))
	env := workQueuePolicyEnvironment(data)
	all := strings.Join(env, "")
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
		data := &WorkflowData{Tools: map[string]any{"work-queue": true}, SafeOutputs: safe}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim delivery adapter")
	}
}

func TestWorkQueueRejectsExternalHandlerTransports(t *testing.T) {
	for _, safe := range []*SafeOutputsConfig{
		{CreateWorkItems: &CreateWorkItemConfig{}},
		{UploadWorkItemAttachments: &UploadWorkItemAttachmentConfig{}},
	} {
		data := &WorkflowData{Tools: map[string]any{"work-queue": true}, SafeOutputs: safe}
		require.ErrorContains(t, validateWorkQueueConfiguration(data), "trusted per-Claim target and delivery adapter")
	}
}

func TestWorkQueueRejectsPersistentStandaloneWrites(t *testing.T) {
	for _, data := range []*WorkflowData{
		{LedgerConfig: &LedgerToolConfig{}},
		{RepoMemoryConfig: &RepoMemoryConfig{}},
		{DriveMemoryConfig: &DriveMemoryConfig{}},
	} {
		data.Tools = map[string]any{"work-queue": true}
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
