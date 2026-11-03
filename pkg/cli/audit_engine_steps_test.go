//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures below are trimmed from the jobs API responses, job logs and artifacts of
// github/gh-aw runs 40955414354 (evals judge pre-flight failure), 40779352178
// (external threat-detect pre-flight failure) and 41055931291 (healthy evals).

const evalsPreflightJobLog40955414354 = `2026-10-29T21:18:53.9695805Z ##[group]Run npm install -g @anthropic-ai/claude-code@2.1.312
2026-10-29T21:18:58.9982605Z added 2 packages in 5s
2026-10-29T21:18:59.0000522Z npm warn install-scripts Run ` + "`npm install -g --allow-scripts=@anthropic-ai/claude-code`" + ` to allow these scripts once
2026-10-29T21:18:59.0334305Z ##[group]Run set -o pipefail
2026-10-29T21:18:59.0334780Z set -o pipefail
2026-10-29T21:18:59.0335054Z gh_aw_exit_code=0
2026-10-29T21:19:21.2926322Z [health-check] API Proxy Pre-flight Check
2026-10-29T21:19:21.2927507Z [health-check] ==========================================
2026-10-29T21:19:21.2929938Z [health-check] Checking Anthropic API proxy configuration...
2026-10-29T21:19:21.2931273Z [health-check] ✓ Anthropic credentials NOT in agent environment (correct)
2026-10-29T21:19:21.2932780Z [health-check] Testing connectivity to Anthropic API proxy at http://host.docker.internal:10002...
2026-10-29T21:19:21.4453056Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002
2026-10-29T21:19:21.4454514Z [health-check][ERROR] Proxy may not be running or network is blocked
2026-10-29T21:19:21.7676534Z [INFO] Stopping containers...
2026-10-29T21:19:23.9100968Z [WARN] Command completed with exit code: 1
2026-10-29T21:19:23.9101981Z Process exiting with code: 1
2026-10-29T21:19:41.8398507Z [health-check] Testing connectivity to Anthropic API proxy at http://host.docker.internal:10002...
2026-10-29T21:19:41.8399639Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002
2026-10-29T21:19:41.8400507Z [health-check][ERROR] Proxy may not be running or network is blocked
2026-10-29T21:19:42.1088595Z [INFO] Stopping containers...
2026-10-29T21:19:44.0950845Z [WARN] Command completed with exit code: 1
2026-10-29T21:19:44.0951370Z Process exiting with code: 1
2026-10-29T21:19:44.1197863Z [claude-awf-retry] AWF failed before the claude harness started; recorded evals execution evidence as not_started
2026-10-29T21:19:44.1259996Z ##[error]Agent execution exited with code 1
2026-10-29T21:19:44.1269008Z ##[error]Process completed with exit code 1.
2026-10-29T21:19:44.1335928Z ##[group]Run actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3
2026-10-29T21:19:44.1336376Z with:
2026-10-29T21:19:44.4751805Z Q[docs_reviewed]: UNKNOWN
2026-10-29T21:19:44.4753704Z Q[feedback_produced]: UNKNOWN
`

const evalsPreflightJobs40955414354 = `[{"total_count":1,"jobs":[{"id":123738297744,"run_id":40955414354,"name":"evals","status":"completed","conclusion":"success","started_at":"2026-10-29T21:18:20Z","completed_at":"2026-10-29T21:19:52Z","steps":[
{"name":"Install Claude Code CLI","status":"completed","conclusion":"success","number":14,"started_at":"2026-10-29T21:18:53Z","completed_at":"2026-10-29T21:18:59Z"},
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"},
{"name":"Parse MCP Gateway logs for step summary","status":"completed","conclusion":"success","number":16,"started_at":"2026-10-29T21:19:44Z","completed_at":"2026-10-29T21:19:44Z"},
{"name":"Parse BinEval results","status":"completed","conclusion":"success","number":17,"started_at":"2026-10-29T21:19:44Z","completed_at":"2026-10-29T21:19:44Z"}]}]}]`

const evalsPreflightResults40955414354 = `{"id":"docs_reviewed","question":"Did the agent review the project documentation from the perspective of a Claude Code user?","answer":"UNKNOWN","model":"","timestamp":"2026-10-29T21:19:44.474Z","runid":"40955414354"}
{"id":"feedback_produced","question":"Was feedback or a report produced identifying documentation gaps for non-Copilot users?","answer":"UNKNOWN","model":"","timestamp":"2026-10-29T21:19:44.474Z","runid":"40955414354"}
`

const detectionPreflightJobLog40779352178 = `2026-10-28T22:27:58.2359431Z ##[group]Run bash "${RUNNER_TEMP}/gh-aw/actions/install_threat_detect_binary.sh" v0.5.3 --artifact-base-url https://github.com/github/gh-aw-threat-detection/releases/download
2026-10-28T22:27:58.5115409Z threat-detect v0.5.3
2026-10-28T22:27:58.5119604Z ✓ threat-detect installation complete
2026-10-28T22:27:58.5197572Z ##[group]Run mkdir -p "/tmp/gh-aw/threat-detection"
2026-10-28T22:27:58.5198127Z mkdir -p "/tmp/gh-aw/threat-detection"
2026-10-28T22:28:29.8011593Z [health-check] API Proxy Pre-flight Check
2026-10-28T22:28:29.8013335Z [health-check] ==========================================
2026-10-28T22:28:29.8014426Z [health-check] Checking Anthropic API proxy configuration...
2026-10-28T22:28:29.8015868Z [health-check] ✓ Anthropic credentials NOT in agent environment (correct)
2026-10-28T22:28:29.8017209Z [health-check] Testing connectivity to Anthropic API proxy at http://host.docker.internal:10002...
2026-10-28T22:28:30.1447935Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002
2026-10-28T22:28:30.1448882Z [health-check][ERROR] Proxy may not be running or network is blocked
2026-10-28T22:28:32.7621467Z [WARN] Command completed with exit code: 1
2026-10-28T22:28:32.7622507Z Process exiting with code: 1
2026-10-28T22:28:32.7749904Z ##[error]Process completed with exit code 1.
2026-10-28T22:28:32.7806545Z ##[group]Run mkdir -p /tmp/gh-aw/threat-detection/sandbox/firewall
2026-10-28T22:28:32.7807111Z mkdir -p /tmp/gh-aw/threat-detection/sandbox/firewall
2026-10-28T22:28:36.1835735Z ##[group]Run actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3
2026-10-28T22:28:36.1836186Z with:
2026-10-28T22:28:36.3057767Z [health-check] Testing connectivity to Anthropic API proxy at http://host.docker.internal:10002...
2026-10-28T22:28:36.3059218Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002
2026-10-28T22:28:36.3060389Z [health-check][ERROR] Proxy may not be running or network is blocked
2026-10-28T22:28:36.3061152Z [INFO] Stopping containers...
2026-10-28T22:28:36.3173486Z ##[group]Run actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9
2026-10-28T22:28:38.0062300Z ##[group]Run bash "${RUNNER_TEMP}/gh-aw/actions/conclude_threat_detection.sh" /tmp/gh-aw/threat-detection/detection_result.json
2026-10-28T22:28:38.0264964Z ##[warning]ERR_SYSTEM: ❌ Detection result file not found at: /tmp/gh-aw/threat-detection/detection_result.json
`

