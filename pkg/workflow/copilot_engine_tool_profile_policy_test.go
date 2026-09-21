//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoRepositoryProfileNestedProtectedFileProtection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("cross-language policy contract requires node: %v", err)
	}
	policy, err := projectGoRepositoryPolicy(parseGoRepositoryProfileTestWorkflow(t))
	require.NoError(t, err)
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	helper, err := filepath.Abs(filepath.Join("..", "..", "actions", "setup", "js", "manifest_file_helpers.cjs"))
	require.NoError(t, err)
	const script = `
const { checkFileProtectionPostApply } = require(process.argv[1]);
const policy = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(checkFileProtectionPostApply(["docs/CODEOWNERS"], policy)));
`
	command := exec.CommandContext(t.Context(), node, "-e", script, helper)
	command.Stdin = strings.NewReader(string(raw))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var result struct {
		Action string   `json:"action"`
		Files  []string `json:"files"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	assert.Equal(t, "request_review", result.Action)
	assert.Equal(t, []string{"docs/CODEOWNERS"}, result.Files)
}

func TestGoRepositoryProfileExistingRuntimeResolverContract(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("cross-language placeholder contract requires node: %v", err)
	}
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.SafeOutputs.CreatePullRequests.BaseBranch = "${{ inputs.base }}"
	data.SafeOutputs.CreatePullRequests.BranchPrefix = "${{ inputs.prefix }}"
	_, args := NewCopilotEngine().buildCopilotArgs(data)
	config, _, err := buildGoRepositoryToolConfigRuntimeData(buildCopilotSDKToolConfigJSON(data, args))
	require.NoError(t, err)
	helper, err := filepath.Abs(filepath.Join("..", "..", "actions", "setup", "js", "safe_outputs_config.cjs"))
	require.NoError(t, err)
	const script = `
const { resolveEnvPlaceholders } = require(process.argv[1]);
const config = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(resolveEnvPlaceholders(config)));
`
	const prefix = "quotes'\"backslash\\newline\n${GH_AW_TEST_SENTINEL}"
	command := exec.CommandContext(t.Context(), node, "-e", script, helper)
	command.Stdin = strings.NewReader(config)
	command.Env = append(os.Environ(),
		goRepositoryDefaultBranchEnv+"=trunk",
		"GH_AW_INPUT_BASE=stable", "GH_AW_INPUT_PREFIX="+prefix,
		"GH_AW_TEST_SENTINEL=must-not-expand",
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var resolved copilotSDKToolConfig
	require.NoError(t, json.Unmarshal(output, &resolved))
	require.NotNil(t, resolved.Profile)
	assert.Equal(t, "trunk", resolved.Profile.RepositoryDefaultBranch)
	assert.Equal(t, "stable", resolved.Profile.Policy["base_branch"])
	assert.Equal(t, prefix, resolved.Profile.Policy["branch_prefix"], "inserted data must not be re-evaluated as environment references")
}

func TestGoRepositoryProfileProjectsCanonicalPolicyWithoutCredentials(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	pr := data.SafeOutputs.CreatePullRequests
	pr.TargetRepoSlug = "${{ github.repository }}"
	pr.BaseBranch = "release"
	pr.AllowedBaseBranches = []string{"release/*", "stable"}
	pr.AllowedBranches = []string{"fix/*"}
	pr.BranchPrefix = "automation/"
	pr.PreserveBranchName = true
	pr.RecreateRef = true
	pr.AllowedFiles = []string{"**/*.go", "**/CHANGELOG.md"}
	pr.ExcludedFiles = []string{"vendor/**"}
	pr.ProtectedFilesExclude = []string{"AGENTS.md", ".github/"}
	pr.MaxPatchSize = 2048
	pr.MaxPatchFiles = 17
	pr.GitHubToken = "${{ secrets.PUBLICATION_TOKEN }}"
	pr.HeadGitHubToken = "${{ secrets.HEAD_TOKEN }}"

	canonical := map[string]any{}
	addStandardHandlerConfigs(canonical, data)
	handler := canonical["create_pull_request"].(map[string]any)
	require.Contains(t, handler, "github-token")
	require.Contains(t, handler, "head-github-token")
	require.Contains(t, handler, "max")
	policy, err := projectGoRepositoryPolicy(data)
	require.NoError(t, err)
	expected := map[string]any{}
	for _, field := range []string{
		"target-repo", "base_branch", "allowed_base_branches", "allowed_branches", "branch_prefix",
		"preserve_branch_name", "recreate_ref", "allowed_files", "excluded_files",
		"protected_files_policy", "protected_files", "protected_path_prefixes",
		"protect_top_level_dot_folders", "protected_dot_folder_excludes", "max_patch_size", "max_patch_files",
	} {
		if value, exists := handler[field]; exists {
			expected[field] = value
		}
	}
	assert.Equal(t, expected, policy, "only the whitelisted, normalized policy may cross into the SDK")
	assert.Contains(t, policy["protected_files"], "CODEOWNERS", "basename protection also covers nested protected files")
	assert.NotContains(t, policy["protected_files"], "AGENTS.md")
	if prefixes, present := policy["protected_path_prefixes"]; present {
		assert.NotContains(t, prefixes, ".github/")
	}
	assert.Contains(t, policy, "protected_dot_folder_excludes")
	assert.Equal(t, createPullRequestProtectedFilesPolicy(pr), policy["protected_files_policy"])
	assert.Equal(t, 2048, policy["max_patch_size"])
	assert.Equal(t, 17, policy["max_patch_files"])
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "secrets.")
	assert.NotContains(t, string(encoded), "github-token")
	for _, field := range []string{"max", "draft", "staged", "stacked", "github-app", "head-github-app", "_protected_files_exclude"} {
		assert.NotContains(t, policy, field)
	}
}

func TestGoRepositoryProfileKeepsPRBaseSeparateFromRepositoryDefault(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	_, args := NewCopilotEngine().buildCopilotArgs(data)
	for _, base := range []string{"", "stable", "${{ inputs.base }}"} {
		t.Run(base, func(t *testing.T) {
			data.SafeOutputs.CreatePullRequests.BaseBranch = base
			config := buildCopilotSDKToolConfig(data, args)
			assert.Equal(t, goRepositoryDefaultBranchExpression, config.Profile.RepositoryDefaultBranch)
			if base == "" {
				assert.NotContains(t, config.Profile.Policy, "base_branch")
			} else {
				assert.Equal(t, base, config.Profile.Policy["base_branch"])
			}
		})
	}
}

func TestGoRepositoryProfileProjectsCanonicalCheckoutBinding(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.CheckoutConfigs = []*CheckoutConfig{{Repository: "owner/repo", Path: "src", Current: true}}
	data.SafeOutputs.CreatePullRequests.TargetRepoSlug = "owner/repo"
	policy, err := projectGoRepositoryPolicy(data)
	require.NoError(t, err)
	assert.Equal(t, "src", policy["patch_workspace_path"])
	assert.Equal(t, "owner/repo", policy["current_checkout_repo"])
	require.Error(t, validateCopilotToolProfile(data), "projection must not bypass the initial current-root checkout restriction")
}

func TestGoRepositoryProfileTransportRejectsMalformedEnvelope(t *testing.T) {
	for _, raw := range []string{"", "not json", "{}", `{"profile":{}}`, `{"profile":{"policy":[]}}`} {
		_, _, err := buildGoRepositoryToolConfigRuntimeData(raw)
		require.Error(t, err, raw)
	}
}

func TestGoRepositoryProfileExpressionTransport(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.SafeOutputs.CreatePullRequests.BaseBranch = "${{ inputs.base }}"
	data.SafeOutputs.CreatePullRequests.BranchPrefix = "work/${{ github.ref_name }}/"
	data.SafeOutputs.CreatePullRequests.AllowedBranches = []string{"${{ fromJSON(inputs.branches) }}"}
	_, args := NewCopilotEngine().buildCopilotArgs(data)
	raw := buildCopilotSDKToolConfigJSON(data, args)
	compiled, bindings, err := buildGoRepositoryToolConfigRuntimeData(raw)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"GH_AW_INPUT_BASE":           "${{ inputs.base }}",
		"GITHUB_REF_NAME":            "${{ github.ref_name }}",
		goRepositoryDefaultBranchEnv: goRepositoryDefaultBranchExpression,
	}, bindings)
	assert.Contains(t, compiled, `"repositoryDefaultBranch":"${GH_AW_GITHUB_EVENT_REPOSITORY_DEFAULT_BRANCH}"`)
	assert.Contains(t, compiled, `"base_branch":"${GH_AW_INPUT_BASE}"`)
	assert.Contains(t, compiled, `"branch_prefix":"work/${GITHUB_REF_NAME}/"`)
	const typedExpression = "${{ toJSON(fromJSON(inputs.branches)) }}"
	assert.Contains(t, compiled, `"allowed_branches":`+typedExpression)

	branches := []string{"fix/*", "quote\"backslash\\newline\n"}
	branchesJSON, err := json.Marshal(branches)
	require.NoError(t, err)
	evaluated := strings.ReplaceAll(compiled, typedExpression, string(branchesJSON))
	var decoded copilotSDKToolConfig
	require.NoError(t, json.Unmarshal([]byte(evaluated), &decoded), "toJSON results must remain data, never break the enclosing JSON")
	assert.Equal(t, []any{branches[0], branches[1]}, decoded.Profile.Policy["allowed_branches"])
	assert.Equal(t, "${GH_AW_INPUT_BASE}", decoded.Profile.Policy["base_branch"])

	env := map[string]string{
		"GH_AW_INPUT_BASE":           "untrusted override",
		goRepositoryDefaultBranchEnv: "untrusted override",
	}
	NewCopilotEngine().addCopilotSDKStepEnv(env, data, "[]", raw)
	for name, value := range bindings {
		assert.Equal(t, value, env[name], "compiler bindings must be applied after authored env")
	}
	assert.Equal(t, compiled, env[constants.CopilotSDKToolConfigEnvVar])
	require.NoError(t, validateCopilotToolProfile(data))
}

func TestGoRepositoryProfileComplexStringTransport(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.SafeOutputs.CreatePullRequests.BaseBranch = "${{ vars.PR_BASE || github.ref_name }}"
	data.SafeOutputs.CreatePullRequests.BranchPrefix = "fix/'${{ vars.PREFIX }}'/{literal}"
	require.NoError(t, validateCopilotToolProfile(data))
	_, args := NewCopilotEngine().buildCopilotArgs(data)
	compiled, _, err := buildGoRepositoryToolConfigRuntimeData(buildCopilotSDKToolConfigJSON(data, args))
	require.NoError(t, err)
	const baseExpression = "${{ toJSON(vars.PR_BASE || github.ref_name) }}"
	const prefixExpression = "${{ toJSON(format('fix/''{0}''/{{literal}}', vars.PREFIX)) }}"
	assert.Contains(t, compiled, `"base_branch":`+baseExpression)
	assert.Contains(t, compiled, `"branch_prefix":`+prefixExpression)
	assert.NotContains(t, compiled, `\u0026`)
	for _, value := range []string{"stable", "quoted\"value\\\n", ""} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		evaluated := strings.ReplaceAll(compiled, baseExpression, string(encoded))
		evaluated = strings.ReplaceAll(evaluated, prefixExpression, `"fix/value"`)
		var config copilotSDKToolConfig
		require.NoError(t, json.Unmarshal([]byte(evaluated), &config))
		assert.Equal(t, value, config.Profile.Policy["base_branch"])
	}
}

func TestGoRepositoryProfilePolicyExpressionRejections(t *testing.T) {
	for _, expression := range []string{
		"${{ secrets.BRANCH }}", "${{ SECRETS['BRANCH'] }}",
		"${{ github.token }}", "${{ github['token'] }}", "${{ toJSON(github) }}",
		"${{ inputs.branch || secrets.BRANCH }}",
		"${{ env.BRANCH }}", "${{ steps.prepare.outputs.branch }}",
		"${{ needs.publish.outputs.branches }}", "${{ needs['publish'].outputs.branches }}",
		"${{ NEEDS.publish.outputs.branches }}", "${{ needs[inputs.job].outputs.branches }}",
		"${HOME}", "${{ '${GH_TOKEN}' }}", "${{ inputs.branch",
	} {
		t.Run(expression, func(t *testing.T) {
			require.Error(t, validateGoRepositoryPolicyExpressions(map[string]any{"allowed_branches": expression}))
		})
	}
	for _, expression := range []string{
		"${{ inputs.branch }}", "${{ inputs['branch'] }}", "${{ github.event.inputs.branch }}",
		"${{ github.event.repository.default_branch }}", "${{ vars.RELEASE_BRANCH }}",
		"${{ fromJSON(inputs.branches) }}", "${{ format('release/{0}', vars.VERSION) }}",
		"${{ inputs.branch || 'stable' }}", "${{ inputs.release && 'release' || 'stable' }}",
	} {
		require.NoError(t, validateGoRepositoryPolicyExpressions(map[string]any{"allowed_branches": expression}), expression)
	}
}

func TestGoRepositoryProfileRejectsLaterJobPolicyBeforeNeutralization(t *testing.T) {
	data := parseGoRepositoryProfileTestWorkflow(t)
	data.SafeOutputs.Needs = []string{"publish"}
	data.SafeOutputs.CreatePullRequests.AllowedBranches = []string{"${{ needs.publish.outputs.branches }}"}
	policy, err := projectGoRepositoryPolicy(data)
	require.NoError(t, err)
	assert.Equal(t, "${{ needs.publish.outputs.branches }}", policy["allowed_branches"])
	require.ErrorContains(t, validateCopilotToolProfile(data), "needs")
	legacy, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	assert.NotContains(t, legacy, "${{ needs.publish.outputs.branches }}", "legacy collection neutralization must remain unchanged")
	require.ErrorContains(t, validateCopilotToolProfile(data), "needs", "earlier collection generation must not erase profile policy")
}
