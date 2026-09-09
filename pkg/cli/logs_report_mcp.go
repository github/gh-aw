package cli

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

// buildMCPFailuresSummary aggregates MCP failures across all runs
func buildMCPFailuresSummary(processedRuns []ProcessedRun) []MCPFailureSummary {
	reportLog.Printf("Building MCP failures summary from %d processed runs", len(processedRuns))
	result := aggregateSummaryItems(
		processedRuns,
		// getItems: extract MCP failures from each run
		func(pr ProcessedRun) []MCPFailureReport {
			return pr.MCPFailures
		},
		// getKey: use server name as the aggregation key
		func(failure MCPFailureReport) string {
			return failure.ServerName
		},
		// createSummary: create new summary for first occurrence
		func(failure MCPFailureReport) *MCPFailureSummary {
			return &MCPFailureSummary{
				ServerName: failure.ServerName,
				AggregatedSummaryBase: AggregatedSummaryBase{
					Count:     1,
					Workflows: []string{failure.WorkflowName},
					RunIDs:    []int64{failure.RunID},
				},
			}
		},
		// updateSummary: update existing summary with new occurrence
		func(summary *MCPFailureSummary, failure MCPFailureReport) {
			summary.Count++
			summary.Workflows = sliceutil.MergeUnique(summary.Workflows, failure.WorkflowName)
			summary.RunIDs = append(summary.RunIDs, failure.RunID)
		},
		// finalizeSummary: populate display fields for console rendering
		func(summary *MCPFailureSummary) {
			summary.WorkflowsDisplay = strings.Join(summary.Workflows, ", ")
		},
	)

	// Sort by count descending
	slices.SortFunc(result, func(a, b MCPFailureSummary) int {
		return cmp.Compare(b.Count, a.Count)
	})

	return result
}

func summarizeIntegrityFilterEvents(events []DifcFilteredEvent) *IntegrityFilterSummary {
	if len(events) == 0 {
		return nil
	}
	summary := &IntegrityFilterSummary{
		TotalFiltered:        len(events),
		FilteredServerCounts: make(map[string]int),
		FilteredToolCounts:   make(map[string]int),
		FilteredReasonCounts: make(map[string]int),
	}
	for _, event := range events {
		if event.ServerID != "" {
			summary.FilteredServerCounts[event.ServerID]++
		}
		if event.ToolName != "" {
			summary.FilteredToolCounts[event.ToolName]++
		}
		if event.Reason != "" {
			summary.FilteredReasonCounts[event.Reason]++
		}
	}
	return summary
}

func mergeIntegrityFilterCounts(destination map[string]int, source map[string]int) {
	for key, count := range source {
		destination[key] += count
	}
}

func mergeRunIntegrityFilterSummary(destination **IntegrityFilterSummary, usage *MCPToolUsageData) {
	runIntegrity := summarizeIntegrityFilterEvents(usage.FilteredEvents)
	if runIntegrity == nil {
		runIntegrity = usage.Integrity
	}
	if runIntegrity == nil || runIntegrity.TotalFiltered == 0 {
		return
	}
	if *destination == nil {
		*destination = &IntegrityFilterSummary{
			FilteredServerCounts: make(map[string]int),
			FilteredToolCounts:   make(map[string]int),
			FilteredReasonCounts: make(map[string]int),
		}
	}
	(*destination).TotalFiltered += runIntegrity.TotalFiltered
	(*destination).RunsWithFilteredEvents++
	mergeIntegrityFilterCounts((*destination).FilteredServerCounts, runIntegrity.FilteredServerCounts)
	mergeIntegrityFilterCounts((*destination).FilteredToolCounts, runIntegrity.FilteredToolCounts)
	mergeIntegrityFilterCounts((*destination).FilteredReasonCounts, runIntegrity.FilteredReasonCounts)
}

func mergeMCPToolSummaries(destination map[string]*MCPToolSummary, summaries []MCPToolSummary) {
	for _, summary := range summaries {
		summary.syncFieldsFromBase()
		summary.syncBaseFromFields()
		key := summary.ServerName + ":" + summary.ToolName

		existing, exists := destination[key]
		if !exists {
			newSummary := summary
			newSummary.syncBaseFromFields()
			destination[key] = &newSummary
			continue
		}

		previousCallCount := existing.CallCount
		existing.CallCount += summary.CallCount
		existing.TotalInputSize += summary.TotalInputSize
		existing.TotalOutputSize += summary.TotalOutputSize
		existing.MaxInputSize = max(existing.MaxInputSize, summary.MaxInputSize)
		existing.MaxOutputSize = max(existing.MaxOutputSize, summary.MaxOutputSize)
		existing.ErrorCount += summary.ErrorCount
		if summary.AvgDuration != "" && existing.CallCount > 0 {
			existingDuration := parseDurationString(existing.AvgDuration)
			newDuration := parseDurationString(summary.AvgDuration)
			weightedDuration := (existingDuration*time.Duration(previousCallCount) + newDuration*time.Duration(summary.CallCount)) / time.Duration(existing.CallCount)
			existing.AvgDuration = timeutil.FormatDuration(weightedDuration)
		}
		if summary.MaxDuration != "" && parseDurationString(summary.MaxDuration) > parseDurationString(existing.MaxDuration) {
			existing.MaxDuration = summary.MaxDuration
		}
		existing.syncBaseFromFields()
	}
}

