package workflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

// maintenanceCompactLedgerOperation is the Agentic Maintenance operation used for both
// scheduled and explicitly requested ledger compaction. The ledger_request_compaction safe
// output dispatches the maintenance workflow with this operation and a ledger name.
const maintenanceCompactLedgerOperation = "compact_ledger"

// maintenanceLedgerCompactionPlanDir/File are where the untrusted plan job writes, and the trusted
// apply job downloads, the compaction plan artifact.
const (
	maintenanceLedgerCompactionPlanDir  = "${{ runner.temp }}/gh-aw/ledger-compaction"
	maintenanceLedgerCompactionPlanFile = "${{ runner.temp }}/gh-aw/ledger-compaction/plan.json"
)

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
	return map[string]any{
		"name":           ledger.Name,
		"branch_name":    ledger.BranchName,
		"max_record_kb":  ledger.MaxRecordKB,
		"max_segment_kb": ledger.MaxSegmentKB,
		"max_patch_kb":   ledger.MaxPatchKB,
		"compaction":     ledger.Compaction,
	}
}

func encodeLedgerCompactionConfigBase64(ledger LedgerConfig) (string, error) {
	encoded, err := json.Marshal(ledgerCompactionPayload(ledger))
	if err != nil {
		return "", fmt.Errorf("failed to serialize ledger compaction configuration for %s: %w", ledger.Name, err)
	}
	encodedBase64 := base64.StdEncoding.EncodeToString(encoded)
	if len(encodedBase64) > maxLedgerConfigBase64Bytes {
		return "", fmt.Errorf("serialized ledger compaction configuration for %s exceeds the %d-byte environment limit", ledger.Name, maxLedgerConfigBase64Bytes)
	}
	return encodedBase64, nil
}

// buildLedgerCompactionJobCondition selects the plan job on scheduled maintenance, on a
// dispatch/call without an operation, or on an explicit compact_ledger request that targets
// this ledger (or all ledgers when no ledger input is provided).
func buildLedgerCompactionJobCondition(ledgerName string) ConditionNode {
	return BuildAnd(
		buildNotForkAndScheduleOnlyOrOperation(maintenanceCompactLedgerOperation),
		BuildOr(
			BuildNotEquals(BuildPropertyAccess("inputs.operation"), BuildStringLiteral(maintenanceCompactLedgerOperation)),
			BuildOr(
				BuildEquals(BuildPropertyAccess("inputs.ledger"), BuildStringLiteral("")),
				BuildEquals(BuildPropertyAccess("inputs.ledger"), BuildStringLiteral(ledgerName)),
			),
		),
	)
}

// buildMaintenanceLedgerCompactionJobs emits one isolated plan/apply job pair per ledger.
//
// Trust boundary:
//   - ledger_compaction_plan_<name> is untrusted. It has contents: read only, may run the
//     user-configured selection script, and its only durable output is a plan artifact.
//   - ledger_compaction_apply_<name> is trusted. It has contents: write, runs only first-party
//     scripts, never executes the compaction script, and revalidates the hostile plan against the
//     latest ledger state before committing atomically with an expected-head guard.
func buildMaintenanceLedgerCompactionJobs(opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string) (string, error) {
	var b strings.Builder
	for _, ledger := range opts.compactionLedgers {
		config, err := encodeLedgerCompactionConfigBase64(ledger)
		if err != nil {
			return "", err
		}
		job := ledgerCompactionJobSpec{
			ledger:       ledger.Name,
			planJob:      "ledger_compaction_plan_" + ledger.Name,
			applyJob:     "ledger_compaction_apply_" + ledger.Name,
			artifactName: "ledger-compaction-plan-" + ledger.Name,
			planFile:     maintenanceLedgerCompactionPlanFile,
			concurrency: `    concurrency:
      group: gh-aw-ledger-compaction-${{ github.repository }}-` + ledger.Name + `
      cancel-in-progress: false
`,
			env: `          GH_AW_LEDGER_COMPACTION_CONFIG_B64: ` + config + `
          GH_AW_LEDGER_COMPACTION_TRIGGER: ${{ inputs.operation == '` + maintenanceCompactLedgerOperation + `' && 'requested' || 'scheduled' }}
          GH_AW_LEDGER_COMPACTION_PLAN_FILE: ` + maintenanceLedgerCompactionPlanFile + `
`,
			githubScriptPin: getCachedActionPinFromResolver("actions/github-script", opts.resolver),
		}
		writeLedgerCompactionPlanJob(&b, opts, setupActionRef, job)
		writeLedgerCompactionApplyJob(&b, opts, setupActionRef, job)
	}
	return b.String(), nil
}

type ledgerCompactionJobSpec struct {
	ledger, planJob, applyJob, artifactName, planFile, concurrency, env, githubScriptPin string
}

func writeLedgerCompactionPlanJob(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string, job ledgerCompactionJobSpec) {
	b.WriteString(`
  ` + job.planJob + `:
    if: ${{ ` + RenderCondition(buildLedgerCompactionJobCondition(job.ledger)) + ` }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: read
` + job.concurrency + `    outputs:
      plan_created: ${{ steps.plan.outputs.plan_created }}
      plan_id: ${{ steps.plan.outputs.plan_id }}
    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(b, opts)
	writeMaintenanceSetupScriptsStep(b, setupActionRef)
	b.WriteString(`      - name: Plan ledger compaction (untrusted, read-only)
        id: plan
        uses: ` + job.githubScriptPin + `
        env:
          GH_TOKEN: ${{ github.token }}
` + job.env + `        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { main } = require('${{ runner.temp }}/gh-aw/actions/ledger_compaction_plan.cjs');
            await main();

      - name: Upload ledger compaction plan
        if: ${{ steps.plan.outputs.plan_created == 'true' }}
        uses: ` + getActionPin("actions/upload-artifact") + `
        with:
          name: ` + job.artifactName + `
          path: ` + job.planFile + `
          retention-days: 1
          if-no-files-found: error
`)
}

func writeLedgerCompactionApplyJob(b *strings.Builder, opts buildMaintenanceWorkflowYAMLOptions, setupActionRef string, job ledgerCompactionJobSpec) {
	b.WriteString(`
  ` + job.applyJob + `:
    needs: ` + job.planJob + `
    if: ${{ needs.` + job.planJob + `.outputs.plan_created == 'true' }}
    runs-on: ` + opts.runsOnValue + `
    permissions:
      contents: write
` + job.concurrency + `    steps:
`)
	writeMaintenanceConditionalActionsCheckoutStep(b, opts)
	writeMaintenanceSetupScriptsStep(b, setupActionRef)
	b.WriteString(`      - name: Download ledger compaction plan
        uses: ` + getActionPin("actions/download-artifact") + `
        with:
          name: ` + job.artifactName + `
          path: ` + maintenanceLedgerCompactionPlanDir + `

      - name: Validate and apply ledger compaction (trusted)
        uses: ` + job.githubScriptPin + `
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
