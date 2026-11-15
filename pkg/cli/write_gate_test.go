//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteGateAuditLogsJSON(t *testing.T) {
	for _, verdict := range []string{"allow", "deny", "partial"} {
		t.Run(verdict, func(t *testing.T) {
			root := t.TempDir()
			event := `{"type":"gate.decision","data":{"verdict":"` + verdict + `","reason":"policy","integrity":"unknown","conclusionCheck":"unknown","counts":{"apply":1,"hold":2,"review":0,"stage":0,"drop":0}},"provenance":{"component":"write_gate","phase":"conclusion","path":"usage/aw_session.jsonl","index":0}}` + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(root, "aw_session.jsonl"), []byte(event), 0o600))
			gate := readWriteGate(root)
			require.NotNil(t, gate)
			expected := `{"verdict":"` + verdict + `","reason":"policy","integrity":"unknown","conclusion_check":"unknown","counts":{"apply":1,"drop":0,"hold":2,"review":0,"stage":0}}`
			content, err := json.Marshal(gate)
			require.NoError(t, err)
			require.JSONEq(t, expected, string(content))
			processed := ProcessedRun{Run: WorkflowRun{LogsPath: root}}
			runData := buildRunData(processed, []ProcessedRun{processed}, "", &logsAggregate{engineCounts: make(map[string]int)}, false)
			auditData, _ := buildLocalAuditData(processed, LogMetrics{}, nil)
			for _, value := range []any{runData, auditData} {
				content, err := json.Marshal(value)
				require.NoError(t, err)
				require.Contains(t, string(content), `"write_gate":`+stringMustMarshal(t, gate))
			}
		})
	}
	t.Run("ungated and untrusted stay absent", func(t *testing.T) {
		require.Nil(t, readWriteGate(t.TempDir()))
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "aw_session.jsonl"), []byte(`{"type":"gate.decision","data":{"verdict":"allow"},"provenance":{"component":"agent","phase":"agent"}}`), 0o600))
		require.Nil(t, readWriteGate(root))
		content, err := json.Marshal(RunData{})
		require.NoError(t, err)
		require.NotContains(t, string(content), `"write_gate"`)
	})
	t.Run("invalid trusted evidence is unknown", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "aw_session.jsonl"), []byte(`{"type":"gate.decision","data":{"verdict":"allow"},"provenance":{"component":"write_gate","phase":"conclusion","path":"usage/aw_session.jsonl"}}`), 0o600))
		gate := readWriteGate(root)
		require.Equal(t, "unknown", gate.ConclusionCheck)
		require.NotEmpty(t, gate.Error)
	})
}

func stringMustMarshal(t *testing.T, value any) string {
	t.Helper()
	content, err := json.Marshal(value)
	require.NoError(t, err)
	return string(content)
}

func TestWriteGateOutcomesNoAPIAndSummary(t *testing.T) {
	items, err := decodeLogsOutcomeItems(strings.NewReader(`
{"type":"create_issue","status":"skipped","gate_signal":"gate_denied"}
{"type":"create_pull_request","status":"skipped","gate_decision":"hold"}
{"type":"add_comment","status":"staged","gate_decision":"stage"}
{"type":"create_issue","status":"skipped","gate_decision":"drop"}
`))
	require.NoError(t, err)
	reports := evaluateOutcomes(context.Background(), items, "acme/repo", nil, "github.com")
	require.Len(t, reports, 4)
	for _, report := range reports {
		require.Equal(t, OutcomeStatusSkipped, report.OutcomeStatus)
		require.Equal(t, EvidenceStrong, report.EvidenceStrength)
		require.Empty(t, report.EvalError)
	}

	summary := ComputeOutcomeSummary(reports, nil)
	require.Equal(t, 4, summary.Total)
	require.Equal(t, 0, summary.Pending+summary.Unknown+summary.Errors)
	require.Equal(t, 4, summary.GateDenied+summary.GateHeld+summary.GateStaged+summary.GateDropped)
	var rendered strings.Builder
	require.NoError(t, renderLogsOutcomes(&rendered, LogsData{Outcomes: []OutcomesData{{Summary: summary}}}))
	require.Contains(t, rendered.String(), "Gate skipped: 1 denied, 1 held, 1 staged, 1 dropped")
}

