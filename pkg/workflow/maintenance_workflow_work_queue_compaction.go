package workflow

import "strings"

// buildMaintenanceWorkQueueCompactionJobs emits the trusted job that converts a
// v3 ledger, releases stranded dispatches, checkpoints a long ledger and projects Issues.
func buildMaintenanceWorkQueueCompactionJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	var b strings.Builder
	b.WriteString(`
  work_queue_compaction:
    name: Reconcile and checkpoint work queue
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnly()) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: read
      contents: write
      issues: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	b.WriteString(`      - name: Reconcile and checkpoint work queue
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + "\n")
	writeWorkQueueRepositoryPolicyEnvironment(&b, opts.workQueuePolicy)
	b.WriteString(`        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { maintenance } = require('${{ runner.temp }}/gh-aw/actions/reconcile_work_queue_run.cjs');
            await maintenance({ projectIssues: true });
`)
	return b.String()
}

// workQueueRepositoryPolicyJSON selects the compiled policy for repository-level
// queue jobs. Every participant in a directory compiles the same aw.json settings
// and worker set; a workflow-level Issues setting is preferred when present.
func workQueueRepositoryPolicyJSON(workflowDataList []*WorkflowData) string {
	selected := ""
	for _, data := range workflowDataList {
		if data == nil || data.WorkQueuePolicy == nil || data.WorkQueuePolicy.PolicyJSON == "" {
			continue
		}
		if workQueueIssuesConfig(data) != nil {
			return data.WorkQueuePolicy.PolicyJSON
		}
		if selected == "" {
			selected = data.WorkQueuePolicy.PolicyJSON
		}
	}
	return selected
}

func writeWorkQueueRepositoryPolicyEnvironment(b *strings.Builder, policy string) {
	if policy == "" {
		return
	}
	b.WriteString("        env:\n")
	for _, line := range workQueuePolicyJSONEnvironment(policy) {
		b.WriteString(line)
	}
}
