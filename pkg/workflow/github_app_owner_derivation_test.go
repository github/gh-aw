//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGitHubAppTokenMintStepOwnerDerivation(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tests := []struct {
		name               string
		app                *GitHubAppConfig
		ownerSourceRepo    string
		expectedOwner      string
		expectedContains   string
		unexpectedContains string
	}{
		{
			name: "no owner source falls back to github.repository_owner",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			expectedOwner:      "owner: ${{ github.repository_owner }}",
			unexpectedContains: "id: safe-outputs-app-token-owner",
		},
		{
			name: "explicit owner wins",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
				Owner:      "explicit-org",
			},
			ownerSourceRepo:    "${{ github.event.inputs.trigger_ref }}",
			expectedOwner:      "owner: explicit-org",
			unexpectedContains: "id: safe-outputs-app-token-owner",
		},
		{
			name: "literal repository derives owner without helper step",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			ownerSourceRepo:    "acme/project",
			expectedOwner:      "owner: acme",
			unexpectedContains: "id: safe-outputs-app-token-owner",
		},
		{
			name: "repository expression derives owner with helper step",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			ownerSourceRepo:  "${{ github.event.inputs.trigger_ref }}",
			expectedOwner:    "owner: ${{ steps.safe-outputs-app-token-owner.outputs.owner }}",
			expectedContains: "id: safe-outputs-app-token-owner",
		},
		{
			name: "bare repository name emits helper step",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			ownerSourceRepo:  "just-a-repo",
			expectedOwner:    "owner: ${{ steps.safe-outputs-app-token-owner.outputs.owner }}",
			expectedContains: "GH_AW_TARGET_REPOSITORY: just-a-repo",
		},
		{
			name: "repository expression includes wiki stripping",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			ownerSourceRepo:  "${{ inputs.wiki_repo }}",
			expectedOwner:    "owner: ${{ steps.safe-outputs-app-token-owner.outputs.owner }}",
			expectedContains: "repo=\"${GH_AW_TARGET_REPOSITORY%.wiki}\"",
		},
		{
			name: "repository expression is whitespace-normalized before yaml emission",
			app: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			ownerSourceRepo:  "${{ inputs.target_\nrepo }}",
			expectedOwner:    "owner: ${{ steps.safe-outputs-app-token-owner.outputs.owner }}",
			expectedContains: "GH_AW_TARGET_REPOSITORY: ${{ inputs.target_ repo }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := compiler.buildGitHubAppTokenMintStepWithMeta(
				tt.app,
				nil,
				"",
				tt.ownerSourceRepo,
				"Generate GitHub App token",
				"safe-outputs-app-token",
			)
			stepsStr := strings.Join(steps, "")

			assert.Contains(t, stepsStr, tt.expectedOwner, "case %q should include expected owner", tt.name)
			if tt.expectedContains != "" {
				assert.Contains(t, stepsStr, tt.expectedContains, "case %q should include expected content", tt.name)
			}
			if tt.unexpectedContains != "" {
				assert.NotContains(t, stepsStr, tt.unexpectedContains, "case %q should not include unexpected content", tt.name)
			}

			if strings.Contains(stepsStr, "id: safe-outputs-app-token-owner") {
				ownerIdx := -1
				mintIdx := -1
				for i, step := range steps {
					if strings.Contains(step, "id: safe-outputs-app-token-owner") {
						ownerIdx = i
					}
					if strings.Contains(step, "id: safe-outputs-app-token\n") {
						mintIdx = i
					}
				}
				require.NotEqual(t, -1, ownerIdx, "case %q should include owner helper step id", tt.name)
				require.NotEqual(t, -1, mintIdx, "case %q should include app token mint step id", tt.name)
				require.Greater(t, mintIdx, ownerIdx, "case %q should emit owner derivation step before mint step", tt.name)
			}
		})
	}
}

