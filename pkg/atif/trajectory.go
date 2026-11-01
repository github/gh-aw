// Package atif converts unified gh-aw session JSONL into ATIF-v1.8 trajectories.
package atif

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var trajectoryLog = logger.New("atif:trajectory")

const SchemaVersion = "ATIF-v1.8"

// Trajectory is the ATIF document, with gh-aw evidence in schema-approved extras.
type Trajectory struct {
	SchemaVersion string         `json:"schema_version"`
	SessionID     string         `json:"session_id,omitempty"`
	Agent         Agent          `json:"agent"`
	Steps         []Step         `json:"steps"`
	Notes         string         `json:"notes,omitempty"`
	FinalMetrics  *FinalMetrics  `json:"final_metrics,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
}

type Agent struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	ModelName string `json:"model_name,omitempty"`
}

type Step struct {
	StepID           int            `json:"step_id"`
	Timestamp        string         `json:"timestamp,omitempty"`
	Source           string         `json:"source"`
	Message          string         `json:"message"`
	ModelName        string         `json:"model_name,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	Observation      *Observation   `json:"observation,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type ToolCall struct {
	ToolCallID   string                     `json:"tool_call_id"`
	FunctionName string                     `json:"function_name"`
	Arguments    map[string]json.RawMessage `json:"arguments"`
}

type Observation struct {
	Results []ObservationResult `json:"results"`
}

type ObservationResult struct {
	SourceCallID string         `json:"source_call_id,omitempty"`
	Content      string         `json:"content"`
	Extra        map[string]any `json:"extra,omitempty"`
}

type FinalMetrics struct {
	PromptTokens     *int64         `json:"total_prompt_tokens,omitempty"`
	CompletionTokens *int64         `json:"total_completion_tokens,omitempty"`
	CachedTokens     *int64         `json:"total_cached_tokens,omitempty"`
	CostUSD          *float64       `json:"total_cost_usd,omitempty"`
	TotalSteps       int            `json:"total_steps"`
	Extra            map[string]any `json:"extra,omitempty"`
}

// Validate enforces the emitted ATIF subset, including same-step tool references.
func (t *Trajectory) Validate() error {
	if t == nil || t.SchemaVersion != SchemaVersion || t.Agent.Name == "" || t.Agent.Version == "" || len(t.Steps) == 0 {
		trajectoryLog.Print("Rejecting ATIF trajectory: missing schema version, agent identity, or steps")
		return errors.New("ATIF requires a supported schema, agent name/version, and at least one step")
	}
	for i, step := range t.Steps {
		if step.StepID != i+1 {
			return fmt.Errorf("ATIF step %d is not sequential", i+1)
		}
		if step.Source != "agent" && step.Source != "system" && step.Source != "user" {
			return fmt.Errorf("invalid ATIF source at step %d", step.StepID)
		}
		if step.Source != "agent" && (len(step.ToolCalls) != 0 || step.ReasoningContent != "" || step.ModelName != "" || step.Observation != nil) {
			return fmt.Errorf("ATIF agent-only fields on step %d", step.StepID)
		}
		if step.Timestamp != "" {
			if _, err := time.Parse(time.RFC3339Nano, step.Timestamp); err != nil {
				return fmt.Errorf("invalid ATIF timestamp at step %d", step.StepID)
			}
		}
		calls := make(map[string]struct{})
		for _, call := range step.ToolCalls {
			_, duplicate := calls[call.ToolCallID]
			if call.ToolCallID == "" || call.FunctionName == "" || call.Arguments == nil || duplicate {
				return fmt.Errorf("invalid ATIF tool call at step %d", step.StepID)
			}
			calls[call.ToolCallID] = struct{}{}
		}
		if step.Observation != nil {
			for _, result := range step.Observation.Results {
				if _, found := calls[result.SourceCallID]; result.SourceCallID != "" && !found {
					return fmt.Errorf("unmatched ATIF observation at step %d", step.StepID)
				}
			}
		}
	}
	trajectoryLog.Printf("Validated ATIF trajectory: steps=%d", len(t.Steps))
	return nil
}

// Encode validates before writing formatted JSON with a trailing newline.
func Encode(w io.Writer, trajectory *Trajectory) error {
	if err := trajectory.Validate(); err != nil {
		return err
	}
	trajectoryLog.Printf("Encoding ATIF trajectory: steps=%d", len(trajectory.Steps))
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(trajectory)
}
