//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
)

// Sample log content for benchmarking
const (
	sampleClaudeLog = `[{"type":"session_created","timestamp":"2024-01-15T10:00:00.000Z"}]
[{"type":"message","timestamp":"2024-01-15T10:00:01.000Z","message":"Starting analysis"}]
[{"type":"tool_use","timestamp":"2024-01-15T10:00:02.000Z","tool":"github.issue_read"}]
[{"type":"tool_result","timestamp":"2024-01-15T10:00:03.000Z"}]
[{"type":"usage","timestamp":"2024-01-15T10:00:04.000Z","input_tokens":1000,"output_tokens":500}]
[{"type":"message","timestamp":"2024-01-15T10:00:05.000Z","message":"Analysis complete"}]
[{"type":"result","timestamp":"2024-01-15T10:00:06.000Z","total_input_tokens":1000,"total_output_tokens":500,"cost":0.015}]`

	sampleCopilotLog = `2024-01-15T10:00:00.123Z [INFO] Copilot started
2024-01-15T10:00:01.456Z [INFO] Processing request
2024-01-15T10:00:02.789Z [DEBUG] Tool call: github.issue_read
2024-01-15T10:00:03.012Z [DEBUG] Tool result received
2024-01-15T10:00:04.345Z [INFO] Token usage: 1500 total
2024-01-15T10:00:05.678Z [ERROR] Minor issue detected
2024-01-15T10:00:06.901Z [INFO] Request completed`

	sampleCodexLog = `] tool github.search_issues(...)
tool result: [{"id": 123, "title": "Issue 1"}]
] exec ls -la in /tmp
exec result: total 8
] tool github.issue_read(...)
tool result: {"id": 123, "body": "Issue content"}
] success in 2.5s`

	largeClaudeLog = sampleClaudeLog + "\n" + sampleClaudeLog + "\n" + sampleClaudeLog + "\n" + sampleClaudeLog + "\n" + sampleClaudeLog

	largeCopilotLog = sampleCopilotLog + "\n" + sampleCopilotLog + "\n" + sampleCopilotLog + "\n" + sampleCopilotLog + "\n" + sampleCopilotLog
)

// BenchmarkParseClaudeLog benchmarks Claude log parsing
func BenchmarkParseClaudeLog(b *testing.B) {
	engine := &workflow.ClaudeEngine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.ParseLogMetrics(sampleClaudeLog, false)
	}
}

// BenchmarkParseClaudeLog_Large benchmarks parsing large Claude log file
func BenchmarkParseClaudeLog_Large(b *testing.B) {
	engine := &workflow.ClaudeEngine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.ParseLogMetrics(largeClaudeLog, false)
	}
}

// BenchmarkParseCopilotLog benchmarks Copilot log parsing
func BenchmarkParseCopilotLog(b *testing.B) {
	engine := &workflow.CopilotEngine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.ParseLogMetrics(sampleCopilotLog, false)
	}
}

// BenchmarkParseCopilotLog_Large benchmarks parsing large Copilot log file
func BenchmarkParseCopilotLog_Large(b *testing.B) {
	engine := &workflow.CopilotEngine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.ParseLogMetrics(largeCopilotLog, false)
	}
}

// BenchmarkParseCodexLog benchmarks Codex log parsing
func BenchmarkParseCodexLog(b *testing.B) {
	engine := &workflow.CodexEngine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.ParseLogMetrics(sampleCodexLog, false)
	}
}

// BenchmarkParseCodexLog_WithErrors benchmarks Codex log parsing with errors
func BenchmarkParseCodexLog_WithErrors(b *testing.B) {
	logWithErrors := sampleCodexLog + `
] error: connection timeout
] warning: retry attempt
] error: max retries exceeded
] tool github.get_repository(...)
] success in 1.2s`

	engine := &workflow.CodexEngine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.ParseLogMetrics(logWithErrors, false)
	}
}

// BenchmarkAggregateWorkflowStats benchmarks log aggregation across multiple runs
func BenchmarkAggregateWorkflowStats(b *testing.B) {
	// Create sample workflow runs
	runs := []WorkflowRun{
		{
			DatabaseID:    12345,
			WorkflowName:  "test-workflow-1",
			Status:        "completed",
			Conclusion:    "success",
			TokenUsage:    1500,
			EstimatedCost: 0.015,
			Turns:         3,
			ErrorCount:    0,
			WarningCount:  1,
		},
		{
			DatabaseID:    12346,
			WorkflowName:  "test-workflow-2",
			Status:        "completed",
			Conclusion:    "failure",
			TokenUsage:    2500,
			EstimatedCost: 0.025,
			Turns:         5,
			ErrorCount:    2,
			WarningCount:  3,
		},
		{
			DatabaseID:    12347,
			WorkflowName:  "test-workflow-1",
			Status:        "completed",
			Conclusion:    "success",
			TokenUsage:    1800,
			EstimatedCost: 0.018,
			Turns:         4,
			ErrorCount:    0,
			WarningCount:  0,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate aggregation logic
		totalTokens := 0
		totalCost := 0.0
		totalTurns := 0
		totalErrors := 0
		totalWarnings := 0

		for _, run := range runs {
			totalTokens += run.TokenUsage
			totalCost += run.EstimatedCost
			totalTurns += run.Turns
			totalErrors += run.ErrorCount
			totalWarnings += run.WarningCount
		}

		_ = totalTokens
		_ = totalCost
		_ = totalTurns
		_ = totalErrors
		_ = totalWarnings
	}
}

// BenchmarkAggregateWorkflowStats_Large benchmarks aggregation with many runs
func BenchmarkAggregateWorkflowStats_Large(b *testing.B) {
	// Create 100 sample workflow runs
	runs := make([]WorkflowRun, 100)
	for i := 0; i < 100; i++ {
		runs[i] = WorkflowRun{
			DatabaseID:    int64(12345 + i),
			WorkflowName:  "test-workflow",
			Status:        "completed",
			Conclusion:    "success",
			TokenUsage:    1500 + i*10,
			EstimatedCost: 0.015 + float64(i)*0.001,
			Turns:         3 + i%5,
			ErrorCount:    i % 3,
			WarningCount:  i % 2,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		totalTokens := 0
		totalCost := 0.0
		totalTurns := 0
		totalErrors := 0
		totalWarnings := 0

		for _, run := range runs {
			totalTokens += run.TokenUsage
			totalCost += run.EstimatedCost
			totalTurns += run.Turns
			totalErrors += run.ErrorCount
			totalWarnings += run.WarningCount
		}

		_ = totalTokens
		_ = totalCost
		_ = totalTurns
		_ = totalErrors
		_ = totalWarnings
	}
}

// BenchmarkExtractJSONMetrics benchmarks JSON metrics extraction
func BenchmarkExtractJSONMetrics(b *testing.B) {
	jsonLine := `{"type":"usage","input_tokens":1000,"output_tokens":500,"cost":0.015}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = workflow.ExtractJSONMetrics(jsonLine, false)
	}
}

// BenchmarkExtractJSONMetrics_Complex benchmarks complex JSON metrics extraction
func BenchmarkExtractJSONMetrics_Complex(b *testing.B) {
	jsonLine := `{"type":"result","total_input_tokens":5000,"total_output_tokens":2500,"cost":0.075,"metadata":{"tool_calls":["github.issue_read","github.add_comment"],"duration_ms":1500}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = workflow.ExtractJSONMetrics(jsonLine, false)
	}
}
