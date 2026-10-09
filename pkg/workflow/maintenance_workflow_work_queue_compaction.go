package workflow

import "strings"

// Queue planning has read-only Git access. Applying the plan runs separately
// with write access and must validate the branch head before publication.
func buildMaintenanceWorkQueueCompactionJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  work_queue_compaction_plan:
    name: Plan work queue compaction
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnly()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: read
    outputs:
      plan_created: ${{ steps.plan.outputs.plan_created }}
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	b.WriteString(`      - name: Plan existing work queue
        id: plan
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_WORK_QUEUE_COMPACTION_PLAN_FILE: ${{ runner.temp }}/gh-aw/work-queue-compaction/plan.json
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/work_queue_compaction_plan.cjs');
            await main();
      - name: Upload work queue compaction plan
        if: ${{ steps.plan.outputs.plan_created == 'true' }}
        uses: ` + getActionPin("actions/upload-artifact") + `
        with:
          name: work-queue-compaction-plan
          path: ${{ runner.temp }}/gh-aw/work-queue-compaction/plan.json
          retention-days: 1
          if-no-files-found: error
`)
	b.WriteString(buildMaintenanceWorkQueueCompactionApplyJob(opts, setupActionRef))
	return b.String()
}

func buildMaintenanceWorkQueueCompactionApplyJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  work_queue_compaction_apply:
    name: Apply work queue compaction
    needs: work_queue_compaction_plan
    if: ${{ !cancelled() && needs.work_queue_compaction_plan.outputs.plan_created == 'true' }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	b.WriteString(`      - name: Download work queue compaction plan
        uses: ` + getActionPin("actions/download-artifact") + `
        with:
          name: work-queue-compaction-plan
          path: ${{ runner.temp }}/gh-aw/work-queue-compaction
      - name: Validate and apply work queue compaction
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_WORK_QUEUE_COMPACTION_PLAN_FILE: ${{ runner.temp }}/gh-aw/work-queue-compaction/plan.json
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/work_queue_compaction_apply.cjs');
            await main();
`)
	return b.String()
}
