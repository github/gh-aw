//go:build !integration

package workflow

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLedgerCompactionConfigParsing(t *testing.T) {
	t.Run("defaults to daily maintenance compaction", func(t *testing.T) {
		config, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{}})
		require.NoError(t, err)
		require.Len(t, config.Ledgers, 1)
		assert.Equal(t, &LedgerCompactionConfig{Schedule: "daily", MinSegments: 32, MaxSegments: 128}, config.Ledgers[0].Compaction)
		assert.Len(t, config.compactionEnabledLedgers(), 1)
	})

	t.Run("supports per-ledger policies and opt-out", func(t *testing.T) {
		config, err := parseLedgerToolConfig(map[string]any{
			"findings": map[string]any{"compaction": map[string]any{"schedule": "weekly", "min-segments": 4, "script": "return { sources: [] }"}},
			"metrics":  map[string]any{"compaction": false},
		})
		require.NoError(t, err)
		require.Len(t, config.Ledgers, 2)
		assert.Equal(t, &LedgerCompactionConfig{Schedule: "weekly", MinSegments: 4, MaxSegments: 128, Script: "return { sources: [] }"}, config.Ledgers[0].Compaction)
		assert.Nil(t, config.Ledgers[1].Compaction)
		enabled := config.compactionEnabledLedgers()
		require.Len(t, enabled, 1)
		assert.Equal(t, "findings", enabled[0].Name)
	})

	t.Run("treats a compaction policy as the single-ledger form", func(t *testing.T) {
		config, err := parseLedgerToolConfig(map[string]any{"compaction": map[string]any{"schedule": "manual"}})
		require.NoError(t, err)
		require.Len(t, config.Ledgers, 1)
		assert.Equal(t, defaultLedgerName, config.Ledgers[0].Name)
		assert.Equal(t, "manual", config.Ledgers[0].Compaction.Schedule)
	})

	t.Run("raises max-segments to min-segments when only min is set", func(t *testing.T) {
		compaction, err := parseLedgerCompactionConfig("findings", map[string]any{"min-segments": 200})
		require.NoError(t, err)
		assert.Equal(t, 200, compaction.MaxSegments)
	})

	for name, value := range map[string]any{
		"unknown schedule":   map[string]any{"schedule": "hourly"},
		"cron schedule":      map[string]any{"schedule": "0 0 * * *"},
		"too few segments":   map[string]any{"min-segments": 1},
		"too many segments":  map[string]any{"max-segments": 1000},
		"max below min":      map[string]any{"min-segments": 10, "max-segments": 5},
		"unknown property":   map[string]any{"command": "git push"},
		"expression script":  map[string]any{"script": "return ${{ secrets.TOKEN }}"},
		"empty script":       map[string]any{"script": " "},
		"non-object setting": "daily",
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := parseLedgerCompactionConfig("findings", value)
			require.Error(t, err)
		})
	}

	t.Run("excludes compaction policy from the agent-facing ledger config", func(t *testing.T) {
		config, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{"compaction": map[string]any{"script": "return { sources: [] }"}}})
		require.NoError(t, err)
		encoded, err := encodeLedgerConfigBase64(config)
		require.NoError(t, err)
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		assert.NotContains(t, string(decoded), "compaction")
	})
}

func TestLedgerPromptDescribesMaintenanceOwnedCompaction(t *testing.T) {
	enabled, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{}})
	require.NoError(t, err)
	section := buildLedgerPromptSection(enabled)
	require.NotNil(t, section)
	assert.Contains(t, section.Content, "Ledger compaction is owned by Agentic Maintenance")
	assert.Contains(t, section.Content, "ledger request compaction safe output")

	disabled, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{"compaction": false}})
	require.NoError(t, err)
	section = buildLedgerPromptSection(disabled)
	require.NotNil(t, section)
	assert.Contains(t, section.Content, "Ledger compaction is owned by Agentic Maintenance")
	assert.NotContains(t, section.Content, "ledger request compaction safe output")
}

func TestLedgerRequestCompactionHandlerConfig(t *testing.T) {
	assert.Nil(t, buildLedgerRequestCompactionHandlerConfig(nil))
	disabled, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{"compaction": false}})
	require.NoError(t, err)
	assert.Nil(t, buildLedgerRequestCompactionHandlerConfig(disabled))

	config, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{}, "metrics": map[string]any{}})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"max": 2, "ledgers": []string{"findings", "metrics"}, "workflow": "agentics-maintenance.yml"}, buildLedgerRequestCompactionHandlerConfig(config))
}

