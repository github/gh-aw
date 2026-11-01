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
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var auditEngineStepsLog = logger.New("cli:audit_engine_steps")

const (
	healthCheckErrorMarker        = "[health-check][ERROR]"
	ghaErrorMarker                = "##[error]"
	engineStepErrorMaxLen         = 500
	evalsResultMaxLineBytes       = 1024 * 1024
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
	// EvalsPrefix is the evals artifact prefix of the job's invocation; it is
	// meaningful only when EvalsPrefixKnown is true.
	EvalsPrefix      string
	EvalsPrefixKnown bool
}

// buildEvalsSummary summarizes the evals answers of every evals invocation of the
// audited run attempt and the evals execution evidence. It returns nil when the run
// has no evals results, when any invocation's results cannot be read, or when the
// invocations of the audited attempt cannot be identified.
func buildEvalsSummary(run WorkflowRun) *EvalsSummary {
	if run.LogsPath == "" {
		return nil
	}
	files, ok := auditedAttemptEvalsResultFiles(run, findEvalsInvocationResultFiles(run.LogsPath))
	if !ok || len(files) == 0 {
		return nil
	}
	invocations := summarizeEvalsInvocationFiles(run, files)
	summary := &EvalsSummary{}
	for _, invocation := range invocations {
		if invocation == nil {
			return nil
		}
		summary.Questions += invocation.Questions
		summary.Yes += invocation.Yes
		summary.No += invocation.No
		summary.Unknown += invocation.Unknown
	}
	summary.ExecutionState = readEvalsExecutionState(run, "", false)
	return summary
}

// buildEvalsInvocationSummaries returns the evals answers summary of each evals
// invocation of a run, keyed by artifact prefix ("" or "<hash>-"). A nil summary
// means that invocation's results are unavailable.
func buildEvalsInvocationSummaries(run WorkflowRun) map[string]*EvalsSummary {
	if run.LogsPath == "" {
		return nil
	}
	files := findEvalsInvocationResultFiles(run.LogsPath)
	if len(files) == 0 {
		return nil
	}
	return summarizeEvalsInvocationFiles(run, files)
}

// summarizeEvalsInvocationFiles summarizes each invocation's evals.jsonl, keyed by
// artifact prefix. A nil summary means that invocation's results are unavailable.
func summarizeEvalsInvocationFiles(run WorkflowRun, files map[string]string) map[string]*EvalsSummary {
	runID := ""
	if run.DatabaseID != 0 {
		runID = strconv.FormatInt(run.DatabaseID, 10)
	}
	summaries := make(map[string]*EvalsSummary, len(files))
	for prefix, path := range files {
		summary, err := summarizeEvalsResultFile(path, runID)
		if err != nil {
			auditEngineStepsLog.Printf("Evals results unavailable for invocation %q (%s): %v", prefix, path, err)
		}
		summaries[prefix] = summary
	}
	return summaries
}

// findEvalsInvocationResultFiles returns one evals.jsonl path per evals invocation,
// keyed by artifact prefix. The evals artifact and the compact usage artifact of the
// same invocation carry copies of the same file, so only the first existing copy is
// used. The run-root evals.jsonl (a flattened evals artifact or the evals state
// branch history) belongs to the unprefixed invocation.
func findEvalsInvocationResultFiles(runDir string) map[string]string {
	evalsName := constants.EvalsArtifactName.String()
	usageName := constants.UsageArtifactName.String()
	resultName := constants.EvalsResultFilename.String()
	candidates := map[string][]string{
		"": {
			filepath.Join(runDir, resultName),
			filepath.Join(runDir, usageName, resultName),
			filepath.Join(runDir, evalsName, resultName),
		},
	}
	if entries, err := os.ReadDir(runDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, name := range []string{evalsName, usageName} {
				if hash, ok := strings.CutSuffix(entry.Name(), "-"+name); ok && hash != "" {
					prefix := hash + "-"
					candidates[prefix] = append(candidates[prefix], filepath.Join(runDir, entry.Name(), resultName))
				}
			}
		}
	}
	files := map[string]string{}
	for prefix, paths := range candidates {
		for _, path := range paths {
			if fileutil.FileExists(path) {
				files[prefix] = path
				break
			}
		}
	}
	return files
}

