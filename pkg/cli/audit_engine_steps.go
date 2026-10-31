package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var auditEngineStepsLog = logger.New("cli:audit_engine_steps")

const (
	healthCheckErrorMarker        = "[health-check][ERROR]"
	ghaErrorMarker                = "##[error]"
	engineStepErrorMaxLen         = 500
	evalsExecutionStateNotStarted = "not_started"
)

const ghaProcessExitCodeMarker = "##[error]Process completed with exit code "

// EvalsSummary summarizes the BinEval answers and execution evidence of the evals job.
type EvalsSummary struct {
	Questions      int    `json:"questions"`
	Yes            int    `json:"yes"`
	No             int    `json:"no"`
	Unknown        int    `json:"unknown"`
	ExecutionState string `json:"execution_state,omitempty"`
}

// allUnknown reports whether every evals answer is UNKNOWN.
func (s *EvalsSummary) allUnknown() bool {
	return s != nil && s.Questions > 0 && s.Unknown == s.Questions
}

// engineStepFailure describes a failed compiler-emitted continue-on-error engine step.
type engineStepFailure struct {
	Job            string
	StepName       string
	StepNumber     int
	ErrorLine      string
	ExecutionState string
}

// buildEvalsSummary reads evals.jsonl and the evals execution evidence for a run.
// It returns nil when the run has no evals results.
func buildEvalsSummary(run WorkflowRun) *EvalsSummary {
	if run.LogsPath == "" {
		return nil
	}
	path := findEvalsResultFile(run.LogsPath)
	if path == "" {
		return nil
	}
	data, err := readFileLimited(path, 1024*1024)
	if err != nil {
		auditEngineStepsLog.Printf("Failed to read evals results %s: %v", path, err)
		return nil
	}
	answers := map[string]string{}
	order := []string{}
	for _, record := range parseEvalResultRecords(data) {
		if _, seen := answers[record.ID]; !seen {
			order = append(order, record.ID)
		}
		answers[record.ID] = strings.ToUpper(strings.TrimSpace(record.Answer))
	}
	summary := &EvalsSummary{Questions: len(order), ExecutionState: readEvalsExecutionState(run)}
	for _, id := range order {
		switch answers[id] {
		case "YES":
			summary.Yes++
		case "NO":
			summary.No++
		default:
			summary.Unknown++
		}
	}
	return summary
}

func readFileLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer closeAuditEngineStepFile(file, path)
	return io.ReadAll(io.LimitReader(file, limit))
}

func closeAuditEngineStepFile(file *os.File, path string) {
	if err := file.Close(); err != nil {
		auditEngineStepsLog.Printf("Failed to close %s: %v", path, err)
	}
}

// readEvalsExecutionState returns the evals execution evidence state recorded by the
// evals job ("not_started" or "started"), or an empty string when unavailable.
func readEvalsExecutionState(run WorkflowRun) string {
	if run.LogsPath == "" {
		return ""
	}
	component := string(constants.EvalsJobName)
	candidates := []string{
		filepath.Join(run.LogsPath, constants.UsageArtifactName.String(), component, "execution.json"),
		filepath.Join(run.LogsPath, constants.EvalsArtifactName.String(), component, "execution.json"),
	}
	if entries, err := os.ReadDir(run.LogsPath); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() && (strings.HasSuffix(name, "-"+constants.UsageArtifactName.String()) ||
				strings.HasSuffix(name, "-"+constants.EvalsArtifactName.String())) {
				candidates = append(candidates, filepath.Join(run.LogsPath, name, component, "execution.json"))
			}
		}
	}
	for _, candidate := range candidates {
		data, err := readFileLimited(candidate, 1024*1024)
		if err != nil {
			continue
		}
		var evidence detectionExecutionEvidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			continue
		}
		if evidence.Version != 1 || evidence.Component != component || evidence.State == "" {
			continue
		}
		if run.DatabaseID != 0 && evidence.RunID != 0 && evidence.RunID != run.DatabaseID {
			continue
		}
		if run.Attempt != 0 && evidence.RunAttempt != 0 && evidence.RunAttempt != run.Attempt {
			continue
		}
		return evidence.State
	}
	return ""
}

// generateEngineStepFailureFindings reports compiler-emitted continue-on-error engine
// or judge steps in the evals and detection jobs that failed while their job stayed
// green. It reads the cached jobs API steps, the step's time window in the downloaded
// job log, and the evals execution evidence. It does not depend on the run conclusion.
func generateEngineStepFailureFindings(processedRun ProcessedRun) []AuditFinding {
	failures := findEngineStepFailures(processedRun)
	if len(failures) == 0 {
		return nil
	}
	evals := buildEvalsSummary(processedRun.Run)
	findings := make([]AuditFinding, 0, len(failures))
	for _, failure := range failures {
		findings = append(findings, buildEngineStepFailureFinding(failure, evals))
	}
	return findings
}

