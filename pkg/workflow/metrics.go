package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var metricsLog = logger.New("workflow:metrics")

// ToolCallInfo represents statistics for a single tool
type ToolCallInfo struct {
	Name          string        // Prettified tool name (e.g., "github::search_issues", "bash")
	CallCount     int           // Number of times this tool was called
	MaxInputSize  int           // Maximum input size in tokens for any call
	MaxOutputSize int           // Maximum output size in tokens for any call
	MaxDuration   time.Duration // Maximum execution duration for any call
}

// LogMetrics represents extracted metrics from log files
type LogMetrics struct {
	TokenUsage    int
	EstimatedCost float64
	Turns         int            // Number of turns needed to complete the task
	ToolCalls     []ToolCallInfo // Tool call statistics
	ToolSequences [][]string     // Sequences of tool calls preserving order
	// Timestamp removed - use GitHub API timestamps instead of parsing from logs
}

// ExtractFirstMatch extracts the first regex match from a string
// Note: This function compiles the regex on each call. For frequently-used patterns,
// consider pre-compiling at package level or caching the compiled regex.
func ExtractFirstMatch(text, pattern string) string {
	re := regexp.MustCompile(`(?i)` + pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
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
	inputTop := ConvertToInt(data["input_tokens"])
	outputTop := ConvertToInt(data["output_tokens"])
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
			if tokens := ConvertToInt(val); tokens > 0 {
				return tokens
			}
		}
	}

	// Check nested usage objects (Claude and OpenAI API formats)
	if usage, exists := data["usage"]; exists {
		if usageMap, ok := usage.(map[string]any); ok {
			// Claude format: {"usage": {"input_tokens": 10, "output_tokens": 5, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 200}}
			inputTokens := ConvertToInt(usageMap["input_tokens"])
			outputTokens := ConvertToInt(usageMap["output_tokens"])
			cacheCreationTokens := ConvertToInt(usageMap["cache_creation_input_tokens"])
			cacheReadTokens := ConvertToInt(usageMap["cache_read_input_tokens"])

			// OpenAI format: {"usage": {"prompt_tokens": 100, "completion_tokens": 50}}
			// If Claude fields are not present, try OpenAI fields
			if inputTokens == 0 {
				inputTokens = ConvertToInt(usageMap["prompt_tokens"])
			}
			if outputTokens == 0 {
				outputTokens = ConvertToInt(usageMap["completion_tokens"])
			}

			totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
			if totalTokens > 0 {
				return totalTokens
			}

			// Generic token count fields inside usage
			for _, field := range tokenFields {
				if val, exists := usageMap[field]; exists {
					if tokens := ConvertToInt(val); tokens > 0 {
						return tokens
					}
				}
			}
		}
	}

	// Check for delta structures (streaming format)
	if delta, exists := data["delta"]; exists {
		if deltaMap, ok := delta.(map[string]any); ok {
			if usage, exists := deltaMap["usage"]; exists {
				if usageMap, ok := usage.(map[string]any); ok {
					inputTokens := ConvertToInt(usageMap["input_tokens"])
					outputTokens := ConvertToInt(usageMap["output_tokens"])
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
		if cost := ConvertToFloat(val); cost > 0 {
			if metricsLog.Enabled() {
				metricsLog.Printf("Cost extracted: value=%.6f", cost)
			}
			return cost
		}
	}

	for _, field := range costFields {
		if val, exists := data[field]; exists {
			if cost := ConvertToFloat(val); cost > 0 {
				return cost
			}
		}
	}

	// Check nested billing or pricing objects
	if billing, exists := data["billing"]; exists {
		if billingMap, ok := billing.(map[string]any); ok {
			for _, field := range costFields {
				if val, exists := billingMap[field]; exists {
					if cost := ConvertToFloat(val); cost > 0 {
						return cost
					}
				}
			}
		}
	}

	return 0
}

// ConvertToInt safely converts any to int
func ConvertToInt(val any) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		intVal := int(v)
		// Warn if truncation occurs (value has fractional part)
		if v != float64(intVal) {
			metricsLog.Printf("Float value %.2f truncated to integer %d", v, intVal)
		}
		return intVal
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

// ConvertToFloat safely converts any to float64
func ConvertToFloat(val any) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

// PrettifyToolName removes "mcp__" prefix and formats tool names nicely
func PrettifyToolName(toolName string) string {
	// Handle MCP tools: "mcp__github__search_issues" -> "github_search_issues"
	// Avoid colons and leave underscores as-is
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.Split(toolName, "__")
		if len(parts) >= 3 {
			provider := parts[1]
			method := strings.Join(parts[2:], "_")
			return fmt.Sprintf("%s_%s", provider, method)
		}
		// If format is unexpected, just remove the mcp__ prefix
		return strings.TrimPrefix(toolName, "mcp__")
	}

	// Handle bash specially - keep as "bash"
	if strings.ToLower(toolName) == "bash" {
		return "bash"
	}

	// Return other tool names as-is
	return toolName
}

// ExtractMCPServer extracts the MCP server name from a tool name
func ExtractMCPServer(toolName string) string {
	// For MCP tools with pattern "mcp__server__method", extract "server"
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.Split(toolName, "__")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	// For non-MCP tools, return the tool name as-is
	return toolName
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
	sort.Slice(opts.Metrics.ToolCalls, func(i, j int) bool {
		return opts.Metrics.ToolCalls[i].Name < opts.Metrics.ToolCalls[j].Name
	})
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
	sort.Slice(metrics.ToolCalls, func(i, j int) bool {
		return metrics.ToolCalls[i].Name < metrics.ToolCalls[j].Name
	})
}