const detectionPreflightJobs40779352178 = `[{"total_count":1,"jobs":[{"id":123139270013,"run_id":40779352178,"name":"detection","status":"completed","conclusion":"success","started_at":"2026-10-28T22:27:22Z","completed_at":"2026-10-28T22:28:41Z","steps":[
{"name":"Install threat-detect binary","status":"completed","conclusion":"success","number":21,"started_at":"2026-10-28T22:27:58Z","completed_at":"2026-10-28T22:27:58Z"},
{"name":"Execute threat detection with AWF","status":"completed","conclusion":"success","number":22,"started_at":"2026-10-28T22:27:58Z","completed_at":"2026-10-28T22:28:32Z"},
{"name":"Copy detection firewall logs","status":"completed","conclusion":"success","number":23,"started_at":"2026-10-28T22:28:32Z","completed_at":"2026-10-28T22:28:32Z"},
{"name":"Render detection log","status":"completed","conclusion":"success","number":27,"started_at":"2026-10-28T22:28:36Z","completed_at":"2026-10-28T22:28:36Z"},
{"name":"Upload threat detection artifact","status":"completed","conclusion":"success","number":28,"started_at":"2026-10-28T22:28:36Z","completed_at":"2026-10-28T22:28:38Z"},
{"name":"Conclude threat detection","status":"completed","conclusion":"success","number":29,"started_at":"2026-10-28T22:28:38Z","completed_at":"2026-10-28T22:28:38Z"}]}]}]`

const healthyEvalsJobLog41055931291 = `2026-10-30T12:39:00.5459959Z ##[group]Run npm install -g @anthropic-ai/claude-code@2.1.312
2026-10-30T12:39:04.1593643Z npm warn install-scripts
2026-10-30T12:39:04.1875314Z ##[group]Run set -o pipefail
2026-10-30T12:39:04.1875812Z set -o pipefail
2026-10-30T12:39:28.8630192Z [health-check] ✓ Anthropic API proxy is reachable at http://api-proxy:10002
2026-10-30T12:39:28.8780410Z [health-check] ✓ All API proxy health checks passed
2026-10-30T12:39:40.9823490Z [SUCCESS] Command completed successfully
2026-10-30T12:39:40.9825157Z Process exiting with code: 0
2026-10-30T12:39:41.0075171Z ##[group]Run actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3
2026-10-30T12:39:41.0075965Z with:
2026-10-30T12:39:41.3499279Z Q[docs_reviewed]: YES
2026-10-30T12:39:41.3499901Z Q[feedback_produced]: YES
`

const healthyEvalsJobs41055931291 = `[{"total_count":1,"jobs":[{"id":124066459608,"run_id":41055931291,"name":"evals","status":"completed","conclusion":"success","started_at":"2026-10-30T12:38:30Z","completed_at":"2026-10-30T12:39:50Z","steps":[
{"name":"Install Claude Code CLI","status":"completed","conclusion":"success","number":14,"started_at":"2026-10-30T12:39:00Z","completed_at":"2026-10-30T12:39:04Z"},
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-30T12:39:04Z","completed_at":"2026-10-30T12:39:40Z"},
{"name":"Parse MCP Gateway logs for step summary","status":"completed","conclusion":"success","number":16,"started_at":"2026-10-30T12:39:41Z","completed_at":"2026-10-30T12:39:41Z"}]}]}]`

const healthyEvalsResults41055931291 = `{"id":"docs_reviewed","question":"Did the agent review the project documentation from the perspective of a Claude Code user?","answer":"YES","model":"","timestamp":"2026-10-30T12:39:41.349Z","runid":"41055931291"}
{"id":"feedback_produced","question":"Was feedback or a report produced identifying documentation gaps for non-Copilot users?","answer":"YES","model":"","timestamp":"2026-10-30T12:39:41.349Z","runid":"41055931291"}
`

func writeEngineStepFixture(t *testing.T, runDir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(runDir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

func engineStepFixtureRun(t *testing.T, runDir string, runID int64, workflowName, conclusion string) ProcessedRun {
	t.Helper()
	var jobs []JobInfoWithDuration
	if data, err := os.ReadFile(filepath.Join(runDir, jobsAPIResponseFileName)); err == nil {
		parsed, _, parseErr := parseJobsAPIResponses(data, runID, false)
		require.NoError(t, parseErr)
		jobs = parsed
	}
	return ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   runID,
			Attempt:      1,
			WorkflowName: workflowName,
			Status:       "completed",
			Conclusion:   conclusion,
			CreatedAt:    time.Date(2026, 10, 29, 21, 0, 0, 0, time.UTC),
			LogsPath:     runDir,
		},
		JobDetails: jobs,
	}
}

func findingsWithCode(findings []AuditFinding, code AuditFindingCode) []AuditFinding {
	var matched []AuditFinding
	for _, finding := range findings {
		if finding.Code == code {
			matched = append(matched, finding)
		}
	}
	return matched
}

