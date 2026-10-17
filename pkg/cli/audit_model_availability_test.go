//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modelUnavailableStartupLog = `[gh-aw/pi-models-json] fatal: Pi model "claude-opus-5" is not advertised by the configured copilot proxy endpoint; choose a model listed by AWF /reflect
`

func writeModelAvailabilityEvidence(t *testing.T, runDir, relative, content string) {
	t.Helper()
	path := filepath.Join(runDir, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestAuditModelAvailability(t *testing.T) {
	tests := []struct {
		name       string
		reflection string
		code       AuditFindingCode
		detail     string
	}{
		{
			name: "completed same-run inventory confirms absence",
			reflection: `{"models_fetch_complete":true,"endpoints":[
				{"provider":"openai","configured":true,"models":["claude-opus-5"]},
				{"provider":"copilot","configured":true,"models":["claude-sonnet-5","claude-opus-5.5","claude-opus-5.5"],
				 "routing_models":[{"model_id":"claude-opus-5.5","supported_endpoints":["/v1/messages","/chat/completions","untrusted\nendpoint"]}]}]}`,
			code:   AuditFindingModelUnavailable,
			detail: "claude-opus-5.5 (/v1/messages, /chat/completions)",
		},
		{
			name:       "provider alias and object inventory",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"github","configured":true,"models":[{"id":"claude-opus-5.5"},{"name":"claude-sonnet-5"}]}]}`,
			code:       AuditFindingModelUnavailable,
			detail:     "claude-opus-5.5",
		},
		{
			name:       "complete empty inventory",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":[]}]}`,
			code:       AuditFindingModelUnavailable,
			detail:     "advertises 0 model(s)",
		},
		{
			name:       "exact dated identity is not merged",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5-20261016"]}]}`,
			code:       AuditFindingModelUnavailable,
			detail:     "but not \"claude-opus-5\"",
		},
		{
			name:       "model actually present",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["CLAUDE-OPUS-5"]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "contradicting the startup rejection",
		},
		{
			name:       "incomplete discovery",
			reflection: `{"models_fetch_complete":false,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5.5"]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "Model discovery did not complete",
		},
		{
			name:       "no discovery status",
			reflection: `{"endpoints":[{"provider":"copilot","configured":true,"models":[]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "Model discovery did not complete",
		},
		{
			name:       "disabled endpoint",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":false,"models":[]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "No configured endpoint",
		},
		{
			name:       "wrong provider",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"anthropic","configured":true,"models":[]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "No configured endpoint",
		},
		{
			name:       "ambiguous provider",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":[]},{"provider":"github","configured":true,"models":[]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "Multiple configured endpoints",
		},
		{
			name:       "null inventory",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":null}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "no model inventory",
		},
		{
			name:       "missing inventory",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "no model inventory",
		},
		{
			name:       "invalid entry",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":[42]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "invalid entries",
		},
		{
			name:       "unsafe identifier",
			reflection: `{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5.5\nsensitive-payload"]}]}`,
			code:       AuditFindingModelAvailabilityUnknown,
			detail:     "invalid entries",
		},
		{name: "invalid json", reflection: "{", code: AuditFindingModelAvailabilityUnknown, detail: "awf-reflect.json is invalid"},
		{name: "missing reflection", code: AuditFindingModelAvailabilityUnknown, detail: "awf-reflect.json is unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDir := t.TempDir()
			writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", modelUnavailableStartupLog)
			if tt.reflection != "" {
				writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json", tt.reflection)
			}
			findings := generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure", LogsPath: runDir})
			require.Len(t, findings, 1)
			assert.Equal(t, tt.code, findings[0].Code)
			assert.Contains(t, findings[0].Description, tt.detail)
			assert.Contains(t, findings[0].Description, "copilot")
			assert.NotContains(t, findings[0].Description, "sensitive-payload")
			assert.NotContains(t, findings[0].Description, "untrusted")
		})
	}
}

func TestAuditModelAvailabilityRequiresStartupFailure(t *testing.T) {
	for _, log := range []string{
		"",
		`[WARN] Failed to publish resolved config artifact: EACCES`,
		`[gh-aw/pi-models-json] warning: awf-reflect model availability check failed (provider=copilot, model=claude-opus-5)`,
		`Model endpoint mismatch: gpt-5.4`,
		`Example: ` + modelUnavailableStartupLog,
	} {
		runDir := t.TempDir()
		writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", log)
		require.Empty(t, generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure", LogsPath: runDir}))
	}
	runDir := t.TempDir()
	writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", modelUnavailableStartupLog)
	require.Empty(t, generateModelAvailabilityFindings(WorkflowRun{Conclusion: "success", LogsPath: runDir}))
	require.Empty(t, generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure"}))
}

func TestAuditModelAvailabilityDoesNotUseOtherRunEvidence(t *testing.T) {
	runDir := t.TempDir()
	writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", modelUnavailableStartupLog)
	writeModelAvailabilityEvidence(t, runDir, "baseline-42/sandbox/firewall/awf-reflect.json",
		`{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":[]}]}`)
	findings := generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure", LogsPath: runDir})
	require.Len(t, findings, 1)
	assert.Equal(t, AuditFindingModelAvailabilityUnknown, findings[0].Code)
}

func TestAuditModelAvailabilityRejectsSymlinks(t *testing.T) {
	runDir := t.TempDir()
	writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", modelUnavailableStartupLog)
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(runDir, "sandbox")))
	findings := generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure", LogsPath: runDir})
	require.Len(t, findings, 1)
	assert.Equal(t, AuditFindingModelAvailabilityUnknown, findings[0].Code)
	_, err := readModelAvailabilityArtifact(runDir, "agent-stdio.log", 1)
	require.Error(t, err, "oversized artifacts must not be read")
}

