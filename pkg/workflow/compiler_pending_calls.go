package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
)

func (c *Compiler) addPendingCallVerificationJob(data *WorkflowData, call *Job, publication pendingWritePublication) error {
	name := "verify-pending-" + call.Name
	steps := append([]string{}, c.generateCheckoutActionsFolder(data)...)
	steps = append(steps, c.generateSetupStepForJob(name, data, c.resolveActionReference("./actions/setup", data), SetupActionDestination, false, "", "", "")...)
	steps = append(steps, c.pendingWritesDownloadSteps(data)...)
	steps = append(steps,
		"      - name: Download authorized write decisions\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
		"        with:\n",
		"          name: "+artifactPrefixExprForDownstreamJob(data)+"write-decisions\n",
		"          path: /tmp/gh-aw/\n")
	encoded, err := json.Marshal(publication)
	if err != nil {
		return fmt.Errorf("encoding pending workflow call: %w", err)
	}
	steps = append(steps,
		"      - name: Verify pending workflow call\n",
		"        id: verify_pending_call\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        env:\n",
		fmt.Sprintf("          GH_AW_PENDING_PUBLICATION: %q\n", encoded),
		"          GH_AW_CALL_WORKFLOW_NAME: ${{ needs.safe_outputs.outputs.call_workflow_name }}\n",
		"          GH_AW_CALL_WORKFLOW_PAYLOAD: ${{ needs.safe_outputs.outputs.call_workflow_payload }}\n",
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            require('"+SetupActionDestination+"/pending_writes_runtime.cjs').checkPublicationFromEnv();\n")
	verification := &Job{
		Name: name, RunsOn: c.formatFrameworkJobRunsOn(data), If: call.If,
		Needs: append([]string{}, call.Needs...), Permissions: "permissions:\n      contents: read\n      actions: read",
		Steps: steps, Env: map[string]string{},
		Outputs: map[string]string{
			"pending_writes_verified":    "${{ steps.verify_pending_call.outputs.pending_writes_verified }}",
			"pending_writes_item_sha256": "${{ steps.verify_pending_call.outputs.pending_writes_item_sha256 }}",
		},
	}
	maps.Copy(verification.Env, call.Env)
	call.Env = nil
	if err := c.jobManager.AddJob(verification); err != nil {
		return fmt.Errorf("adding workflow call verification: %w", err)
	}
	call.Needs = append(call.Needs, name)
	guard := fmt.Sprintf("needs.%s.result == 'success' && needs.%s.outputs.pending_writes_verified == 'true'", name, name)
	if call.If != "" {
		guard += " && (" + c.extractExpressionFromIfString(call.If) + ")"
	}
	call.If = guard
	return nil
}
