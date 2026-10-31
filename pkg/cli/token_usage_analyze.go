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

	if dirs, episode, err := readEpisodeArtifactDirs(runDir); episode || err != nil {
		if err != nil {
			return nil, err
		}
		return analyzeEpisodeUsage(dirs, verbose, true)
	}

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
			applyDecisionGateTokenUsage(summary, runDir)
			applyGatewaySteeringSummary(summary, runDir)
			applySteeringNotices(summary, runDir)
			augmentSubagentModelAttribution(runDir, summary)
			return summary, nil
		}
	}

	agentUsagePath := findAgentUsageFile(runDir)
	if agentUsagePath == "" {
		summary := decisionGateTokenUsageSummary(runDir)
		return summary, nil
	}
	agentFileInfo, _ := os.Stat(agentUsagePath)
	if agentFileInfo != nil {
		console.LogVerbose(verbose, fmt.Sprintf("  Found agent usage file: %s (%d bytes)", filepath.Base(agentUsagePath), agentFileInfo.Size()))
	}

	summary, err := parseAgentUsageFile(agentUsagePath)
	if err != nil || summary == nil {
		return summary, err
	}
	applyDecisionGateTokenUsage(summary, runDir)
	applyGatewaySteeringSummary(summary, runDir)
	applySteeringNotices(summary, runDir)
	augmentSubagentModelAttribution(runDir, summary)
	return summary, nil
}

// analyzeTokenUsageAICOnly parses token usage inputs and computes only TotalAIC.
// It intentionally skips the full token-usage breakdown for callers that only need cost.
func analyzeTokenUsageAICOnly(runDir string, verbose bool) (*TokenUsageSummary, error) {
	tokenUsageLog.Printf("Analyzing token usage (AIC only) in: %s", runDir)

	if dirs, episode, err := readEpisodeArtifactDirs(runDir); episode || err != nil {
		if err != nil {
			return nil, err
		}
		return analyzeEpisodeUsage(dirs, verbose, false)
	}

	usageJSONLFiles, warnings := findUsageJSONLFiles(runDir)
	if len(usageJSONLFiles) > 0 {
		console.LogVerbose(verbose, "  Found usage JSONL files: "+strings.Join(usageJSONLFiles, ", "))
	}
	phaseFiles := groupTokenUsageAICPhaseFiles(runDir, usageJSONLFiles)
	phases := make(map[string]*TokenUsageSummary)
	seenRequestIDs := make(map[string]struct{})
	for _, phase := range []string{"agent", "detection", "evals"} {
		files := phaseFiles[phase]
		if len(files) == 0 {
			continue
		}
		total, found, fileWarnings, err := sumAICFromUsageJSONLFilesWithSeen(files, seenRequestIDs)
		if err != nil {
			return nil, err
		}
		if !found && len(fileWarnings) == 0 && !slices.ContainsFunc(files, func(path string) bool {
			return strings.Contains(strings.ToLower(filepath.Base(path)), "usage")
		}) {
			continue
		}
		if phase == "detection" && !found && len(fileWarnings) == 0 {
			continue // Collection creates an empty detection placeholder even when the phase was skipped.
		}
		phases[phase] = &TokenUsageSummary{TotalAIC: total, AICFound: found, Warnings: fileWarnings}
	}
	markTokenUsageExecutedPhases(runDir, phases)
	if err := applyTokenUsageAICFallbacks(runDir, verbose, usageJSONLFiles, seenRequestIDs, phases); err != nil {
		return nil, err
	}
	if activation := decisionGateTokenUsageSummary(runDir); activation != nil {
		phases["activation"] = activation
	}
	return combineTokenUsageAICPhases(phases, warnings), nil
}

func analyzeEpisodeUsage(dirs []string, verbose, details bool) (*TokenUsageSummary, error) {
	summary := &TokenUsageSummary{ByModel: make(map[string]*ModelTokenUsage), agentModels: make(map[string]*ModelTokenUsage)}
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		files, warnings := findUsageJSONLFilesInDirs([]string{dir})
		usage, hadAWFRecords, err := summarizeUsageJSONLFilesWithSeen(files, seen)
		if err != nil {
			return nil, err
		}
		usage.Warnings = append(warnings, usage.Warnings...)
		if !hadAWFRecords && !usage.AICFound {
			path := findAgentUsageFile(dir)
			if path != "" {
				fallback, err := parseAgentUsageFile(path)
				if err != nil {
					return nil, err
				}
				if fallback != nil {
					mergeTokenUsageSummary(usage, fallback)
				}
			}
		}
		if details {
			applyGatewaySteeringSummary(usage, dir)
			applySteeringNotices(usage, dir)
			augmentSubagentModelAttribution(dir, usage)
		}
		applyDecisionGateTokenUsage(usage, dir)
		mergeTokenUsageSummary(summary, usage)
		console.LogVerbose(verbose, fmt.Sprintf("  Analyzed %d compact usage streams", len(files)))
	}
	if summary.TotalRequests == 0 && !summary.AICFound && len(summary.Warnings) == 0 && summary.TotalSteeringEvents == 0 {
		return nil, nil
	}
	return summary, nil
}

func applyTokenUsageAICFallbacks(runDir string, verbose bool, usageJSONLFiles []string, seenRequestIDs map[string]struct{}, phases map[string]*TokenUsageSummary) error {
	agent := phases["agent"]
	if agent == nil {
		agent = &TokenUsageSummary{}
	}
	filePath := findTokenUsageFile(runDir)
	if filePath != "" && !slices.Contains(usageJSONLFiles, filePath) {
		phases["agent"] = agent
		fileAIC, fileFound, fileWarnings, err := sumAICFromUsageJSONLFilesWithSeen([]string{filePath}, seenRequestIDs)
		if err != nil {
			if !tokenUsageAICPhaseCostFound(phases) {
				return err
			}
			addTokenUsageWarning(agent, fmt.Sprintf("Cannot inspect usage file %s; observed cost may be incomplete: %v", filepath.Base(filePath), err))
		} else {
			agent.Warnings = append(agent.Warnings, fileWarnings...)
			// Usage artifacts remain authoritative; inspect other layouts for gaps
			// without counting a mirrored proxy stream a second time.
			if !agent.AICFound {
				agent.TotalAIC, agent.AICFound = fileAIC, fileFound
			}
		}
	}
	if !agent.AICFound {
		fallback, err := analyzeAgentUsageAICOnly(runDir, verbose, nil)
		if err != nil {
			if !tokenUsageAICPhaseCostFound(phases) {
				return err
			}
			addTokenUsageWarning(agent, fmt.Sprintf("Cannot inspect agent usage; observed cost may be incomplete: %v", err))
			phases["agent"] = agent
		} else if fallback != nil {
			agent.TotalAIC, agent.AICFound = fallback.TotalAIC, fallback.AICFound
			agent.Warnings = append(agent.Warnings, fallback.Warnings...)
			phases["agent"] = agent
		}
	}
	return nil
}

func analyzeAgentUsageAICOnly(runDir string, verbose bool, warnings []string) (*TokenUsageSummary, error) {
	agentUsagePath := findAgentUsageFile(runDir)
	if agentUsagePath == "" {
		if len(warnings) > 0 {
			return &TokenUsageSummary{Warnings: warnings}, nil
		}
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
	if !summary.AICFound {
		addTokenUsageWarning(summary, filepath.Base(agentUsagePath)+": unpriced usage; observed cost may be incomplete")
	}
	return &TokenUsageSummary{
		TotalAIC: summary.TotalAIC,
		AICFound: summary.AICFound,
		Warnings: append(warnings, summary.Warnings...),
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