func findingCodes(findings []AuditFinding) []AuditFindingCode {
	codes := make([]AuditFindingCode, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	return codes
}

func TestAuditEngineStepFailure_EvalsPreflight(t *testing.T) {
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:      evalsPreflightJobs40955414354,
		"workflow-logs/3_evals.txt":  evalsPreflightJobLog40955414354,
		"usage/evals/execution.json": `{"version":1,"component":"evals","run_id":40955414354,"run_attempt":1,"state":"not_started"}` + "\n",
		"usage/evals.jsonl":          evalsPreflightResults40955414354,
		"evals/evals.jsonl":          evalsPreflightResults40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Claude Code User Documentation Review", "success")

	auditData := buildAuditData(context.Background(), processedRun, LogMetrics{}, nil)

	stepFindings := findingsWithCode(auditData.KeyFindings, AuditFindingEngineStepFailed)
	require.Len(t, stepFindings, 1, "evals pre-flight failure must produce one engine_step_failed finding")
	finding := stepFindings[0]
	assert.Equal(t, "high", string(finding.Severity))
	assert.Contains(t, finding.Description, `Job "evals"`)
	assert.Contains(t, finding.Description, `step 15 "Execute Claude Code CLI"`)
	assert.Contains(t, finding.Description, "Cannot connect to Anthropic API proxy at http://host.docker.internal:10002")
	assert.Contains(t, finding.Description, "execution state: not_started")
	assert.Contains(t, finding.Description, "all 2 evals answers are UNKNOWN")
	assert.NotContains(t, finding.Description, "Proxy may not be running", "only the first health-check error is reported")
	assert.Equal(t, auditDebugPlaybookURL+"#audit-findings", finding.PlaybookURL)
	assert.Empty(t, auditData.Errors, "the errors section of a green run is unchanged")

	require.NotNil(t, auditData.Evals, "runs with evals results must have an evals section")
	assert.Equal(t, EvalsSummary{Questions: 2, Yes: 0, No: 0, Unknown: 2, ExecutionState: "not_started"}, *auditData.Evals)

	encoded, err := json.Marshal(auditData)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"evals":{"questions":2,"yes":0,"no":0,"unknown":2,"execution_state":"not_started"}`)
}

func TestAuditEngineStepFailure_DetectionPreflight(t *testing.T) {
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:          detectionPreflightJobs40779352178,
		"workflow-logs/3_detection.txt":  detectionPreflightJobLog40779352178,
		"usage/detection/execution.json": `{"version":1,"component":"detection","run_id":40779352178,"run_attempt":1,"state":"started"}` + "\n",
	})
	processedRun := engineStepFixtureRun(t, runDir, 40779352178, "Daily BYOK Ollama Test", "success")

	findings := generateFindings(processedRun, MetricsData{}, nil)

	stepFindings := findingsWithCode(findings, AuditFindingEngineStepFailed)
	require.Len(t, stepFindings, 1, "detection pre-flight failure must produce one engine_step_failed finding")
	finding := stepFindings[0]
	assert.Contains(t, finding.Description, `Job "detection"`)
	assert.Contains(t, finding.Description, `step 22 "Execute threat detection with AWF"`)
	assert.Contains(t, finding.Description, "Cannot connect to Anthropic API proxy at http://host.docker.internal:10002")
	assert.NotContains(t, finding.Description, "execution state")
	assert.Empty(t, findingsWithCode(findings, AuditFindingDetectionJobFailed), "threat_detection_job_failed logic is unchanged")

	// The reprinted health-check lines at 22:28:36 belong to step 27, not step 22.
	step := processedRun.JobDetails[0].Steps[1]
	lines := readEngineStepLogLines(filepath.Join(runDir, "workflow-logs"), processedRun.JobDetails[0].JobInfo, step)
	require.NotEmpty(t, lines)
	assert.True(t, strings.HasPrefix(lines[0], "2026-10-28T22:27:58.5197572Z ##[group]Run mkdir"), "step 22 begins at its own header, not step 21's")
	for _, line := range lines {
		assert.False(t, strings.HasPrefix(line, "2026-10-28T22:28:36"), "line outside step 22 window attributed: %s", line)
		assert.NotContains(t, line, "threat-detect installation complete", "step 21 output attributed to step 22")
		assert.NotContains(t, line, "sandbox/firewall", "step 23 output attributed to step 22")
	}
	failed, errorLine := engineStepFailureFromLog(lines)
	assert.True(t, failed)
	assert.Equal(t, "Cannot connect to Anthropic API proxy at http://host.docker.internal:10002", errorLine)

	auditData := buildAuditData(context.Background(), processedRun, LogMetrics{}, nil)
	assert.Nil(t, auditData.Evals, "detection-only runs have no evals section")
}

func TestAuditEngineStepFailure_HealthyEvalsNoFinding(t *testing.T) {
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:      healthyEvalsJobs41055931291,
		"workflow-logs/3_evals.txt":  healthyEvalsJobLog41055931291,
		"usage/evals/execution.json": `{"version":1,"component":"evals","run_id":41055931291,"run_attempt":1,"state":"started"}` + "\n",
		"usage/evals.jsonl":          healthyEvalsResults41055931291,
	})
	processedRun := engineStepFixtureRun(t, runDir, 41055931291, "Claude Code User Documentation Review", "success")

	auditData := buildAuditData(context.Background(), processedRun, LogMetrics{}, nil)

	assert.Empty(t, findingsWithCode(auditData.KeyFindings, AuditFindingEngineStepFailed))
	require.NotNil(t, auditData.Evals)
	assert.Equal(t, EvalsSummary{Questions: 2, Yes: 2, ExecutionState: "started"}, *auditData.Evals)
}

func TestAuditEngineStepFailure_NoEvalsOrDetectionUnchanged(t *testing.T) {
	t.Run("no evals or detection jobs", func(t *testing.T) {
		runDir := t.TempDir()
		writeEngineStepFixture(t, runDir, map[string]string{
			jobsAPIResponseFileName: `[{"total_count":1,"jobs":[{"id":1,"name":"agent","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":20,"started_at":"2026-10-29T21:00:00Z","completed_at":"2026-10-29T21:05:00Z"}]}]}]`,
			"workflow-logs/2_agent.txt": "2026-10-29T21:00:00.1000000Z ##[group]Run set -o pipefail\n2026-10-29T21:04:59.0000000Z ##[error]Process completed with exit code 1.\n",
		})
		processedRun := engineStepFixtureRun(t, runDir, 1, "No Evals", "success")

		assert.Empty(t, generateEngineStepFailureFindings(processedRun), "agent job engine steps are out of scope")
		auditData := buildAuditData(context.Background(), processedRun, LogMetrics{}, nil)
		assert.Equal(t, []AuditFindingCode{AuditFindingWorkflowSucceeded}, findingCodes(auditData.KeyFindings))
		assert.Nil(t, auditData.Evals)
	})

	t.Run("detection step succeeded", func(t *testing.T) {
		runDir := t.TempDir()
		healthyDetectionLog := strings.NewReplacer(
			"[health-check][ERROR] Cannot connect", "[health-check] ✓ Connected",
			"[health-check][ERROR] Proxy may not be running or network is blocked", "[health-check] ✓ All API proxy health checks passed",
			"Command completed with exit code: 1", "Command completed successfully",
			"Process exiting with code: 1", "Process exiting with code: 0",
			"2026-10-28T22:28:32.7749904Z ##[error]Process completed with exit code 1.\n", "",
		).Replace(detectionPreflightJobLog40779352178)
		writeEngineStepFixture(t, runDir, map[string]string{
			jobsAPIResponseFileName:          detectionPreflightJobs40779352178,
			"workflow-logs/3_detection.txt":  healthyDetectionLog,
			"usage/detection/execution.json": `{"version":1,"component":"detection","run_id":40779352178,"run_attempt":1,"state":"started"}` + "\n",
		})
		processedRun := engineStepFixtureRun(t, runDir, 40779352178, "Daily BYOK Ollama Test", "success")

		assert.Empty(t, generateEngineStepFailureFindings(processedRun))
		auditData := buildAuditData(context.Background(), processedRun, LogMetrics{}, nil)
		assert.Equal(t, []AuditFindingCode{AuditFindingWorkflowSucceeded}, findingCodes(auditData.KeyFindings))
		assert.Nil(t, auditData.Evals)
	})
}

func TestAuditEngineStepFailure_NestedStepLogLayout(t *testing.T) {
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName: detectionPreflightJobs40779352178,
		"workflow-logs/detection/22_Execute threat detection with AWF.txt": "2026-10-28T22:28:30.1447935Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002\n" +
			"2026-10-28T22:28:32.7749904Z ##[error]Process completed with exit code 1.\n",
	})
	processedRun := engineStepFixtureRun(t, runDir, 40779352178, "Daily BYOK Ollama Test", "failure")

	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 1, "the finding fires regardless of the run conclusion")
	assert.Contains(t, findings[0].Description, `step 22 "Execute threat detection with AWF"`)
	assert.Contains(t, findings[0].Description, "Cannot connect to Anthropic API proxy")
}

func TestAuditEngineStepFailure_ErrorLineSanitized(t *testing.T) {
	runDir := t.TempDir()
	// Built from parts so the fixture holds no literal credential.
	tokenValue := "gh" + "p_" + strings.Repeat("Ab12", 9)
	userinfo := "x-access-token:" + "s3cr3t" + "Passw0rd"
	leakyLog := strings.Replace(detectionPreflightJobLog40779352178,
		"[health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002\n",
		"[health-check][ERROR] Cannot connect to https://"+userinfo+"@proxy.example.com:10002 with token "+tokenValue+" and ANTHROPIC_API_KEY\n", 1)
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:         detectionPreflightJobs40779352178,
		"workflow-logs/3_detection.txt": leakyLog,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40779352178, "Daily BYOK Ollama Test", "success")

	findings := generateEngineStepFailureFindings(processedRun)

	require.Len(t, findings, 1)
	description := findings[0].Description
	assert.Contains(t, description, "Cannot connect to https://[REDACTED]@proxy.example.com:10002", "the error line keeps its diagnostic context")
	assert.NotContains(t, description, userinfo, "URL userinfo must not reach the finding")
	assert.NotContains(t, description, "x-access-token", "URL userinfo must not reach the finding")
	assert.NotContains(t, description, tokenValue, "token values must not reach the finding")
	assert.Contains(t, description, "with token [REDACTED]")
	assert.NotContains(t, description, "ANTHROPIC_API_KEY", "secret names must not reach the finding")
}

func TestAuditEngineStepFailure_EvalsSummaryOnlyAuditedRun(t *testing.T) {
	runDir := t.TempDir()
	olderRunRow := `{"id":"legacy_question","question":"Removed question","answer":"YES","model":"","timestamp":"2026-10-20T10:00:00.000Z","runid":"40000000000"}` + "\n"
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:     evalsPreflightJobs40955414354,
		"workflow-logs/3_evals.txt": evalsPreflightJobLog40955414354,
		// evals.jsonl restored from the evals state branch holds earlier runs' rows.
		"evals.jsonl": olderRunRow + evalsPreflightResults40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Claude Code User Documentation Review", "success")

	summary := buildEvalsSummary(processedRun.Run)

	require.NotNil(t, summary)
	assert.Equal(t, EvalsSummary{Questions: 2, Unknown: 2}, *summary, "only the audited run's answers are counted")
	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Description, "all 2 evals answers are UNKNOWN")
}

func TestAuditEngineStepFailure_SameSecondSuccessorStep(t *testing.T) {
	runDir := t.TempDir()
	jobLog := `2026-10-28T22:28:40.1000000Z ##[group]Run cp /tmp/gh-aw/aw-prompts/prompt.txt /tmp/gh-aw/threat-detection/prompt.txt