func buildEngineStepFailureFinding(failure engineStepFailure, evals *EvalsSummary) AuditFinding {
	step := "engine step"
	if failure.StepName != "" {
		step = fmt.Sprintf("step %d %q", failure.StepNumber, failure.StepName)
	}
	description := fmt.Sprintf("Job %q %s failed under continue-on-error", failure.Job, step)
	if failure.ErrorLine != "" {
		description += ": " + failure.ErrorLine
	}
	if failure.ExecutionState != "" {
		description += " (execution state: " + failure.ExecutionState + ")"
	}
	title := "Threat Detection Engine Step Failed"
	impact := "The detection job can report success without a threat verdict; the security check did not complete"
	if failure.Job == string(constants.EvalsJobName) {
		title = "Evals Judge Step Failed"
		impact = "The evals job can report success without judging; evals answers are not a real result"
		if evals.allUnknown() {
			description += fmt.Sprintf("; all %d evals answers are UNKNOWN", evals.Questions)
		}
	}
	return AuditFinding{
		Code:        AuditFindingEngineStepFailed,
		Category:    "error",
		Severity:    "high",
		Title:       title,
		Description: description,
		Impact:      impact,
	}
}

func findEngineStepFailures(processedRun ProcessedRun) []engineStepFailure {
	run := processedRun.Run
	jobs := processedRun.JobDetails
	if len(jobs) == 0 {
		jobs = loadCachedJobDetails(run.LogsPath)
	}
	workflowLogsDir := ""
	if run.LogsPath != "" {
		workflowLogsDir = filepath.Join(run.LogsPath, "workflow-logs")
	}
	evalsState := ""
	var failures []engineStepFailure
	evalsReported := false
	for _, job := range jobs {
		kind := engineStepJobKind(job.Name)
		if kind == "" || strings.EqualFold(strings.TrimSpace(job.Conclusion), "skipped") {
			continue
		}
		if kind == string(constants.EvalsJobName) && evalsState == "" {
			evalsState = readEvalsExecutionState(run)
		}
		var engineStep *JobStep
		for i := range job.Steps {
			step := job.Steps[i]
			if !constants.IsEngineExecutionStepName(step.Name) || strings.EqualFold(step.Conclusion, "skipped") {
				continue
			}
			engineStep = &job.Steps[i]
			lines := readEngineStepLogLines(workflowLogsDir, job.JobInfo, step)
			failed, errorLine := engineStepFailureFromLog(lines)
			notStarted := kind == string(constants.EvalsJobName) && evalsState == evalsExecutionStateNotStarted
			if !failed && !notStarted {
				continue
			}
			failure := engineStepFailure{Job: kind, StepName: step.Name, StepNumber: step.Number, ErrorLine: errorLine}
			if kind == string(constants.EvalsJobName) {
				failure.ExecutionState = evalsState
				evalsReported = true
			}
			auditEngineStepsLog.Printf("Engine step failed: job=%s step=%d name=%q", kind, step.Number, step.Name)
			failures = append(failures, failure)
		}
		if kind == string(constants.EvalsJobName) && engineStep == nil && !evalsReported && evalsState == evalsExecutionStateNotStarted {
			failures = append(failures, engineStepFailure{Job: kind, ExecutionState: evalsState})
			evalsReported = true
		}
	}
	return failures
}

// engineStepJobKind returns "evals" or "detection" when the job runs a compiler-emitted
// engine or judge step under continue-on-error, or an empty string otherwise.
// Reusable workflow jobs are named "<caller> / <job>"; only the final segment is compared.
func engineStepJobKind(jobName string) string {
	name := jobName
	if idx := strings.LastIndex(name, " / "); idx >= 0 {
		name = name[idx+len(" / "):]
	}
	switch normalizeJobName(name) {
	case string(constants.EvalsJobName):
		return string(constants.EvalsJobName)
	case string(constants.DetectionJobName):
		return string(constants.DetectionJobName)
	}
	return ""
}

func loadCachedJobDetails(logsPath string) []JobInfoWithDuration {
	if logsPath == "" {
		return nil
	}
	data, err := readFileLimited(filepath.Join(logsPath, jobsAPIResponseFileName), 16*1024*1024)
	if err != nil {
		return nil
	}
	jobs, _, err := parseJobsAPIResponses(data, 0, false)
	if err != nil {
		auditEngineStepsLog.Printf("Failed to parse cached jobs API response: %v", err)
		return nil
	}
	return jobs
}

