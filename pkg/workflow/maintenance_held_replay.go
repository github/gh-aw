package workflow

import "strings"

func buildMaintenanceHeldReplayJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	if opts.maintenanceConfig.IsJobDisabled("apply_safe_outputs") {
		return ""
	}
	var result strings.Builder
	result.WriteString(`
  prepare_safe_output_replay:
    name: Prepare safe-output replay approval
    if: ${{ ` + RenderCondition(buildDispatchOperationCondition("safe_outputs")) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: read
      contents: read
    outputs:
      mode: ${{ steps.prepare_replay.outputs.mode }}
      approval_environment: ${{ steps.prepare_replay.outputs.approval_environment }}
      source_run_attempt: ${{ steps.prepare_replay.outputs.source_run_attempt }}
      source_workflow_sha: ${{ steps.prepare_replay.outputs.source_workflow_sha }}
      pending_writes_sha256: ${{ steps.prepare_replay.outputs.pending_writes_sha256 }}
      write_decisions_sha256: ${{ steps.prepare_replay.outputs.write_decisions_sha256 }}
      pending_write_inputs_sha256: ${{ steps.prepare_replay.outputs.pending_write_inputs_sha256 }}
      safe_output_items_sha256: ${{ steps.prepare_replay.outputs.safe_output_items_sha256 }}
    steps:
`)
	writeMaintenanceActionsFolderCheckoutStep(&result)
	writeMaintenanceSetupScriptsStep(&result, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&result, opts, "")
	result.WriteString(`      - name: Resolve original replay approval
        id: prepare_replay
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_RUN_URL: ${{ inputs.run_url }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { prepareHeldReplayFromEnv } = require('${{ runner.temp }}/gh-aw/actions/write_gate_replay_driver.cjs');
            await prepareHeldReplayFromEnv();

  apply_held_safe_outputs:
    name: Apply approved held safe outputs
    needs: [prepare_safe_output_replay]
    if: ${{ needs.prepare_safe_output_replay.result == 'success' && needs.prepare_safe_output_replay.outputs.mode == 'held' && needs.prepare_safe_output_replay.outputs.approval_environment != '' }}
    environment: ${{ needs.prepare_safe_output_replay.outputs.approval_environment }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: read
      contents: write
      discussions: write
      issues: write
      pull-requests: write
    outputs:
      run_url: ${{ steps.record.outputs.run_url }}
    steps:
`)
	writeMaintenanceActionsFolderCheckoutStep(&result)
	writeMaintenanceSetupScriptsStep(&result, setupActionRef)
	writeMaintenanceAdminPermissionsStep(&result, opts, "")
	result.WriteString(`      - name: Apply approved held safe outputs
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_JOB_CHECK_RUN_ID: ${{ job.check_run_id }}
          GH_AW_RUN_URL: ${{ inputs.run_url }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_AW_REPLAY_SOURCE_RUN_ATTEMPT: ${{ needs.prepare_safe_output_replay.outputs.source_run_attempt }}
          GH_AW_REPLAY_SOURCE_WORKFLOW_SHA: ${{ needs.prepare_safe_output_replay.outputs.source_workflow_sha }}
          GH_AW_REPLAY_PENDING_WRITES_SHA256: ${{ needs.prepare_safe_output_replay.outputs.pending_writes_sha256 }}
          GH_AW_REPLAY_WRITE_DECISIONS_SHA256: ${{ needs.prepare_safe_output_replay.outputs.write_decisions_sha256 }}
          GH_AW_REPLAY_PENDING_WRITE_INPUTS_SHA256: ${{ needs.prepare_safe_output_replay.outputs.pending_write_inputs_sha256 }}
          GH_AW_REPLAY_SAFE_OUTPUT_ITEMS_SHA256: ${{ needs.prepare_safe_output_replay.outputs.safe_output_items_sha256 }}
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { restorePendingInputs } = require('${{ runner.temp }}/gh-aw/actions/pending_write_inputs.cjs');
            const { executeHeldReplayFromEnv } = require('${{ runner.temp }}/gh-aw/actions/write_gate_replay_driver.cjs');
            await executeHeldReplayFromEnv({ restorePendingInputs });
      - name: Record approved replay source
        id: record
        env:
          GH_AW_RUN_URL: ${{ inputs.run_url }}
        run: echo "run_url=$GH_AW_RUN_URL" >> "$GITHUB_OUTPUT"
`)
	return result.String()
}
