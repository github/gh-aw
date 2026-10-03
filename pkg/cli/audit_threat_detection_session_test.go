package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unifiedDetectionTestHeader = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector","phase":"conclusion"}}` + "\n"

func unifiedDetectionTestEvent(data string) string {
	return `{"type":"detection.result","data":` + data + `,"provenance":{"component":"detection","phase":"detection"}}` + "\n"
}

func writeUnifiedDetectionTestSession(t *testing.T, content string) string {
	t.Helper()
	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), []byte(content), 0o600))
	return runDir
}

func TestGenerateThreatDetectionFindingsFromUnifiedSession(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		data       string
		hasVerdict bool
		hasResult  bool
		count      int
		title      string
	}{
		{"clean", `{"jobResult":"success","conclusion":"success","reason":"","promptInjection":false,"secretLeak":false,"maliciousPatch":false}`, true, true, 0, ""},
		{"warn mode threat", `{"jobResult":"success","conclusion":"warning","reason":"threat_detected","promptInjection":true,"secretLeak":false,"maliciousPatch":true,"reasons":["PRIVATE_DETAIL"]}`, true, true, 2, "Threat Detection Warning"},
		{"engine failure", `{"jobResult":"failure","conclusion":"failure","reason":"agent_failure"}`, false, true, 1, "Threat Detection Job Failed"},
		{"parse warning", `{"jobResult":"success","conclusion":"warning","reason":"parse_error"}`, false, true, 1, "Threat Detection Warning"},
		{"threat without verdict", `{"jobResult":"failure","conclusion":"failure","reason":"threat_detected"}`, false, true, 2, "Threat Detection Job Failed"},
		{"cancelled", `{"jobResult":"cancelled","conclusion":"","reason":""}`, false, true, 1, "Threat Detection Job Failed"},
		{"skipped", `{"jobResult":"skipped","conclusion":"skipped","reason":"detection_skipped"}`, false, true, 0, ""},
		{"partial verdict", `{"promptInjection":false,"secretLeak":false}`, false, false, 0, ""},
		{"verdict only", `{"promptInjection":false,"secretLeak":true,"maliciousPatch":false}`, true, false, 1, "Security Threat Detected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runDir := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader+unifiedDetectionTestEvent(tt.data))
			evidence := readThreatDetectionEvidence(runDir)
			assert.Equal(t, tt.hasVerdict, evidence.HasVerdict)
			assert.Equal(t, tt.hasResult, evidence.HasResult)
			findings := generateThreatDetectionFindings(ProcessedRun{Run: WorkflowRun{LogsPath: runDir}})
			require.Len(t, findings, tt.count)
			if tt.count > 0 {
				assert.Equal(t, tt.title, findings[0].Title)
			}
			if tt.count == 2 {
				assert.Equal(t, AuditFindingThreatDetected, findings[1].Code)
				if tt.hasVerdict {
					assert.Contains(t, findings[1].Description, "prompt injection")
					assert.Contains(t, findings[1].Description, "malicious patch")
				} else {
					assert.Contains(t, findings[1].Description, "no specific threat category")
				}
			}
			serialized, err := json.Marshal(findings)
			require.NoError(t, err)
			assert.NotContains(t, string(serialized), "PRIVATE_DETAIL")
		})
	}
}

