//go:build !integration

package workflow

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWriteGateConclusionCompiledTransport(t *testing.T) {
	for _, reusable := range []bool{false, true} {
		for _, ghes := range []bool{false, true} {
			t.Run(strconv.FormatBool(reusable)+"/ghes="+strconv.FormatBool(ghes), func(t *testing.T) {
				trigger := "workflow_dispatch"
				if reusable {
					trigger = "workflow_call"
				}
				source := "---\non: " + trigger + "\nengine: claude\nstrict: false\nsafe-outputs:\n  add-comment:\njobs:\n  safe_outputs:\n    gate-steps:\n      - run: echo '{\"decision\":\"deny\",\"reason\":\"policy\"}' > \"$GH_AW_WRITE_DECISIONS\"\n---\nRead the issue.\n"
				compiler := NewCompiler()
				compiler.SetGHESCompat(ghes)
				compiler.configureGHESCompatibility()
				path := filepath.Join(t.TempDir(), "gate.md")
				data, err := compiler.ParseWorkflowString(source, path)
				require.NoError(t, err)
				output, err := compiler.CompileToYAML(data, path)
				require.NoError(t, err)
				var document map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(output), &document))
				conclusion := document["jobs"].(map[string]any)["conclusion"].(map[string]any)
				env := conclusion["env"].(map[string]any)
				for name, bound := range map[string]string{
					"GH_AW_WRITE_GATE":                  "write_gate",
					"GH_AW_WRITE_GATE_REASON":           "write_gate_reason",
					"GH_AW_PENDING_WRITES_SHA256":       "pending_writes_sha256",
					"GH_AW_PENDING_WRITES_COMPLETE":     "pending_writes_complete",
					"GH_AW_WRITE_GATE_DECISIONS_SHA256": "write_gate_decisions_sha256",
				} {
					require.Equal(t, "${{ needs.safe_outputs.outputs."+bound+" }}", env[name])
				}
				require.Equal(t, "${{ needs.safe_outputs.result }}", env["GH_AW_WRITE_GATE_JOB_RESULT"])
				require.Equal(t, "[]", env["GH_AW_WRITE_GATE_WRITE_JOB_NAMES"])
				require.Equal(t, "{}", env["GH_AW_WRITE_GATE_PUBLICATION_RESULTS"])
				steps := conclusion["steps"].([]any)
				indices := make(map[string]int)
				var usageIndex int
				for index, raw := range steps {
					step := raw.(map[string]any)
					if id, ok := step["id"].(string); ok {
						indices[id] = index
					}
					if step["name"] == "Generate usage activity summary and unified session" {
						usageIndex = index
					}
				}
				for _, id := range []string{"download_write_gate_pending_writes", "download_write_gate_write_decisions", "check_write_gate", "handle_agent_failure", "record_write_gate"} {
					require.Contains(t, indices, id)
				}
				for _, artifact := range []string{"pending-writes", "write-decisions"} {
					id := "download_write_gate_" + strings.ReplaceAll(artifact, "-", "_")
					step := steps[indices[id]].(map[string]any)
					require.Equal(t, "always()", step["if"])
					with := step["with"].(map[string]any)
					require.Equal(t, artifactPrefixExprForDownstreamJob(data)+artifact, with["name"])
					require.Equal(t, "/tmp/gh-aw/"+artifact+"/", with["path"])
					reference, _, _ := strings.Cut(compiler.getActionPin("actions/download-artifact"), " #")
					require.Equal(t, reference, step["uses"])
					require.Less(t, indices[id], indices["check_write_gate"])
				}
				check := steps[indices["check_write_gate"]].(map[string]any)
				require.Equal(t, "always()", check["if"])
				require.NotContains(t, check, "continue-on-error")
				require.Contains(t, check["with"].(map[string]any)["script"], "checkConclusion({ core })")
				require.Less(t, indices["check_write_gate"], indices["handle_agent_failure"])
				require.Less(t, indices["check_write_gate"], usageIndex)
				require.Less(t, usageIndex, indices["record_write_gate"])
				require.Contains(t, env["GH_AW_JOB_RESULTS"], "needs.safe_outputs.result")
			})
		}
	}
}

