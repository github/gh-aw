package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

const gatewayProvenanceJobName = "gateway_provenance"

func (c *Compiler) addGatewayProvenanceJob(data *WorkflowData) error {
	if len(data.WebhookSubscriptions) == 0 {
		return nil
	}
	if _, exists := data.Jobs[gatewayProvenanceJobName]; exists {
		return fmt.Errorf("jobs.%s is compiler-owned and cannot be customized; configure the approved CAO App through the central GH_AW_GATEWAY_APP_ID and GH_AW_GATEWAY_APP_BOT_ID Actions secrets or variables", gatewayProvenanceJobName)
	}
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef == "" {
		return errors.New("setup action reference is required for gateway provenance verification")
	}
	// Identity verification must not mint user-configured App or OTLP credentials.
	setupData := &WorkflowData{Name: data.Name, WorkflowID: data.WorkflowID}
	steps := c.generateCheckoutActionsFolder(data)
	steps = append(steps, c.generateSetupStepForJob(gatewayProvenanceJobName, setupData, setupActionRef, SetupActionDestination, false, "", "", "")...)
	steps = append(steps,
		"      - name: Verify CAO App provenance\n",
		"        id: verify-gateway-provenance\n",
		"        uses: "+getCachedActionPin("actions/github-script", data)+"\n",
		"        env:\n",
		"          GH_AW_GATEWAY_APP_ID: ${{ secrets.GH_AW_GATEWAY_APP_ID || vars.GH_AW_GATEWAY_APP_ID }}\n",
		"          GH_AW_GATEWAY_APP_BOT_ID: ${{ secrets.GH_AW_GATEWAY_APP_BOT_ID || vars.GH_AW_GATEWAY_APP_BOT_ID }}\n",
		"          GH_AW_GATEWAY_EVENT_NAME: ${{ github.event_name }}\n",
		"          GH_AW_GATEWAY_EVENT_TYPE: "+WebhookDispatchEventType(data.WorkflowID)+"\n",
		"          GH_AW_GATEWAY_ACTOR_ID: ${{ github.actor_id }}\n",
		"          GH_AW_GATEWAY_REPOSITORY_ID: ${{ github.repository_id }}\n",
		"          GH_AW_GATEWAY_REPOSITORY: ${{ github.repository }}\n",
		"        with:\n",
		"          github-token: ${{ secrets.GITHUB_TOKEN }}\n",
		"          script: |\n",
		generateGitHubScriptWithRequire("check_gateway_provenance.cjs"),
	)
	return c.jobManager.AddJob(&Job{
		Name:           gatewayProvenanceJobName,
		DisplayName:    "Verify CAO App provenance",
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Permissions:    NewPermissionsContentsRead().RenderToYAML(),
		TimeoutMinutes: 5,
		Steps:          steps,
		Outputs:        map[string]string{"verified": "${{ steps.verify-gateway-provenance.outputs.verified }}"},
		Env: map[string]string{
			"GITHUB_API_URL":    "${{ github.api_url }}",
			"GITHUB_SERVER_URL": "${{ github.server_url }}",
			"GITHUB_EVENT_PATH": "${{ github.event_path }}",
		},
	})
}

// Apply after job augmentation and typed condition lowering so no author-defined
// job, status function, or activation output can bypass either gateway gate.
func (c *Compiler) guardGatewayProvenanceJobs(data *WorkflowData) error {
	if len(data.WebhookSubscriptions) == 0 {
		return nil
	}
	for name, job := range c.jobManager.GetAllJobs() {
		// Central trust settings must not resolve from an author-selected environment.
		if name == gatewayProvenanceJobName {
			job.Environment = ""
			continue
		}
		gates := []string{gatewayProvenanceJobName}
		if name == gatewayAdmissionJobName {
			job.Environment = ""
		} else {
			gates = append(gates, gatewayAdmissionJobName)
		}
		conditions := make([]string, 0, len(gates))
		for _, gate := range gates {
			if !slices.Contains(job.Needs, gate) {
				job.Needs = append(job.Needs, gate)
			}
			conditions = append(conditions, "needs."+gate+".result == 'success' && needs."+gate+".outputs.verified == 'true'")
		}
		condition := strings.Join(conditions, " && ")
		if job.If != "" {
			existing := stripExpressionWrapper(job.If)
			if !isGatewayProvenanceCondition(existing) {
				return fmt.Errorf("jobs.%s.if must be one GitHub Actions expression, not interpolated text; use one complete ${{ }} wrapper or omit the wrapper", name)
			}
			condition += " && (" + existing + ")"
		}
		job.If = "${{ " + condition + " }}"
		// Folded YAML would append text outside the expression and make it truthy.
		job.actionsCondition = true
	}
	return nil
}

// Interpolation could turn a false condition into a nonempty, truthy string.
func isGatewayProvenanceCondition(condition string) bool {
	expression, err := actionslanguageservice.ParseSyntax(condition)
	return err == nil && expression != nil
}
