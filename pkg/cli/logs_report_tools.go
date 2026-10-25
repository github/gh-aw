package cli

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// ToolUsageStatsBase contains the identity and metrics shared by tool usage summaries.
type ToolUsageStatsBase struct {
	ToolName      string
	CallCount     int
	MaxOutputSize int
	MaxDuration   string
}

func (b *ToolUsageStatsBase) syncFromFields(toolName string, callCount int, maxOutputSize int, maxDuration string) {
	*b = ToolUsageStatsBase{
		ToolName:      toolName,
		CallCount:     callCount,
		MaxOutputSize: maxOutputSize,
		MaxDuration:   maxDuration,
	}
}

func (b *ToolUsageStatsBase) syncFields(toolName *string, callCount *int, maxOutputSize *int, maxDuration *string) {
	if *toolName == "" {
		*toolName = b.ToolName
	}
	if *callCount == 0 {
		*callCount = b.CallCount
	}
	if *maxOutputSize == 0 {
		*maxOutputSize = b.MaxOutputSize
	}
	if *maxDuration == "" {
		*maxDuration = b.MaxDuration
	}
}

// ToolUsageSummary contains aggregated tool usage statistics
type ToolUsageSummary struct {
	ToolUsageStatsBase `json:"-" console:"-"`
	Name               string `json:"name" console:"header:Tool"`
	TotalCalls         int    `json:"total_calls" console:"header:Total Calls,format:number"`
	Runs               int    `json:"runs" console:"header:Runs"` // Number of runs that used this tool
	MaxOutputSize      int    `json:"max_output_size,omitempty" console:"header:Max Output,format:filesize,default:N/A,omitempty"`
	MaxDuration        string `json:"max_duration,omitempty" console:"header:Max Duration,default:N/A,omitempty"`
}

// MarshalJSON preserves the generic tool usage report schema.
func (s ToolUsageSummary) MarshalJSON() ([]byte, error) {
	normalized := s
	normalized.syncFieldsFromBase()
	return json.Marshal(struct {
		Name          string `json:"name"`
		TotalCalls    int    `json:"total_calls"`
		Runs          int    `json:"runs"`
		MaxOutputSize int    `json:"max_output_size,omitempty"`
		MaxDuration   string `json:"max_duration,omitempty"`
	}{
		Name:          normalized.Name,
		TotalCalls:    normalized.TotalCalls,
		Runs:          normalized.Runs,
		MaxOutputSize: normalized.MaxOutputSize,
		MaxDuration:   normalized.MaxDuration,
	})
}

