package workflow

import "strings"

// Planning and applying the work queue checkpoint run in one trusted job.
func buildMaintenanceWorkQueueCompactionJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  work_queue_compaction:
    name: Compact work queue
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnly()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: write
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
      - name: Validate and apply work queue compaction
        if: ${{ steps.plan.outputs.plan_created == 'true' }}
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
