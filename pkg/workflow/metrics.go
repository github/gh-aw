package workflow

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var metricsLog = logger.New("workflow:metrics")

// ToolCallInfo represents statistics for a single tool
type ToolCallInfo struct {
	Name          string        // Prettified tool name (e.g., "github::search_issues", "bash")
	CallCount     int           // Number of times this tool was called
	MaxInputSize  int           // Maximum input size for any call (engine-dependent units, often bytes/chars)
	MaxOutputSize int           // Maximum output size for any call (engine-dependent units, often bytes/chars)
	MaxDuration   time.Duration // Maximum execution duration for any call
	OutputSample  string        // Preview of the largest tool response (first few lines, truncated)
}

// LogMetrics represents extracted metrics from log files
type LogMetrics struct {
	TokenUsage             int
	EstimatedCost          float64
	Turns                  int            // Number of turns needed to complete the task
	ToolCalls              []ToolCallInfo // Tool call statistics
	ToolSequences          [][]string     // Sequences of tool calls preserving order
	AvgTimeBetweenTurns    time.Duration  // Mean time between consecutive LLM API calls (computed from per-turn timestamps when available)
	MaxTimeBetweenTurns    time.Duration  // Maximum time between any two consecutive LLM API calls
	MedianTimeBetweenTurns time.Duration  // Median time between consecutive LLM API calls
	// StdDevTimeBetweenTurns is the sample standard deviation (Bessel's correction, n-1
	// denominator) of inter-turn intervals, treating the observed turns as a sample of
	// the agent's execution behaviour rather than an exhaustive population.
	StdDevTimeBetweenTurns time.Duration
	// Timestamp removed - use GitHub API timestamps instead of parsing from logs
}

// ExtractJSONMetrics extracts metrics from streaming JSON log lines
func ExtractJSONMetrics(line string, verbose bool) LogMetrics {
	var metrics LogMetrics

	// Trim the line first
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return metrics
	}

	metricsLog.Printf("Extracting metrics from JSON line: line_length=%d", len(trimmed))

	// If the line isn't a clean JSON object, try to extract a JSON object substring
	jsonStr := trimmed
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		// Find first '{' and last '}' and attempt to parse that slice
		open := strings.Index(trimmed, "{")
		close := strings.LastIndex(trimmed, "}")
		if open == -1 || close == -1 || close <= open {
			return metrics
		}
		jsonStr = trimmed[open : close+1]
	}

	// Try to parse as generic JSON
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &jsonData); err != nil {
		// If parsing fails, try a relaxed approach: sometimes logs contain a JSON-like object with single quotes
		// Replace single quotes with double quotes as a last resort (not ideal, but helpful for noisy logs)
		relaxed := strings.ReplaceAll(jsonStr, "'", "\"")
		if err2 := json.Unmarshal([]byte(relaxed), &jsonData); err2 != nil {
			return metrics
		}
	}

	// Extract token usage from various possible fields and structures
	if tokens := ExtractJSONTokenUsage(jsonData); tokens > 0 {
		metrics.TokenUsage = tokens
	}

	// Extract cost information from various possible fields
	if cost := ExtractJSONCost(jsonData); cost > 0 {
		metrics.EstimatedCost = cost
	}

	return metrics
}

// ExtractJSONTokenUsage extracts token usage from JSON data
func ExtractJSONTokenUsage(data map[string]any) int {
	// Prefer explicit input+output sums at the top-level
	inputTop := typeutil.ConvertToInt(data["input_tokens"])
	outputTop := typeutil.ConvertToInt(data["output_tokens"])
	if inputTop > 0 || outputTop > 0 {
		totalTokens := inputTop + outputTop
		if metricsLog.Enabled() {
			metricsLog.Printf("Token usage extracted: input=%d, output=%d, total=%d", inputTop, outputTop, totalTokens)
		}
		return totalTokens
	}

	// Check top-level token fields that represent a single total value
	tokenFields := []string{"tokens", "token_count", "total_tokens"}
	for _, field := range tokenFields {
		if val, exists := data[field]; exists {
			if tokens := typeutil.ConvertToInt(val); tokens > 0 {
				return tokens
			}
		}
	}

	// Check nested usage objects (Claude and OpenAI API formats)
	if usage, exists := data["usage"]; exists {
		if usageMap, ok := usage.(map[string]any); ok {
			if tokens := extractNestedJSONTokenUsage(usageMap, tokenFields); tokens > 0 {
				return tokens
			}
		}
	}

	return extractDeltaJSONTokenUsage(data)
}

