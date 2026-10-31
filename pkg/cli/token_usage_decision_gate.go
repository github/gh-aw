package cli

import "strings"

const maxDecisionGateQuestionTokens = int64(1_000_000_000)

type decisionGateUsageRecord struct {
	provider string
	model    string
	usage    *DecisionGateUsage
	duration int64
}

func decisionGateTokenUsageSummary(runDir string) *TokenUsageSummary {
	data := extractDecisionGateData(runDir)
	if data == nil {
		return nil
	}

	records := decisionGateUsageRecords(data)
	if len(records) == 0 {
		return nil
	}

	summary := &TokenUsageSummary{
		ByModel:     make(map[string]*ModelTokenUsage),
		agentModels: make(map[string]*ModelTokenUsage),
	}
	for _, record := range records {
		appendDecisionGateUsage(summary, record)
	}
	if summary.TotalRequests == 0 {
		return nil
	}
	return summary
}

func decisionGateUsageRecords(data *DecisionGateData) []decisionGateUsageRecord {
	records := make([]decisionGateUsageRecord, 0, len(data.Questions))
	hasQuestionUsage := false
	for _, question := range data.Questions {
		if !question.Evaluated || question.Usage == nil {
			continue
		}
		hasQuestionUsage = true
		records = append(records, decisionGateUsageRecord{
			provider: question.Provider,
			model:    question.ResolvedModel,
			usage:    question.Usage,
			duration: question.DurationMS,
		})
	}
	if !hasQuestionUsage && data.Usage != nil {
		records = append(records, decisionGateUsageRecord{
			provider: data.Provider,
			model:    data.ResolvedModel,
			usage:    data.Usage,
			duration: data.DurationMS,
		})
	}
	return records
}

func appendDecisionGateUsage(summary *TokenUsageSummary, record decisionGateUsageRecord) {
	if !validDecisionGateUsage(record.usage) {
		return
	}
	provider, model := decisionGateUsageModel(record.provider, record.model)
	modelUsage := &ModelTokenUsage{Provider: provider}
	if model != "" {
		modelUsage = summary.ByModel[model]
		if modelUsage == nil {
			modelUsage = &ModelTokenUsage{Provider: provider}
			summary.ByModel[model] = modelUsage
		}
	}

	inputTokens := int(record.usage.InputTokens)
	outputTokens := int(record.usage.OutputTokens)
	modelUsage.InputTokens += inputTokens
	modelUsage.OutputTokens += outputTokens
	modelUsage.Requests++
	if record.duration > 0 && record.duration <= int64(^uint(0)>>1) {
		duration := int(record.duration)
		modelUsage.DurationMs += duration
		summary.TotalDurationMs += duration
	}
	summary.TotalInputTokens += inputTokens
	summary.TotalOutputTokens += outputTokens
	summary.TotalRequests++

	if model != "" {
		if _, found := findModelPricing(provider, model); found {
			aic := computeModelInferenceAIC(provider, model, inputTokens, outputTokens, 0, 0, 0)
			modelUsage.AIC += aic
			summary.TotalAIC += aic
			summary.AICFound = true
		} else {
			addTokenUsageWarning(summary, "activation usage: model pricing unavailable; activation AIC may be incomplete")
		}
	} else {
		addTokenUsageWarning(summary, "activation usage: model metadata unavailable; activation AIC may be incomplete")
	}
}

func applyDecisionGateTokenUsage(summary *TokenUsageSummary, runDir string) {
	activation := decisionGateTokenUsageSummary(runDir)
	if activation == nil {
		return
	}
	if summary.ByModel == nil {
		summary.ByModel = make(map[string]*ModelTokenUsage)
	}
	mergeTokenUsageSummary(summary, activation)
}

func validDecisionGateUsage(usage *DecisionGateUsage) bool {
	if usage == nil || usage.InputTokens < 0 || usage.OutputTokens < 0 ||
		usage.InputTokens > maxDecisionGateQuestionTokens || usage.OutputTokens > maxDecisionGateQuestionTokens ||
		usage.TotalTokens < 0 || usage.TotalTokens < usage.InputTokens+usage.OutputTokens {
		return false
	}
	return true
}

func decisionGateUsageModel(provider, resolvedModel string) (string, string) {
	provider = strings.TrimSpace(provider)
	resolvedModel = strings.TrimSpace(resolvedModel)
	if modelProvider, model, ok := strings.Cut(resolvedModel, "/"); ok && model != "" {
		if provider == "" {
			provider = modelProvider
		}
		if strings.EqualFold(provider, modelProvider) {
			resolvedModel = model
		}
	}
	return provider, resolvedModel
}
