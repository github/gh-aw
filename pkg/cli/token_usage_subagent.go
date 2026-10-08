package cli

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var tokenUsageSubagentLog = logger.New("cli:token_usage_subagent")

var subagentDispatchPattern = regexp.MustCompile(`^●\s+([A-Za-z0-9][A-Za-z0-9._ -]*?)\s*\((?:model:\s*)?([A-Za-z0-9][A-Za-z0-9._:-]*)\)`)

func augmentSubagentModelAttribution(runDir string, summary *TokenUsageSummary) {
	if summary == nil {
		return
	}

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

	actuals := make([]SubagentModelActual, 0, len(summary.ByModel))
	observedModels := make(map[string]string, len(summary.ByModel))
	for model, usage := range summary.ByModel {
		if usage == nil || model == "" {
			continue
		}
		actuals = append(actuals, SubagentModelActual{
			Model:    model,
			Provider: usage.Provider,
			Requests: usage.Requests,
		})
		observedModels[model] = usage.Provider
	}
	sortSubagentModelActuals(actuals)
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
		if _, ok := observedModels[row.RequestedModel]; ok {
			row.EffectiveModel = row.RequestedModel
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

func sortSubagentModelActuals(actuals []SubagentModelActual) {
	slices.SortStableFunc(actuals, func(a, b SubagentModelActual) int {
		if a.Requests > b.Requests {
			return -1
		}
		if a.Requests < b.Requests {
			return 1
		}
		return strings.Compare(a.Model, b.Model)
	})
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

type subagentModelKey struct {
	agent string
	model string
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
		agentName, requestedModel := "", ""
		for index, match := range subagentDispatchPattern.FindStringSubmatch(line) {
			switch index {
			case 1:
				agentName = strings.TrimSpace(match)
			case 2:
				requestedModel = strings.TrimSpace(match)
			}
		}
		if agentName != "" && requestedModel != "" {
			counts[subagentModelKey{agent: agentName, model: requestedModel}]++
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
			return 0
		}
	})
	return rows
}