func extractNestedJSONTokenUsage(usageMap map[string]any, tokenFields []string) int {
	// Claude format: {"usage": {"input_tokens": 10, "output_tokens": 5, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 200}}
	inputTokens := typeutil.ConvertToInt(usageMap["input_tokens"])
	outputTokens := typeutil.ConvertToInt(usageMap["output_tokens"])
	cacheCreationTokens := typeutil.ConvertToInt(usageMap["cache_creation_input_tokens"])
	cacheReadTokens := typeutil.ConvertToInt(usageMap["cache_read_input_tokens"])

	// OpenAI format: {"usage": {"prompt_tokens": 100, "completion_tokens": 50}}
	// If Claude fields are not present, try OpenAI fields
	if inputTokens == 0 {
		inputTokens = typeutil.ConvertToInt(usageMap["prompt_tokens"])
	}
	if outputTokens == 0 {
		outputTokens = typeutil.ConvertToInt(usageMap["completion_tokens"])
	}

	totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
	if totalTokens > 0 {
		return totalTokens
	}

	// Generic token count fields inside usage
	for _, field := range tokenFields {
		if val, exists := usageMap[field]; exists {
			if tokens := typeutil.ConvertToInt(val); tokens > 0 {
				return tokens
			}
		}
	}
	return 0
}

func extractDeltaJSONTokenUsage(data map[string]any) int {
	// Check for delta structures (streaming format)
	if delta, exists := data["delta"]; exists {
		if deltaMap, ok := delta.(map[string]any); ok {
			if usage, exists := deltaMap["usage"]; exists {
				if usageMap, ok := usage.(map[string]any); ok {
					inputTokens := typeutil.ConvertToInt(usageMap["input_tokens"])
					outputTokens := typeutil.ConvertToInt(usageMap["output_tokens"])
					if inputTokens > 0 || outputTokens > 0 {
						return inputTokens + outputTokens
					}
				}
			}
		}
	}

	return 0
}

// ExtractJSONCost extracts cost information from JSON data
func ExtractJSONCost(data map[string]any) float64 {
	// Common cost field names
	costFields := []string{"total_cost_usd", "cost", "price", "amount", "total_cost", "estimated_cost"}

	// Prefer explicit total_cost_usd at top-level
	if val, exists := data["total_cost_usd"]; exists {
		if cost := typeutil.ConvertToFloat(val); cost > 0 {
			if metricsLog.Enabled() {
				metricsLog.Printf("Cost extracted: value=%.6f", cost)
			}
			return cost
		}
	}

	for _, field := range costFields {
		if val, exists := data[field]; exists {
			if cost := typeutil.ConvertToFloat(val); cost > 0 {
				return cost
			}
		}
	}

	// Check nested billing or pricing objects
	if billing, exists := data["billing"]; exists {
		if billingMap, ok := billing.(map[string]any); ok {
			for _, field := range costFields {
				if val, exists := billingMap[field]; exists {
					if cost := typeutil.ConvertToFloat(val); cost > 0 {
						return cost
					}
				}
			}
		}
	}

	return 0
}

// FinalizeToolMetricsOptions holds the options for FinalizeToolMetrics
type FinalizeToolMetricsOptions struct {
	Metrics         *LogMetrics
	ToolCallMap     map[string]*ToolCallInfo
	CurrentSequence []string
	Turns           int
	TokenUsage      int
}

// FinalizeToolMetrics completes the metric collection process by finalizing sequences,
// converting tool call maps to sorted slices, and optionally counting errors using patterns.
// This function is called by engine-specific ParseLogMetrics implementations to avoid code duplication.
func FinalizeToolMetrics(opts FinalizeToolMetricsOptions) {
	// Add final sequence if any
	if len(opts.CurrentSequence) > 0 {
		opts.Metrics.ToolSequences = append(opts.Metrics.ToolSequences, opts.CurrentSequence)
	}

	opts.Metrics.TokenUsage = opts.TokenUsage
	opts.Metrics.Turns = opts.Turns

	// Convert tool call map to slice
	for _, toolInfo := range opts.ToolCallMap {
		opts.Metrics.ToolCalls = append(opts.Metrics.ToolCalls, *toolInfo)
	}

	// Sort tool calls by name for consistent output
	slices.SortFunc(opts.Metrics.ToolCalls, func(a, b ToolCallInfo) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	metricsLog.Printf("FinalizeToolMetrics: turns=%d, tokenUsage=%d, toolCalls=%d, sequences=%d",
		opts.Metrics.Turns, opts.Metrics.TokenUsage, len(opts.Metrics.ToolCalls), len(opts.Metrics.ToolSequences))
}

// FinalizeToolCallsAndSequence completes the tool call and sequence finalization.
// Use this function when the engine extracts token usage and turns from structured result entries,
// rather than accumulating them during line-by-line log parsing. This is a lighter version of
// FinalizeToolMetrics for engines that do not need to finalize token usage and turns here.
func FinalizeToolCallsAndSequence(
	metrics *LogMetrics,
	toolCallMap map[string]*ToolCallInfo,
	currentSequence []string,
) {
	// Add final sequence if any
	if len(currentSequence) > 0 {
		metrics.ToolSequences = append(metrics.ToolSequences, currentSequence)
	}

	// Convert tool call map to slice
	for _, toolInfo := range toolCallMap {
		metrics.ToolCalls = append(metrics.ToolCalls, *toolInfo)
	}

	// Sort tool calls by name for consistent output
	slices.SortFunc(metrics.ToolCalls, func(a, b ToolCallInfo) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	metricsLog.Printf("FinalizeToolCallsAndSequence: toolCalls=%d, sequences=%d", len(metrics.ToolCalls), len(metrics.ToolSequences))
}