2026-10-28T22:28:41.1000000Z ##[group]Run mkdir -p "/tmp/gh-aw/threat-detection"
2026-10-28T22:28:41.2000000Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002
2026-10-28T22:28:41.3000000Z ##[error]Process completed with exit code 1.
2026-10-28T22:28:41.4000000Z ##[group]Run mkdir -p /tmp/gh-aw/threat-detection/sandbox/firewall
2026-10-28T22:28:41.5000000Z mkdir -p /tmp/gh-aw/threat-detection/sandbox/firewall
`
	jobs := `[{"total_count":1,"jobs":[{"id":1,"run_id":40779352178,"name":"detection","status":"completed","conclusion":"success","started_at":"2026-10-28T22:28:30Z","completed_at":"2026-10-28T22:28:42Z","steps":[
{"name":"Set up job","status":"completed","conclusion":"success","number":1,"started_at":"2026-10-28T22:28:30Z","completed_at":"2026-10-28T22:28:31Z"},
{"name":"Prepare threat detection files","status":"completed","conclusion":"success","number":21,"started_at":"2026-10-28T22:28:40Z","completed_at":"2026-10-28T22:28:41Z"},
{"name":"Execute threat detection with AWF","status":"completed","conclusion":"success","number":22,"started_at":"2026-10-28T22:28:41Z","completed_at":"2026-10-28T22:28:41Z"},
{"name":"Copy detection firewall logs","status":"completed","conclusion":"success","number":23,"started_at":"2026-10-28T22:28:41Z","completed_at":"2026-10-28T22:28:41Z"}]}]}]`
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:         jobs,
		"workflow-logs/3_detection.txt": jobLog,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40779352178, "Daily BYOK Ollama Test", "success")

	job := processedRun.JobDetails[0]
	lines := readEngineStepLogLines(filepath.Join(runDir, "workflow-logs"), job.JobInfo, job.Steps[2])
	require.Len(t, lines, 3, "step 22 spans its own header up to step 23's header")
	assert.Contains(t, lines[0], `##[group]Run mkdir -p "/tmp/gh-aw/threat-detection"`)

	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 1, "the engine's exit-code line must stay in its window when its successor starts in the same second")
	assert.Contains(t, findings[0].Description, `step 22 "Execute threat detection with AWF"`)
	assert.Contains(t, findings[0].Description, "Cannot connect to Anthropic API proxy")
}