func TestGenerateMaintenanceWorkflow_LedgerCompaction(t *testing.T) {
	findings, err := parseLedgerToolConfig(map[string]any{
		"findings": map[string]any{"compaction": map[string]any{"script": "return { sources: [] }"}},
		"metrics":  map[string]any{"compaction": false},
	})
	require.NoError(t, err)
	audit, err := parseLedgerToolConfig(map[string]any{"audit": map[string]any{"compaction": map[string]any{"schedule": "weekly"}}})
	require.NoError(t, err)

	tmpDir := t.TempDir()
	err = GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{Name: "wf1", WorkflowID: "wf1", LedgerConfig: findings},
			{Name: "wf2", WorkflowID: "wf2", LedgerConfig: audit},
		},
		WorkflowDir: tmpDir,
		Version:     "v1.0.0",
		ActionMode:  ActionModeDev,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.NoError(t, err, "maintenance workflow should be generated for ledgers even without expiring safe outputs")
	yaml := string(content)

	jobs := parseMaintenanceJobs(t, yaml)
	plan := jobs["ledger_compaction_plan"]
	apply := jobs["ledger_compaction_apply"]
	require.NotEmpty(t, plan)
	require.NotEmpty(t, apply)
	for name := range jobs {
		assert.False(t, strings.HasPrefix(name, "ledger_compaction_plan_") || strings.HasPrefix(name, "ledger_compaction_apply_"), "unexpected per-ledger job %s", name)
	}
	assert.Contains(t, plan, "contents: read")
	assert.NotContains(t, plan, "contents: write")
	assert.NotContains(t, plan, "secrets.GITHUB_TOKEN")
	assert.Contains(t, plan, "ledger_compaction_plan.cjs")
	assert.NotContains(t, plan, "ledger_compaction_apply.cjs")
	assert.Contains(t, plan, "GH_AW_LEDGER_COMPACTION_CONFIG: |-")
	assert.NotContains(t, plan, "GH_AW_LEDGER_COMPACTION_CONFIG_B64")
	assert.Equal(t, 1, strings.Count(plan, "upload-artifact"))
	assert.Contains(t, plan, "steps.plan_0.outputs.plan_created == 'true' || steps.plan_1.outputs.plan_created == 'true'")
	assert.Contains(t, plan, "steps.plan_0.outcome == 'failure' || steps.plan_1.outcome == 'failure'")
	assert.Contains(t, plan, "plans_uploaded: ${{ steps.upload_plans.outcome == 'success' }}")
	assert.Equal(t, 2, strings.Count(plan, "continue-on-error: true"))
	assert.Equal(t, 2, strings.Count(plan, "timeout-minutes: 15"))
	assert.Contains(t, apply, "needs: ledger_compaction_plan")
	assert.Contains(t, apply, "!cancelled() && needs.ledger_compaction_plan.outputs.plans_uploaded == 'true' && needs.ledger_compaction_plan.outputs.plan_created == 'true'")
	assert.Contains(t, apply, "contents: write")
	assert.Equal(t, 1, strings.Count(apply, "download-artifact"))
	assert.Contains(t, apply, "ledger_compaction_apply.cjs")
	assert.NotContains(t, apply, "ledger_compaction_plan.cjs")
	assert.Contains(t, apply, "steps.apply_0.outcome == 'failure' || steps.apply_1.outcome == 'failure'")
	assert.Equal(t, 2, strings.Count(apply, "continue-on-error: true"))
	for _, body := range []string{plan, apply} {
		assert.NotContains(t, body, "concurrency:")
		assert.Contains(t, body, "name: ledger-compaction-plans")
	}
	for i, name := range []string{"audit", "findings"} {
		stepID := "plan_" + strconv.Itoa(i)
		assert.Contains(t, plan, "inputs.ledger == '"+name+"'")
		assert.Contains(t, plan, "id: "+stepID)
		assert.Contains(t, plan, stepID+"_created: ${{ steps."+stepID+".outputs.plan_created }}")
		assert.Contains(t, apply, "needs.ledger_compaction_plan.outputs."+stepID+"_created == 'true'")
		for _, body := range []string{plan, apply} {
			assert.Contains(t, body, "GH_AW_LEDGER_COMPACTION_PLAN_FILE: ${{ runner.temp }}/gh-aw/ledger-compaction/plan-"+strconv.Itoa(i)+".json")
		}
	}
	assert.NotContains(t, plan, "plan-metrics.json", "ledgers with compaction disabled get no maintenance steps")
	assert.Contains(t, yaml, "- 'compact_ledger'")
	assert.Contains(t, yaml, "ledger:")
	assert.Contains(t, jobs["run_operation"], "'compact_ledger'", "the generic operation job must not handle compact_ledger")
	assert.Contains(t, yaml, "schedule:")
}

func TestGenerateMaintenanceWorkflow_NoLedgerCompactionInputsWithoutLedgers(t *testing.T) {
	tmpDir := t.TempDir()
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{{Name: "wf1", SafeOutputs: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{Expires: 48}}}},
		WorkflowDir:      tmpDir,
		Version:          "v1.0.0",
		ActionMode:       ActionModeDev,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(content), "compact_ledger")
	assert.NotContains(t, string(content), "ledger_compaction_")
	assert.NotContains(t, string(content), "dispatch_work_coordinator_compaction")
}