// UnmarshalJSON preserves support for both the legacy generic schema ("name",
// "total_calls") and the MCP-style schema ("tool_name", "call_count").
// Keep this shadow struct aligned with ToolUsageSummary and ToolUsageStatsBase;
// TestToolUsageSummaryUnmarshalJSONCompatibility guards this contract.
func (s *ToolUsageSummary) UnmarshalJSON(data []byte) error {
	var summary struct {
		Name          string `json:"name"`
		ToolName      string `json:"tool_name"`
		TotalCalls    *int   `json:"total_calls"`
		CallCount     *int   `json:"call_count"`
		Runs          int    `json:"runs"`
		MaxOutputSize int    `json:"max_output_size"`
		MaxDuration   string `json:"max_duration"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return err
	}

	if summary.Name != "" {
		s.Name = summary.Name
	} else {
		s.Name = summary.ToolName
	}
	switch {
	case summary.TotalCalls != nil:
		s.TotalCalls = *summary.TotalCalls
	case summary.CallCount != nil:
		s.TotalCalls = *summary.CallCount
	default:
		s.TotalCalls = 0
	}
	s.MaxOutputSize = summary.MaxOutputSize
	s.MaxDuration = summary.MaxDuration
	s.Runs = summary.Runs
	s.syncBaseFromFields()
	return nil
}

func (s *ToolUsageSummary) syncBaseFromFields() {
	s.syncFromFields(s.Name, s.TotalCalls, s.MaxOutputSize, s.MaxDuration)
}

func (s *ToolUsageSummary) syncFieldsFromBase() {
	s.syncFields(&s.Name, &s.TotalCalls, &s.MaxOutputSize, &s.MaxDuration)
}

// toolNameStopWords is a set of common English words that should never be treated as tool names.
// Built once at package init and reused across all isValidToolName calls.
var toolNameStopWords = map[string]bool{
	"calls": true, "to": true, "for": true, "the": true, "a": true, "an": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true, "might": true,
	"Testing": true, "multiple": true, "launches": true, "command": true, "invocation": true,
	"with": true, "from": true, "by": true, "at": true, "in": true, "on": true,
}

// isValidToolName checks if a tool name appears to be valid.
// Filters out single words, common words, and other garbage that shouldn't be tools.
func isValidToolName(toolName string) bool {
	name := strings.TrimSpace(toolName)

	// Filter out empty names
	if name == "" || name == "-" {
		return false
	}

	// Filter out single character names
	if len(name) == 1 {
		return false
	}

	// Filter out common English words that are likely from error messages
	if toolNameStopWords[name] {
		return false
	}

	// Tool names should typically contain underscores, hyphens, or be camelCase
	// or be all lowercase. Single words without these patterns are suspect.
	hasUnderscore := strings.Contains(name, "_")
	hasHyphen := strings.Contains(name, "-")
	hasCapital := strings.ToLower(name) != name

	// Reject short, all-lowercase, single-word names with no separators — these
	// are almost certainly log-message fragments rather than real tool names.
	words := strings.Fields(name)
	if len(words) == 1 && !hasUnderscore && !hasHyphen && len(name) < 10 && !hasCapital {
		return false
	}

	return true
}

// buildToolUsageSummary aggregates tool usage across all runs
// Filters out invalid tool names that appear to be fragments or garbage
func buildToolUsageSummary(processedRuns []ProcessedRun) []ToolUsageSummary {
	reportLog.Printf("Building tool usage summary from %d processed runs", len(processedRuns))
	toolStats := make(map[string]*ToolUsageSummary)

	for _, pr := range processedRuns {
		accumulateRunToolUsage(pr, toolStats)
	}

	var result []ToolUsageSummary
	for _, info := range toolStats {
		result = append(result, *info)
	}

	// Sort by total calls descending
	slices.SortFunc(result, func(a, b ToolUsageSummary) int {
		return cmp.Compare(b.TotalCalls, a.TotalCalls)
	})

	return result
}

func accumulateRunToolUsage(pr ProcessedRun, toolStats map[string]*ToolUsageSummary) {
	// Extract metrics from run's logs
	metrics := ExtractLogMetricsFromRun(pr)

	// Track which runs use each tool
	toolRunTracker := make(map[string]struct {
	})

	for _, toolCall := range metrics.ToolCalls {
		displayKey := workflow.PrettifyToolName(toolCall.Name)

		// Filter out invalid tool names
		if !isValidToolName(displayKey) {
			continue
		}

		toolRunTracker[displayKey] = struct {
		}{}

		if existing, exists := toolStats[displayKey]; exists {
			existing.TotalCalls += toolCall.CallCount
			if toolCall.MaxOutputSize > existing.MaxOutputSize {
				existing.MaxOutputSize = toolCall.MaxOutputSize
			}
			if toolCall.MaxDuration > 0 {
				maxDur := timeutil.FormatDuration(toolCall.MaxDuration)
				if existing.MaxDuration == "" || toolCall.MaxDuration > parseDurationString(existing.MaxDuration) {
					existing.MaxDuration = maxDur
				}
			}
			existing.syncBaseFromFields()
		} else {
			info := &ToolUsageSummary{
				Name:          displayKey,
				TotalCalls:    toolCall.CallCount,
				MaxOutputSize: toolCall.MaxOutputSize,
				Runs:          0, // Will be incremented below
			}
			if toolCall.MaxDuration > 0 {
				info.MaxDuration = timeutil.FormatDuration(toolCall.MaxDuration)
			}
			info.syncBaseFromFields()
			toolStats[displayKey] = info
		}
	}

	// Increment run count for tools used in this run
	for toolName := range toolRunTracker {
		if stat, exists := toolStats[toolName]; exists {
			stat.Runs++
		}
	}
}
