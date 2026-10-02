package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const maintenanceDispatchWorkCoordinatorOperation = "dispatch_work_coordinator_compaction"

type maintenanceDispatchWorkCoordinator struct {
	workflowID string
	config     string
}

func collectMaintenanceDispatchWorkCoordinators(workflowDataList []*WorkflowData) ([]maintenanceDispatchWorkCoordinator, error) {
	sorted := make([]*WorkflowData, 0, len(workflowDataList))
	for _, workflowData := range workflowDataList {
		if workflowData != nil && workflowData.DispatchWorkCoordinator != nil {
			sorted = append(sorted, workflowData)
		}
	}
	slices.SortStableFunc(sorted, func(a, b *WorkflowData) int {
		return strings.Compare(a.WorkflowID, b.WorkflowID)
	})

	coordinators := make([]maintenanceDispatchWorkCoordinator, 0, len(sorted))
	seen := make(map[string]string)
	for _, workflowData := range sorted {
		if workflowData.WorkflowID == "" ||
			workflowData.WorkflowID == "." ||
			workflowData.WorkflowID == ".." ||
			strings.ContainsAny(workflowData.WorkflowID, `/\\`) ||
			strings.ContainsAny(workflowData.WorkflowID, "\x00\r\n") {
			return nil, errors.New("dispatch work coordinator requires a workflow ID")
		}
		if workflowData.DispatchWorkCoordinator.Schema == nil || workflowData.DispatchWorkCoordinator.Schema["type"] != "object" {
			return nil, fmt.Errorf("dispatch work coordinator for workflow %s requires an object schema", workflowData.WorkflowID)
		}
		identity := workflowData.DispatchWorkCoordinator.identityForWorkflow(workflowData.WorkflowID)
		config, err := json.Marshal(map[string]any{
			"identity": identity,
			"schema":   workflowData.DispatchWorkCoordinator.Schema,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to serialize Dispatch Work Coordinator configuration for %s: %w", workflowData.WorkflowID, err)
		}
		if existing, ok := seen[identity]; ok {
			if existing != string(config) {
				return nil, fmt.Errorf("conflicting Dispatch Work Coordinator configurations for workflow %s", workflowData.WorkflowID)
			}
			continue
		}
		seen[identity] = string(config)
		coordinators = append(coordinators, maintenanceDispatchWorkCoordinator{
			workflowID: workflowData.WorkflowID,
			config:     string(config),
		})
	}
	return coordinators, nil
}

func buildMaintenanceDispatchWorkCoordinatorJob(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) string {
	if len(opts.dispatchWorkCoordinators) == 0 {
		return ""
	}

	failed := make([]string, 0, len(opts.dispatchWorkCoordinators))
	var b strings.Builder
	b.WriteString(`
  dispatch_work_coordinator_compaction:
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnlyOrOperation(maintenanceDispatchWorkCoordinatorOperation)) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      actions: read
      contents: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(&b, opts)
	writeMaintenanceSetupScriptsStep(&b, setupActionRef)
	for index, coordinator := range opts.dispatchWorkCoordinators {
		stepID := fmt.Sprintf("coordinator_%d", index)
		failed = append(failed, "steps."+stepID+".outcome == 'failure'")
		b.WriteString(`      - name: Maintain Dispatch Work Coordinator (` + stepID + `)
        id: ` + stepID + `
        continue-on-error: true
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_AW_DISPATCH_WORK_COORDINATOR_CONFIG: |-
            ` + strings.ReplaceAll(coordinator.config, "\n", "\n            ") + `
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/dispatch_work_coordinator_maintenance.cjs');
            await main();

`)
	}
	b.WriteString(`      - name: Report Dispatch Work Coordinator maintenance failures
        if: ${{ ` + strings.Join(failed, " || ") + ` }}
        run: exit 1
`)
	return b.String()
}