func TestAuditModelAvailabilityPreservesExactIdentityAndBoundsCandidates(t *testing.T) {
	runDir := t.TempDir()
	log := `[gh-aw/pi-models-json] fatal: Pi model "github/claude-opus-5?effort=high" is not advertised by the configured copilot proxy endpoint; choose a model listed by AWF /reflect`
	writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", log)
	writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json",
		`{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5.5","claude-opus-6","claude-opus-7","claude-opus-8","claude-opus-9","claude-opus-99","claude-sonnet-5"]}]}`)
	findings := generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure", LogsPath: runDir})
	require.Len(t, findings, 1)
	assert.Equal(t, AuditFindingModelUnavailable, findings[0].Code)
	assert.Contains(t, findings[0].Description, `but not "claude-opus-5"`)
	assert.Contains(t, findings[0].Description, "claude-opus-5.5")
	assert.NotContains(t, findings[0].Description, "claude-opus-99")
	assert.NotContains(t, findings[0].Description, "claude-sonnet")

	recs := appendModelAvailabilityRecommendations(nil, []AuditFinding{{
		Code: AuditFindingModelAvailabilityUnknown, Description: "Inventory unavailable",
	}})
	require.Len(t, recs, 1)
	assert.Contains(t, recs[0].Action, "before changing model pins")
	assert.Contains(t, recs[0].Example, "gh aw audit")
}

func TestAuditModelAvailabilityRenderedAndCached(t *testing.T) {
	runDir := t.TempDir()
	writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", modelUnavailableStartupLog)
	writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json",
		`{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5.5"]}]}`)
	run := WorkflowRun{DatabaseID: 38948906270, Conclusion: "failure", Status: "completed", LogsPath: runDir}
	data, _ := buildLocalAuditData(ProcessedRun{Run: run}, LogMetrics{}, nil)
	var found bool
	for _, finding := range data.KeyFindings {
		if finding.Code == AuditFindingModelUnavailable {
			found = true
			assert.Equal(t, auditFindingPlaybookURL(finding.Code), finding.PlaybookURL)
		}
	}
	require.True(t, found)
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"code":"model_unavailable"`)
	require.NotEmpty(t, data.Recommendations)
	assert.Contains(t, data.Recommendations[len(data.Recommendations)-1].Example, "gh aw models")
	output := captureStderrForGuardPolicyReportTest(func() { renderConsole(data, runDir) })
	assert.Contains(t, output, "Model Unavailable")
	assert.Contains(t, output, "claude-opus-5.5")
	assert.Contains(t, output, "debug-playbook.md#model-availability")
	require.NoError(t, writeAuditData(runDir, data))
	cached, ok := loadCachedAuditData(runDir, run, "")
	require.True(t, ok)
	assert.Equal(t, data.KeyFindings, cached.KeyFindings)
}

func TestAuditFindingPlaybookRoutes(t *testing.T) {
	for _, tt := range []struct {
		code AuditFindingCode
		path string
	}{
		{AuditFindingWorkflowFailed, "debug-playbook.md#audit-findings"},
		{AuditFindingModelUnavailable, "debug-playbook.md#model-availability"},
		{AuditFindingModelAvailabilityUnknown, "debug-playbook.md#model-availability"},
		{AuditFindingSubagentModelUnavailable, "debug-model-engine-misconfiguration.md"},
	} {
		assert.Equal(t, "https://github.com/github/gh-aw/blob/main/.github/aw/"+tt.path, auditFindingPlaybookURL(tt.code))
	}
}

// TestAuditModelAvailabilityRecognizesSharedResolutionMessage verifies that the pi startup
// line written by pi_models_json.cjs (Pi phrase followed by the shared model_unavailable
// message from resolve_model_alias.cjs) is still classified as model_unavailable.
func TestAuditModelAvailabilityRecognizesSharedResolutionMessage(t *testing.T) {
	runDir := t.TempDir()
	log := `[gh-aw/pi-models-json] model resolution: engine=pi role=main configured=copilot/claude-opus-5 resolved=claude-opus-5 provider=copilot status=exact
[gh-aw/pi-models-json] warning: awf-reflect model availability check failed (provider=copilot, model=claude-opus-5)
[gh-aw/pi-models-json] fatal: Pi model "claude-opus-5" is not advertised by the configured copilot proxy endpoint; The requested model is not supported: 'copilot/claude-opus-5' is not available in the workflow's live model catalog. For Copilot, availability also depends on the agentic-workflows integration, not just organization policy. Select a model offered to this workflow; enabling a missing model for that integration requires Copilot API support.
`
	writeModelAvailabilityEvidence(t, runDir, "agent-stdio.log", log)
	writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json",
		`{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5.5"]}]}`)
	findings := generateModelAvailabilityFindings(WorkflowRun{Conclusion: "failure", LogsPath: runDir})
	require.Len(t, findings, 1)
	assert.Equal(t, AuditFindingModelUnavailable, findings[0].Code)
	assert.Contains(t, findings[0].Description, `but not "claude-opus-5"`)
}