func TestAuditEngineStepFailure_HeaderInSecondAfterAPIStart(t *testing.T) {
	runDir := t.TempDir()
	// The jobs API starts step 22 at 22:27:58Z, the same second as step 21, but the
	// runner logs step 22's header at 22:27:59.010Z.
	jobLog := strings.NewReplacer(
		"2026-10-28T22:27:58.5197572Z ##[group]Run mkdir", "2026-10-28T22:27:59.0100000Z ##[group]Run mkdir",
		"2026-10-28T22:27:58.5198127Z mkdir", "2026-10-28T22:27:59.0100500Z mkdir",
	).Replace(detectionPreflightJobLog40779352178)
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:         detectionPreflightJobs40779352178,
		"workflow-logs/3_detection.txt": jobLog,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40779352178, "Daily BYOK Ollama Test", "success")

	job := processedRun.JobDetails[0]
	lines := readEngineStepLogLines(filepath.Join(runDir, "workflow-logs"), job.JobInfo, job.Steps[1])
	require.NotEmpty(t, lines)
	assert.True(t, strings.HasPrefix(lines[0], "2026-10-28T22:27:59.0100000Z ##[group]Run mkdir"), "step 22 begins at its own header, not step 21's")
	for _, line := range lines {
		assert.NotContains(t, line, "threat-detect installation complete", "step 21 output attributed to step 22")
	}

	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 1, "the engine's non-zero exit must be reported when its header is logged after its API start second")
	assert.Contains(t, findings[0].Description, `step 22 "Execute threat detection with AWF"`)
	assert.Contains(t, findings[0].Description, "Cannot connect to Anthropic API proxy")
}

func TestAuditEngineStepFailure_CustomNamedEngineStep(t *testing.T) {
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:     strings.ReplaceAll(evalsPreflightJobs40955414354, "Execute Claude Code CLI", "Execute DeepSeek Harness"),
		"workflow-logs/3_evals.txt": evalsPreflightJobLog40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Smoke DeepSeek Harness", "success")

	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 1, "a custom-named engine step that exits non-zero must be reported")
	assert.Contains(t, findings[0].Description, `step 15 "Execute DeepSeek Harness"`)
	assert.Contains(t, findings[0].Description, "Cannot connect to Anthropic API proxy")
}

func TestAuditEngineStepFailure_FallbackNamedEngineStep(t *testing.T) {
	runDir := t.TempDir()
	// An engine without behaviors.execution.step-name compiles to "Execute <display name>".
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:     strings.ReplaceAll(evalsPreflightJobs40955414354, "Execute Claude Code CLI", "Execute Harness Engine"),
		"workflow-logs/3_evals.txt": evalsPreflightJobLog40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Smoke Harness Engine", "success")

	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 1, "a fallback-named engine step that exits non-zero must be reported")
	assert.Contains(t, findings[0].Description, `step 15 "Execute Harness Engine"`)
	assert.Contains(t, findings[0].Description, "Cannot connect to Anthropic API proxy")
}

func TestAuditEngineStepFailure_EvalsExecutionStatePerInvocation(t *testing.T) {
	invocationLog := func(prefix string) string {
		return `2026-10-29T21:18:59.0334305Z ##[group]Run set -o pipefail
2026-10-29T21:19:40.0000000Z Process exiting with code: 0
2026-10-29T21:19:44.1335928Z ##[group]Run actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9
2026-10-29T21:19:44.1336376Z with:
2026-10-29T21:19:44.1336400Z   name: ` + prefix + `evals
2026-10-29T21:19:44.1336500Z   if-no-files-found: ignore
`
	}
	jobs := `[{"total_count":2,"jobs":[
{"id":1,"run_id":40955414354,"name":"caller-a / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]},
{"id":2,"run_id":40955414354,"name":"caller-b / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]}]}]`
	writeFixture := func(t *testing.T, withLogs bool) ProcessedRun {
		runDir := t.TempDir()
		files := map[string]string{
			jobsAPIResponseFileName:               jobs,
			"aaaaaaaa-usage/evals/execution.json": `{"version":1,"component":"evals","run_id":40955414354,"run_attempt":1,"state":"not_started"}` + "\n",
			"bbbbbbbb-usage/evals/execution.json": `{"version":1,"component":"evals","run_id":40955414354,"run_attempt":1,"state":"started"}` + "\n",
		}
		if withLogs {
			files["workflow-logs/3_caller-a _ evals.txt"] = invocationLog("aaaaaaaa-")
			files["workflow-logs/4_caller-b _ evals.txt"] = invocationLog("bbbbbbbb-")
		}
		writeEngineStepFixture(t, runDir, files)
		return engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")
	}

	t.Run("evidence attributed by artifact prefix", func(t *testing.T) {
		findings := generateEngineStepFailureFindings(writeFixture(t, true))

		require.Len(t, findings, 1, "only the not_started invocation is reported")
		assert.Contains(t, findings[0].Description, `Job "caller-a / evals"`)
		assert.Contains(t, findings[0].Description, "execution state: not_started")
	})

	t.Run("ambiguous evidence is unavailable", func(t *testing.T) {
		processedRun := writeFixture(t, false)

		assert.Empty(t, generateEngineStepFailureFindings(processedRun), "conflicting unattributed evidence must not mark clean invocations as failed")
		assert.Empty(t, readEvalsExecutionState(processedRun.Run, "", false))
	})
}

