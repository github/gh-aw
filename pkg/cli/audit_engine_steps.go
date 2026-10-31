package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

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

var (
	engineStepURLUserinfoPattern = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@]+@`)
	engineStepTokenValuePattern  = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,}|sk-[A-Za-z0-9_-]{16,})`)
	engineStepBearerPattern      = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{8,}`)
	// evalsArtifactNamePattern matches constants.EvalsArtifactName with an optional
	// workflow_call artifact prefix computed by compute_artifact_prefix.sh.
	evalsArtifactNamePattern = regexp.MustCompile(`^(?:[0-9a-f]{8}-)?evals$`)
)

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
	Kind           string
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
	runID := ""
	if run.DatabaseID != 0 {
		runID = strconv.FormatInt(run.DatabaseID, 10)
	}
	for _, record := range parseEvalResultRecords(data) {
		// evals.jsonl restored from the evals state branch also holds other runs' rows.
		if runID != "" && record.RunID != "" && strings.TrimSpace(record.RunID) != runID {
			continue
		}
		if _, seen := answers[record.ID]; !seen {
			order = append(order, record.ID)
		}
		answers[record.ID] = strings.ToUpper(strings.TrimSpace(record.Answer))
	}
	summary := &EvalsSummary{Questions: len(order), ExecutionState: readEvalsExecutionState(run, "", false)}
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
// Reusable-workflow invocations upload separate "<hash>-usage" and "<hash>-evals"
// artifacts with the same run ID and attempt. When prefixKnown is true, only evidence
// from the artifacts with that prefix ("" for unprefixed artifacts) is used. Otherwise
// the state is returned only when every matching artifact records the same state.
func readEvalsExecutionState(run WorkflowRun, prefix string, prefixKnown bool) string {
	if run.LogsPath == "" {
		return ""
	}
	component := string(constants.EvalsJobName)
	artifactNames := []string{constants.UsageArtifactName.String(), constants.EvalsArtifactName.String()}
	type candidate struct {
		prefix string
		path   string
	}
	var candidates []candidate
	for _, name := range artifactNames {
		candidates = append(candidates, candidate{path: filepath.Join(run.LogsPath, name, component, "execution.json")})
	}
	if entries, err := os.ReadDir(run.LogsPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, name := range artifactNames {
				if hash, ok := strings.CutSuffix(entry.Name(), "-"+name); ok && hash != "" {
					candidates = append(candidates, candidate{prefix: hash + "-", path: filepath.Join(run.LogsPath, entry.Name(), component, "execution.json")})
				}
			}
		}
	}
	state := ""
	for _, c := range candidates {
		if prefixKnown && c.prefix != prefix {
			continue
		}
		candidateState := readEvalsExecutionEvidenceState(c.path, component, run)
		if candidateState == "" {
			continue
		}
		if prefixKnown {
			return candidateState
		}
		if state != "" && state != candidateState {
			auditEngineStepsLog.Printf("Evals execution evidence is ambiguous across invocations: %q and %q", state, candidateState)
			return ""
		}
		state = candidateState
	}
	return state
}

func readEvalsExecutionEvidenceState(path, component string, run WorkflowRun) string {
	data, err := readFileLimited(path, 1024*1024)
	if err != nil {
		return ""
	}
	var evidence detectionExecutionEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return ""
	}
	if evidence.Version != 1 || evidence.Component != component || evidence.State == "" {
		return ""
	}
	if run.DatabaseID != 0 && evidence.RunID != 0 && evidence.RunID != run.DatabaseID {
		return ""
	}
	if run.Attempt != 0 && evidence.RunAttempt != 0 && evidence.RunAttempt != run.Attempt {
		return ""
	}
	return evidence.State
}

// evalsArtifactPrefixFromLog returns the artifact name prefix ("" or "<hash>-") that an
// evals job used, read from the "name:" input that GitHub Actions logs for the step
// uploading the evals artifact. The second result is false when no such input is logged.
func evalsArtifactPrefixFromLog(lines []string) (string, bool) {
	for _, raw := range lines {
		line := strings.TrimSpace(stripGHALogTimestamps(raw))
		value, found := strings.CutPrefix(line, "name: ")
		if !found {
			continue
		}
		if name := strings.TrimSpace(value); evalsArtifactNamePattern.MatchString(name) {
			return strings.TrimSuffix(name, constants.EvalsArtifactName.String()), true
		}
	}
	return "", false
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
	if failure.Kind == string(constants.EvalsJobName) {
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
	var failures []engineStepFailure
	for _, job := range jobs {
		kind := engineStepJobKind(job.Name)
		if kind == "" || strings.EqualFold(strings.TrimSpace(job.Conclusion), "skipped") {
			continue
		}
		isEvals := kind == string(constants.EvalsJobName)
		evalsState := ""
		if isEvals {
			prefix, prefixKnown := evalsArtifactPrefixFromLog(readJobLogLines(workflowLogsDir, job.JobInfo))
			evalsState = readEvalsExecutionState(run, prefix, prefixKnown)
		}
		notStarted := isEvals && evalsState == evalsExecutionStateNotStarted
		hasEngineStep := false
		for _, step := range job.Steps {
			if !constants.IsEngineExecutionStepName(step.Name) || strings.EqualFold(step.Conclusion, "skipped") {
				continue
			}
			hasEngineStep = true
			failed, errorLine := engineStepFailureFromLog(readEngineStepLogLines(workflowLogsDir, job.JobInfo, step))
			if !failed && !notStarted {
				continue
			}
			failure := engineStepFailure{Job: job.Name, Kind: kind, StepName: step.Name, StepNumber: step.Number, ErrorLine: errorLine}
			if isEvals {
				failure.ExecutionState = evalsState
			}
			auditEngineStepsLog.Printf("Engine step failed: job=%s step=%d name=%q", job.Name, step.Number, step.Name)
			failures = append(failures, failure)
		}
		if notStarted && !hasEngineStep {
			failures = append(failures, engineStepFailure{Job: job.Name, Kind: kind, ExecutionState: evalsState})
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

// jobLogMatcher reports whether a downloaded log file or directory name belongs to job.
// Names are compared on letters and digits only, so reusable workflow jobs
// ("<caller> / <job>") match however the "/" was written in the log archive.
func jobLogMatcher(job JobInfo) func(string) bool {
	jobKey := jobLogKey(job.Name)
	jobBaseKey := engineStepJobKind(job.Name)
	return func(name string) bool {
		key := jobLogKey(name)
		return key == jobKey || key == jobBaseKey
	}
}

func jobLogKey(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}

// readJobLogLines returns every log line of a job: the concatenated nested per-step
// log files when present, otherwise the flat per-job log.
func readJobLogLines(workflowLogsDir string, job JobInfo) []string {
	if workflowLogsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(workflowLogsDir)
	if err != nil {
		return nil
	}
	matchesJob := jobLogMatcher(job)
	for _, entry := range entries {
		if !entry.IsDir() || !matchesJob(entry.Name()) {
			continue
		}
		stepFiles, err := os.ReadDir(filepath.Join(workflowLogsDir, entry.Name()))
		if err != nil {
			continue
		}
		var lines []string
		for _, stepFile := range stepFiles {
			if !stepFile.IsDir() && strings.HasSuffix(stepFile.Name(), ".txt") {
				lines = append(lines, readLogLines(filepath.Join(workflowLogsDir, entry.Name(), stepFile.Name()))...)
			}
		}
		if len(lines) > 0 {
			return lines
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		if num, name := parseStepFilename(entry.Name()); num > 0 && matchesJob(name) {
			return readLogLines(filepath.Join(workflowLogsDir, entry.Name()))
		}
	}
	return nil
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
	matchesJob := jobLogMatcher(job)
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
		return filterLogLinesByWindow(readLogLines(filepath.Join(workflowLogsDir, entry.Name())), job.Steps, step)
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
// keeps lines whose GitHub Actions timestamp falls within the step's
// [started_at, completed_at] window; the jobs API reports whole seconds, so the window
// covers the full start and completed seconds and lines without a timestamp inherit
// the previous decision. Neighbouring steps can log in those same seconds, so the
// result is then narrowed to the step's own "##[group]Run " header up to the next step
// header. The step's header is identified from the jobs API steps that start in the
// same second: the step's position among them is its position among the headers
// logged in that second.
func filterLogLinesByWindow(lines []string, steps []JobStep, step JobStep) []string {
	windowStart := step.StartedAt.Truncate(time.Second)
	windowEnd := step.CompletedAt.Truncate(time.Second).Add(time.Second)
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
	var startSecondHeaders []int
	for i, entry := range kept {
		if isStepHeaderLine(entry.text) && entry.ts.Before(startSecondEnd) {
			startSecondHeaders = append(startSecondHeaders, i)
		}
	}
	begin := -1
	if position := min(sameSecondPredecessorSteps(steps, step), len(startSecondHeaders)-1); position >= 0 && position < len(startSecondHeaders) {
		begin = startSecondHeaders[position]
	}
	finalSecond := step.CompletedAt.Truncate(time.Second)
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

// sameSecondPredecessorSteps counts the steps that start in the same second as step,
// run before it and log a "##[group]Run " header.
func sameSecondPredecessorSteps(steps []JobStep, step JobStep) int {
	startSecond := step.StartedAt.Truncate(time.Second)
	count := 0
	for _, other := range steps {
		if other.Number <= 0 || other.Number >= step.Number || !other.StartedAt.Truncate(time.Second).Equal(startSecond) {
			continue
		}
		if strings.EqualFold(other.Conclusion, "skipped") || !stepLogsRunHeader(other.Name) {
			continue
		}
		count++
	}
	return count
}

// stepLogsRunHeader reports whether a step logs a "##[group]Run " header. Runner-managed
// steps and post-job steps do not.
func stepLogsRunHeader(name string) bool {
	switch name {
	case "Set up job", "Complete job", "Initialize containers", "Stop containers":
		return false
	}
	return !strings.HasPrefix(name, "Post ")
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
	return failed, stringutil.Truncate(sanitizeEngineStepErrorLine(errorLine), engineStepErrorMaxLen)
}

// sanitizeEngineStepErrorLine redacts secret-shaped names, token values, bearer
// credentials and URL userinfo from a log-derived error line, and replaces control
// characters with spaces, before the line reaches audit findings.
func sanitizeEngineStepErrorLine(line string) string {
	if line == "" {
		return line
	}
	sanitized := stringutil.SanitizeErrorMessage(line)
	sanitized = engineStepURLUserinfoPattern.ReplaceAllString(sanitized, "${1}[REDACTED]@")
	sanitized = engineStepTokenValuePattern.ReplaceAllString(sanitized, "[REDACTED]")
	sanitized = engineStepBearerPattern.ReplaceAllString(sanitized, "${1} [REDACTED]")
	sanitized = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, sanitized)
	return strings.TrimSpace(sanitized)
}