func TestWorkflowBuildersDeriveGitHubAppOwnerFromCheckoutRepository(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name: "Test Workflow",
		On:   `"on": "workflow_dispatch"`,
		Permissions: `"permissions":
  contents: read
  issues: read
  pull-requests: read`,
		CheckoutConfigs: []*CheckoutConfig{
			{
				Repository: "${{ github.event.inputs.trigger_ref }}",
				Path:       "target",
				GitHubApp: &GitHubAppConfig{
					AppID:        "${{ vars.CHECKOUT_APP_ID }}",
					PrivateKey:   "${{ secrets.CHECKOUT_APP_PRIVATE_KEY }}",
					Repositories: []string{"*"},
				},
			},
		},
		ParsedTools: &Tools{
			GitHub: &GitHubToolConfig{
				Mode: "local",
				GitHubApp: &GitHubAppConfig{
					AppID:        "${{ vars.MCP_APP_ID }}",
					PrivateKey:   "${{ secrets.MCP_APP_PRIVATE_KEY }}",
					Repositories: []string{"*"},
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:        "${{ vars.SAFE_OUTPUTS_APP_ID }}",
				PrivateKey:   "${{ secrets.SAFE_OUTPUTS_APP_PRIVATE_KEY }}",
				Repositories: []string{"*"},
			},
			CreateIssues: &CreateIssuesConfig{TitlePrefix: "[automated] "},
		},
	}

	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)
	checkoutSteps := strings.Join(checkoutMgr.GenerateCheckoutAppTokenSteps(compiler, nil), "")
	assert.Contains(t, checkoutSteps, "id: checkout-app-token-0-owner", "checkout app token should include owner helper step")
	assert.Contains(t, checkoutSteps, "owner: ${{ steps.checkout-app-token-0-owner.outputs.owner }}", "checkout app token should consume derived owner output")
	assert.Contains(t, checkoutSteps, "GH_AW_TARGET_REPOSITORY: ${{ github.event.inputs.trigger_ref }}", "checkout owner helper should read target repository expression")

	mcpSteps := strings.Join(compiler.generateGitHubMCPAppTokenMintingSteps(data), "")
	assert.Contains(t, mcpSteps, "id: github-mcp-app-token-owner", "github MCP token should include owner helper step")
	assert.Contains(t, mcpSteps, "owner: ${{ steps.github-mcp-app-token-owner.outputs.owner }}", "github MCP token should consume derived owner output")
	assert.Contains(t, mcpSteps, "GH_AW_TARGET_REPOSITORY: ${{ github.event.inputs.trigger_ref }}", "github MCP owner helper should read target repository expression")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(data, string(constants.AgentJobName), "test.md")
	require.NoError(t, err, "safe outputs job compilation should succeed")
	safeOutputsSteps := strings.Join(job.Steps, "")
	assert.Contains(t, safeOutputsSteps, "id: safe-outputs-app-token-owner", "safe outputs app token should include owner helper step")
	assert.Contains(t, safeOutputsSteps, "owner: ${{ steps.safe-outputs-app-token-owner.outputs.owner }}", "safe outputs app token should consume derived owner output")
	assert.Contains(t, safeOutputsSteps, "GH_AW_TARGET_REPOSITORY: ${{ github.event.inputs.trigger_ref }}", "safe outputs owner helper should read target repository expression")
}

func TestInferSingleCheckoutRepositoryForGitHubAppOwner(t *testing.T) {
	t.Run("returns empty for multiple distinct repositories", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "org-a/repo"},
				{Repository: "org-b/other"},
			},
		}
		assert.Empty(t, inferSingleCheckoutRepositoryForGitHubAppOwner(data), "multiple distinct repositories should not infer a single owner source")
	})

	t.Run("returns repository when repeated repositories are the same", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "org-a/repo"},
				{Repository: "org-a/repo"},
			},
		}
		assert.Equal(t, "org-a/repo", inferSingleCheckoutRepositoryForGitHubAppOwner(data), "matching repositories should infer a single owner source")
	})

	t.Run("returns empty when no explicit repositories are configured", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Path: "default"},
			},
		}
		assert.Empty(t, inferSingleCheckoutRepositoryForGitHubAppOwner(data), "no explicit repository should not infer owner source")
	})

	t.Run("workflow_call defaults to activation target repository", func(t *testing.T) {
		data := &WorkflowData{
			On: `"on":
  workflow_call: {}`,
			CheckoutConfigs: []*CheckoutConfig{
				{Path: "default"},
			},
		}
		assert.Equal(t, "${{ needs.activation.outputs.target_repo || github.repository }}", inferSingleCheckoutRepositoryForGitHubAppOwner(data), "workflow_call should infer activation target repository when no explicit checkout repository is set")
	})
}

func TestWorkflowCallSafeOutputsAppTokenUsesRepositoryOwnerSource(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name: "workflow-call-safe-outputs",
		On: `"on":
  workflow_call: {}`,
		Permissions: `"permissions":
  contents: read
  issues: read`,
		CheckoutConfigs: []*CheckoutConfig{
			{
				Repository: "${{ inputs.target_repository }}",
				GitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.CHECKOUT_APP_ID }}",
					PrivateKey: "${{ secrets.CHECKOUT_APP_PRIVATE_KEY }}",
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.SAFE_OUTPUTS_APP_ID }}",
				PrivateKey: "${{ secrets.SAFE_OUTPUTS_APP_PRIVATE_KEY }}",
			},
			CreateIssues: &CreateIssuesConfig{TitlePrefix: "[automated] "},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(data, string(constants.AgentJobName), "test.md")
	require.NoError(t, err, "safe outputs job compilation should succeed for workflow_call")
	require.NotNil(t, job, "safe outputs job should be generated for workflow_call")

	safeOutputsSteps := strings.Join(job.Steps, "")
	assert.Contains(t, safeOutputsSteps, "id: safe-outputs-app-token-owner", "safe outputs app token should include owner helper for expression repository")
	assert.Contains(t, safeOutputsSteps, "owner: ${{ steps.safe-outputs-app-token-owner.outputs.owner }}", "safe outputs app token should consume derived owner output")
	assert.Contains(t, safeOutputsSteps, "GH_AW_TARGET_REPOSITORY: ${{ inputs.target_repository }}", "safe outputs owner helper should target explicit checkout repository expression")
	assert.Contains(t, safeOutputsSteps, "repositories: ${{ needs.activation.outputs.target_repo_name || github.event.repository.name }}", "workflow_call safe outputs app token should default repositories to target_repo_name")
}
