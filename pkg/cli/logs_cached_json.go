package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

type cachedLogsRuns map[int64]cachedLogsJSONLRunData

const cachedLogsJSONLSchemaVersion = 2

const (
	cachedLogsJSONLKindRun          = "run"
	cachedLogsJSONLKindWorkflowRuns = "workflow_runs"
	cachedLogsJSONLKindRateLimit    = "github_api_rate_limit"
	cachedLogsJSONLKindSafeOutput   = "safe_output_item"
)

type cachedWorkflowRunsRequest struct {
	Host       string   `json:"host"`
	Repository string   `json:"repository"`
	Args       []string `json:"args"`
}

// cachedLogsJSONLRunData extends the reusable run summary with the safe,
// per-run evidence needed by dashboard adapters to derive Jobs, Sessions,
// and Events. Raw tool errors, arguments, responses, and artifact bodies are
// deliberately excluded from these projections.
type cachedLogsJSONLRunData struct {
	RunData
	EngineVersion   string                           `json:"engine_version,omitempty"`
	Model           string                           `json:"model,omitempty"`
	GhAwVersion     string                           `json:"gh_aw_version,omitempty"`
	AgentRuntime    string                           `json:"agent_runtime,omitempty"`
	FirewallVersion string                           `json:"firewall_version,omitempty"`
	GatewayVersion  string                           `json:"gateway_version,omitempty"`
	JobDetails      []cachedLogsJSONLJobData         `json:"job_details,omitempty"`
	MCPToolUsage    *cachedLogsJSONLMCPToolUsageData `json:"mcp_tool_usage,omitempty"`
	Audit           *AuditData                       `json:"audit,omitempty"`
	AwInfo          *AwInfo                          `json:"aw_info,omitempty"`
}

