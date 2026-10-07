package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

// maintenanceCompactLedgerOperation is the Agentic Maintenance operation used for both
// scheduled and explicitly requested ledger compaction. The ledger_request_compaction safe
// output dispatches the maintenance workflow with this operation and a ledger name.
const maintenanceCompactLedgerOperation = "compact_ledger"

// maintenanceLedgerCompactionPlanDir is where the untrusted plan job writes, and the
// trusted apply job downloads, the per-ledger compaction plans.
const maintenanceLedgerCompactionPlanDir = "${{ runner.temp }}/gh-aw/ledger-compaction"

// collectMaintenanceCompactionLedgers returns the compaction-enabled ledgers across all
// workflows, deduplicated by ledger name (ledgers are stored on shared ledgers/<name>
// branches). When workflows declare the same ledger with different compaction policies, the
// first workflow in WorkflowID order wins and a warning is emitted.
func collectMaintenanceCompactionLedgers(workflowDataList []*WorkflowData) []LedgerConfig {
	sorted := make([]*WorkflowData, 0, len(workflowDataList))
	for _, workflowData := range workflowDataList {
		if workflowData != nil && workflowData.LedgerConfig != nil {
			sorted = append(sorted, workflowData)
		}
	}
	slices.SortStableFunc(sorted, func(a, b *WorkflowData) int { return strings.Compare(a.WorkflowID, b.WorkflowID) })
	byName := make(map[string]LedgerConfig)
	owners := make(map[string]string)
	for _, workflowData := range sorted {
		for _, ledger := range workflowData.LedgerConfig.compactionEnabledLedgers() {
			key := ledger.Name
			existing, ok := byName[key]
			if !ok {
				byName[key] = ledger
				owners[key] = workflowData.WorkflowID
				continue
			}
			if !reflect.DeepEqual(ledgerCompactionPayload(existing), ledgerCompactionPayload(ledger)) {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf(
					"Ledger '%s' is configured differently in workflows '%s' and '%s'; Agentic Maintenance uses the compaction settings from '%s'.",
					ledger.Name, owners[key], workflowData.WorkflowID, owners[key])))
			}
		}
	}
	ledgers := make([]LedgerConfig, 0, len(byName))
	for _, ledger := range byName {
		ledgers = append(ledgers, ledger)
	}
	slices.SortFunc(ledgers, func(a, b LedgerConfig) int { return strings.Compare(a.Name, b.Name) })
	if len(ledgers) > 0 {
		maintenanceLog.Printf("Found %d compaction-enabled ledger(s) for agentic maintenance", len(ledgers))
	}
	return ledgers
}

// ledgerCompactionPayload is the trusted per-ledger configuration consumed by
// actions/setup/js/ledger_compaction.cjs (parseCompactionConfig).
func ledgerCompactionPayload(ledger LedgerConfig) map[string]any {
	payload := map[string]any{
		"name":           ledger.Name,
		"branch_name":    ledger.BranchName,
		"max_record_kb":  ledger.MaxRecordKB,
		"max_segment_kb": ledger.MaxSegmentKB,
		"max_patch_kb":   ledger.MaxPatchKB,
		"compaction":     ledger.Compaction,
	}
	if ledger.Type != "" {
		payload["type"] = ledger.Type
		payload["key"] = ledger.Key
		payload["schema"] = ledger.Schema
	}
	return payload
}

func encodeLedgerCompactionConfig(ledger LedgerConfig) (string, error) {
	encoded, err := json.MarshalIndent(ledgerCompactionPayload(ledger), "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize ledger compaction configuration for %s: %w", ledger.Name, err)
	}
	config := string(encoded)
	if len(config) > maxLedgerConfigBase64Bytes {
		return "", fmt.Errorf("serialized ledger compaction configuration for %s exceeds the %d-byte environment limit", ledger.Name, maxLedgerConfigBase64Bytes)
	}
	return config, nil
}

