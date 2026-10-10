//go:build !integration

package workflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivationRepositoryFallbackResolution(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for repository expression resolution tests")
	}
	jsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", `
const assert = require("node:assert/strict");
global.context = {
  repo: { owner: "caller-org", repo: "caller-repo" },
  payload: { repository: { name: "caller-repo" } }
};
const { evaluateExpression } = require("./runtime_import.cjs");
const [repository, name] = process.argv.slice(1).map(
  expression => expression.slice(3, -2).trim()
);
delete process.env.GH_AW_NEEDS_ACTIVATION_OUTPUTS_TARGET_REPO;
delete process.env.GH_AW_NEEDS_ACTIVATION_OUTPUTS_TARGET_REPO_NAME;
assert.equal(evaluateExpression(repository), "caller-org/caller-repo");
assert.equal(evaluateExpression(name), "caller-repo");
process.env.GH_AW_NEEDS_ACTIVATION_OUTPUTS_TARGET_REPO = "host-org/host-repo";
process.env.GH_AW_NEEDS_ACTIVATION_OUTPUTS_TARGET_REPO_NAME = "host-repo";
assert.equal(evaluateExpression(repository), "host-org/host-repo");
assert.equal(evaluateExpression(name), "host-repo");
`, activationTargetRepoExpr, activationTargetRepoNameExpr)
	cmd.Dir = jsDir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestMainCheckoutUsesExportedHostRepository(t *testing.T) {
	for _, inlined := range []bool{false, true} {
		t.Run(map[bool]string{false: "host imports", true: "inlined imports"}[inlined], func(t *testing.T) {
			compiler := NewCompiler()
			data := &WorkflowData{
				Name:           "repository-checkout",
				On:             "on:\n  workflow_call:\n",
				InlinedImports: inlined,
				AI:             "copilot",
			}
			activation, err := compiler.buildActivationJob(data, false, "", "test.lock.yml")
			require.NoError(t, err)

			var steps strings.Builder
			manager, _, err := compiler.generateInitialAndCheckoutSteps(&steps, data)
			require.NoError(t, err)
			if inlined {
				assert.NotContains(t, activation.Outputs, "target_repo")
				assert.Empty(t, manager.GetCrossRepoTargetRepo())
				return
			}

			require.Equal(t, "${{ steps.resolve-host-repo.outputs.target_repo }}", activation.Outputs["target_repo"])
			assert.Contains(t, strings.Join(activation.Steps, ""), "JOB_WORKFLOW_REPOSITORY: ${{ job.workflow_repository }}")
			checkout := strings.Join(manager.GenerateGitHubFolderCheckoutStep(manager.GetCrossRepoTargetRepo(), "", "", compiler.getActionPin), "")
			assert.Contains(t, checkout, "repository: ${{ needs.activation.outputs.target_repo }}")
			assert.NotContains(t, checkout, "|| github.repository")
			assert.Contains(t, checkout, "persist-credentials: false")
		})
	}
}

func TestInlinedWorkflowCallRepositoryFallbacks(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	app := &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	data := &WorkflowData{
		Name:           "inlined-repository-fallbacks",
		On:             "on:\n  workflow_call:\n",
		InlinedImports: true,
		AI:             "copilot",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp:        app,
			AddComments:      &AddCommentsConfig{},
			CreateIssues:     &CreateIssuesConfig{},
			DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}},
			Mentions:         &MentionsConfig{GitHubApp: app},
		},
	}
	activation, err := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotContains(t, activation.Outputs, "target_repo")
	require.NotContains(t, activation.Outputs, "target_repo_name")

	safeOutputs, _, err := compiler.buildConsolidatedSafeOutputsJob(data, string(constants.AgentJobName), "test.md")
	require.NoError(t, err)
	conclusion, err := compiler.buildConclusionJob(data, string(constants.AgentJobName), nil)
	require.NoError(t, err)
	output, err := compiler.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName: "create_issue", StepName: "Create issue", StepID: "create_issue", MainJobName: string(constants.AgentJobName),
		Script: "return {}",
	})
	require.NoError(t, err)

	for name, steps := range map[string][]string{
		"safe outputs": safeOutputs.Steps,
		"conclusion":   conclusion.Steps,
		"output job":   output.Steps,
		"mentions":     compiler.addAppTokenMintingSteps(data),
	} {
		t.Run(name, func(t *testing.T) {
			generated := strings.Join(steps, "")
			assert.Contains(t, generated, "GH_AW_TARGET_REPOSITORY: "+activationTargetRepoExpr)
			assert.Contains(t, generated, "repositories: "+activationTargetRepoNameExpr)
		})
	}

	var configSteps []string
	compiler.addHandlerManagerConfigEnvVar(&configSteps, data)
	config := extractHandlerConfig(t, strings.Join(configSteps, ""))
	assert.Equal(t, activationTargetRepoExpr, config["dispatch_workflow"]["target-repo"])
}