type cachedLogsJSONLJobData struct {
	ID          int64     `json:"id"`
	RunAttempt  int       `json:"run_attempt,omitempty"`
	Name        string    `json:"name"`
	Status      string    `json:"status,omitempty"`
	Conclusion  string    `json:"conclusion,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitzero"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	CompletedAt time.Time `json:"completed_at,omitzero"`
}

type cachedLogsJSONLMCPToolUsageData struct {
	ToolCalls []cachedLogsJSONLMCPToolCall `json:"tool_calls,omitempty"`
}

type cachedLogsJSONLMCPToolCall struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	Timestamp  string `json:"timestamp"`
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	Method     string `json:"method,omitempty"`
	InputSize  int    `json:"input_size"`
	OutputSize int    `json:"output_size"`
	Duration   string `json:"duration,omitempty"`
	Status     string `json:"status"`
}

type cachedLogsJSONLRecord struct {
	SchemaVersion int                           `json:"schema_version"`
	Kind          string                        `json:"kind,omitempty"`
	Run           *cachedLogsJSONLRunData       `json:"run,omitempty"`
	Request       *cachedWorkflowRunsRequest    `json:"request,omitempty"`
	Payload       json.RawMessage               `json:"payload,omitempty"`
	RateLimit     *GitHubAPIRateLimitReport     `json:"rate_limit,omitempty"`
	SafeOutput    *cachedLogsJSONLSafeOutputRow `json:"safe_output,omitempty"`
}

// cachedLogsJSONLSafeOutputRow projects a single safe-output entity as its own
// cached logs JSONL event, correlated back to the run that produced it, so
// downstream consumers can process created/modified entities individually
// without re-parsing every "run" record's nested safe_outputs array.
type cachedLogsJSONLSafeOutputRow struct {
	RunID int64 `json:"run_id"`
	CreatedItemReport
}

type cachedLogsJSONLCache struct {
	runs             cachedLogsRuns
	workflowRunLists map[string]json.RawMessage
}

type preparedCachedLogsJSONL struct {
	cache       *cachedLogsJSONLCache
	writer      *cachedLogsJSONLWriter
	sourcePaths []string
	wildcard    bool
}

func loadCachedLogsJSONL(path string) (*cachedLogsJSONLCache, error) {
	if path == "" {
		return nil, nil
	}
	cache := &cachedLogsJSONLCache{
		runs:             make(cachedLogsRuns),
		workflowRunLists: make(map[string]json.RawMessage),
	}
	recordCount, err := visitCachedLogsJSONLRecords(path, func(record cachedLogsJSONLRecord, recordNumber int) error {
		return cache.addRecord(record, recordNumber)
	})
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cached logs JSONL file not found: "+path))
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf(
		"Found cached logs JSONL file: %s (lines=%d, runs=%d, workflow_run_lists=%d)",
		path, recordCount, len(cache.runs), len(cache.workflowRunLists),
	)))
	logsCacheLog.Printf("Loaded %d run records and %d workflow run lists from cached logs JSONL", len(cache.runs), len(cache.workflowRunLists))
	return cache, nil
}

func visitCachedLogsJSONLRecords(path string, visit func(cachedLogsJSONLRecord, int) error) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read cached logs JSONL: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	recordCount := 0
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		recordCount++
		var record cachedLogsJSONLRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if index == len(lines)-1 {
				logsCacheLog.Printf("Ignoring incomplete final cached logs JSONL record: %v", err)
				break
			}
			return recordCount, fmt.Errorf("failed to parse cached logs JSONL record %d: %w", index+1, err)
		}
		if err := visit(record, index+1); err != nil {
			return recordCount, err
		}
	}
	return recordCount, nil
}

func loadCachedLogsJSONLFiles(paths []string) (*cachedLogsJSONLCache, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	merged := &cachedLogsJSONLCache{
		runs:             make(cachedLogsRuns),
		workflowRunLists: make(map[string]json.RawMessage),
	}
	for _, path := range paths {
		cache, err := loadCachedLogsJSONL(path)
		if err != nil {
			return nil, err
		}
		if cache == nil {
			continue
		}
		for id, run := range cache.runs {
			if _, exists := merged.runs[id]; exists {
				warnDuplicateCachedLogsJSONLRecord(fmt.Sprintf("Duplicate cached logs JSONL run record for run %d in %s; newer cache file wins", id, path))
			}
			merged.runs[id] = run
		}
		for key, payload := range cache.workflowRunLists {
			if _, exists := merged.workflowRunLists[key]; exists {
				warnDuplicateCachedLogsJSONLRecord(fmt.Sprintf("Duplicate cached workflow runs JSONL record in %s; newer cache file wins", path))
			}
			merged.workflowRunLists[key] = append(json.RawMessage(nil), payload...)
		}
	}
	return merged, nil
}

func warnDuplicateCachedLogsJSONLRecord(message string) {
	logsCacheLog.Print(message)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
}

func prepareCachedLogsJSONL(opts *LogsDownloadOptions) error {
	if opts.CachedJSONL == "" {
		return nil
	}
	if opts.collectionStats == nil {
		opts.collectionStats = &logsCollectionStats{}
	}
	if opts.cachedJSONLWriter != nil {
		return nil
	}
	prepared, err := prepareCachedLogsJSONLPath(opts.CachedJSONL)
	if err != nil {
		return err
	}
	opts.CachedJSONL = prepared.writer.path
	opts.cachedJSONLCache = prepared.cache
	opts.cachedJSONLWriter = prepared.writer
	opts.cachedJSONLSourcePaths = prepared.sourcePaths
	opts.cachedJSONLWildcard = prepared.wildcard
	return nil
}

func prepareCachedLogsJSONLPath(path string) (preparedCachedLogsJSONL, error) {
	sourcePaths, writerPath, wildcard, err := resolveCachedLogsJSONLPaths(path)
	if err != nil {
		return preparedCachedLogsJSONL{}, err
	}
	cache, err := loadCachedLogsJSONLFiles(sourcePaths)
	if err != nil {
		return preparedCachedLogsJSONL{}, err
	}
	return preparedCachedLogsJSONL{
		cache:       cache,
		writer:      newCachedLogsJSONLWriter(writerPath),
		sourcePaths: sourcePaths,
		wildcard:    wildcard,
	}, nil
}

func resolveCachedLogsJSONLPaths(path string) ([]string, string, bool, error) {
	if path == "" {
		return nil, "", false, nil
	}
	if !strings.Contains(path, "*") {
		return []string{path}, path, false, nil
	}
	if !strings.HasSuffix(path, "*") || strings.Count(path, "*") != 1 {
		return nil, "", false, fmt.Errorf("cached logs wildcard must be a trailing prefix match, such as %q", "foo-bar-*")
	}
	dir := filepath.Dir(path)
	prefix := strings.TrimSuffix(filepath.Base(path), "*")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		writerPath, err := uniqueCachedLogsJSONLPath(dir, prefix)
		if err != nil {
			return nil, "", false, err
		}
		return nil, writerPath, true, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to list cached logs JSONL directory: %w", err)
	}
	sourcePaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".jsonl") {
			sourcePaths = append(sourcePaths, filepath.Join(dir, name))
		}
	}
	sortCachedLogsJSONLSourcePaths(sourcePaths, prefix)
	writerPath, err := uniqueCachedLogsJSONLPath(dir, prefix)
	if err != nil {
		return nil, "", false, err
	}
	return sourcePaths, writerPath, true, nil
}

func sortCachedLogsJSONLSourcePaths(paths []string, prefix string) {
	slices.SortStableFunc(paths, func(leftPath, rightPath string) int {
		left, leftErr := os.Stat(leftPath)
		right, rightErr := os.Stat(rightPath)
		if leftErr == nil && rightErr == nil && !left.ModTime().Equal(right.ModTime()) {
			if left.ModTime().Before(right.ModTime()) {
				return -1
			}
			return 1
		}
		leftUnix, leftOK := cachedLogsJSONLUnixSuffix(leftPath, prefix)
		rightUnix, rightOK := cachedLogsJSONLUnixSuffix(rightPath, prefix)
		if leftOK && rightOK && leftUnix != rightUnix {
			if leftUnix < rightUnix {
				return -1
			}
			return 1
		}
		return strings.Compare(leftPath, rightPath)
	})
}

func cachedLogsJSONLUnixSuffix(path, prefix string) (int64, bool) {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	suffix, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return 0, false
	}
	digitCount := 0
	for digitCount < len(suffix) {
		ch := suffix[digitCount] //nolint:uncheckedsliceindex // digitCount is checked against len(suffix) above.
		if ch < '0' || ch > '9' {
			break
		}
		digitCount++
	}
	if digitCount == 0 {
		return 0, false
	}
	value, err := strconv.ParseInt(suffix[:digitCount], 10, 64)
	return value, err == nil
}

func uniqueCachedLogsJSONLPath(dir, prefix string) (string, error) {
	for range 16 {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("failed to generate cached logs JSONL file name: %w", err)
		}
		name := fmt.Sprintf("%s%d-%s.jsonl", prefix, time.Now().Unix(), hex.EncodeToString(random[:]))
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("failed to check cached logs JSONL file name: %w", err)
		}
	}
	return "", errors.New("failed to generate a unique cached logs JSONL file name")
}

func finalizeCachedLogsJSONL(writer *cachedLogsJSONLWriter, sourcePaths []string, wildcard bool, startDate, endDate string) error {
	return errors.Join(
		writer.filterDateRange(startDate, endDate),
		pruneCachedLogsJSONLWildcardSources(sourcePaths, wildcard, startDate, endDate),
	)
}

func pruneCachedLogsJSONLWildcardSources(sourcePaths []string, wildcard bool, startDate, endDate string) error {
	if !wildcard || len(sourcePaths) == 0 || (startDate == "" && endDate == "") {
		return nil
	}
	dateRange, err := newCachedLogsJSONLDateRange(startDate, endDate)
	if err != nil {
		return err
	}
	var result error
	for _, path := range sourcePaths {
		hasMatch, canDelete, err := cachedLogsJSONLFileDateRangeStatus(path, dateRange)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if hasMatch || !canDelete {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("failed to delete out-of-range cached logs JSONL file %s: %w", path, err))
		}
	}
	return result
}

func cachedLogsJSONLFileDateRangeStatus(path string, dateRange cachedLogsJSONLDateRange) (bool, bool, error) {
	hasMatchingRun := false
	hasDatedRun := false
	hasPreservedRecord := false
	_, err := visitCachedLogsJSONLRecords(path, func(record cachedLogsJSONLRecord, _ int) error {
		if record.Kind != cachedLogsJSONLKindRun ||
			record.SchemaVersion != cachedLogsJSONLSchemaVersion ||
			record.Run == nil ||
			record.Run.CreatedAt.IsZero() {
			hasPreservedRecord = true
			return nil
		}
		hasDatedRun = true
		if dateRange.includes(record.Run.CreatedAt) {
			hasMatchingRun = true
		}
		return nil
	})
	if err != nil {
		return false, false, err
	}
	return hasMatchingRun, hasDatedRun && !hasPreservedRecord, nil
}

func (cache *cachedLogsJSONLCache) addRecord(record cachedLogsJSONLRecord, recordNumber int) error {
	if record.SchemaVersion != cachedLogsJSONLSchemaVersion {
		logsCacheLog.Printf("Ignoring incompatible cached logs JSONL record: record=%d, schema_version=%d", recordNumber, record.SchemaVersion)
		return nil
	}
	switch record.Kind {
	case cachedLogsJSONLKindWorkflowRuns:
		if record.Request == nil || len(record.Payload) == 0 {
			return nil
		}
		// Validate the cached payload shape while retaining its complete raw JSON.
		var runs []WorkflowRun
		if err := json.Unmarshal(record.Payload, &runs); err != nil {
			return fmt.Errorf("failed to parse cached workflow runs payload in record %d: %w", recordNumber, err)
		}
		if runs == nil {
			return fmt.Errorf("failed to parse cached workflow runs payload in record %d: expected an array", recordNumber)
		}
		key, err := record.Request.key()
		if err != nil {
			return fmt.Errorf("failed to parse cached workflow runs request in record %d: %w", recordNumber, err)
		}
		cache.workflowRunLists[key] = append(json.RawMessage(nil), record.Payload...)
		return nil
	case cachedLogsJSONLKindRateLimit:
		return nil
	case cachedLogsJSONLKindSafeOutput:
		// Individual safe-output entity events are informational projections of
		// data already captured under the "run" record's safe_outputs array;
		// they are not indexed separately in the in-memory cache.
		return nil
	case cachedLogsJSONLKindRun:
	default:
		return nil
	}
	if record.Run == nil {
		return nil
	}
	run := *record.Run
	if err := normalizeCachedLogRun(&run.RunData); err != nil {
		return err
	}
	if run.RunID != 0 {
		cache.runs[run.RunID] = run
	}
	return nil
}

type cachedLogsJSONLWriter struct {
	path string
	mu   sync.Mutex
}

func newCachedLogsJSONLWriter(path string) *cachedLogsJSONLWriter {
	if path == "" {
		return nil
	}
	return &cachedLogsJSONLWriter{path: path}
}

func (w *cachedLogsJSONLWriter) Append(run ProcessedRun) error {
	return w.appendRun(run, false)
}

func (w *cachedLogsJSONLWriter) AppendAudit(run ProcessedRun) error {
	return w.appendRun(run, true)
}

func (w *cachedLogsJSONLWriter) appendRun(run ProcessedRun, includeAudit bool) error {
	if w == nil {
		return nil
	}
	// Cached runs are already persisted in a source JSONL shard; do not copy
	// them into the new shard created for this invocation.
	if run.cachedData != nil {
		return nil
	}
	logsData := buildLogsData([]ProcessedRun{run}, "", nil)
	if len(logsData.Runs) == 0 {
		return errors.New("failed to build cached logs JSONL record: no run data produced")
	}
	if len(logsData.Runs) > 1 {
		return errors.New("failed to build cached logs JSONL record: multiple run records produced for one run")
	}
	runData := buildCachedLogsJSONLRunData(run, logsData.Runs[0]) //nolint:uncheckedsliceindex // The custom linter cannot infer the len checks above.
	if includeAudit {
		if audit, ok := loadCachedAuditData(run.Run.LogsPath, run.Run, auditCacheSourceLogs); ok {
			runData.Audit = &audit
			if len(runData.SafeOutputs) == 0 {
				runData.SafeOutputs = audit.CreatedItems
			}
		}
		if awInfo, err := parseAwInfo(filepath.Join(run.Run.LogsPath, "aw_info.json"), false); err == nil {
			runData.AwInfo = awInfo
		}
	}
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRun,
		Run:           runData,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached logs JSONL record: %w", err)
	}
	if err := w.appendRecord(record); err != nil {
		return err
	}
	return w.appendSafeOutputItems(runData.RunID, runData.SafeOutputs)
}

// appendSafeOutputItems writes one cached logs JSONL record per safe-output
// entity, correlated to runID, in addition to the entities already nested
// under the "run" record's safe_outputs array. This lets downstream
// consumers stream and process individual created/modified entities as
// events without reconstructing them from the aggregate run record.
func (w *cachedLogsJSONLWriter) appendSafeOutputItems(runID int64, items []CreatedItemReport) error {
	if w == nil {
		return nil
	}
	for _, item := range items {
		record, err := json.Marshal(cachedLogsJSONLRecord{
			SchemaVersion: cachedLogsJSONLSchemaVersion,
			Kind:          cachedLogsJSONLKindSafeOutput,
			SafeOutput: &cachedLogsJSONLSafeOutputRow{
				RunID:             runID,
				CreatedItemReport: item,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to marshal cached safe-output item JSONL record: %w", err)
		}
		if err := w.appendRecord(record); err != nil {
			return err
		}
	}
	return nil
}

func buildCachedLogsJSONLRunData(run ProcessedRun, runData RunData) *cachedLogsJSONLRunData {
	data := &cachedLogsJSONLRunData{
		RunData:      runData,
		JobDetails:   projectCachedLogsJSONLJobs(run.JobDetails),
		MCPToolUsage: projectCachedLogsJSONLMCPToolUsage(run.MCPToolUsage),
	}
	if info := runData.awInfo; info != nil {
		data.EngineVersion = info.Version
		data.Model = info.Model
		data.GhAwVersion = info.CLIVersion
		data.AgentRuntime = info.AgentRuntime
		data.FirewallVersion = info.GetFirewallVersion()
		data.GatewayVersion = info.AwmgVersion
	}
	return data
}

func projectCachedLogsJSONLJobs(jobs []JobInfoWithDuration) []cachedLogsJSONLJobData {
	projected := make([]cachedLogsJSONLJobData, 0, len(jobs))
	for _, job := range jobs {
		if job.ID <= 0 || job.Name == "" {
			continue
		}
		projected = append(projected, cachedLogsJSONLJobData{
			ID:          job.ID,
			RunAttempt:  job.RunAttempt,
			Name:        job.Name,
			Status:      job.Status,
			Conclusion:  job.Conclusion,
			CreatedAt:   job.CreatedAt,
			StartedAt:   job.StartedAt,
			CompletedAt: job.CompletedAt,
		})
	}
	return projected
}

func projectCachedLogsJSONLMCPToolUsage(usage *MCPToolUsageData) *cachedLogsJSONLMCPToolUsageData {
	if usage == nil || len(usage.ToolCalls) == 0 {
		return nil
	}
	toolCalls := make([]cachedLogsJSONLMCPToolCall, 0, len(usage.ToolCalls))
	for _, call := range usage.ToolCalls {
		if call.Timestamp == "" || (call.ServerName == "" && call.ToolName == "") {
			continue
		}
		toolCalls = append(toolCalls, call.cachedLogsJSONLProjection())
	}
	if len(toolCalls) == 0 {
		return nil
	}
	return &cachedLogsJSONLMCPToolUsageData{ToolCalls: toolCalls}
}

func (call MCPToolCall) cachedLogsJSONLProjection() cachedLogsJSONLMCPToolCall {
	return cachedLogsJSONLMCPToolCall{
		ToolCallID: call.ToolCallID,
		Timestamp:  call.Timestamp,
		ServerName: call.ServerName,
		ToolName:   call.ToolName,
		Method:     call.Method,
		InputSize:  call.InputSize,
		OutputSize: call.OutputSize,
		Duration:   call.Duration,
		Status:     call.Status,
	}
}

func (w *cachedLogsJSONLWriter) AppendWorkflowRuns(request cachedWorkflowRunsRequest, payload []byte) error {
	if w == nil {
		return nil
	}
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindWorkflowRuns,
		Request:       &request,
		Payload:       append(json.RawMessage(nil), payload...),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached workflow runs JSONL record: %w", err)
	}
	return w.appendRecord(record)
}

func (w *cachedLogsJSONLWriter) AppendRateLimit(report GitHubAPIRateLimitReport) error {
	if w == nil || (report.Start == nil && report.End == nil) {
		return nil
	}
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRateLimit,
		RateLimit:     &report,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal cached GitHub API rate limit JSONL record: %w", err)
	}
	return w.appendRecord(record)
}

func (w *cachedLogsJSONLWriter) appendRecord(record []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, record); err != nil {
		return fmt.Errorf("failed to encode cached logs JSONL record: %w", err)
	}
	record = append(compact.Bytes(), '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create cached logs JSONL directory: %w", err)
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, constants.FilePermSensitive)
	if err != nil {
		return fmt.Errorf("failed to open cached logs JSONL: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(record); err != nil {
		return fmt.Errorf("failed to append cached logs JSONL: %w", err)
	}
	return nil
}

func (w *cachedLogsJSONLWriter) filterDateRange(startDate, endDate string) error {
	if w == nil || (startDate == "" && endDate == "") {
		return nil
	}
	dateRange, err := newCachedLogsJSONLDateRange(startDate, endDate)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	data, err := os.ReadFile(w.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read cached logs JSONL for date filtering: %w", err)
	}
	filtered := make([]byte, 0, len(data))
	for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			filtered = append(filtered, line...)
			continue
		}
		var record cachedLogsJSONLRecord
		if err := json.Unmarshal(trimmed, &record); err != nil ||
			record.Kind != cachedLogsJSONLKindRun ||
			record.SchemaVersion != cachedLogsJSONLSchemaVersion ||
			record.Run == nil ||
			record.Run.CreatedAt.IsZero() {
			filtered = append(filtered, line...)
			continue
		}
		if !dateRange.includes(record.Run.CreatedAt) {
			continue
		}
		filtered = append(filtered, line...)
	}
	if bytes.Equal(data, filtered) {
		return nil
	}
	if err := writeFileAtomically(w.path, filtered); err != nil {
		return fmt.Errorf("failed to filter cached logs JSONL by date range: %w", err)
	}
	return nil
}

type cachedLogsJSONLDateRange struct {
	start         time.Time
	end           time.Time
	endIsDateOnly bool
}

func newCachedLogsJSONLDateRange(startDate, endDate string) (cachedLogsJSONLDateRange, error) {
	resolvedStart, resolvedEnd, err := resolveLogsDateRange(startDate, endDate, time.Now())
	if err != nil {
		return cachedLogsJSONLDateRange{}, err
	}
	dateRange := cachedLogsJSONLDateRange{endIsDateOnly: len(resolvedEnd) == len("2006-01-02")}
	if resolvedStart != "" {
		dateRange.start, err = parseFilterDate(resolvedStart)
		if err != nil {
			return cachedLogsJSONLDateRange{}, fmt.Errorf("failed to parse cached logs JSONL start date: %w", err)
		}
	}
	if resolvedEnd != "" {
		dateRange.end, err = parseFilterDate(resolvedEnd)
		if err != nil {
			return cachedLogsJSONLDateRange{}, fmt.Errorf("failed to parse cached logs JSONL end date: %w", err)
		}
	}
	return dateRange, nil
}

func (r cachedLogsJSONLDateRange) includes(createdAt time.Time) bool {
	if !r.start.IsZero() && createdAt.Before(r.start) {
		return false
	}
	if r.end.IsZero() {
		return true
	}
	if r.endIsDateOnly {
		return createdAt.Before(r.end.AddDate(0, 0, 1))
	}
	return !createdAt.After(r.end)
}

func (request cachedWorkflowRunsRequest) key() (string, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to build cached workflow runs request key: %w", err)
	}
	return string(data), nil
}

func (cache *cachedLogsJSONLCache) lookupWorkflowRuns(request cachedWorkflowRunsRequest) (json.RawMessage, bool) {
	if cache == nil {
		return nil, false
	}
	key, err := request.key()
	if err != nil {
		return nil, false
	}
	payload, ok := cache.workflowRunLists[key]
	return payload, ok
}

func (runs cachedLogsRuns) lookup(run WorkflowRun, filters runFilterOpts) (RunData, bool) {
	cached, ok := runs[run.DatabaseID]
	if !ok {
		return RunData{}, false
	}
	if cached.Status != "completed" || run.Status != "completed" || cached.Conclusion != run.Conclusion {
		return RunData{}, false
	}
	if cached.RunAttempt == "" || run.Attempt <= 0 || cached.RunAttempt != strconv.Itoa(run.Attempt) {
		return RunData{}, false
	}
	if cached.UpdatedAt.IsZero() || run.UpdatedAt.IsZero() || !cached.UpdatedAt.Equal(run.UpdatedAt) {
		return RunData{}, false
	}
	if cached.Repository == "" || run.Repository == "" || !strings.EqualFold(cached.Repository, run.Repository) {
		return RunData{}, false
	}
	if filters.engine != "" &&
		!strings.EqualFold(filters.engine, cached.EngineID) &&
		!strings.EqualFold(filters.engine, cached.Engine) &&
		!strings.EqualFold(filters.engine, cached.Agent) {
		return RunData{}, false
	}
	// Compact logs JSON does not retain enough per-run evidence to safely
	// re-evaluate these artifact-dependent filters.
	if filters.runtime != "" || filters.noStaged || filters.firewallOnly || filters.noFirewall ||
		filters.safeOutputType != "" || filters.filteredIntegrity || filters.evalsOnly || filters.gradersOnly {
		return RunData{}, false
	}
	return cached.RunData, true
}

func normalizeCachedLogRun(run *RunData) error {
	if run.RunAttempt == "" {
		return nil
	}
	attempt, err := strconv.Atoi(run.RunAttempt)
	if err != nil || attempt <= 0 {
		return fmt.Errorf("failed to parse cached logs JSONL: invalid run_attempt for run %d", run.RunID)
	}
	run.RunAttempt = strconv.Itoa(attempt)
	return nil
}

// cachedJSONLCanSatisfy permits cached records only for the compact usage
// artifact, whose JSON includes the metadata required for cached reports.
// Audit mode uses the available cached data on a best-effort basis. Parsing,
// explicit training, and tool graphs require raw artifact files.
func cachedJSONLCanSatisfy(artifactFilter []string, parse, train, toolGraph bool) bool {
	return isUsageOnlyArtifactFilter(artifactFilter) && !parse && !train && !toolGraph
}

func processedRunFromCachedData(data RunData, audit *AuditData, outputDir string) ProcessedRun {
	return ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:       data.RunID,
			Number:           data.Number,
			URL:              data.URL,
			Status:           data.Status,
			Conclusion:       data.Conclusion,
			WorkflowName:     data.WorkflowName,
			WorkflowPath:     data.WorkflowPath,
			CreatedAt:        data.CreatedAt,
			StartedAt:        data.StartedAt,
			UpdatedAt:        data.UpdatedAt,
			Event:            data.Event,
			HeadBranch:       data.Branch,
			HeadSha:          data.HeadSHA,
			DisplayTitle:     data.DisplayTitle,
			Repository:       data.Repository,
			Actor:            data.Actor,
			Duration:         parseDurationString(data.Duration),
			ActionMinutes:    data.ActionMinutes,
			TokenUsage:       data.TokenUsage,
			Turns:            data.Turns,
			ErrorCount:       data.ErrorCount,
			WarningCount:     data.WarningCount,
			MissingToolCount: data.MissingToolCount,
			MissingDataCount: data.MissingDataCount,
			SafeItemsCount:   data.SafeItemsCount,
			LogsPath:         filepath.Join(outputDir, fmt.Sprintf("run-%d", data.RunID)),
		},
		AwContext:           data.AwContext,
		TaskDomain:          data.TaskDomain,
		BehaviorFingerprint: data.BehaviorFingerprint,
		AgenticAssessments:  data.AgenticAssessments,
		TokenUsage:          data.TokenUsageSummary,
		WorkingSet:          data.WorkingSet,
		cachedData:          &data,
		cachedAudit:         audit,
	}
}