func TestGenerateMaintenanceWorkflow_DispatchWorkCoordinatorMaintenance(t *testing.T) {
	tmpDir := t.TempDir()
	err := GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{
				Name:       "worker",
				WorkflowID: "worker",
				DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{
					ID: "shared-queue",
					Schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"title": map[string]any{"type": "string"},
						},
					},
				},
			},
			{
				Name:       "dispatcher",
				WorkflowID: "dispatcher",
				DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{
					ID: "shared-queue",
					Schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"title": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		WorkflowDir: tmpDir,
		Version:     "v1.0.0",
		ActionMode:  ActionModeDev,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.NoError(t, err)
	yaml := string(content)

	jobs := parseMaintenanceJobs(t, yaml)
	job := jobs["dispatch_work_coordinator_compaction"]
	require.NotEmpty(t, job)
	assert.Equal(t, 1, strings.Count(yaml, "\n  dispatch_work_coordinator_compaction:\n"))
	assert.Equal(t, 1, strings.Count(job, "Maintain Dispatch Work Coordinator"))
	assert.Contains(t, job, "actions: read")
	assert.Contains(t, job, "contents: write")
	assert.Contains(t, job, "dispatch_work_coordinator_maintenance.cjs")
	assert.Contains(t, job, `"identity":"shared-queue"`)
	assert.Contains(t, job, `"schema":{"properties":{"title":{"type":"string"}},"type":"object"}`)
	assert.Contains(t, yaml, "'dispatch_work_coordinator_compaction'")
	assert.Contains(t, jobs["run_operation"], "inputs.operation != 'dispatch_work_coordinator_compaction'")
}

func TestGenerateMaintenanceWorkflow_RejectsSharedCoordinatorIDWithDifferentSchemas(t *testing.T) {
	_, err := collectMaintenanceDispatchWorkCoordinators([]*WorkflowData{
		{
			WorkflowID: "worker",
			DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{
				ID:     "shared-queue",
				Schema: map[string]any{"type": "object"},
			},
		},
		{
			WorkflowID: "dispatcher",
			DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{
				ID:     "shared-queue",
				Schema: map[string]any{"type": "object", "required": []any{"title"}},
			},
		},
	})
	require.ErrorContains(t, err, "conflicting Dispatch Work Coordinator configurations")
}

func TestGenerateMaintenanceWorkflow_LedgerCompaction_CaseSensitivity(t *testing.T) {
	fooLedger, err := parseLedgerToolConfig(map[string]any{"foo": map[string]any{}})
	require.NoError(t, err)
	upperFooLedger, err := parseLedgerToolConfig(map[string]any{"Foo": map[string]any{}})
	require.NoError(t, err)

	tmpDir := t.TempDir()
	err = GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: []*WorkflowData{
			{Name: "wf1", WorkflowID: "wf1", LedgerConfig: fooLedger},
			{Name: "wf2", WorkflowID: "wf2", LedgerConfig: upperFooLedger},
		},
		WorkflowDir: tmpDir,
		Version:     "v1.0.0",
		ActionMode:  ActionModeDev,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(tmpDir, "agentics-maintenance.yml"))
	require.NoError(t, err)
	yaml := string(content)

	jobs := parseMaintenanceJobs(t, yaml)
	plan := jobs["ledger_compaction_plan"]
	apply := jobs["ledger_compaction_apply"]
	require.NotEmpty(t, plan)
	require.NotEmpty(t, apply)
	assert.Contains(t, plan, "inputs.ledger == 'foo'")
	assert.Contains(t, plan, "inputs.ledger == 'Foo'")
	for _, filename := range []string{"plan-0.json", "plan-1.json"} {
		assert.Contains(t, plan, filename)
		assert.Contains(t, apply, filename)
	}
	assert.NotContains(t, plan, "plan-foo.json")
	assert.NotContains(t, plan, "plan-Foo.json")
}

func TestComputeEnabledToolNames_LedgerNilSafety(t *testing.T) {
	t.Run("nil SafeOutputs and nil LedgerConfig does not panic", func(t *testing.T) {
		names := computeEnabledToolNames(&WorkflowData{SafeOutputs: nil, LedgerConfig: nil})
		assert.Empty(t, names)
	})

	t.Run("nil SafeOutputs and disabled LedgerConfig does not panic", func(t *testing.T) {
		names := computeEnabledToolNames(&WorkflowData{SafeOutputs: nil, LedgerConfig: &LedgerToolConfig{}})
		assert.Empty(t, names)
	})

	t.Run("nil SafeOutputs and enabled LedgerConfig with compaction enables request tool", func(t *testing.T) {
		config, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{}})
		require.NoError(t, err)
		names := computeEnabledToolNames(&WorkflowData{SafeOutputs: nil, LedgerConfig: config})
		assert.Contains(t, names, "ledger_append")
		assert.Contains(t, names, "ledger_request_compaction")
	})
}

// parseMaintenanceJobs splits the generated maintenance workflow into top-level job bodies.
func parseMaintenanceJobs(t *testing.T, content string) map[string]string {
	t.Helper()
	_, jobsSection, found := strings.Cut(content, "\njobs:\n")
	require.True(t, found)
	jobs := make(map[string]string)
	current := ""
	for line := range strings.SplitSeq(jobsSection, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(line, ":") {
			current = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		if current != "" {
			jobs[current] += line + "\n"
		}
	}
	return jobs
}
