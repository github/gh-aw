package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/timeutil"
)

// analyzeTokenUsage finds and parses the token-usage.jsonl file from a run directory.
func analyzeTokenUsage(runDir string, verbose bool) (*TokenUsageSummary, error) {
	tokenUsageLog.Printf("Analyzing token usage in: %s", runDir)

	filePath := findTokenUsageFile(runDir)
	if filePath != "" {
		fileInfo, _ := os.Stat(filePath)
		if fileInfo != nil {
			console.LogVerbose(verbose, fmt.Sprintf("  Found token usage file: %s (%d bytes)", filepath.Base(filePath), fileInfo.Size()))
		}

		summary, err := parseTokenUsageFile(filePath)
		if err != nil {
			return summary, err
		}
		// When the file exists but contains no entries (e.g. usage artifact has an
		// empty placeholder token_usage.jsonl), fall through to the agent_usage.json
		// fallback rather than returning nil immediately.
		if summary != nil {
			summary.TotalSteeringEvents = countAPIProxySteeringEvents(runDir)
			augmentSubagentModelAttribution(runDir, summary)
			return summary, nil
		}
	}

	agentUsagePath := findAgentUsageFile(runDir)
	if agentUsagePath == "" {
		return nil, nil
	}
	agentFileInfo, _ := os.Stat(agentUsagePath)
	if agentFileInfo != nil {
		console.LogVerbose(verbose, fmt.Sprintf("  Found agent usage file: %s (%d bytes)", filepath.Base(agentUsagePath), agentFileInfo.Size()))
	}

	summary, err := parseAgentUsageFile(agentUsagePath)
	if err != nil || summary == nil {
		return summary, err
	}
	summary.TotalSteeringEvents = countAPIProxySteeringEvents(runDir)
	augmentSubagentModelAttribution(runDir, summary)
	return summary, nil
}

// analyzeTokenUsageAICOnly parses token usage inputs and computes only TotalAIC.
// It intentionally skips effective-token computation for callers that only need cost.
func analyzeTokenUsageAICOnly(runDir string, verbose bool) (*TokenUsageSummary, error) {
	tokenUsageLog.Printf("Analyzing token usage (AIC only) in: %s", runDir)

	usageJSONLFiles := findUsageJSONLFiles(runDir)
	if len(usageJSONLFiles) > 0 {
		console.LogVerbose(verbose, "  Found usage JSONL files: "+strings.Join(usageJSONLFiles, ", "))
		totalAIC, found, warnings, err := sumAICFromUsageJSONLFilesWithWarnings(usageJSONLFiles)
		if err != nil {
			return nil, err
		}
		if found {
			for _, warning := range warnings {
				tokenUsageLog.Printf("AIC-only analysis warning: %s", warning)
			}
			return &TokenUsageSummary{TotalAIC: totalAIC, Warnings: warnings}, nil
		}
	}

	filePath := findTokenUsageFile(runDir)
	if filePath != "" {
		fileInfo, _ := os.Stat(filePath)
		if fileInfo != nil {
			console.LogVerbose(verbose, fmt.Sprintf("  Found token usage file: %s (%d bytes)", filepath.Base(filePath), fileInfo.Size()))
		}

		summary, err := parseTokenUsageFile(filePath)
		if err != nil {
			return nil, err
		}
		if summary == nil || !summary.AICFound {
			goto fallback
		}
		for _, warning := range summary.Warnings {
			tokenUsageLog.Printf("AIC-only analysis warning: %s", warning)
		}
		return &TokenUsageSummary{TotalAIC: summary.TotalAIC, Warnings: summary.Warnings}, nil
	}

fallback:
	agentUsagePath := findAgentUsageFile(runDir)
	if agentUsagePath == "" {
		return nil, nil
	}
	agentFileInfo, _ := os.Stat(agentUsagePath)
	if agentFileInfo != nil {
		console.LogVerbose(verbose, fmt.Sprintf("  Found agent usage file: %s (%d bytes)", filepath.Base(agentUsagePath), agentFileInfo.Size()))
	}

	summary, err := parseAgentUsageFile(agentUsagePath)
	if err != nil || summary == nil {
		return summary, err
	}
	return &TokenUsageSummary{
		TotalAIC: summary.TotalAIC,
	}, nil
}

// TotalTokens returns the sum of all token types
func (s *TokenUsageSummary) TotalTokens() int {
	return s.TotalInputTokens + s.TotalOutputTokens + s.TotalCacheReadTokens + s.TotalCacheWriteTokens
}

// AvgDurationMs returns the average request duration in milliseconds
func (s *TokenUsageSummary) AvgDurationMs() int {
	if s.TotalRequests == 0 {
		return 0
	}
	return s.TotalDurationMs / s.TotalRequests
}

// ModelRows returns the by-model data as sorted rows for console rendering
func (s *TokenUsageSummary) ModelRows() []ModelTokenUsageRow {
	rows := make([]ModelTokenUsageRow, 0, len(s.ByModel))
	for model, usage := range s.ByModel {
		avgDur := 0
		if usage.Requests > 0 {
			avgDur = usage.DurationMs / usage.Requests
		}
		rows = append(rows, ModelTokenUsageRow{
			Model:            model,
			Provider:         usage.Provider,
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			AIC:              usage.AIC,
			Requests:         usage.Requests,
			AvgDuration:      timeutil.FormatDurationMs(avgDur),
		})
	}
	// Sort by total tokens descending
	slices.SortFunc(rows, func(a, b ModelTokenUsageRow) int {
		iTot := a.InputTokens + a.OutputTokens + a.CacheReadTokens + a.CacheWriteTokens
		jTot := b.InputTokens + b.OutputTokens + b.CacheReadTokens + b.CacheWriteTokens
		if iTot > jTot {
			return -1
		}
		if iTot < jTot {
			return 1
		}
		return 0
	})
	return rows
}

func populateAIC(summary *TokenUsageSummary) {
	if summary == nil {
		return
	}

	total := 0.0
	for model, usage := range summary.ByModel {
		if usage == nil {
			continue
		}
		aic := computeModelInferenceAIC(usage.Provider, model, usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, usage.ReasoningTokens)
		usage.AIC = aic
		total += aic
	}
	summary.TotalAIC = total
}
