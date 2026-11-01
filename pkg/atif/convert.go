package atif

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var convertLog = logger.New("atif:convert")

const MaxSessionBytes = 64 * 1024 * 1024

type event struct {
	Type           string                     `json:"type"`
	Data           map[string]json.RawMessage `json:"data"`
	AgentID        string                     `json:"agentId"`
	SessionID      json.RawMessage            `json:"sessionId"`
	SessionIDAlias json.RawMessage            `json:"session_id"`
	Timestamp      json.RawMessage            `json:"timestamp"`
	Provenance     struct {
		Component string `json:"component"`
		Phase     string `json:"phase"`
		Path      string `json:"path"`
	} `json:"provenance"`
}

type toolIdentity struct {
	id string
	eventScope
}

type sourceScope struct {
	component string
	phase     string
	path      string
}

type eventScope struct {
	sourceScope
	sessionID string
}

type converter struct {
	trajectory *Trajectory
	calls      map[toolIdentity][]int
	terminal   map[string]json.RawMessage
	terminals  map[eventScope]map[string]json.RawMessage
	engines    map[sourceScope]string
	headerSeen bool
}

// Convert reads one versioned unified session. It preserves source records in
// extras, including observations which cannot safely become ATIF tool calls.
func Convert(ctx context.Context, input io.Reader) (*Trajectory, error) {
	limited := &io.LimitedReader{R: input, N: MaxSessionBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), MaxSessionBytes+1)
	trajectory := &Trajectory{
		SchemaVersion: SchemaVersion,
		Agent:         Agent{Name: "gh-aw-unified", Version: "unknown"},
		Notes: "Converted from a gh-aw unified session. Source records and their order are retained in extra.unified_event and extra.line. " +
			"Tool observations are attached to their initiating step. Unmapped and subagent evidence remains in system-step extras. " +
			"Unknown agent versions are labeled unknown; empty tool arguments may indicate unavailable input. Structured non-text content is JSON-encoded, not exported as media assets.",
	}
	conversion := converter{
		trajectory: trajectory, calls: make(map[toolIdentity][]int),
		terminals: make(map[eventScope]map[string]json.RawMessage), engines: make(map[sourceScope]string),
	}
	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if limited.N <= 0 {
			return nil, errors.New("unified session exceeds the 64 MiB ATIF input limit")
		}
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		if err := conversion.consume(raw, line); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read unified session for ATIF: %w", err)
	}
	if limited.N <= 0 {
		return nil, errors.New("unified session exceeds the 64 MiB ATIF input limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !conversion.headerSeen {
		return nil, errors.New("unified session is missing its leading session.format header")
	}
	trajectory.FinalMetrics = finalMetrics(conversion.terminal, len(trajectory.Steps))
	if err := trajectory.Validate(); err != nil {
		return nil, err
	}
	convertLog.Printf("Converted unified session: steps=%d", len(trajectory.Steps))
	return trajectory, nil
}

func (c *converter) consume(raw []byte, line int) error {
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return fmt.Errorf("invalid unified session JSONL at line %d", line)
	}
	if !strings.Contains(ev.Type, ".") || ev.Data == nil {
		return fmt.Errorf("invalid unified session event at line %d: expected namespaced type and object data", line)
	}
	header := ev.Type == "session.format" && ev.Provenance.Component == "collector"
	if !c.headerSeen {
		if !header {
			return errors.New("unified session is missing its leading session.format header")
		}
		if string(ev.Data["version"]) != "1" {
			return errors.New("unsupported unified session file-format version")
		}
		c.trajectory.Extra = map[string]any{"unified_header": json.RawMessage(bytes.Clone(raw))}
		c.headerSeen = true
		convertLog.Print("Parsed unified session.format header")
		return nil
	}
	if header {
		return errors.New("unified session contains multiple collector format headers")
	}
	step := Step{
		StepID: len(c.trajectory.Steps) + 1, Source: "system",
		Extra: map[string]any{"unified_event": json.RawMessage(bytes.Clone(raw)), "line": line},
	}
	if timestamp := text(ev.Timestamp); timestamp != "" {
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			step.Timestamp = timestamp
		}
	}
	// Identified subagent events remain evidence, never primary-agent dialogue.
	if c.isPrimaryEvent(ev) {
		mapped, err := c.mapPrimaryEvent(ev, &step)
		if err != nil {
			return err
		}
		if mapped {
			return nil
		}
	}
	c.trajectory.Steps = append(c.trajectory.Steps, step)
	return nil
}