func mergeMCPServerStats(destination map[string]*MCPServerStats, servers []MCPServerStats) {
	for _, server := range servers {
		existing, exists := destination[server.ServerName]
		if !exists {
			newStats := server
			destination[server.ServerName] = &newStats
			continue
		}

		previousRequestCount := existing.RequestCount
		existing.RequestCount += server.RequestCount
		existing.ToolCallCount += server.ToolCallCount
		existing.TotalInputSize += server.TotalInputSize
		existing.TotalOutputSize += server.TotalOutputSize
		existing.ErrorCount += server.ErrorCount
		if server.AvgDuration != "" && existing.RequestCount > 0 {
			existingDuration := parseDurationString(existing.AvgDuration)
			newDuration := parseDurationString(server.AvgDuration)
			weightedDuration := (existingDuration*time.Duration(previousRequestCount) + newDuration*time.Duration(server.RequestCount)) / time.Duration(existing.RequestCount)
			existing.AvgDuration = timeutil.FormatDuration(weightedDuration)
		}
	}
}

func sortedMCPToolSummaries(summaryMap map[string]*MCPToolSummary) []MCPToolSummary {
	summaries := make([]MCPToolSummary, 0, len(summaryMap))
	for _, summary := range summaryMap {
		summaries = append(summaries, *summary)
	}
	slices.SortFunc(summaries, func(a, b MCPToolSummary) int {
		return cmp.Or(cmp.Compare(a.ServerName, b.ServerName), cmp.Compare(a.ToolName, b.ToolName))
	})
	return summaries
}

func sortedMCPServerStats(serverMap map[string]*MCPServerStats) []MCPServerStats {
	servers := make([]MCPServerStats, 0, len(serverMap))
	for _, stats := range serverMap {
		servers = append(servers, *stats)
	}
	slices.SortFunc(servers, func(a, b MCPServerStats) int {
		return cmp.Compare(a.ServerName, b.ServerName)
	})
	return servers
}

// buildMCPToolUsageSummary aggregates MCP tool usage data across all runs
func buildMCPToolUsageSummary(processedRuns []ProcessedRun) *MCPToolUsageSummary {
	reportLog.Printf("Building MCP tool usage summary from %d processed runs", len(processedRuns))

	// Maps for aggregating data
	toolSummaryMap := make(map[string]*MCPToolSummary) // Key: serverName:toolName
	serverStatsMap := make(map[string]*MCPServerStats) // Key: serverName
	var allToolCalls []MCPToolCall
	var allFilteredEvents []DifcFilteredEvent
	var integrity *IntegrityFilterSummary

	// Aggregate data from all runs
	for _, pr := range processedRuns {
		if pr.MCPToolUsage == nil {
			continue
		}

		// Aggregate tool calls
		allToolCalls = append(allToolCalls, pr.MCPToolUsage.ToolCalls...)

		// Aggregate DIFC filtered events
		allFilteredEvents = append(allFilteredEvents, pr.MCPToolUsage.FilteredEvents...)
		mergeRunIntegrityFilterSummary(&integrity, pr.MCPToolUsage)
		mergeMCPToolSummaries(toolSummaryMap, pr.MCPToolUsage.Summary)
		mergeMCPServerStats(serverStatsMap, pr.MCPToolUsage.Servers)
	}

	// Return nil if no MCP tool usage data was found
	if len(toolSummaryMap) == 0 && len(serverStatsMap) == 0 && len(allToolCalls) == 0 && len(allFilteredEvents) == 0 && integrity == nil {
		return nil
	}

	summaries := sortedMCPToolSummaries(toolSummaryMap)
	servers := sortedMCPServerStats(serverStatsMap)

	reportLog.Printf("Built MCP tool usage summary: %d tool summaries, %d servers, %d total tool calls, %d DIFC filtered events",
		len(summaries), len(servers), len(allToolCalls), len(allFilteredEvents))

	return &MCPToolUsageSummary{
		Summary:        summaries,
		Servers:        servers,
		ToolCalls:      allToolCalls,
		FilteredEvents: allFilteredEvents,
		Integrity:      integrity,
	}
}
