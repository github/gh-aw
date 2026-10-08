package cli

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
)

type declaredSubagentModel struct {
	Name     string   `json:"name"`
	Model    string   `json:"model"`
	Patterns []string `json:"patterns"`
}

func readDeclaredSubagentModels(runDir string, summary *TokenUsageSummary) []declaredSubagentModel {
	infoPath := findAwInfoPath(runDir)
	if infoPath == "" {
		return nil
	}
	content, err := os.ReadFile(infoPath)
	if err != nil {
		addTokenUsageWarning(summary, "cannot read declared sub-agent models: "+err.Error())
		return nil
	}
	var info struct {
		Models []declaredSubagentModel `json:"sub_agent_models"`
	}
	if err := json.Unmarshal(content, &info); err != nil {
		addTokenUsageWarning(summary, "cannot parse declared sub-agent models: "+err.Error())
		return nil
	}
	return info.Models
}

func augmentDeclaredSubagentModels(runDir string, summary *TokenUsageSummary) {
	models := readDeclaredSubagentModels(runDir, summary)
	if len(models) == 0 {
		return
	}
	resolver := newModelIdentityResolver(runDir)
	observedModels := subagentObservedModels(summary)
	observedNames := make([]string, 0, len(observedModels))
	for name := range observedModels {
		observedNames = append(observedNames, name)
	}
	slices.Sort(observedNames)
	for _, model := range models {
		row, sessionObserved := declaredSessionModelRow(model, summary.SubagentModelRequests, resolver, observedNames)
		if !sessionObserved {
			row = declaredTrafficModelRow(model, observedModels, observedNames, resolver)
		}
		summary.DeclaredSubagentModels = append(summary.DeclaredSubagentModels, row)
	}
}

func declaredSessionModelRow(model declaredSubagentModel, requests []SubagentModelRequest, resolver *modelIdentityResolver, observedNames []string) (SubagentModelRequest, bool) {
	row := SubagentModelRequest{AgentName: model.Name, RequestedModel: model.Model}
	for _, request := range requests {
		if !strings.EqualFold(request.AgentName, model.Name) {
			continue
		}
		row.InvocationCount += request.InvocationCount
		row.CompletedCount += request.CompletedCount
		row.FailedCount += request.FailedCount
		row.IncompleteCount += request.IncompleteCount
		row.Effort = combineSubagentEffort(row.Effort, request.Effort)
		if row.Error == "" {
			row.Error = request.Error
		}
		if request.EffectiveModel == "" {
			continue
		}
		for _, served := range request.ServedModels {
			row.ServedModels = appendUnique(row.ServedModels, served)
		}
		row.ServedModels = appendUnique(row.ServedModels, request.EffectiveModel)
		if declaredModelMatches(model, request.EffectiveModel, "", resolver) {
			row.EffectiveModel = request.EffectiveModel
			row.ResolvedModel = firstNonEmptyModel(request.ResolvedModel, resolver.resolve(request.EffectiveModel, "", observedNames))
		}
	}
	if row.InvocationCount == 0 {
		return row, false
	}
	if row.EffectiveModel == "" {
		if row.FailedCount > 0 && row.CompletedCount == 0 {
			row.ReasonCode = modelMismatchReasonSubagentFailed
		} else {
			row.ReasonCode = modelMismatchReasonModelNotObserved
		}
	}
	return row, true
}

func combineSubagentEffort(current, next string) string {
	if current == "mixed" || next == "mixed" {
		return "mixed"
	}
	if current == "" {
		return next
	}
	if next != "" && current != next {
		return "mixed"
	}
	return current
}

func declaredTrafficModelRow(model declaredSubagentModel, observedModels map[string]*ModelTokenUsage, observedNames []string, resolver *modelIdentityResolver) SubagentModelRequest {
	row := SubagentModelRequest{AgentName: model.Name, RequestedModel: model.Model}
	for _, observed := range observedNames {
		usage := observedModels[observed]
		if usage == nil || usage.Requests == 0 {
			continue
		}
		if declaredModelMatches(model, observed, usage.Provider, resolver) {
			row.EffectiveModel = normalizeModelIdentity(observed)
			row.ResolvedModel = row.EffectiveModel
			row.ServedModels = []string{observed}
			break
		}
	}
	if row.EffectiveModel == "" {
		row.ReasonCode = modelMismatchReasonModelNotObserved
		if len(observedModels) == 0 {
			row.ReasonCode = modelMismatchReasonTokenUsageMissing
		}
	}
	return row
}

func declaredModelMatches(model declaredSubagentModel, observed, provider string, resolver *modelIdentityResolver) bool {
	patterns := model.Patterns
	if len(patterns) == 0 {
		patterns = []string{model.Model}
	}
	for _, pattern := range patterns {
		if resolver.matches(pattern, observed, provider) {
			return true
		}
	}
	return false
}

func subagentObservedModels(summary *TokenUsageSummary) map[string]*ModelTokenUsage {
	if summary.agentModels != nil {
		return summary.agentModels
	}
	return summary.ByModel
}

func matchesDeclaredModel(pattern, observed, provider string) bool {
	return newModelIdentityResolver("").matches(pattern, observed, provider)
}

func generateSubagentModelFindings(summary *TokenUsageSummary) []AuditFinding {
	if summary == nil {
		return nil
	}
	var findings []AuditFinding
	for _, model := range summary.SubagentModelRequests {
		if model.FailedCount == 0 {
			continue
		}
		description := "Sub-agent " + model.AgentName + " failed " + strconv.Itoa(model.FailedCount) + " invocation(s)"
		if model.Error != "" {
			description += ": " + model.Error
		}
		findings = append(findings, AuditFinding{
			Code: AuditFindingSubagentFailed, Category: "tooling", Severity: "high",
			Title: "Sub-agent Failed", Description: description,
			Impact: "The delegated task failed and may have reduced the completeness or correctness of the workflow result.",
		})
	}
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