func TestAuditEngineStepFailure_EvalsSummaryStreamsLargeHistory(t *testing.T) {
	olderRunRow := `{"id":"legacy_question","question":"Removed question with a long explanation of what was asked","answer":"YES","model":"","timestamp":"2026-10-20T10:00:00.000Z","runid":"40000000000"}` + "\n"
	history := strings.Repeat(olderRunRow, (1024*1024)/len(olderRunRow)+100)
	require.Greater(t, len(history), 1024*1024, "the history must exceed 1 MiB")

	t.Run("audited run records after the first MiB are counted", func(t *testing.T) {
		runDir := t.TempDir()
		writeEngineStepFixture(t, runDir, map[string]string{
			jobsAPIResponseFileName:     evalsPreflightJobs40955414354,
			"workflow-logs/3_evals.txt": evalsPreflightJobLog40955414354,
			// The evals state branch fallback writes the append-only history to the root.
			"evals.jsonl": history + evalsPreflightResults40955414354,
		})
		processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Claude Code User Documentation Review", "success")

		summary := buildEvalsSummary(processedRun.Run)

		require.NotNil(t, summary)
		assert.Equal(t, EvalsSummary{Questions: 2, Unknown: 2}, *summary)
		findings := generateEngineStepFailureFindings(processedRun)
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Description, "all 2 evals answers are UNKNOWN")
	})

	t.Run("read error makes the summary unavailable", func(t *testing.T) {
		runDir := t.TempDir()
		overlongLine := `{"id":"oversized","answer":"YES","runid":"40000000000","question":"` + strings.Repeat("x", evalsResultMaxLineBytes) + `"}` + "\n"
		writeEngineStepFixture(t, runDir, map[string]string{
			"evals.jsonl": evalsPreflightResults40955414354 + overlongLine,
		})
		processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Claude Code User Documentation Review", "success")

		assert.Nil(t, buildEvalsSummary(processedRun.Run), "a partially read file must not be summarized")
	})
}

func TestAuditEngineStepFailure_EvalsSummaryPerInvocation(t *testing.T) {
	invocationLog := func(prefix string, failed bool) string {
		exit := "2026-10-29T21:19:40.0000000Z Process exiting with code: 0\n"
		if failed {
			exit = "2026-10-29T21:19:40.0000000Z [health-check][ERROR] Cannot connect to Anthropic API proxy at http://host.docker.internal:10002\n" +
				"2026-10-29T21:19:43.0000000Z ##[error]Process completed with exit code 1.\n"
		}
		return "2026-10-29T21:18:59.0334305Z ##[group]Run set -o pipefail\n" + exit +
			"2026-10-29T21:19:44.1335928Z ##[group]Run actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9\n" +
			"2026-10-29T21:19:44.1336376Z with:\n" +
			"2026-10-29T21:19:44.1336400Z   name: " + prefix + "evals\n"
	}
	jobs := `[{"total_count":2,"jobs":[
{"id":1,"run_id":40955414354,"name":"caller-a / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]},
{"id":2,"run_id":40955414354,"name":"caller-b / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]}]}]`
	yesResults := strings.ReplaceAll(evalsPreflightResults40955414354, `"answer":"UNKNOWN"`, `"answer":"YES"`)
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:                jobs,
		"workflow-logs/3_caller-a _ evals.txt": invocationLog("aaaaaaaa-", true),
		"workflow-logs/4_caller-b _ evals.txt": invocationLog("bbbbbbbb-", true),
		"aaaaaaaa-evals/evals.jsonl":           yesResults,
		"aaaaaaaa-usage/evals.jsonl":           yesResults,
		"bbbbbbbb-evals/evals.jsonl":           evalsPreflightResults40955414354,
		"bbbbbbbb-usage/evals.jsonl":           evalsPreflightResults40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")

	assert.True(t, runHasEvals(runDir, false), "logs filters still see evals results")
	summary := buildEvalsSummary(processedRun.Run)
	require.NotNil(t, summary)
	assert.Equal(t, EvalsSummary{Questions: 4, Yes: 2, Unknown: 2}, *summary, "each invocation is counted once, from either its evals or usage copy")

	findings := generateEngineStepFailureFindings(processedRun)
	require.Len(t, findings, 2)
	assert.Contains(t, findings[0].Description, `Job "caller-a / evals"`)
	assert.NotContains(t, findings[0].Description, "UNKNOWN", "caller-a answered YES")
	assert.Contains(t, findings[1].Description, `Job "caller-b / evals"`)
	assert.Contains(t, findings[1].Description, "all 2 evals answers are UNKNOWN")
}

