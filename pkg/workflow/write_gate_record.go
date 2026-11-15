package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (c *Compiler) buildWriteGateReplayConfig(data *WorkflowData) (map[string]any, error) {
	if isWorkQueueParticipant(data) {
		return nil, nil
	}
	if data.SafeOutputs != nil && data.SafeOutputs.Staged != nil && isExpression(data.SafeOutputs.Staged.String()) {
		return nil, nil
	}
	if strings.Contains(data.Env, "GH_AW_SAFE_OUTPUTS_STAGED") {
		return nil, nil
	}
	serialized, err := json.Marshal(c.buildHandlerManagerConfig(data))
	if err != nil {
		return nil, fmt.Errorf("cannot serialize original replay policy: %w", err)
	}
	var config map[string]any
	if err := json.Unmarshal(serialized, &config); err != nil {
		return nil, fmt.Errorf("cannot decode original replay policy: %w", err)
	}
	removeReplayCredentials(config)
	serialized, err = json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize credential-free replay policy: %w", err)
	}
	// Today's vars, secrets, needs, or environment cannot stand in for the
	// original runtime-resolved restrictions.
	if strings.Contains(string(serialized), "${{") || strings.Contains(string(serialized), "${GH_AW_INPUT_") {
		return nil, nil
	}
	return config, nil
}

func removeReplayCredentials(value any) {
	switch value := value.(type) {
	case map[string]any:
		delete(value, "github-token")
		delete(value, "head-github-token")
		for _, child := range value {
			removeReplayCredentials(child)
		}
	case []any:
		for _, child := range value {
			removeReplayCredentials(child)
		}
	}
}

func isStaticWriteGateReplayType(name string, data *WorkflowData) bool {
	switch name {
	case "noop", "missing_tool", "missing_data", "report_incomplete", "dispatch_repository":
		return true
	case "create_issue":
		mentions := data.SafeOutputs.Mentions
		staticMentions := mentions != nil && ((mentions.Enabled != nil && !*mentions.Enabled) ||
			(mentions.AllowedCollaborators != nil && !*mentions.AllowedCollaborators && mentions.AllowContext != nil && !*mentions.AllowContext))
		// Grouping and contextual mention allowances need the original resolved
		// target/context snapshot, not maintenance's current issue or authors.
		return staticMentions && (data.SafeOutputs.CreateIssues == nil ||
			data.SafeOutputs.CreateIssues.Group == nil || *data.SafeOutputs.CreateIssues.Group == "false")
	default:
		return false
	}
}

func (c *Compiler) addWriteGateRecordSteps(data *WorkflowData) error {
	if !hasWriteGateSteps(data) {
		return nil
	}
	job, exists := c.jobManager.GetJob("conclusion")
	if !exists {
		return fmt.Errorf("jobs.safe_outputs.gate-steps requires conclusion to record the verdict")
	}
	perms := NewPermissionsParser(job.Permissions).ToPermissions()
	if level, exists := perms.Get(PermissionActions); !exists || level == PermissionNone {
		perms.Set(PermissionActions, PermissionRead)
	}
	job.Permissions = perms.RenderToYAML()
	job.Steps = append(job.Steps, `      - name: Record write gate verdict
        id: record_write_gate
        if: always()
        uses: `+c.getActionPin("actions/github-script")+`
        env:
          GH_AW_WRITE_GATE: ${{ needs.safe_outputs.outputs.write_gate }}
          GH_AW_WRITE_GATE_REASON: ${{ needs.safe_outputs.outputs.write_gate_reason }}
          GH_AW_WRITE_GATE_JOB_RESULT: ${{ needs.safe_outputs.result }}
          GH_AW_WRITE_GATE_REPOSITORY: ${{ github.repository }}
          GH_AW_WRITE_GATE_RUN_ID: ${{ github.run_id }}
          GH_AW_WRITE_GATE_RUN_ATTEMPT: ${{ github.run_attempt }}
          GH_AW_WRITE_GATE_HEAD_SHA: ${{ github.sha }}
          GH_AW_WRITE_GATE_WORKFLOW_SHA: ${{ github.workflow_sha }}
          GH_AW_WRITE_GATE_WORKFLOW_REF: ${{ github.workflow_ref }}
          GH_AW_PENDING_WRITES_SHA256: ${{ needs.safe_outputs.outputs.pending_writes_sha256 }}
          GH_AW_WRITE_GATE_DECISIONS_SHA256: ${{ needs.safe_outputs.outputs.write_gate_decisions_sha256 }}
          GH_AW_PENDING_WRITE_INPUTS_SHA256: ${{ needs.safe_outputs.outputs.pending_write_inputs_sha256 }}
          GH_AW_WRITE_GATE_ITEMS_SHA256: ${{ needs.safe_outputs.outputs.write_gate_items_sha256 }}
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/record_write_gate.cjs');
            await main();
`, `      - name: Upload write gate verdict
        id: upload_write_gate
        if: always() && steps.record_write_gate.outcome == 'success'
        uses: `+c.getActionPin("actions/upload-artifact")+`
        with:
          name: `+artifactPrefixExprForDownstreamJob(data)+`write-gate
          path: /tmp/gh-aw/write-gate/write_gate.json
          if-no-files-found: error
`)
	return nil
}