// buildLedgerCompactionStepCondition selects a ledger on scheduled maintenance,
// on a dispatch/call without an operation, or on an explicit request for that ledger.
func buildLedgerCompactionStepCondition(ledgerName string) ConditionNode {
	return BuildOr(
		BuildNotEquals(BuildPropertyAccess("inputs.operation"), BuildStringLiteral(maintenanceCompactLedgerOperation)),
		BuildOr(
			BuildEquals(BuildPropertyAccess("inputs.ledger"), BuildStringLiteral("")),
			BuildEquals(BuildPropertyAccess("inputs.ledger"), BuildStringLiteral(ledgerName)),
		),
	)
}

// buildMaintenanceLedgerCompactionJobs emits one plan/apply job pair for all ledgers.
//
// Trust boundary:
//   - ledger_compaction_plan is untrusted. It has contents: read only, uses built-in
//     segment selection, and its only durable output is a plan artifact.
//   - ledger_compaction_apply is trusted. It has contents: write, runs only first-party
//     scripts and revalidates each hostile plan
//     against the latest ledger state before committing with an expected-head guard.
func buildMaintenanceLedgerCompactionJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) (string, error) {
	if len(opts.compactionLedgers) == 0 {
		return "", nil
	}
	jobs := make([]ledgerCompactionJobSpec, 0, len(opts.compactionLedgers))
	for i, ledger := range opts.compactionLedgers {
		config, err := encodeLedgerCompactionConfig(ledger)
		if err != nil {
			return "", err
		}
		planFile := path.Join(maintenanceLedgerCompactionPlanDir, fmt.Sprintf("plan-%d.json", i))
		jobs = append(jobs, ledgerCompactionJobSpec{
			ledger: ledger.Name,
			stepID: fmt.Sprintf("plan_%d", i),
			env: `          GH_AW_LEDGER_COMPACTION_CONFIG: |-
            ` + strings.ReplaceAll(config, "\n", "\n            ") + `
          GH_AW_LEDGER_COMPACTION_TRIGGER: ${{ inputs.operation == '` + maintenanceCompactLedgerOperation + `' && 'requested' || 'scheduled' }}
          GH_AW_LEDGER_COMPACTION_PLAN_FILE: ` + planFile + `
`,
		})
	}
	var b strings.Builder
	writeLedgerCompactionPlanJob(&b, opts, setupActionRef, jobs)
	writeLedgerCompactionApplyJob(&b, opts, setupActionRef, jobs)
	return b.String(), nil
}

type ledgerCompactionJobSpec struct {
	ledger, stepID, env string
}

