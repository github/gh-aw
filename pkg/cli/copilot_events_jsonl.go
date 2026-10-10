// This file provides command-line interface functionality for gh-aw.
// This file (copilot_events_jsonl.go) contains functions for finding and
// parsing Copilot CLI events.jsonl files from session-state artifacts.
//
// Key responsibilities:
//   - Locating events.jsonl in the copilot-session-state artifact directory
//   - Parsing the structured event log to extract tool calls, turns, and usage
//   - Providing precise, structured metrics as the primary data source for
//     Copilot CLI log analysis (before falling back to debug log parsing)

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stats"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var copilotEventsJSONLLog = logger.New("cli:copilot_events_jsonl")

// copilotEventsJSONLEntry represents a single event in a Copilot events.jsonl file.
// All events share the same envelope: type, id, timestamp, and a type-specific data object.
//
// The events.jsonl file is written by the Copilot CLI to:
//
//	~/.copilot/session-state/<session-uuid>/events.jsonl
//
// After artifact upload/download it is located at:
//
//	<logDir>/sandbox/agent/logs/copilot-session-state/<uuid>/events.jsonl
type copilotEventsJSONLEntry struct {
	Type                       string                      `json:"type"`
	ID                         string                      `json:"id"`
	Timestamp                  string                      `json:"timestamp"`
	ParentID                   string                      `json:"parentId,omitempty"`
	UsageInputTokens           int                         `json:"usage_input_tokens,omitempty"`
	UsageOutputTokens          int                         `json:"usage_output_tokens,omitempty"`
	AlternateUsageInputTokens  int                         `json:"usageInputTokens,omitempty"`
	AlternateUsageOutputTokens int                         `json:"usageOutputTokens,omitempty"`
	Data                       copilotEventsJSONLEntryData `json:"data"`
}

