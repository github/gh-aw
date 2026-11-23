package workflow

import (
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

const gatewayAdmissionJobName = "gateway_admission"

func (c *Compiler) addGatewayAdmissionJob(data *WorkflowData) error {
	if len(data.WebhookSubscriptions) == 0 {
		return nil
	}
	if _, exists := data.Jobs[gatewayAdmissionJobName]; exists {
		return fmt.Errorf("jobs.%s is compiler-owned and cannot be customized; configure GH_AW_GATEWAY_GET_TOKEN_URL and GH_AW_GATEWAY_OIDC_AUDIENCE through central Actions secrets or variables", gatewayAdmissionJobName)
	}
	if data.FrontmatterHash == "" {
		return errors.New("gateway workflow admission requires the compiler-generated frontmatter hash; compile the workflow from its Markdown source")
	}
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef == "" {
		return errors.New("setup action reference is required for gateway workflow admission")
	}
	setupData := &WorkflowData{Name: data.Name, WorkflowID: data.WorkflowID}
	steps := c.generateCheckoutActionsFolder(data)
	steps = append(steps, c.generateSetupStepForJob(gatewayAdmissionJobName, setupData, setupActionRef, SetupActionDestination, false, "", "", "")...)
	steps = append(steps,
		"      - name: Verify gateway workflow version\n",
		"        id: verify-gateway-admission\n",
		"        uses: "+getCachedActionPin("actions/github-script", data)+"\n",
		"        env:\n",
		"          GH_AW_GATEWAY_GET_TOKEN_URL: ${{ secrets.GH_AW_GATEWAY_GET_TOKEN_URL || vars.GH_AW_GATEWAY_GET_TOKEN_URL }}\n",
		"          GH_AW_GATEWAY_OIDC_AUDIENCE: ${{ secrets.GH_AW_GATEWAY_OIDC_AUDIENCE || vars.GH_AW_GATEWAY_OIDC_AUDIENCE }}\n",
		"          GH_AW_GATEWAY_REPOSITORY: ${{ github.repository }}\n",
		"          GH_AW_GATEWAY_REPOSITORY_ID: ${{ github.repository_id }}\n",
		"          GH_AW_GATEWAY_OWNER_ID: ${{ github.repository_owner_id }}\n",
		"          GH_AW_GATEWAY_WORKFLOW_REF: ${{ github.workflow_ref }}\n",
		"          GH_AW_GATEWAY_WORKFLOW_SHA: ${{ github.workflow_sha }}\n",
		fmt.Sprintf("          GH_AW_GATEWAY_WORKFLOW_HASH: %q\n", data.FrontmatterHash),
		fmt.Sprintf("          GH_AW_GATEWAY_WORKFLOW_PATH: %q\n", constants.WorkflowsDirSlash+data.WorkflowID+".lock.yml"),
		"          GH_AW_GATEWAY_RUN_ID: ${{ github.run_id }}\n",
		"          GH_AW_GATEWAY_RUN_ATTEMPT: ${{ github.run_attempt }}\n",
		"          GH_AW_GATEWAY_ACTOR_ID: ${{ github.actor_id }}\n",
		"          GH_AW_GATEWAY_REF: ${{ github.ref }}\n",
		"          GH_AW_GATEWAY_EVENT_NAME: ${{ github.event_name }}\n",
		"          GH_AW_GATEWAY_EVENT_TYPE: "+WebhookDispatchEventType(data.WorkflowID)+"\n",
		"        with:\n",
		"          github-token: ${{ secrets.GITHUB_TOKEN }}\n",
		"          script: |\n",
		generateGitHubScriptWithRequire("check_gateway_admission.cjs"),
	)
	if c.actionMode.IsScript() {
		steps = append(steps, c.generateScriptModeCleanupStep(setupData))
	}
	permissions := NewPermissionsContentsRead()
	permissions.Set(PermissionIdToken, PermissionWrite)
	return c.jobManager.AddJob(&Job{
		Name:           gatewayAdmissionJobName,
		DisplayName:    "Verify gateway workflow version",
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Needs:          []string{gatewayProvenanceJobName},
		Permissions:    permissions.RenderToYAML(),
		TimeoutMinutes: 5,
		Steps:          steps,
		Outputs:        map[string]string{"verified": "${{ steps.verify-gateway-admission.outputs.verified }}"},
		Env: map[string]string{
			"GITHUB_API_URL":    "${{ github.api_url }}",
			"GITHUB_SERVER_URL": "${{ github.server_url }}",
		},
	})
}