func writeLedgerCompactionPlanJob(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string, jobs []ledgerCompactionJobSpec) {
	created := make([]string, 0, len(jobs))
	for _, job := range jobs {
		created = append(created, "steps."+job.stepID+".outputs.plan_created == 'true'")
	}
	failed := make([]string, 0, len(jobs))
	for _, job := range jobs {
		failed = append(failed, "steps."+job.stepID+".outcome == 'failure'")
	}
	b.WriteString(`
  ledger_compaction_plan:
    name: Plan ledger compaction
    if: ${{ ` + RenderCondition(buildNotForkAndScheduleOnlyOrOperation(maintenanceCompactLedgerOperation)) + ` }}
    runs-on: ` + opts.runsOnValue + `
    # Planning treats ledger content as untrusted and must remain read-only.
    permissions:
      contents: read
    outputs:
      plan_created: ${{ ` + strings.Join(created, " || ") + ` }}
      plans_uploaded: ${{ steps.upload_plans.outcome == 'success' }}
`)
	for _, job := range jobs {
		b.WriteString("      " + job.stepID + "_created: ${{ steps." + job.stepID + ".outputs.plan_created }}\n")
	}
	b.WriteString(`    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(b, opts)
	writeMaintenanceSetupScriptsStep(b, setupActionRef)
	for _, job := range jobs {
		b.WriteString(`      - name: Plan ledger compaction (` + job.ledger + `, untrusted, read-only)
        if: ${{ ` + RenderCondition(buildLedgerCompactionStepCondition(job.ledger)) + ` }}
        continue-on-error: true
        timeout-minutes: 15
        id: ` + job.stepID + `
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_TOKEN: ${{ github.token }}
` + job.env + `        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/ledger_compaction_plan.cjs');
            await main();

`)
	}
	b.WriteString(`      - name: Upload ledger compaction plans
        if: ${{ ` + strings.Join(created, " || ") + ` }}
        id: upload_plans
        uses: ` + getActionPin("actions/upload-artifact") + `
        with:
          name: ledger-compaction-plans
          path: ` + maintenanceLedgerCompactionPlanDir + `
          retention-days: 1
          if-no-files-found: error
`)
	b.WriteString(`      - name: Report ledger compaction planning failures
        if: ${{ ` + strings.Join(failed, " || ") + ` }}
        run: exit 1
`)
}

func writeLedgerCompactionApplyJob(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string, jobs []ledgerCompactionJobSpec) {
	failed := make([]string, 0, len(jobs))
	for _, job := range jobs {
		applyStepID := "apply_" + strings.TrimPrefix(job.stepID, "plan_")
		failed = append(failed, "steps."+applyStepID+".outcome == 'failure'")
	}
	b.WriteString(`
  ledger_compaction_apply:
    name: Apply ledger compaction
    needs: ledger_compaction_plan
    if: ${{ !cancelled() && needs.ledger_compaction_plan.outputs.plans_uploaded == 'true' && needs.ledger_compaction_plan.outputs.plan_created == 'true' }}
    runs-on: ` + opts.runsOnValue + `
    # Applying a validated plan commits updated ledger content.
    permissions:
      contents: write
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(b, opts)
	writeMaintenanceSetupScriptsStep(b, setupActionRef)
	b.WriteString(`      - name: Download ledger compaction plans
        uses: ` + getActionPin("actions/download-artifact") + `
        with:
          name: ledger-compaction-plans
          path: ` + maintenanceLedgerCompactionPlanDir + `
`)
	for _, job := range jobs {
		applyStepID := "apply_" + strings.TrimPrefix(job.stepID, "plan_")
		b.WriteString(`
      - name: Validate and apply ledger compaction (` + job.ledger + `, trusted)
        if: ${{ needs.ledger_compaction_plan.outputs.` + job.stepID + `_created == 'true' }}
        id: ` + applyStepID + `
        continue-on-error: true
        uses: ` + getCachedActionPinFromResolver("actions/github-script", opts.resolver) + `
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
` + job.env + `        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/ledger_compaction_apply.cjs');
            await main();
`)
	}
	b.WriteString(`      - name: Report ledger compaction application failures
        if: ${{ ` + strings.Join(failed, " || ") + ` }}
        run: exit 1
`)
}

// ledgerRequestCompactionHandlerKey is the safe-output type that asks Agentic Maintenance to
// compact a ledger. It only dispatches maintenance; it never compacts directly.
const ledgerRequestCompactionHandlerKey = "ledger_request_compaction"

// buildLedgerRequestCompactionHandlerConfig returns the handler configuration for
// ledger_request_compaction, or nil when no ledger has compaction enabled.
func buildLedgerRequestCompactionHandlerConfig(config *LedgerToolConfig) map[string]any {
	if config == nil || !config.Enabled() {
		return nil
	}
	ledgers := config.compactionEnabledLedgers()
	if len(ledgers) == 0 {
		return nil
	}
	names := make([]string, 0, len(ledgers))
	for _, ledger := range ledgers {
		names = append(names, ledger.Name)
	}
	return map[string]any{
		"max":      len(names),
		"ledgers":  names,
		"workflow": "agentics-maintenance.yml",
	}
}
