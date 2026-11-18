package workflow

import (
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
)

// Reusable-workflow jobs cannot contain steps. A separate job obtains a fresh
// decision immediately before releasing the selected launch to Actions.
func (c *Compiler) buildCallWorkflowLaunchAuthorization(data *WorkflowData, workflowName string, callJob *Job) (*Job, error) {
	if c.dryRun || data.SafeOutputs.AuthorizationRequirements["call_workflow"] == nil {
		return nil, nil
	}
	cfg := data.SafeOutputs.CallWorkflow
	permissions := NewPermissionsContentsRead()
	permissions.Set(PermissionActions, PermissionRead)
	if !isHandlerStaged(templatableBoolIsTrue(data.SafeOutputs.Staged), cfg.Staged) {
		permissions.Set(PermissionScope("id-token"), PermissionWrite)
	}
	steps := c.buildSafeOutputsSetupSteps(data)
	steps = append(steps, buildAgentOutputDownloadSteps(artifactPrefixExprForAgentDownstreamJob(data), c.getActionPin)...)
	implementation := map[string]any{
		"workflow": workflowName, "path": callJob.Uses, "with": callJob.With,
		"revision": "${{ github.workflow_sha }}", "repository_id": "${{ github.repository_id }}",
	}
	gate, err := c.buildAuthorizationGateStep(data, "call_workflow", "workflow", implementation, cfg.Staged, authorizationGateOptions{
		Payload:    "${{ format('{{\"workflow\":{0},\"inputs\":{1}}}', toJSON(needs.safe_outputs.outputs.call_workflow_name), needs.safe_outputs.outputs.call_workflow_payload) }}",
		OutputPath: "${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}",
		Phase:      "launch:" + stringutil.NormalizeSafeOutputIdentifier(workflowName),
	})
	if err != nil {
		return nil, err
	}
	steps = append(steps, gate...)
	steps = append(steps, c.buildAuthorizationEvidenceUploadStep(data, "authorize-"+callJob.Name)...)
	job := &Job{
		Name: "authorize-" + callJob.Name, RunsOn: c.formatFrameworkJobRunsOn(data),
		Needs: []string{"safe_outputs", "activation", "agent"}, Actions: &actions.Job{If: callJob.Actions.If},
		Permissions: permissions.RenderToYAML(), TimeoutMinutes: 5,
		Env:     c.buildJobLevelSafeOutputEnvVars(data, GetWorkflowIDFromPath(c.markdownPath)),
		Steps:   steps,
		Outputs: map[string]string{"authorized": "${{ steps.authorize_call_workflow.outputs.authorized }}"},
	}
	job.Env["GH_AW_AUTHORIZATION_SOURCE_RECORD_INDEX"] = `"${{ needs.safe_outputs.outputs.call_workflow_record_index }}"`
	callJob.Needs = append(callJob.Needs, "safe_outputs", job.Name)
	callJob.Actions.If = actions.Binary{
		Left: callJob.Actions.If, Operator: "&&",
		Right: actions.Binary{Left: actions.OutputRef{Job: actions.JobID(job.Name), Name: "authorized"}, Operator: "==", Right: actions.Literal("true")},
	}
	return job, nil
}