// auditedAttemptEvalsResultFiles narrows the evals result files found for a run to the
// invocations of the audited attempt, each counted once. Evals rows carry no attempt,
// and reruns of reusable-workflow invocations upload under a new artifact prefix, so
// prefixes are attributed first by the upload prefixes logged by the evals jobs of the
// cached jobs API response (the audited attempt's jobs), then by the run_attempt in
// each prefix's evals execution evidence. The second result is false when several
// invocations are present on a rerun and their attempt cannot be determined, or when
// the audited attempt is unknown (0) and the selected invocations' execution evidence
// records more than one attempt.
func auditedAttemptEvalsResultFiles(run WorkflowRun, files map[string]string) (map[string]string, bool) {
	if len(files) == 0 {
		return files, true
	}
	logPrefixes, logsComplete := evalsUploadPrefixesFromJobLogs(run.LogsPath)
	files = assignRootEvalsResultFile(run.LogsPath, files, logPrefixes)
	selected := make(map[string]string, len(files))
	for prefix, path := range files {
		if logPrefixes[prefix] {
			selected[prefix] = path
			continue
		}
		if logsComplete {
			auditEngineStepsLog.Printf("Evals invocation %q was not uploaded by the audited attempt's evals jobs", prefix)
			continue
		}
		if attempt, ok := evalsExecutionAttempt(run, prefix); ok {
			// An unknown audited attempt is checked against the other selected
			// invocations' evidence below.
			if run.Attempt == 0 || attempt == run.Attempt {
				selected[prefix] = path
			} else {
				auditEngineStepsLog.Printf("Evals invocation %q belongs to attempt %d, not audited attempt %d", prefix, attempt, run.Attempt)
			}
			continue
		}
		if run.Attempt > 1 && len(files) > 1 {
			auditEngineStepsLog.Printf("Evals invocation %q cannot be attributed to audited attempt %d", prefix, run.Attempt)
			return nil, false
		}
		selected[prefix] = path
	}
	if run.Attempt == 0 {
		attempts := map[int]bool{}
		for prefix := range selected {
			if attempt, ok := evalsExecutionAttempt(run, prefix); ok {
				attempts[attempt] = true
			}
		}
		if len(attempts) > 1 {
			auditEngineStepsLog.Printf("Evals invocations of an unknown audited attempt record %d run attempts", len(attempts))
			return nil, false
		}
	}
	return selected, true
}

// evalsUploadPrefixesFromJobLogs returns the evals artifact prefixes uploaded by the
// evals jobs listed in the cached jobs API response. The second result is true only
// when at least one evals job is listed and every listed evals job logged its prefix
// in its own log location; a location shared by several evals jobs (such as one
// base-name log matched by two callers) leaves the other jobs' prefixes unknown.
func evalsUploadPrefixesFromJobLogs(logsPath string) (map[string]bool, bool) {
	prefixes := map[string]bool{}
	if logsPath == "" {
		return prefixes, false
	}
	workflowLogsDir := filepath.Join(logsPath, "workflow-logs")
	complete := true
	seenLocations := map[jobLogLocation]bool{}
	for _, job := range loadCachedJobDetails(logsPath) {
		if engineStepJobKind(job.Name) != string(constants.EvalsJobName) || strings.EqualFold(strings.TrimSpace(job.Conclusion), "skipped") {
			continue
		}
		if location := locateJobLogs(workflowLogsDir, job.JobInfo); location != (jobLogLocation{}) {
			if seenLocations[location] {
				auditEngineStepsLog.Printf("Evals job %q reuses another evals job's log location", job.Name)
				complete = false
				continue
			}
			seenLocations[location] = true
		}
		prefix, known := evalsArtifactPrefixFromLog(readJobLogLines(workflowLogsDir, job.JobInfo))
		if !known {
			complete = false
			continue
		}
		prefixes[prefix] = true
	}
	return prefixes, complete && len(prefixes) > 0
}

// assignRootEvalsResultFile attributes a run-root evals.jsonl that has no unprefixed
// artifact copy. A single-file "<hash>-evals" artifact is flattened to the run root, so
// the root file belongs to the only logged prefix whose evals artifact directory is
// absent. When it cannot be assigned but prefixed copies exist, those copies are used
// and the root file is not counted as another invocation.
func assignRootEvalsResultFile(runDir string, files map[string]string, logPrefixes map[string]bool) map[string]string {
	resultName := constants.EvalsResultFilename.String()
	evalsName := constants.EvalsArtifactName.String()
	rootPath := filepath.Join(runDir, resultName)
	if files[""] != rootPath || logPrefixes[""] ||
		fileutil.FileExists(filepath.Join(runDir, constants.UsageArtifactName.String(), resultName)) ||
		fileutil.FileExists(filepath.Join(runDir, evalsName, resultName)) {
		return files
	}
	owner, owners := "", 0
	for prefix := range logPrefixes {
		if prefix != "" && !fileutil.DirExists(filepath.Join(runDir, prefix+evalsName)) {
			owner = prefix
			owners++
		}
	}
	if owners != 1 && len(files) == 1 {
		return files
	}
	assigned := make(map[string]string, len(files))
	for prefix, path := range files {
		if prefix != "" {
			assigned[prefix] = path
		}
	}
	if owners == 1 {
		if _, ok := assigned[owner]; !ok {
			assigned[owner] = rootPath
		}
	}
	return assigned
}

