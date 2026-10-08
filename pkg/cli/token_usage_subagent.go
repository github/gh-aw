package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var tokenUsageSubagentLog = logger.New("cli:token_usage_subagent")

var subagentDispatchPattern = regexp.MustCompile(`^●\s+([A-Za-z0-9][A-Za-z0-9._ -]*?)\s*\((?:model:\s*)?([A-Za-z0-9][A-Za-z0-9._:-]*)\)`)

type subagentModelKey struct {
	agent    string
	model    string
	resolved string
}

func augmentSubagentModelAttribution(runDir string, summary *TokenUsageSummary) {
	if summary == nil {
		return
	}
	augmentDeclaredSubagentModels(runDir, summary)

	requests, actuals, found, err := readSessionSubagentModels(runDir)
	if err != nil {
		addTokenUsageWarning(summary, "failed to parse unified subagent information: "+err.Error())
		tokenUsageSubagentLog.Printf("failed to parse unified subagent information: %v", err)
	}
	if found {
		summary.SubagentModelRequests = requests
		summary.SubagentModelActuals = actuals
		summary.MismatchCount = 0
		return
	}

	requests = extractSubagentModelRequests(runDir)
	if len(requests) == 0 {
		tokenUsageSubagentLog.Print("no subagent model dispatch requests found, skipping attribution")
		return
	}
	augmentHeuristicSubagentModelAttribution(requests, summary)
}

func augmentHeuristicSubagentModelAttribution(requests []SubagentModelRequest, summary *TokenUsageSummary) {
	addTokenUsageWarning(summary, subagentStdioWarning)

	actuals, observedModels := collectSubagentModelActuals(summary)
	summary.SubagentModelActuals = actuals

	var fallbackEffectiveModel string
	if len(observedModels) == 1 {
		for model := range observedModels {
			fallbackEffectiveModel = model
		}
	}

	requestRows := make([]SubagentModelRequest, 0, len(requests))
	mismatchCount := 0
	for _, row := range requests {
		requested := row.RequestedModel
		if row.ResolvedModel != "" {
			requested = row.ResolvedModel
		}
		if _, ok := observedModels[requested]; ok {
			row.EffectiveModel = requested
		} else {
			row.EffectiveModel = fallbackEffectiveModel
			if len(observedModels) == 0 {
				row.ReasonCode = modelMismatchReasonTokenUsageMissing
			} else {
				row.ReasonCode = modelMismatchReasonModelNotObserved
			}
			mismatchCount += row.InvocationCount
		}
		requestRows = append(requestRows, row)
	}
	summary.SubagentModelRequests = requestRows
	summary.MismatchCount = mismatchCount
	tokenUsageSubagentLog.Printf("attributed %d subagent request(s), %d mismatch(es)", len(requestRows), mismatchCount)
}

func addTokenUsageWarning(summary *TokenUsageSummary, warning string) {
	if summary == nil || warning == "" {
		return
	}
	if slices.Contains(summary.Warnings, warning) {
		return
	}
	summary.Warnings = append(summary.Warnings, warning)
}

func extractSubagentModelRequests(runDir string) []SubagentModelRequest {
	agentStdioPath := findAgentStdioFile(runDir)
	if agentStdioPath == "" {
		tokenUsageSubagentLog.Printf("no agent stdio file found under %s", runDir)
		return nil
	}

	file, err := os.Open(agentStdioPath)
	if err != nil {
		tokenUsageSubagentLog.Printf("failed to open agent stdio file %s: %v", agentStdioPath, err)
		return nil
	}
	defer file.Close()

	counts := make(map[subagentModelKey]int)

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			countSubagentDispatchLine(counts, line)
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tokenUsageSubagentLog.Printf("failed to read agent stdio file %s: %v", agentStdioPath, readErr)
			return nil
		}
	}

	return subagentModelRequestRows(counts)
}

func subagentModelRequestRows(counts map[subagentModelKey]int) []SubagentModelRequest {
	rows := make([]SubagentModelRequest, 0, len(counts))
	for k, n := range counts {
		rows = append(rows, SubagentModelRequest{
			AgentName:       k.agent,
			RequestedModel:  k.model,
			ResolvedModel:   k.resolved,
			InvocationCount: n,
		})
	}
	slices.SortStableFunc(rows, func(a, b SubagentModelRequest) int {
		if a.AgentName != b.AgentName {
			if a.AgentName < b.AgentName {
				return -1
			}
			return 1
		}
		switch {
		case a.RequestedModel < b.RequestedModel:
			return -1
		case a.RequestedModel > b.RequestedModel:
			return 1
		default:
			return strings.Compare(a.ResolvedModel, b.ResolvedModel)
		}
	})
	return rows
}

func collectSubagentModelActuals(summary *TokenUsageSummary) ([]SubagentModelActual, map[string]string) {
	actuals := make([]SubagentModelActual, 0, len(summary.ByModel))
	observedModels := make(map[string]string, len(summary.ByModel))
	for model, usage := range subagentObservedModels(summary) {
		if usage == nil || model == "" || usage.Requests == 0 {
			continue
		}
		actuals = append(actuals, SubagentModelActual{
			Model: model, Provider: usage.Provider, Requests: usage.Requests,
		})
		observedModels[model] = usage.Provider
	}
	slices.SortStableFunc(actuals, func(a, b SubagentModelActual) int {
		if a.Requests > b.Requests {
			return -1
		}
		if a.Requests < b.Requests {
			return 1
		}
		return strings.Compare(a.Model, b.Model)
	})
	return actuals, observedModels
}

func countSubagentDispatchLine(counts map[subagentModelKey]int, line string) {
	var dispatch struct {
		Type      string `json:"type"`
		Agent     string `json:"agent"`
		Requested string `json:"requested_model"`
		Resolved  string `json:"resolved_model"`
	}
	if json.Unmarshal([]byte(line), &dispatch) == nil && dispatch.Type == "gh_aw_subagent_dispatch" {
		if dispatch.Agent != "" && dispatch.Requested != "" {
			counts[subagentModelKey{agent: dispatch.Agent, model: dispatch.Requested, resolved: dispatch.Resolved}]++
		}
		return
	}
	var agent, model string
	for index, match := range subagentDispatchPattern.FindStringSubmatch(line) {
		switch index {
		case 1:
			agent = strings.TrimSpace(match)
		case 2:
			model = strings.TrimSpace(match)
		}
	}
	if agent != "" && model != "" {
		counts[subagentModelKey{agent: agent, model: model}]++
	}
}