func TestWriteGateConclusionRegistryReceiptsRemainData(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	conclusion := &Job{Name: "conclusion", Env: map[string]string{"EXISTING": `"kept"`}}
	for _, job := range []*Job{
		conclusion, {Name: "safe_outputs"},
		{Name: "publish_result", Outputs: map[string]string{"private_payload": "PRIVATE_BODY"}},
		{Name: "push_repo_memory"}, {Name: "push_experiments_state"},
	} {
		require.NoError(t, compiler.jobManager.AddJob(job))
	}
	require.NoError(t, compiler.finalizeWriteGateConclusionEnv(writeGateTestData()))
	require.Equal(t, `"kept"`, conclusion.Env["EXISTING"])
	names, err := strconv.Unquote(conclusion.Env["GH_AW_WRITE_GATE_WRITE_JOB_NAMES"])
	require.NoError(t, err)
	require.JSONEq(t, `["publish_result","push_repo_memory"]`, names)
	template, err := strconv.Unquote(conclusion.Env["GH_AW_WRITE_GATE_PUBLICATION_RESULTS"])
	require.NoError(t, err)
	require.NotContains(t, template, "private_payload")
	require.NotContains(t, template, "PRIVATE_BODY")
	require.NotContains(t, template, "push_experiments_state")
	for _, name := range []string{"publish_result", "push_repo_memory"} {
		for _, field := range []string{"pending_writes_verified", "pending_writes_item_sha256"} {
			value := "true"
			if field == "pending_writes_item_sha256" {
				value = "{\"job_id\":\"" + strings.Repeat("a", 64) + "\"}\n"
			}
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			expression := "${{ toJSON(needs." + name + ".outputs." + field + ") }}"
			require.Contains(t, template, expression)
			template = strings.ReplaceAll(template, expression, string(encoded))
		}
	}
	var receipts map[string]map[string]string
	require.NoError(t, json.Unmarshal([]byte(template), &receipts))
	require.Equal(t, "true", receipts["publish_result"]["pending_writes_verified"])
	require.Equal(t, "{\"job_id\":\""+strings.Repeat("a", 64)+"\"}\n", receipts["push_repo_memory"]["pending_writes_item_sha256"])
}

func TestWriteGateConclusionUngatedIsUntouched(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	conclusion := &Job{Name: "conclusion", Env: map[string]string{"EXISTING": `"kept"`}}
	require.NoError(t, compiler.jobManager.AddJob(conclusion))
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}
	require.NoError(t, compiler.finalizeWriteGateConclusionEnv(data))
	require.Equal(t, map[string]string{"EXISTING": `"kept"`}, conclusion.Env)
	steps, err := compiler.buildWriteGateConclusionInputSteps(data)
	require.NoError(t, err)
	require.Empty(t, steps)
}

func TestWriteGateConclusionCallReceiptUsesVerificationJob(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	conclusion := &Job{Name: "conclusion"}
	for _, job := range []*Job{
		conclusion,
		{Name: "safe_outputs"},
		{Name: "call-worker-one", Uses: "owner/repo/.github/workflows/worker.yml@main"},
		{Name: "verify-pending-call-worker-one"},
	} {
		require.NoError(t, compiler.jobManager.AddJob(job))
	}
	require.NoError(t, compiler.finalizeWriteGateConclusionEnv(writeGateTestData()))
	require.Contains(t, conclusion.Needs, "verify-pending-call-worker-one")
	metadata, err := strconv.Unquote(conclusion.Env["GH_AW_WRITE_GATE_PUBLICATION_RESULTS"])
	require.NoError(t, err)
	require.Contains(t, metadata, `"call-worker-one"`)
	require.Contains(t, metadata, "toJSON(needs.verify-pending-call-worker-one.outputs.pending_writes_verified)")
	require.NotContains(t, metadata, "toJSON(needs.call-worker-one.outputs.pending_writes_verified)")
}