func (ev event) scope() eventScope {
	sessionID, ok := ev.Data["sessionId"]
	if !ok {
		sessionID = ev.SessionID
		if len(sessionID) == 0 {
			sessionID = ev.SessionIDAlias
		}
		if len(sessionID) == 0 {
			sessionID = ev.Data["session_id"]
		}
	}
	return eventScope{
		sourceScope: sourceScope{component: ev.Provenance.Component, phase: ev.Provenance.Phase, path: ev.Provenance.Path},
		sessionID:   text(sessionID),
	}
}

func (c *converter) isPrimaryEvent(ev event) bool {
	parent, explicit := ev.Data["parentToolUseId"]
	engine := text(ev.Data["sourceEngine"])
	if engine == "" {
		engine = c.engines[ev.scope().sourceScope]
	}
	if !explicit && engine == "copilot" {
		parent = ev.Data["parentToolCallId"]
	}
	if ev.AgentID != "" || text(ev.Data["agentId"]) != "" || text(parent) != "" {
		return false
	}
	return ev.Provenance.Component == "" || ev.Provenance.Component == "agent" || strings.HasPrefix(ev.Type, "prompt.")
}

func (c *converter) mapPrimaryEvent(ev event, step *Step) (bool, error) {
	switch ev.Type {
	case "session.init", "session.start":
		if name := text(ev.Data["sourceEngine"]); name != "" {
			c.engines[ev.scope().sourceScope] = name
			c.trajectory.Agent.Name = name
		}
		if model := text(ev.Data["model"]); model != "" {
			c.trajectory.Agent.ModelName = model
		}
		version := text(ev.Data["sourceEngineVersion"])
		if version == "" {
			version = text(ev.Data["cliVersion"])
		}
		if version != "" {
			c.trajectory.Agent.Version = version
		}
		if id := text(ev.Data["sessionId"]); id != "" {
			c.trajectory.SessionID = id
		}
		convertLog.Printf("Identified session agent: name=%s version=%s", c.trajectory.Agent.Name, c.trajectory.Agent.Version)
	case "prompt.system":
		step.Message = content(ev.Data["content"])
	case "prompt.user", "user.message":
		step.Source, step.Message = "user", content(ev.Data["content"])
	case "assistant.message", "assistant.refusal":
		step.Source, step.Message = "agent", content(ev.Data["content"])
		step.ModelName = text(ev.Data["model"])
	case "assistant.reasoning":
		step.Source, step.ReasoningContent = "agent", content(ev.Data["content"])
	case "tool.execution_start", "tool.execution_complete":
		return c.mapToolEvent(ev, step), nil
	case "session.result":
		scope := ev.scope()
		terminal, err := reconcileMetrics(c.terminals[scope], ev.Data)
		if err != nil {
			return false, fmt.Errorf("reconcile unified ATIF metrics: %w", err)
		}
		c.terminal = terminal
		c.terminals[scope] = c.terminal
	}
	return false, nil
}

func (c *converter) mapToolEvent(ev event, step *Step) bool {
	id := text(ev.Data["toolCallId"])
	key := toolIdentity{id: id, eventScope: ev.scope()}
	if ev.Type == "tool.execution_start" {
		name := text(ev.Data["toolName"])
		args, ok := arguments(ev.Data)
		if id != "" && name != "" && ok {
			step.Source = "agent"
			step.ToolCalls = []ToolCall{{ToolCallID: id, FunctionName: name, Arguments: args}}
			c.calls[key] = append(c.calls[key], len(c.trajectory.Steps))
		}
		return false
	}
	if indexes := c.calls[key]; len(indexes) == 1 {
		index := indexes[0]                  //nolint:uncheckedsliceindex // len(indexes) == 1
		target := &c.trajectory.Steps[index] //nolint:uncheckedsliceindex // indexes refer to previously appended steps
		if target.Observation == nil {
			target.Observation = &Observation{}
		}
		target.Observation.Results = append(target.Observation.Results, ObservationResult{
			SourceCallID: id, Content: toolOutput(ev.Data), Extra: step.Extra,
		})
		return true
	}
	convertLog.Printf("No unambiguous initiating step for tool observation: toolCallId=%s", id)
	return false
}

func text(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func content(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return string(raw)
}

func arguments(data map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	raw, found := data["input"]
	if !found {
		raw, found = data["parameters"]
	}
	if !found {
		return map[string]json.RawMessage{}, true
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return nil, false
	}
	return args, true
}

func toolOutput(data map[string]json.RawMessage) string {
	for _, field := range []string{"output", "result", "error"} {
		if value, ok := data[field]; ok {
			return content(value)
		}
	}
	return ""
}
