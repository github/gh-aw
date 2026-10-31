package cli

import (
	"os"
	"path/filepath"
	"strings"
)

func groupTokenUsageAICPhaseFiles(runDir string, files []string) map[string][]string {
	phases := make(map[string][]string)
	for _, path := range files {
		phase := "agent"
		relative, _ := filepath.Rel(runDir, path)
		for part := range strings.SplitSeq(filepath.ToSlash(relative), "/") {
			if part == "detection" || strings.HasPrefix(part, "detection_usage.") {
				phase = "detection"
				break
			}
			if part == "evals" {
				phase = "evals"
				break
			}
		}
		phases[phase] = append(phases[phase], path)
	}
	return phases
}

func markTokenUsageExecutedPhases(runDir string, phases map[string]*TokenUsageSummary) {
	usageDir := findArtifactDir(runDir, "usage", "")
	if usageDir == "" {
		return
	}
	for _, phase := range []string{"activation", "agent", "detection", "evals"} {
		path := filepath.Join(usageDir, phase, "execution.json")
		if info, err := os.Stat(path); err == nil && !info.IsDir() && phases[phase] == nil {
			phases[phase] = &TokenUsageSummary{}
		}
	}
}

func tokenUsageAICPhaseCostFound(phases map[string]*TokenUsageSummary) bool {
	for _, phase := range phases {
		if phase.AICFound {
			return true
		}
	}
	return false
}

func combineTokenUsageAICPhases(phases map[string]*TokenUsageSummary, warnings []string) *TokenUsageSummary {
	summary := &TokenUsageSummary{Warnings: warnings}
	for _, phase := range []string{"activation", "agent", "detection", "evals"} {
		if usage := phases[phase]; usage != nil {
			summary.TotalAIC += usage.TotalAIC
			summary.AICFound = summary.AICFound || usage.AICFound
			summary.Warnings = append(summary.Warnings, usage.Warnings...)
		}
	}
	if summary.AICFound {
		for _, phase := range []string{"activation", "agent", "detection", "evals"} {
			if usage := phases[phase]; usage != nil && !usage.AICFound && len(usage.Warnings) == 0 {
				addTokenUsageWarning(summary, phase+" usage: no usable accounting; observed cost may be incomplete")
			}
		}
	}
	if !summary.AICFound && len(summary.Warnings) == 0 {
		return nil
	}
	for _, warning := range summary.Warnings {
		tokenUsageLog.Printf("AIC-only analysis warning: %s", warning)
	}
	return summary
}