func TestUnifiedDetectionResultOverridesLegacyEvidence(t *testing.T) {
	t.Parallel()
	runDir := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader+unifiedDetectionTestEvent(`{"jobResult":"skipped","conclusion":"skipped","reason":""}`))
	detectionDir := filepath.Join(runDir, "usage", "detection")
	require.NoError(t, os.MkdirAll(detectionDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(detectionDir, "detection_result.json"),
		[]byte(`{"job_result":"failure","conclusion":"warning","reason":"threat_detected","prompt_injection":true,"secret_leak":true,"malicious_patch":true}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(detectionDir, "execution.json"),
		[]byte(`{"version":1,"component":"detection","state":"not_started"}`), 0o600))
	result, found := readDetectionUsageResult(runDir)
	require.True(t, found)
	assert.Equal(t, detectionUsageResult{JobResult: "skipped", Conclusion: "skipped"}, result)
	_, found = findThreatDetectionVerdict(runDir)
	assert.False(t, found)
	assert.Empty(t, generateThreatDetectionFindings(ProcessedRun{Run: WorkflowRun{LogsPath: runDir}}))
}

func TestUnifiedDetectionResultPreservesOlderJobOutcomes(t *testing.T) {
	t.Parallel()
	runDir := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader+unifiedDetectionTestEvent(`{"promptInjection":true,"secretLeak":false,"maliciousPatch":false}`))
	detectionDir := filepath.Join(runDir, "usage", "detection")
	require.NoError(t, os.MkdirAll(detectionDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(detectionDir, "detection_result.json"),
		[]byte(`{"job_result":"success","conclusion":"warning","reason":"threat_detected"}`), 0o600))
	result, found := readDetectionUsageResult(runDir)
	require.True(t, found)
	assert.Equal(t, detectionUsageResult{JobResult: "success", Conclusion: "warning", Reason: "threat_detected"}, result)
	require.Len(t, generateThreatDetectionFindings(ProcessedRun{Run: WorkflowRun{LogsPath: runDir}}), 2)
}

func TestMergeUnifiedDetectionResult(t *testing.T) {
	t.Parallel()
	stringPointer := func(value string) *string { return &value }
	t.Run("unified canonical fields override legacy values", func(t *testing.T) {
		unified := &unifiedDetectionResult{JobResult: stringPointer("skipped"), Conclusion: stringPointer(""), Reason: stringPointer("")}
		legacy := detectionUsageResult{JobResult: "success", Conclusion: "warning", Reason: "threat_detected"}
		assert.Equal(t, detectionUsageResult{JobResult: "skipped"}, mergeUnifiedDetectionResult(unified, legacy))
	})
	t.Run("legacy fields backfill missing unified values", func(t *testing.T) {
		unified := &unifiedDetectionResult{JobResult: stringPointer("success")}
		legacy := detectionUsageResult{Conclusion: "warning", Reason: "threat_detected"}
		assert.Equal(t, detectionUsageResult{JobResult: "success", Conclusion: "warning", Reason: "threat_detected"}, mergeUnifiedDetectionResult(unified, legacy))
	})
}

func TestParseUnifiedDetectionResultRejectsInvalidHeadersAndConflicts(t *testing.T) {
	t.Parallel()
	event := unifiedDetectionTestEvent(`{"promptInjection":false,"secretLeak":false,"maliciousPatch":false}`)
	for _, tt := range []struct {
		name    string
		content string
		message string
	}{
		{"empty", "", "missing"},
		{"missing header", event, "missing"},
		{"malformed header", "{bad\n" + event, "invalid"},
		{"unsupported version", strings.Replace(unifiedDetectionTestHeader, `"version":1`, `"version":2`, 1) + event, "unsupported"},
		{"string version", strings.Replace(unifiedDetectionTestHeader, `"version":1`, `"version":"1"`, 1) + event, "unsupported"},
		{"wrong owner", strings.Replace(unifiedDetectionTestHeader, `"collector"`, `"agent"`, 1) + event, "missing"},
		{"duplicate headers", unifiedDetectionTestHeader + event + unifiedDetectionTestHeader, "multiple"},
		{"conflicting verdicts", unifiedDetectionTestHeader + event + unifiedDetectionTestEvent(`{"promptInjection":true,"secretLeak":false,"maliciousPatch":false}`), "conflicting"},
		{"conflicting outcomes", unifiedDetectionTestHeader + unifiedDetectionTestEvent(`{"jobResult":"success"}`) + unifiedDetectionTestEvent(`{"jobResult":"failure"}`), "conflicting"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseUnifiedDetectionResult(strings.NewReader(tt.content))
			require.ErrorContains(t, err, tt.message)
			assert.Nil(t, result)
		})
	}
}

func TestParseUnifiedDetectionResultRecoversAndIgnoresOtherSources(t *testing.T) {
	t.Parallel()
	event := unifiedDetectionTestEvent(`{"promptInjection":false,"secretLeak":true,"maliciousPatch":false}`)
	content := unifiedDetectionTestHeader + "{bad\n" +
		unifiedDetectionTestEvent(`{"promptInjection":"false"}`) +
		strings.Replace(event, `"component":"detection"`, `"component":"agent"`, 1) +
		strings.Replace(event, `"phase":"detection"`, `"phase":"agent"`, 1) +
		`{"type":"session.format","data":{"version":"native"},"provenance":{"component":"agent"}}` + "\n" +
		event + event
	result, err := parseUnifiedDetectionResult(strings.NewReader(content))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, *result.PromptInjection)
	assert.True(t, *result.SecretLeak)
	assert.False(t, *result.MaliciousPatch)
	assert.Nil(t, result.JobResult)
}

func TestParseUnifiedDetectionResultHandlesLargeAgentRecords(t *testing.T) {
	t.Parallel()
	content := unifiedDetectionTestHeader +
		`{"type":"assistant.message","data":{"content":"` + strings.Repeat("x", 2*maxScannerBufferSize) + `"},"provenance":{"component":"agent"}}` + "\n" +
		unifiedDetectionTestEvent(`{"jobResult":"success","promptInjection":false,"secretLeak":false,"maliciousPatch":false}`)
	result, err := parseUnifiedDetectionResult(strings.NewReader(content))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "success", *result.JobResult)
	assert.False(t, *result.SecretLeak)
}

func TestParseUnifiedDetectionResultDiscardsOversizedRecords(t *testing.T) {
	t.Parallel()
	content := unifiedDetectionTestHeader +
		`{"type":"assistant.message","data":{"content":"` + strings.Repeat("x", maxUnifiedSessionLineSize) + `"},"provenance":{"component":"agent"}}` + "\n" +
		unifiedDetectionTestEvent(`{"jobResult":"success","promptInjection":false,"secretLeak":false,"maliciousPatch":false}`)
	result, err := parseUnifiedDetectionResult(strings.NewReader(content))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "success", *result.JobResult)
	assert.False(t, *result.SecretLeak)
}

func TestReadUnifiedDetectionResultRejectsSymlinks(t *testing.T) {
	t.Parallel()
	source := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader)
	for _, target := range []string{"usage", filepath.Join("usage", "aw_session.jsonl")} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(runDir, target)), 0o700))
			require.NoError(t, os.Symlink(filepath.Join(source, target), filepath.Join(runDir, target)))
			result, err := readUnifiedDetectionResult(runDir)
			require.ErrorContains(t, err, "symbolic link")
			assert.Nil(t, result)
		})
	}
}

func TestParseUnifiedDetectionResultRejectsPartialRead(t *testing.T) {
	t.Parallel()
	content := unifiedDetectionTestHeader + unifiedDetectionTestEvent(`{"promptInjection":true,"secretLeak":false,"maliciousPatch":false}`)
	input := io.MultiReader(strings.NewReader(content), iotest.ErrReader(errors.New("fixture read denied")))
	result, err := parseUnifiedDetectionResult(input)
	require.ErrorContains(t, err, "fixture read denied")
	assert.Nil(t, result)
}

func TestUnifiedDetectionResultFallsBackToLegacyArtifacts(t *testing.T) {
	t.Parallel()
	runDir := writeUnifiedDetectionTestSession(t, strings.Replace(unifiedDetectionTestHeader, `"version":1`, `"version":2`, 1))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "detection_result.json"),
		[]byte(`{"prompt_injection":false,"secret_leak":true,"malicious_patch":false}`), 0o600))
	verdict, found := findThreatDetectionVerdict(runDir)
	require.True(t, found)
	assert.True(t, verdict.SecretLeak)
}

func TestReadThreatDetectionLiveArtifacts(t *testing.T) {
	root := os.Getenv("GH_AW_LIVE_DETECTION_RUNS")
	if root == "" {
		t.Skip("Set GH_AW_LIVE_DETECTION_RUNS to downloaded workflow artifacts")
	}
	runs, err := filepath.Glob(filepath.Join(root, "*", "run-*"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(runs), 3)
	for _, runDir := range runs {
		t.Run(filepath.Base(runDir), func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(runDir, "usage", "detection", "detection_result.json"))
			require.NoError(t, err)
			var expectedVerdict rawThreatDetectionVerdict
			var expectedResult detectionUsageResult
			require.NoError(t, json.Unmarshal(content, &expectedVerdict))
			require.NoError(t, json.Unmarshal(content, &expectedResult))
			evidence := readThreatDetectionEvidence(runDir)
			verdict, hasVerdict := validateThreatDetectionVerdict(expectedVerdict)
			assert.Equal(t, hasVerdict, evidence.HasVerdict)
			assert.Equal(t, verdict, evidence.Verdict)
			assert.True(t, evidence.HasResult)
			assert.Equal(t, expectedResult, evidence.Result)

			session, err := os.ReadFile(filepath.Join(runDir, "usage", "aw_session.jsonl"))
			require.NoError(t, err)
			isolatedDir := writeUnifiedDetectionTestSession(t, string(session))
			isolatedVerdict, isolatedFound := findThreatDetectionVerdict(isolatedDir)
			require.Equal(t, hasVerdict, isolatedFound)
			assert.Equal(t, verdict, isolatedVerdict)
			t.Logf("Verified live detection result and unified-only verdict from %s", filepath.Base(runDir))
		})
	}
}
