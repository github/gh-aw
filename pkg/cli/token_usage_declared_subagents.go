package cli

import (
	"encoding/json"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	effectiveModelDateSuffix     = regexp.MustCompile(`-[0-9]{4}(?:-[0-9]{2}-[0-9]{2}|[0-9]{4})$`)
	claudeHyphenatedModelVersion = regexp.MustCompile(`^(claude-.+)-([0-9]+)-([0-9]+)$`)
)

func augmentDeclaredSubagentModels(runDir string, summary *TokenUsageSummary) {
	infoPath := findAwInfoPath(runDir)
	if infoPath == "" {
		return
	}
	content, err := os.ReadFile(infoPath)
	if err != nil {
		addTokenUsageWarning(summary, "cannot read declared sub-agent models: "+err.Error())
		return
	}
	var info struct {
		Models []struct {
			Name     string   `json:"name"`
			Model    string   `json:"model"`
			Patterns []string `json:"patterns"`
		} `json:"sub_agent_models"`
	}
	if err := json.Unmarshal(content, &info); err != nil {
		addTokenUsageWarning(summary, "cannot parse declared sub-agent models: "+err.Error())
		return
	}
	observedModels := subagentObservedModels(summary)
	observedNames := slices.Sorted(maps.Keys(observedModels))
	for _, model := range info.Models {
		row := SubagentModelRequest{AgentName: model.Name, RequestedModel: model.Model}
		patterns := model.Patterns
		if len(patterns) == 0 {
			patterns = []string{model.Model}
		}
		for _, observed := range observedNames {
			usage := observedModels[observed]
			if usage == nil || usage.Requests == 0 {
				continue
			}
			for _, pattern := range patterns {
				if matchesDeclaredModel(pattern, observed, usage.Provider) {
					row.EffectiveModel = observed
					break
				}
			}
			if row.EffectiveModel != "" {
				break
			}
		}
		if row.EffectiveModel == "" {
			row.ReasonCode = modelMismatchReasonModelNotObserved
			if len(observedModels) == 0 {
				row.ReasonCode = modelMismatchReasonTokenUsageMissing
			}
		}
		summary.DeclaredSubagentModels = append(summary.DeclaredSubagentModels, row)
	}
}

func subagentObservedModels(summary *TokenUsageSummary) map[string]*ModelTokenUsage {
	if summary.agentModels != nil {
		return summary.agentModels
	}
	return summary.ByModel
}

func matchesDeclaredModel(pattern, observed, provider string) bool {
	pattern, _, _ = strings.Cut(strings.ToLower(pattern), "?")
	observed, _, _ = strings.Cut(strings.ToLower(observed), "?")
	normalize := func(value string) string {
		switch value {
		case "github", "copilot", "github-copilot":
			return "github-copilot"
		case "codex", "openai":
			return "openai"
		case "gemini", "google":
			return "google"
		default:
			return value
		}
	}
	if prefix, model, qualified := strings.Cut(observed, "/"); qualified && (provider == "" || normalize(prefix) == normalize(strings.ToLower(provider))) {
		if provider == "" {
			provider = prefix
		}
		observed = model
	}
	if prefix, model, qualified := strings.Cut(pattern, "/"); qualified {
		if normalize(prefix) != normalize(strings.ToLower(provider)) {
			return false
		}
		pattern = model
	}
	matched, err := path.Match(pattern, observed)
	if err == nil && matched {
		return true
	}
	normalized := normalizeEffectiveModelName(observed)
	if normalized == observed {
		return false
	}
	matched, err = path.Match(pattern, normalized)
	return err == nil && matched
}

func normalizeEffectiveModelName(model string) string {
	model = effectiveModelDateSuffix.ReplaceAllString(model, "")
	return claudeHyphenatedModelVersion.ReplaceAllString(model, "$1-$2.$3")
}

func generateSubagentModelFindings(summary *TokenUsageSummary) []AuditFinding {
	if summary == nil {
		return nil
	}
	var findings []AuditFinding
	for _, model := range summary.DeclaredSubagentModels {
		if model.ReasonCode != modelMismatchReasonModelNotObserved {
			continue
		}
		findings = append(findings, AuditFinding{
			Code:     AuditFindingSubagentModelNotObserved,
			Category: "tooling", Severity: "medium",
			Title:       "Declared Sub-agent Model Not Observed",
			Description: "No observed model requests match sub-agent " + model.AgentName + " model " + model.RequestedModel,
			Impact:      "The sub-agent may not have been invoked or its model was not honored. Model presence alone does not prove delegation.",
		})
	}
	return findings
}