func TestWriteGateDeniedCollectorCLIWithoutHandlerManifest(t *testing.T) {
	require.Contains(t, ResolveArtifactFilter([]string{"outcomes"}), "usage")
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the collector-to-CLI conformance case")
	}
	for _, verdict := range []string{"deny", ""} {
		t.Run("verdict="+verdict, func(t *testing.T) {
			root := t.TempDir()
			script := `
	const fs = require("node:fs"), path = require("node:path"), crypto = require("node:crypto");
	const { canonicalPendingJSON, inputDigest, itemDigest } = require("../../actions/setup/js/pending_writes.cjs");
	const { writeDecisionInventory } = require("../../actions/setup/js/write_gate_inventory.cjs");
	const { writeUnifiedSession } = require("../../actions/setup/js/unified_session.cjs");
	const root = process.argv[1], verdict = process.argv[2];
	const item = {output_id:"so_abcdefghijkl",type:"create_issue",target:{repository:"acme/repo",kind:"issue"},depends_on:[],settings:{},free_text:[],input_sha256:inputDigest({type:"create_issue"}),complete:true,reason_category:"ready"};
	item.item_sha256 = itemDigest(item);
	const manifest = {schema:"gh-aw/pending-writes/v1",run:{repository:"acme/repo",workflow_ref:"acme/repo/.github/workflows/test.lock.yml@refs/heads/main",workflow_sha:"a".repeat(40),run_id:"12",run_attempt:"1",event:"workflow_dispatch"},detection:{enabled:false,mode:"disabled",conclusion:"unknown",reason_category:"unknown",threat_flags:[]},complete:true,inputs:[],items:[item]};
	const bytes = canonicalPendingJSON(manifest), manifestSha = crypto.createHash("sha256").update(bytes).digest("hex");
	fs.writeFileSync(path.join(root,"pending-writes.json"),bytes);
	const decisionSha = writeDecisionInventory(path.join(root,"write-decisions.normalized.json"),{schema:"gh-aw/write-decisions/v1",manifest_sha256:manifestSha,decision:"deny",items:[{output_id:item.output_id,item_sha256:item.item_sha256,input_sha256:item.input_sha256,gate_decision:"drop",decision_reason:"gate_denied"}]});
	writeUnifiedSession({rootDir:root,warn:()=>{},writeGate:{GH_AW_WRITE_GATE:verdict,GH_AW_WRITE_GATE_REASON:"policy",GH_AW_PENDING_WRITES_SHA256:manifestSha,GH_AW_WRITE_GATE_DECISIONS_SHA256:decisionSha,GH_AW_JOB_RESULTS:"{}",GH_AW_WRITE_GATE_WRITE_JOB_NAMES:"[]"}});
	`
			output, err := exec.Command(node, "-e", script, root, verdict).CombinedOutput()
			require.NoError(t, err, string(output))
			_, err = os.Stat(filepath.Join(root, safeOutputItemsManifestFilename))
			require.ErrorIs(t, err, os.ErrNotExist)
			gate := readWriteGate(root)
			require.Equal(t, "deny", gate.Verdict)
			require.Len(t, gate.Items, 1)
			require.Equal(t, "so_abcdefghijkl", gate.Items[0].OutputID)
			require.Equal(t, "drop", gate.Items[0].Decision)
			if verdict == "" {
				require.Equal(t, "gate_error", gate.Reason)
			}
			report, err := collectRunOutcomes(context.Background(), ProcessedRun{Run: WorkflowRun{DatabaseID: 12, Repository: "acme/repo", LogsPath: root}}, nil, false)
			require.NoError(t, err)
			require.Empty(t, report.EvalError)
			require.Len(t, report.Items, 1)
			require.Equal(t, OutcomeStatusSkipped, report.Items[0].OutcomeStatus)
			require.Equal(t, "gate_denied", report.Items[0].Signal)
			require.Empty(t, report.Items[0].ObjectURL)
			require.Equal(t, 1, report.Summary.GateDenied)
			require.Zero(t, report.Summary.Unknown+report.Summary.Pending+report.Summary.Accepted)
			actual := CreatedItemReport{Type: "create_issue", Status: "success", OutputID: "so_abcdefghijkl"}
			require.NoError(t, os.WriteFile(filepath.Join(root, safeOutputItemsManifestFilename), []byte(stringMustMarshal(t, actual)+"\n"), 0o600))
			items, found, err := readLogsOutcomeItems(ProcessedRun{Run: WorkflowRun{LogsPath: root}})
			require.NoError(t, err)
			require.True(t, found)
			require.Len(t, items, 1)
			require.Equal(t, "success", items[0].Status)
		})
	}
}