func TestAuditEngineStepFailure_JobLogMatchingKeepsJobIdentity(t *testing.T) {
	cleanLog := strings.NewReplacer(
		"[health-check][ERROR] Cannot connect", "[health-check] ✓ Connected",
		"[health-check][ERROR] Proxy may not be running or network is blocked", "[health-check] ✓ All API proxy health checks passed",
		"2026-10-28T22:28:32.7749904Z ##[error]Process completed with exit code 1.\n", "",
	).Replace(detectionPreflightJobLog40779352178)
	stepsJSON := detectionPreflightJobs40779352178[strings.Index(detectionPreflightJobs40779352178, `"steps":[`) : len(detectionPreflightJobs40779352178)-len("}]}]")]
	jobsJSON := func(names ...string) string {
		parts := make([]string, 0, len(names))
		for i, name := range names {
			parts = append(parts, `{"id":`+strconv.Itoa(i+1)+`,"run_id":40779352178,"name":"`+name+`","status":"completed","conclusion":"success",`+stepsJSON+`}`)
		}
		return `[{"total_count":` + strconv.Itoa(len(names)) + `,"jobs":[` + strings.Join(parts, ",") + `]}]`
	}
	run := func(t *testing.T, files map[string]string) ProcessedRun {
		runDir := t.TempDir()
		writeEngineStepFixture(t, runDir, files)
		return engineStepFixtureRun(t, runDir, 40779352178, "Reusable Detection", "success")
	}

	t.Run("qualified log wins over basename log", func(t *testing.T) {
		processedRun := run(t, map[string]string{
			jobsAPIResponseFileName:                    jobsJSON("caller-a / detection"),
			"workflow-logs/3_detection.txt":            detectionPreflightJobLog40779352178,
			"workflow-logs/4_caller-a _ detection.txt": cleanLog,
		})

		assert.Empty(t, generateEngineStepFailureFindings(processedRun), "another job's basename log must not be attributed to caller-a")
		job := processedRun.JobDetails[0]
		assert.Equal(t, filepath.Join(processedRun.Run.LogsPath, "workflow-logs", "4_caller-a _ detection.txt"), locateJobLogs(filepath.Join(processedRun.Run.LogsPath, "workflow-logs"), job.JobInfo).file)
	})

	t.Run("unique basename log is the fallback", func(t *testing.T) {
		processedRun := run(t, map[string]string{
			jobsAPIResponseFileName:         jobsJSON("caller-a / detection"),
			"workflow-logs/3_detection.txt": detectionPreflightJobLog40779352178,
		})

		require.Len(t, generateEngineStepFailureFindings(processedRun), 1)
	})

	t.Run("ambiguous basename logs are unavailable", func(t *testing.T) {
		processedRun := run(t, map[string]string{
			jobsAPIResponseFileName:         jobsJSON("caller-a / detection", "caller-b / detection"),
			"workflow-logs/3_detection.txt": detectionPreflightJobLog40779352178,
			"workflow-logs/4_detection.txt": cleanLog,
		})

		assert.Empty(t, generateEngineStepFailureFindings(processedRun))
		for _, job := range processedRun.JobDetails {
			assert.Nil(t, readJobLogLines(filepath.Join(processedRun.Run.LogsPath, "workflow-logs"), job.JobInfo), "job %q", job.Name)
		}
	})

	t.Run("caller-a and caller_a are distinct jobs", func(t *testing.T) {
		processedRun := run(t, map[string]string{
			jobsAPIResponseFileName:                    jobsJSON("caller-a / detection", "caller_a / detection"),
			"workflow-logs/3_caller_a _ detection.txt": detectionPreflightJobLog40779352178,
			"workflow-logs/4_caller-a _ detection.txt": cleanLog,
		})

		findings := generateEngineStepFailureFindings(processedRun)
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Description, `Job "caller_a / detection"`)
	})

	t.Run("upload prefix is read from the job's own log", func(t *testing.T) {
		uploadLog := func(prefix string) string {
			return "2026-10-29T21:19:44.1335928Z ##[group]Run actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9\n" +
				"2026-10-29T21:19:44.1336400Z   name: " + prefix + "evals\n"
		}
		processedRun := run(t, map[string]string{
			jobsAPIResponseFileName:                jobsJSON("caller-a / evals", "caller_a / evals"),
			"workflow-logs/3_evals.txt":            uploadLog("cccccccc-"),
			"workflow-logs/4_caller_a _ evals.txt": uploadLog("bbbbbbbb-"),
			"workflow-logs/5_caller-a _ evals.txt": uploadLog("aaaaaaaa-"),
		})
		workflowLogsDir := filepath.Join(processedRun.Run.LogsPath, "workflow-logs")

		for job, want := range map[int]string{0: "aaaaaaaa-", 1: "bbbbbbbb-"} {
			prefix, known := evalsArtifactPrefixFromLog(readJobLogLines(workflowLogsDir, processedRun.JobDetails[job].JobInfo))
			assert.True(t, known)
			assert.Equal(t, want, prefix, "job %q", processedRun.JobDetails[job].Name)
		}
	})
}

// evalsUploadJobLog is an evals job log whose upload step logs the evals artifact name.
func evalsUploadJobLog(prefix string) string {
	return "2026-10-29T21:18:59.0334305Z ##[group]Run set -o pipefail\n" +
		"2026-10-29T21:19:40.0000000Z Process exiting with code: 0\n" +
		"2026-10-29T21:19:44.1335928Z ##[group]Run actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9\n" +
		"2026-10-29T21:19:44.1336376Z with:\n" +
		"2026-10-29T21:19:44.1336400Z   name: " + prefix + "evals\n"
}

func evalsExecutionEvidence(attempt int, state string) string {
	return `{"version":1,"component":"evals","run_id":40955414354,"run_attempt":` + strconv.Itoa(attempt) + `,"state":"` + state + `"}` + "\n"
}

const reusableEvalsJobs40955414354 = `[{"total_count":1,"jobs":[
{"id":1,"run_id":40955414354,"name":"caller / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]}]}]`

func TestAuditEngineStepFailure_EvalsSummaryRerunCountsAuditedAttempt(t *testing.T) {
	yesResults := strings.ReplaceAll(evalsPreflightResults40955414354, `"answer":"UNKNOWN"`, `"answer":"YES"`)
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		// jobs.json and the job log come from the audited attempt 2.
		jobsAPIResponseFileName:               reusableEvalsJobs40955414354,
		"workflow-logs/3_caller _ evals.txt":  evalsUploadJobLog("a2a2a2a2-"),
		"a1a1a1a1-evals/evals.jsonl":          evalsPreflightResults40955414354,
		"a1a1a1a1-evals/evals/execution.json": evalsExecutionEvidence(1, "not_started"),
		"a1a1a1a1-usage/evals.jsonl":          evalsPreflightResults40955414354,
		"a1a1a1a1-usage/evals/execution.json": evalsExecutionEvidence(1, "not_started"),
		"a2a2a2a2-evals/evals.jsonl":          yesResults,
		"a2a2a2a2-evals/evals/execution.json": evalsExecutionEvidence(2, "started"),
		"a2a2a2a2-usage/evals.jsonl":          yesResults,
		"a2a2a2a2-usage/evals/execution.json": evalsExecutionEvidence(2, "started"),
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")
	processedRun.Run.Attempt = 2

	summary := buildEvalsSummary(processedRun.Run)

	require.NotNil(t, summary)
	assert.Equal(t, EvalsSummary{Questions: 2, Yes: 2, ExecutionState: "started"}, *summary, "only attempt 2's invocation is counted")

	t.Run("execution evidence attributes prefixes without job logs", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(filepath.Join(runDir, "workflow-logs")))

		summary := buildEvalsSummary(processedRun.Run)

		require.NotNil(t, summary)
		assert.Equal(t, EvalsSummary{Questions: 2, Yes: 2, ExecutionState: "started"}, *summary)
	})
}