// evalsExecutionAttempt returns the run attempt recorded in the evals execution
// evidence of the invocation with the given artifact prefix.
func evalsExecutionAttempt(run WorkflowRun, prefix string) (int, bool) {
	component := string(constants.EvalsJobName)
	for _, name := range []string{constants.UsageArtifactName.String(), constants.EvalsArtifactName.String()} {
		data, err := readFileLimited(filepath.Join(run.LogsPath, prefix+name, component, "execution.json"), 1024*1024)
		if err != nil {
			continue
		}
		var evidence detectionExecutionEvidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			continue
		}
		if evidence.Version != 1 || evidence.Component != component || evidence.RunAttempt == 0 {
			continue
		}
		if run.DatabaseID != 0 && evidence.RunID != 0 && evidence.RunID != run.DatabaseID {
			continue
		}
		return evidence.RunAttempt, true
	}
	return 0, false
}

// summarizeEvalsResultFile streams an evals.jsonl file line by line and summarizes
// the answers recorded for runID (every answer when runID is empty). The file can be
// the evals state branch's append-only history, so it has no total size limit; each
// line is limited to evalsResultMaxLineBytes. Any read error, including an over-long
// line, makes the summary unavailable instead of summarizing a partial file.
func summarizeEvalsResultFile(path, runID string) (*EvalsSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer closeAuditEngineStepFile(file, path)
	answers := map[string]string{}
	order := []string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), evalsResultMaxLineBytes)
	for scanner.Scan() {
		record, ok := parseEvalResultRecordLine(scanner.Bytes())
		// evals.jsonl restored from the evals state branch also holds other runs' rows.
		if !ok || (runID != "" && record.RunID != "" && strings.TrimSpace(record.RunID) != runID) {
			continue
		}
		if _, seen := answers[record.ID]; !seen {
			order = append(order, record.ID)
		}
		answers[record.ID] = strings.ToUpper(strings.TrimSpace(record.Answer))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	summary := &EvalsSummary{Questions: len(order)}
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
	return summary, nil
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
	var invocations map[string]*EvalsSummary
	for _, failure := range failures {
		if failure.Kind == string(constants.EvalsJobName) {
			invocations = buildEvalsInvocationSummaries(processedRun.Run)
			break
		}
	}
	findings := make([]AuditFinding, 0, len(failures))
	for _, failure := range failures {
		findings = append(findings, buildEngineStepFailureFinding(failure, evalsSummaryForFailure(failure, invocations)))
	}
	return findings
}