// copilotEventsJSONLEntryData holds the type-specific payload for each event.
// Fields are populated only for the relevant event types.
type copilotEventsJSONLEntryData struct {
	// session.start fields
	SessionID      string `json:"sessionId,omitempty"`
	CopilotVersion string `json:"copilotVersion,omitempty"`

	// session.model_change fields
	NewModel string `json:"newModel,omitempty"`

	// tool.execution_start fields
	ToolCallID    string `json:"toolCallId,omitempty"`
	ToolName      string `json:"toolName,omitempty"`
	MCPServerName string `json:"mcpServerName,omitempty"`
	MCPToolName   string `json:"mcpToolName,omitempty"`

	// tool.execution_complete fields
	Success bool   `json:"success"`
	Model   string `json:"model,omitempty"`

	// user.message / assistant.message / reasoning fields
	Content string         `json:"content,omitempty"`
	Usage   map[string]any `json:"usage,omitempty"`
	// Alternate input/output token fields that may appear directly on this Data
	// payload (instead of nested under the Usage object above).
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`

	// session.shutdown fields
	ShutdownType string                          `json:"shutdownType,omitempty"`
	ModelMetrics map[string]*copilotModelMetrics `json:"modelMetrics,omitempty"`
}

// copilotModelMetrics holds per-model usage statistics from the session.shutdown event.
type copilotModelMetrics struct {
	Requests *copilotRequestMetrics `json:"requests,omitempty"`
	Usage    *copilotUsageMetrics   `json:"usage,omitempty"`
}

// copilotRequestMetrics holds request count and cost for a model.
type copilotRequestMetrics struct {
	Count int `json:"count"`
	Cost  int `json:"cost"`
}

// copilotUsageMetrics holds token usage for a model.
// NOTE: JSON tags intentionally use camelCase to match the Copilot events.jsonl
// format written by the Copilot CLI. This differs from the snake_case convention
// used in TokenCoreMetrics for our own token-usage files.
type copilotUsageMetrics struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
}

// findEventsJSONLFile searches for an events.jsonl file in logDir.
// It first checks the canonical location at
// sandbox/agent/logs/copilot-session-state/<uuid>/events.jsonl
// and then falls back to a full recursive walk of logDir.
// Returns the first path found, or an empty string if not found.
func findEventsJSONLFile(logDir string) string {
	copilotEventsJSONLLog.Printf("Searching for events.jsonl in: %s", logDir)

	// Try the canonical location first (avoids a full directory walk in the common case)
	sessionStateDir := filepath.Join(logDir, "sandbox", "agent", "logs", "copilot-session-state")
	if canonicalPath := findFileInDir(sessionStateDir, "events.jsonl"); canonicalPath != "" {
		copilotEventsJSONLLog.Printf("Found events.jsonl at canonical location: %s", canonicalPath)
		return canonicalPath
	}

	// Fall back to a recursive search of the full log directory
	var foundPath string
	if walkErr := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			copilotEventsJSONLLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "events.jsonl" && foundPath == "" {
			foundPath = path
			return errWalkStop
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", logDir, walkErr)))
	}

	if foundPath != "" {
		copilotEventsJSONLLog.Printf("Found events.jsonl via recursive search: %s", foundPath)
	} else {
		copilotEventsJSONLLog.Printf("events.jsonl not found in: %s", logDir)
	}
	return foundPath
}

// findFileInDir searches for a file by name within dir (recursively).
// Returns the first matching path, or empty string if not found.
func findFileInDir(dir, name string) string {
	var found string
	if walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			copilotEventsJSONLLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil {
			return nil
		}
		if !info.IsDir() && info.Name() == name && found == "" {
			found = path
			return errWalkStop
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", dir, walkErr)))
	}
	return found
}

// parseEventsJSONLMetrics parses a Copilot events.jsonl file and extracts log metrics.
//
// events.jsonl provides precise, structured data about a Copilot CLI session:
//   - "session.start":          session metadata (sessionId, copilotVersion)
//   - "user.message":           one per conversation turn (used to count turns)
//   - "tool.execution_start":   a tool invocation (data.toolName)
//   - "tool.execution_complete": completion of a tool call
//   - "session.shutdown":       session summary (modelMetrics)
//
// Returns the extracted metrics and nil on success, or empty metrics and an
// error if the file cannot be read or contains no recognizable events.
func parseEventsJSONLMetrics(path string, verbose bool) (workflow.LogMetrics, error) {
	copilotEventsJSONLLog.Printf("Parsing events.jsonl from: %s", path)

	var metrics workflow.LogMetrics

	// Sanitize path to prevent traversal
	cleanPath := filepath.Clean(path)

	file, err := os.Open(cleanPath)
	if err != nil {
		return metrics, fmt.Errorf("failed to open events.jsonl: %w", err)
	}
	defer file.Close()

	toolCallMap := make(map[string]*workflow.ToolCallInfo)
	var currentSequence []string
	turns := 0
	totalTokens := 0
	fallbackTokens := 0
	sawShutdownModelMetrics := false
	foundAnyEvent := false

	// Per-turn timestamps used to compute Time Between Turns (TBT)
	var turnTimestamps []time.Time

	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		var entry copilotEventsJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			copilotEventsJSONLLog.Printf("Skipping malformed events.jsonl line: %v", err)
			continue
		}
		fallbackTokens += extractFallbackEventTokens(entry)

		foundAnyEvent = true

		switch entry.Type {
		case "session.start":
			copilotEventsJSONLLog.Printf("session.start: sessionId=%s copilotVersion=%s",
				entry.Data.SessionID, entry.Data.CopilotVersion)

		case "user.message":
			// Each user message represents one conversation turn.
			// Save the current tool sequence before starting a new turn.
			turns++
			if len(currentSequence) > 0 {
				metrics.ToolSequences = append(metrics.ToolSequences, currentSequence)
				currentSequence = []string{}
			}
			// Record the timestamp for TBT computation.
			if entry.Timestamp != "" {
				if ts, parseErr := time.Parse(time.RFC3339Nano, entry.Timestamp); parseErr == nil {
					turnTimestamps = append(turnTimestamps, ts)
				} else if ts, parseErr = time.Parse(time.RFC3339, entry.Timestamp); parseErr == nil {
					turnTimestamps = append(turnTimestamps, ts)
				}
			}
			copilotEventsJSONLLog.Printf("user.message: turn=%d", turns)

		case "tool.execution_start":
			// Record the tool invocation and add to the current turn's sequence.
			toolName := entry.Data.ToolName
			if toolName != "" {
				currentSequence = append(currentSequence, toolName)
				if toolInfo, exists := toolCallMap[toolName]; exists {
					toolInfo.CallCount++
				} else {
					toolCallMap[toolName] = &workflow.ToolCallInfo{
						Name:      toolName,
						CallCount: 1,
					}
				}
				copilotEventsJSONLLog.Printf("tool.execution_start: %s", toolName)
			}

		case "session.shutdown":
			// Aggregate token usage across all models from modelMetrics.
			// Track whether the field was present (even if empty) so the fallback
			// is only applied when modelMetrics is truly absent (nil), not when the
			// totals happen to sum to zero.
			if entry.Data.ModelMetrics != nil {
				sawShutdownModelMetrics = true
				for model, m := range entry.Data.ModelMetrics {
					if m.Usage != nil {
						modelTokens := m.Usage.InputTokens + m.Usage.OutputTokens
						totalTokens += modelTokens
						copilotEventsJSONLLog.Printf("session.shutdown: model=%s inputTokens=%d outputTokens=%d",
							model, m.Usage.InputTokens, m.Usage.OutputTokens)
					}
				}
			}
			copilotEventsJSONLLog.Printf("session.shutdown: type=%s totalTokens=%d",
				entry.Data.ShutdownType, totalTokens)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return metrics, fmt.Errorf("error reading events.jsonl: %w", scanErr)
	}

	if !foundAnyEvent {
		return metrics, errors.New("no events found in events.jsonl")
	}

	// Flush any remaining tool sequence
	if len(currentSequence) > 0 {
		metrics.ToolSequences = append(metrics.ToolSequences, currentSequence)
	}

	// Convert tool call map to slice
	for _, toolInfo := range toolCallMap {
		metrics.ToolCalls = append(metrics.ToolCalls, *toolInfo)
	}

	metrics.TokenUsage = totalTokens
	if !sawShutdownModelMetrics && fallbackTokens > 0 {
		metrics.TokenUsage = fallbackTokens
	}
	metrics.Turns = turns

	// Compute Time Between Turns (TBT) from per-turn timestamps.
	// TBT[i] = timestamp[i] - timestamp[i-1] for i > 0. Two or more timestamps
	// are required to measure at least one interval. Only positive intervals are
	// included so that identical or out-of-order timestamps don't skew the statistics.
	if len(turnTimestamps) >= 2 {
		var tbtStats stats.StatVar
		for i := 1; i < len(turnTimestamps); i++ {
			tbt := turnTimestamps[i].Sub(turnTimestamps[i-1])
			if tbt > 0 {
				tbtStats.Add(float64(tbt))
			}
		}
		if tbtStats.Count() > 0 {
			metrics.AvgTimeBetweenTurns = time.Duration(tbtStats.Mean())
			metrics.MaxTimeBetweenTurns = time.Duration(tbtStats.Max())
			metrics.MedianTimeBetweenTurns = time.Duration(tbtStats.Median())
			metrics.StdDevTimeBetweenTurns = time.Duration(tbtStats.SampleStdDev())
			copilotEventsJSONLLog.Printf("TBT computed: avg=%s max=%s median=%s stddev=%s intervals=%d",
				metrics.AvgTimeBetweenTurns, metrics.MaxTimeBetweenTurns,
				metrics.MedianTimeBetweenTurns, metrics.StdDevTimeBetweenTurns, tbtStats.Count())
		}
	}

	copilotEventsJSONLLog.Printf("Parsed events.jsonl: turns=%d totalTokens=%d toolCalls=%d sequences=%d",
		turns, totalTokens, len(toolCallMap), len(metrics.ToolSequences))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(
			fmt.Sprintf("Parsed events.jsonl: %d turns, %d tokens, %d tool calls",
				turns, totalTokens, len(toolCallMap))))
	}

	return metrics, nil
}

func extractFallbackEventTokens(entry copilotEventsJSONLEntry) int {
	if entry.Data.Usage != nil {
		// Only count input and output tokens; deliberately exclude cache tokens
		// (cache_creation_input_tokens, cache_read_input_tokens) to avoid
		// overcounting in fallback mode.
		inputTokens := usageField(entry.Data.Usage, "input_tokens", "prompt_tokens")
		outputTokens := usageField(entry.Data.Usage, "output_tokens", "completion_tokens")
		if tokens := inputTokens + outputTokens; tokens > 0 {
			return tokens
		}
	}

	if entry.Data.InputTokens > 0 || entry.Data.OutputTokens > 0 {
		return entry.Data.InputTokens + entry.Data.OutputTokens
	}
	if entry.UsageInputTokens > 0 || entry.UsageOutputTokens > 0 {
		return entry.UsageInputTokens + entry.UsageOutputTokens
	}
	if entry.AlternateUsageInputTokens > 0 || entry.AlternateUsageOutputTokens > 0 {
		return entry.AlternateUsageInputTokens + entry.AlternateUsageOutputTokens
	}
	return 0
}

// usageField reads primaryKey from a usage map, falling back to aliasKey when
// the primary is absent or zero (e.g. "input_tokens" → "prompt_tokens").
func usageField(usage map[string]any, primaryKey, aliasKey string) int {
	if v := typeutil.ConvertToInt(usage[primaryKey]); v != 0 {
		return v
	}
	return typeutil.ConvertToInt(usage[aliasKey])
}