func TestAuditEngineStepFailure_EvalsSummaryFlattenedPrefixedArtifact(t *testing.T) {
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName:              reusableEvalsJobs40955414354,
		"workflow-logs/3_caller _ evals.txt": evalsUploadJobLog("aaaaaaaa-"),
		// The single-file aaaaaaaa-evals artifact was flattened to the run root.
		"evals.jsonl":                evalsPreflightResults40955414354,
		"aaaaaaaa-usage/evals.jsonl": evalsPreflightResults40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")

	summary := buildEvalsSummary(processedRun.Run)

	require.NotNil(t, summary)
	assert.Equal(t, EvalsSummary{Questions: 2, Unknown: 2}, *summary, "the flattened root copy is not counted as another invocation")

	t.Run("root copy is used when it is the only copy", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(filepath.Join(runDir, "aaaaaaaa-usage")))

		summary := buildEvalsSummary(processedRun.Run)

		require.NotNil(t, summary)
		assert.Equal(t, EvalsSummary{Questions: 2, Unknown: 2}, *summary)
	})
}

func writeEvalsInvocationsWithoutAttemptEvidence(t *testing.T, attempt int) ProcessedRun {
	t.Helper()
	yesResults := strings.ReplaceAll(evalsPreflightResults40955414354, `"answer":"UNKNOWN"`, `"answer":"YES"`)
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		"aaaaaaaa-evals/evals.jsonl": yesResults,
		"aaaaaaaa-usage/evals.jsonl": yesResults,
		"bbbbbbbb-evals/evals.jsonl": evalsPreflightResults40955414354,
		"bbbbbbbb-usage/evals.jsonl": evalsPreflightResults40955414354,
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")
	processedRun.Run.Attempt = attempt
	return processedRun
}

func TestAuditEngineStepFailure_EvalsSummaryFirstAttemptWithoutAttemptEvidence(t *testing.T) {
	processedRun := writeEvalsInvocationsWithoutAttemptEvidence(t, 1)

	summary := buildEvalsSummary(processedRun.Run)

	require.NotNil(t, summary, "a first attempt has no reruns, so every invocation belongs to it")
	assert.Equal(t, EvalsSummary{Questions: 4, Yes: 2, Unknown: 2}, *summary)
}

func TestAuditEngineStepFailure_EvalsSummaryRerunWithoutAttemptEvidence(t *testing.T) {
	processedRun := writeEvalsInvocationsWithoutAttemptEvidence(t, 2)

	assert.Nil(t, buildEvalsSummary(processedRun.Run), "invocations that cannot be attributed to the audited attempt must not be summed")
}

func writeEvalsInvocationsWithAttemptEvidence(t *testing.T, attemptA, attemptB int) ProcessedRun {
	t.Helper()
	yesResults := strings.ReplaceAll(evalsPreflightResults40955414354, `"answer":"UNKNOWN"`, `"answer":"YES"`)
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		"aaaaaaaa-evals/evals.jsonl":          yesResults,
		"aaaaaaaa-evals/evals/execution.json": evalsExecutionEvidence(attemptA, "started"),
		"bbbbbbbb-evals/evals.jsonl":          evalsPreflightResults40955414354,
		"bbbbbbbb-evals/evals/execution.json": evalsExecutionEvidence(attemptB, "started"),
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")
	// Cached artifacts without run metadata leave the audited attempt unknown.
	processedRun.Run.Attempt = 0
	return processedRun
}

func TestAuditEngineStepFailure_EvalsSummaryUnknownAttemptMixedEvidence(t *testing.T) {
	processedRun := writeEvalsInvocationsWithAttemptEvidence(t, 1, 2)

	assert.Nil(t, buildEvalsSummary(processedRun.Run), "an unknown audited attempt must not sum invocations of different attempts")
}

func TestAuditEngineStepFailure_EvalsSummaryUnknownAttemptSingleAttemptEvidence(t *testing.T) {
	processedRun := writeEvalsInvocationsWithAttemptEvidence(t, 1, 1)

	summary := buildEvalsSummary(processedRun.Run)

	require.NotNil(t, summary)
	assert.Equal(t, 4, summary.Questions)
	assert.Equal(t, 2, summary.Yes)
	assert.Equal(t, 2, summary.Unknown)
}

func TestAuditEngineStepFailure_EvalsSummarySharedJobLogLocation(t *testing.T) {
	jobs := `[{"total_count":2,"jobs":[
{"id":1,"run_id":40955414354,"name":"caller-a / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]},
{"id":2,"run_id":40955414354,"name":"caller-b / evals","status":"completed","conclusion":"success","steps":[
{"name":"Execute Claude Code CLI","status":"completed","conclusion":"success","number":15,"started_at":"2026-10-29T21:18:59Z","completed_at":"2026-10-29T21:19:44Z"}]}]}]`
	yesResults := strings.ReplaceAll(evalsPreflightResults40955414354, `"answer":"UNKNOWN"`, `"answer":"YES"`)
	runDir := t.TempDir()
	writeEngineStepFixture(t, runDir, map[string]string{
		jobsAPIResponseFileName: jobs,
		// Both jobs fall back to the same base-name log.
		"workflow-logs/3_evals.txt":           evalsUploadJobLog("aaaaaaaa-"),
		"aaaaaaaa-evals/evals.jsonl":          yesResults,
		"aaaaaaaa-evals/evals/execution.json": evalsExecutionEvidence(1, "started"),
		"bbbbbbbb-evals/evals.jsonl":          evalsPreflightResults40955414354,
		"bbbbbbbb-evals/evals/execution.json": evalsExecutionEvidence(1, "started"),
	})
	processedRun := engineStepFixtureRun(t, runDir, 40955414354, "Reusable Evals", "success")

	prefixes, complete := evalsUploadPrefixesFromJobLogs(runDir)
	assert.False(t, complete, "a log location shared by two evals jobs cannot identify both prefixes")
	assert.Equal(t, map[string]bool{"aaaaaaaa-": true}, prefixes)

	summary := buildEvalsSummary(processedRun.Run)

	require.NotNil(t, summary)
	assert.Equal(t, 4, summary.Questions, "execution evidence attributes the other invocation")
	assert.Equal(t, 2, summary.Yes)
	assert.Equal(t, 2, summary.Unknown)
}
