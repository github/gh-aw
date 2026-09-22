package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateThreatDetectionFindings(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "detection"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "detection", "detection_result.json"),
		[]byte(`{"prompt_injection":true,"secret_leak":false,"malicious_patch":true,"reasons":["sensitive detail"]}`),
		0o600,
	))

	findings := generateThreatDetectionFindings(ProcessedRun{
		Run: WorkflowRun{LogsPath: runDir},
		JobDetails: []JobInfoWithDuration{{
			JobInfo: JobInfo{Name: "detection", Conclusion: "failure"},
		}},
	})

	require.Len(t, findings, 2)
	assert.Equal(t, AuditFindingDetectionJobFailed, findings[0].Code)
	assert.Equal(t, AuditFindingThreatDetected, findings[1].Code)
	assert.Contains(t, findings[1].Description, "prompt injection")
	assert.Contains(t, findings[1].Description, "malicious patch")
	assert.NotContains(t, findings[1].Description, "sensitive detail")
}

func TestFindThreatDetectionVerdictFromLegacyLog(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "detection.log"),
		[]byte("output\nTHREAT_DETECTION_RESULT:{\"prompt_injection\":false,\"secret_leak\":true,\"malicious_patch\":false,\"reasons\":[]}\n"),
		0o600,
	))

	verdict, found := findThreatDetectionVerdict(runDir)
	require.True(t, found)
	assert.True(t, verdict.SecretLeak)
}

func TestFindThreatDetectionVerdictRejectsMalformedAndConflictingResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "missing required field",
			content: `{"prompt_injection":true,"secret_leak":false}`,
		},
		{
			name: "conflicting legacy verdicts",
			content: "THREAT_DETECTION_RESULT:{\"prompt_injection\":false,\"secret_leak\":false,\"malicious_patch\":false}\n" +
				"THREAT_DETECTION_RESULT:{\"prompt_injection\":true,\"secret_leak\":false,\"malicious_patch\":false}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			filename := "detection_result.json"
			if tt.name == "conflicting legacy verdicts" {
				filename = "detection.log"
			}
			require.NoError(t, os.WriteFile(filepath.Join(runDir, filename), []byte(tt.content), 0o600))

			_, found := findThreatDetectionVerdict(runDir)
			assert.False(t, found)
		})
	}
}

func TestFindThreatDetectionVerdictAcceptsDuplicateResults(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	content := "THREAT_DETECTION_RESULT:{\"prompt_injection\":true,\"secret_leak\":false,\"malicious_patch\":false}\n"
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "detection.log"), []byte(content+content), 0o600))

	verdict, found := findThreatDetectionVerdict(runDir)
	require.True(t, found)
	assert.True(t, verdict.PromptInjection)
}

func TestGenerateThreatDetectionFindingsUsesUsageExecutionEvidence(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage", "detection"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "usage", "detection", "execution.json"),
		[]byte(`{"version":1,"component":"detection","run_id":123,"run_attempt":2,"state":"not_started"}`),
		0o600,
	))

	findings := generateThreatDetectionFindings(ProcessedRun{
		Run: WorkflowRun{
			DatabaseID: 123,
			Attempt:    2,
			LogsPath:   runDir,
		},
	})

	require.Len(t, findings, 1)
	assert.Equal(t, AuditFindingDetectionJobFailed, findings[0].Code)
	assert.Contains(t, findings[0].Description, "Usage artifact")
}

func TestGenerateThreatDetectionFindingsIgnoresSkippedDetectionJob(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage", "detection"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "usage", "detection", "execution.json"),
		[]byte(`{"version":1,"component":"detection","state":"not_started"}`),
		0o600,
	))

	findings := generateThreatDetectionFindings(ProcessedRun{
		Run: WorkflowRun{LogsPath: runDir},
		JobDetails: []JobInfoWithDuration{{
			JobInfo: JobInfo{Name: "detection", Conclusion: "skipped"},
		}},
	})

	assert.Empty(t, findings)
}

func TestGenerateThreatDetectionFindingsRejectsStaleUsageExecutionEvidence(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage", "detection"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "usage", "detection", "execution.json"),
		[]byte(`{"version":1,"component":"detection","run_id":123,"run_attempt":1,"state":"not_started"}`),
		0o600,
	))

	findings := generateThreatDetectionFindings(ProcessedRun{
		Run: WorkflowRun{
			DatabaseID: 123,
			Attempt:    2,
			LogsPath:   runDir,
		},
	})

	assert.Empty(t, findings)
}

func TestThreatDetectionVerdictIgnoresComparisonArtifacts(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "baseline-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "base"), 0o755))
	for _, path := range []string{
		filepath.Join(runDir, "baseline-1", "detection_result.json"),
		filepath.Join(runDir, "base", "detection_result.json"),
	} {
		require.NoError(t, os.WriteFile(
			path,
			[]byte(`{"prompt_injection":true,"secret_leak":false,"malicious_patch":false}`),
			0o600,
		))
	}

	_, found := findThreatDetectionVerdict(runDir)
	assert.False(t, found)
	assert.False(t, hasThreatDetectionArtifact(runDir))
}

func TestGeneratedAuditFindingsHaveCodes(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "detection_result.json"),
		[]byte(`{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":[]}`),
		0o600,
	))
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			Conclusion: "failure",
			LogsPath:   runDir,
		},
		MCPFailures:  []MCPFailureReport{{ServerName: "server"}},
		MissingTools: []MissingToolReport{{Tool: "tool"}},
		FirewallAnalysis: &FirewallAnalysis{
			AnalysisBase: AnalysisBase{BlockedRequests: 1},
		},
	}

	findings := generateFindings(processedRun, MetricsData{
		TokenUsage: 60_000,
		Turns:      11,
	}, []ValidationIssue{
		{Type: "error"}, {Type: "error"}, {Type: "error"},
		{Type: "error"}, {Type: "error"}, {Type: "error"},
	})

	require.NotEmpty(t, findings)
	for _, finding := range findings {
		assert.NotEmpty(t, finding.Code, "finding %q must have a stable code", finding.Title)
	}
}
