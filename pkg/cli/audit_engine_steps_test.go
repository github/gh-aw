//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