// readEngineStepLogLines returns the log lines of a job step. Nested per-step log
// files are read whole; flat per-job logs are limited to the step's
// started_at..completed_at window from the jobs API.
func readEngineStepLogLines(workflowLogsDir string, job JobInfo, step JobStep) []string {
	if workflowLogsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(workflowLogsDir)
	if err != nil {
		return nil
	}
	jobKey := normalizeJobName(job.Name)
	jobBaseKey := engineStepJobKind(job.Name)
	matchesJob := func(name string) bool {
		key := normalizeJobName(name)
		return key == jobKey || key == jobBaseKey
	}
	for _, entry := range entries {
		if !entry.IsDir() || !matchesJob(entry.Name()) {
			continue
		}
		stepFiles, err := os.ReadDir(filepath.Join(workflowLogsDir, entry.Name()))
		if err != nil {
			continue
		}
		for _, stepFile := range stepFiles {
			if stepFile.IsDir() || !strings.HasSuffix(stepFile.Name(), ".txt") {
				continue
			}
			if num, _ := parseStepFilename(stepFile.Name()); num == step.Number && num > 0 {
				return readLogLines(filepath.Join(workflowLogsDir, entry.Name(), stepFile.Name()))
			}
		}
	}
	if step.StartedAt.IsZero() || step.CompletedAt.IsZero() {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		num, name := parseStepFilename(entry.Name())
		if num <= 0 || !matchesJob(name) {
			continue
		}
		return filterLogLinesByWindow(readLogLines(filepath.Join(workflowLogsDir, entry.Name())), step.StartedAt, step.CompletedAt)
	}
	return nil
}

func readLogLines(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer closeAuditEngineStepFile(file, path)
	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		auditEngineStepsLog.Printf("Failed to read step log %s: %v", path, err)
	}
	return lines
}

// filterLogLinesByWindow keeps the lines of one step from a flat job log. It first
// keeps lines whose GitHub Actions timestamp falls within [start, end]; the jobs API
// reports whole seconds, so the window covers the full start and completed seconds and
// lines without a timestamp inherit the previous decision. Neighbouring steps can log
// in those same seconds, so the result is then narrowed to the step's own
// "##[group]Run " header (the last one logged in the start second) up to the next
// step header.
func filterLogLinesByWindow(lines []string, start, end time.Time) []string {
	windowStart := start.Truncate(time.Second)
	windowEnd := end.Truncate(time.Second).Add(time.Second)
	startSecondEnd := windowStart.Add(time.Second)
	type timedLine struct {
		text string
		ts   time.Time
	}
	var kept []timedLine
	inWindow := false
	var lastTS time.Time
	for _, line := range lines {
		if ts, ok := parseGHALogTimestamp(line); ok {
			inWindow = !ts.Before(windowStart) && ts.Before(windowEnd)
			lastTS = ts
		}
		if inWindow {
			kept = append(kept, timedLine{text: line, ts: lastTS})
		}
	}
	begin := -1
	for i, entry := range kept {
		if isStepHeaderLine(entry.text) && entry.ts.Before(startSecondEnd) {
			begin = i
		}
	}
	finalSecond := end.Truncate(time.Second)
	stop := len(kept)
	for i, entry := range kept {
		if i > begin && isStepHeaderLine(entry.text) && (begin >= 0 || !entry.ts.Before(finalSecond)) {
			stop = i
			break
		}
	}
	result := make([]string, 0, stop-max(begin, 0))
	for _, entry := range kept[max(begin, 0):stop] {
		result = append(result, entry.text)
	}
	return result
}

func isStepHeaderLine(line string) bool {
	return strings.HasPrefix(stripGHALogTimestamps(line), "##[group]Run ")
}

func parseGHALogTimestamp(line string) (time.Time, bool) {
	if !ghaLogTimestampPrefixPattern.MatchString(line) {
		return time.Time{}, false
	}
	prefix, _, found := strings.Cut(line, " ")
	if !found {
		prefix = line
	}
	ts, err := time.Parse(time.RFC3339Nano, prefix)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// engineStepFailureFromLog reports whether the step log shows a non-zero process exit
// and returns the first diagnostic line, preferring AWF health-check errors over
// GitHub Actions ##[error] annotations.
func engineStepFailureFromLog(lines []string) (bool, string) {
	failed := false
	firstHealthCheck := ""
	firstGHAError := ""
	for _, raw := range lines {
		line := stripGHALogTimestamps(raw)
		if _, rawCode, found := strings.Cut(line, ghaProcessExitCodeMarker); found {
			code, err := strconv.Atoi(strings.TrimRight(strings.TrimSpace(rawCode), "."))
			if err == nil && code != 0 {
				failed = true
			}
		}
		if firstHealthCheck == "" {
			if _, message, found := strings.Cut(line, healthCheckErrorMarker); found {
				firstHealthCheck = strings.TrimSpace(message)
			}
		}
		if firstGHAError == "" && !isAgentToolResultAnnotation(line) {
			if _, message, found := strings.Cut(line, ghaErrorMarker); found {
				firstGHAError = strings.TrimSpace(message)
			}
		}
	}
	errorLine := firstHealthCheck
	if errorLine == "" {
		errorLine = firstGHAError
	}
	return failed, stringutil.Truncate(errorLine, engineStepErrorMaxLen)
}
