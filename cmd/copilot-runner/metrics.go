// This file provides metrics collection for the copilot-runner binary.
//
// The runner collects metrics from SDK session events during execution,
// including token usage, tool calls, turn counts, and timing information.
// These metrics are output as structured JSON for consumption by gh-aw's
// log parsing pipeline.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RunnerOutput represents the structured JSON output from the runner.
type RunnerOutput struct {
	Success  bool          `json:"success"`
	Response string        `json:"response,omitempty"`
	Metrics  RunnerMetrics `json:"metrics"`
	Errors   []string      `json:"errors,omitempty"`
}

// RunnerMetrics contains metrics collected during execution.
type RunnerMetrics struct {
	TokenUsage    int              `json:"token_usage"`
	Turns         int              `json:"turns"`
	ToolCalls     []RunnerToolCall `json:"tool_calls"`
	ToolSequences [][]string       `json:"tool_sequences"`
	EstimatedCost float64          `json:"estimated_cost"`
	Duration      int              `json:"duration_seconds"`
}

// RunnerToolCall represents a tool call metric.
type RunnerToolCall struct {
	Name          string `json:"name"`
	Count         int    `json:"count"`
	MaxInputSize  int    `json:"max_input_size"`
	MaxOutputSize int    `json:"max_output_size"`
}

// MetricsCollector accumulates metrics from SDK events.
type MetricsCollector struct {
	mu            sync.Mutex
	startTime     time.Time
	tokenUsage    int
	turns         int
	toolCallMap   map[string]*RunnerToolCall
	toolSequence  []string
	toolSequences [][]string
	errors        []string
}

// NewMetricsCollector creates a new MetricsCollector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime:   time.Now(),
		toolCallMap: make(map[string]*RunnerToolCall),
	}
}

// RecordTokenUsage adds token usage from an event.
func (m *MetricsCollector) RecordTokenUsage(inputTokens, outputTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenUsage += inputTokens + outputTokens
}

// RecordTurnEnd records the end of a conversation turn.
func (m *MetricsCollector) RecordTurnEnd() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++

	// Save current tool sequence and start a new one
	if len(m.toolSequence) > 0 {
		m.toolSequences = append(m.toolSequences, m.toolSequence)
		m.toolSequence = nil
	}
}

// RecordToolCall records a tool invocation.
func (m *MetricsCollector) RecordToolCall(toolName string, inputSize int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.toolSequence = append(m.toolSequence, toolName)

	if tc, exists := m.toolCallMap[toolName]; exists {
		tc.Count++
		if inputSize > tc.MaxInputSize {
			tc.MaxInputSize = inputSize
		}
	} else {
		m.toolCallMap[toolName] = &RunnerToolCall{
			Name:         toolName,
			Count:        1,
			MaxInputSize: inputSize,
		}
	}
}

// RecordToolOutput records the output size for a tool call.
func (m *MetricsCollector) RecordToolOutput(toolName string, outputSize int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tc, exists := m.toolCallMap[toolName]; exists {
		if outputSize > tc.MaxOutputSize {
			tc.MaxOutputSize = outputSize
		}
	}
}

// RecordError records an error encountered during execution.
func (m *MetricsCollector) RecordError(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err)
}

// BuildOutput creates the final RunnerOutput with all collected metrics.
func (m *MetricsCollector) BuildOutput(success bool, response string) RunnerOutput {
	m.mu.Lock()
	defer m.mu.Unlock()

	duration := int(time.Since(m.startTime).Seconds())

	// Finalize tool sequences
	if len(m.toolSequence) > 0 {
		m.toolSequences = append(m.toolSequences, m.toolSequence)
	}

	// Convert tool call map to slice sorted by tool name for deterministic output
	var toolCalls []RunnerToolCall
	for _, tc := range m.toolCallMap {
		toolCalls = append(toolCalls, *tc)
	}
	sort.Slice(toolCalls, func(i, j int) bool {
		return toolCalls[i].Name < toolCalls[j].Name
	})

	return RunnerOutput{
		Success:  success,
		Response: response,
		Metrics: RunnerMetrics{
			TokenUsage:    m.tokenUsage,
			Turns:         m.turns,
			ToolCalls:     toolCalls,
			ToolSequences: m.toolSequences,
			Duration:      duration,
		},
		Errors: m.errors,
	}
}

// WriteOutput writes the runner output to a JSON file and prints the marker to stdout.
func WriteOutput(output RunnerOutput, logDir string) error {
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	// Write to file
	outputPath := filepath.Join(logDir, "runner-output.json")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	// Print marker to stdout for log parsing
	// Use compact (non-indented) JSON for the marker line so that parseRunnerOutput
	// can reliably extract it by reading a single line.
	compactData, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal compact output: %w", err)
	}
	fmt.Printf("COPILOT_RUNNER_OUTPUT:%s\n", string(compactData))

	return nil
}