// evalsSummaryForFailure returns the evals answers of the failed job's own invocation:
// the invocation identified by the job's upload prefix, or the only invocation when
// the prefix is unknown. It returns nil when the invocation cannot be attributed.
func evalsSummaryForFailure(failure engineStepFailure, invocations map[string]*EvalsSummary) *EvalsSummary {
	if failure.Kind != string(constants.EvalsJobName) {
		return nil
	}
	if failure.EvalsPrefixKnown {
		return invocations[failure.EvalsPrefix]
	}
	if len(invocations) == 1 {
		for _, summary := range invocations {
			return summary
		}
	}
	return nil
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
		evalsState, prefix, prefixKnown := "", "", false
		if isEvals {
			prefix, prefixKnown = evalsArtifactPrefixFromLog(readJobLogLines(workflowLogsDir, job.JobInfo))
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
				failure.ExecutionState, failure.EvalsPrefix, failure.EvalsPrefixKnown = evalsState, prefix, prefixKnown
			}
			auditEngineStepsLog.Printf("Engine step failed: job=%s step=%d name=%q", job.Name, step.Number, step.Name)
			failures = append(failures, failure)
		}
		if notStarted && !hasEngineStep {
			failures = append(failures, engineStepFailure{Job: job.Name, Kind: kind, ExecutionState: evalsState, EvalsPrefix: prefix, EvalsPrefixKnown: prefixKnown})
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

// jobLogFileReplacer maps a job name to the name GitHub gives its logs in the
// downloaded log archive, where "/" (as in reusable workflow jobs "<caller> / <job>")
// and other characters invalid in file names are written as "_".
var jobLogFileReplacer = strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")

func jobLogFileName(name string) string {
	return strings.TrimSpace(jobLogFileReplacer.Replace(name))
}

// jobLogLocation holds the downloaded logs of one job: the nested per-step log
// directory and the flat per-job log file. Each is empty when absent or ambiguous.
type jobLogLocation struct {
	dir  string
	file string
}

// locateJobLogs finds a job's logs in workflowLogsDir. Log names must match the full
// job name exactly. Only when no log matches it does a reusable workflow job
// ("<caller> / <job>") fall back to logs named after its final segment, and only when
// exactly one such log exists; ambiguous logs are not attributed to the job.
func locateJobLogs(workflowLogsDir string, job JobInfo) jobLogLocation {
	if workflowLogsDir == "" {
		return jobLogLocation{}
	}
	entries, err := os.ReadDir(workflowLogsDir)
	if err != nil {
		return jobLogLocation{}
	}
	qualified := jobLogFileName(job.Name)
	base := ""
	if idx := strings.LastIndex(job.Name, " / "); idx >= 0 {
		base = jobLogFileName(job.Name[idx+len(" / "):])
	}
	var exactDirs, exactFiles, baseDirs, baseFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			num, jobName := parseStepFilename(name)
			if num <= 0 || !strings.HasSuffix(name, ".txt") {
				continue
			}
			name = jobName
		}
		path := filepath.Join(workflowLogsDir, entry.Name())
		switch {
		case name == qualified && entry.IsDir():
			exactDirs = append(exactDirs, path)
		case name == qualified:
			exactFiles = append(exactFiles, path)
		case base != "" && name == base && entry.IsDir():
			baseDirs = append(baseDirs, path)
		case base != "" && name == base:
			baseFiles = append(baseFiles, path)
		}
	}
	dirs, files := exactDirs, exactFiles
	if len(dirs) == 0 && len(files) == 0 {
		dirs, files = baseDirs, baseFiles
	}
	location := jobLogLocation{}
	if len(dirs) == 1 {
		location.dir = dirs[0]
	}
	if len(files) == 1 {
		location.file = files[0]
	}
	if len(dirs) > 1 || len(files) > 1 {
		auditEngineStepsLog.Printf("Job logs for %q are ambiguous: %d directories, %d files", job.Name, len(dirs), len(files))
	}
	return location
}

// readJobLogLines returns every log line of a job: the concatenated nested per-step
// log files when present, otherwise the flat per-job log.
func readJobLogLines(workflowLogsDir string, job JobInfo) []string {
	location := locateJobLogs(workflowLogsDir, job)
	if location.dir != "" {
		if stepFiles, err := os.ReadDir(location.dir); err == nil {
			var lines []string
			for _, stepFile := range stepFiles {
				if !stepFile.IsDir() && strings.HasSuffix(stepFile.Name(), ".txt") {
					lines = append(lines, readLogLines(filepath.Join(location.dir, stepFile.Name()))...)
				}
			}
			if len(lines) > 0 {
				return lines
			}
		}
	}
	if location.file != "" {
		return readLogLines(location.file)
	}
	return nil
}

// readEngineStepLogLines returns the log lines of a job step. Nested per-step log
// files are read whole; flat per-job logs are limited to the step's
// started_at..completed_at window from the jobs API.
func readEngineStepLogLines(workflowLogsDir string, job JobInfo, step JobStep) []string {
	location := locateJobLogs(workflowLogsDir, job)
	if location.dir != "" {
		if stepFiles, err := os.ReadDir(location.dir); err == nil {
			for _, stepFile := range stepFiles {
				if stepFile.IsDir() || !strings.HasSuffix(stepFile.Name(), ".txt") {
					continue
				}
				if num, _ := parseStepFilename(stepFile.Name()); num == step.Number && num > 0 {
					return readLogLines(filepath.Join(location.dir, stepFile.Name()))
				}
			}
		}
	}
	if location.file == "" || step.StartedAt.IsZero() || step.CompletedAt.IsZero() {
		return nil
	}
	return filterLogLinesByWindow(readLogLines(location.file), job.Steps, step)
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
// header. The step's header is identified by its order among all the headers logged
// in the window: the steps that the jobs API places before it in the job and that
// start in its start second log their headers first. The step's header itself can be
// logged in a later second than its API start time, so it is not limited to the start
// second.
func filterLogLinesByWindow(lines []string, steps []JobStep, step JobStep) []string {
	windowStart := step.StartedAt.Truncate(time.Second)
	windowEnd := step.CompletedAt.Truncate(time.Second).Add(time.Second)
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
	var windowHeaders []int
	for i, entry := range kept {
		if isStepHeaderLine(entry.text) {
			windowHeaders = append(windowHeaders, i)
		}
	}
	begin := -1
	if position := min(sameSecondPredecessorSteps(steps, step), len(windowHeaders)-1); position >= 0 && position < len(windowHeaders) {
		begin = windowHeaders[position]
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
