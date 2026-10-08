package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type sessionSubagentEvent struct {
	unifiedDetectionEvent
	AgentID    string          `json:"agentId"`
	Timestamp  json.RawMessage `json:"timestamp"`
	Provenance struct {
		Component     string `json:"component"`
		Phase         string `json:"phase"`
		Path          string `json:"path"`
		TimestampUnit string `json:"timestampUnit"`
		Native        struct {
			Path string `json:"path"`
		} `json:"native"`
	} `json:"provenance"`
}

type sessionSubagentData struct {
	SourceEngine     string `json:"sourceEngine"`
	SessionID        string `json:"sessionId"`
	ToolCallID       string `json:"toolCallId"`
	AgentName        string `json:"agentName"`
	AgentDisplayName string `json:"agentDisplayName"`
	Model            string `json:"model"`
	NewModel         string `json:"newModel"`
	AgentMetrics     map[string]struct {
		AgentName        string `json:"agentName"`
		AgentDisplayName string `json:"agentDisplayName"`
		ModelMetrics     map[string]struct {
			Requests struct {
				Count *int `json:"count"`
			} `json:"requests"`
		} `json:"modelMetrics"`
	} `json:"agentMetrics"`
}

// Unified conclusion evidence precedes the canonical bootstrap trace. Neither
// requires downloading the original engine logs to identify subagents.
func readSessionSubagentModels(runDir string) ([]SubagentModelRequest, []SubagentModelActual, bool, error) {
	var diagnostics error
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl", "agent-session.jsonl"} {
		path := filepath.Join(runDir, filepath.FromSlash(relative))
		if err := validateSubagentSessionSource(path); err != nil {
			diagnostics = errors.Join(diagnostics, err)
			continue
		}
		requests, actuals, found, parseErr := parseSessionSubagentFile(path, relative != "agent-session.jsonl")
		if errors.Is(parseErr, os.ErrNotExist) {
			continue
		}
		if parseErr != nil {
			diagnostics = errors.Join(diagnostics, fmt.Errorf("%s: %w", relative, parseErr))
			continue
		}
		if found {
			return requests, actuals, true, diagnostics
		}
	}
	return nil, nil, false, diagnostics
}

func validateSubagentSessionSource(path string) error {
	for _, source := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("session source is a symbolic link: %s", source)
		}
	}
	return nil
}

func parseSessionSubagentFile(path string, unified bool) (requests []SubagentModelRequest, actuals []SubagentModelActual, found bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close session: %w", closeErr))
		}
	}()
	return parseSessionSubagentModels(file, unified)
}

type subagentSessionModels struct {
	agents       map[string]*SubagentModelRequest
	actualCounts map[string]map[string]int
	found        bool
}

func parseSessionSubagentModels(input io.Reader, unified bool) ([]SubagentModelRequest, []SubagentModelActual, bool, error) {
	reader := bufio.NewReader(input)
	headerSeen := !unified
	sessions := subagentSessionSelection{scopes: make(map[string]*subagentSessionModels), sources: make(map[string]string)}
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return nil, nil, false, fmt.Errorf("oversized session record on line %d", lineNumber)
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var event sessionSubagentEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, nil, false, fmt.Errorf("invalid session event on line %d: %w", lineNumber, err)
			}
			if !headerSeen {
				metadata := event.unifiedDetectionEvent
				metadata.Provenance.Component = event.Provenance.Component
				if err := validateUnifiedDetectionHeader(metadata); err != nil {
					return nil, nil, false, err
				}
				headerSeen = true
			} else if unified && event.Type == "session.format" && event.Provenance.Component == "collector" {
				return nil, nil, false, errors.New("session contains multiple collector format headers")
			} else if !unified || (event.Provenance.Component == "agent" && event.Provenance.Phase == "agent") {
				if err := sessions.observe(event); err != nil {
					return nil, nil, false, fmt.Errorf("invalid %s on line %d: %w", event.Type, lineNumber, err)
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, nil, false, fmt.Errorf("failed to read session: %w", readErr)
			}
			break
		}
	}
	if !headerSeen {
		return nil, nil, false, errors.New("session is missing its leading session.format header")
	}
	models := sessions.scopes[sessions.final]
	if models == nil {
		return nil, nil, false, nil
	}
	requests, actuals := models.rows()
	return requests, actuals, models.found, nil
}

type subagentSessionSelection struct {
	scopes    map[string]*subagentSessionModels
	sources   map[string]string
	final     string
	finalTime time.Time
}

func (sessions *subagentSessionSelection) observe(event sessionSubagentEvent) error {
	source := event.Provenance.Path
	if event.Provenance.Native.Path != "" {
		source = event.Provenance.Native.Path
	}
	scope := sessions.sources[source]
	if scope == "" {
		scope = source
	}
	if (event.Type == "session.init" || event.Type == "session.start") && event.AgentID == "" {
		var data sessionSubagentData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return err
		}
		if data.SessionID != "" {
			scope = source + ":" + data.SessionID
			sessions.sources[source] = scope
		}
		start, err := subagentSessionStart(event)
		if err != nil {
			return err
		}
		if sessions.finalTime.IsZero() || (!start.IsZero() && !start.Before(sessions.finalTime)) {
			sessions.final, sessions.finalTime = scope, start
		}
	}
	models := sessions.scopes[scope]
	if models == nil {
		models = &subagentSessionModels{agents: make(map[string]*SubagentModelRequest), actualCounts: make(map[string]map[string]int)}
		sessions.scopes[scope] = models
		if len(sessions.scopes) == 1 {
			sessions.final = scope
		}
	}
	return models.observe(event)
}

func subagentSessionStart(event sessionSubagentEvent) (time.Time, error) {
	if len(event.Timestamp) == 0 || bytes.Equal(event.Timestamp, []byte("null")) {
		return time.Time{}, nil
	}
	if bytes.HasPrefix(event.Timestamp, []byte(`"`)) {
		var timestamp string
		if err := json.Unmarshal(event.Timestamp, &timestamp); err != nil {
			return time.Time{}, err
		}
		if timestamp == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.RFC3339Nano, timestamp)
	}
	var timestamp float64
	if err := json.Unmarshal(event.Timestamp, &timestamp); err != nil {
		return time.Time{}, err
	}
	if event.Provenance.TimestampUnit == "seconds" {
		timestamp *= 1000
	}
	return time.UnixMilli(int64(timestamp)), nil
}

func (models *subagentSessionModels) observe(event sessionSubagentEvent) error {
	switch event.Type {
	case "session.start", "session.init", "subagent.started", "subagent.configured", "subagent.completed", "subagent.failed", "session.model_change", "session.result", "session.shutdown":
	default:
		return nil
	}
	var data sessionSubagentData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return err
	}
	if event.Type == "session.init" && data.SourceEngine == "copilot" {
		if data.SessionID == "" && event.AgentID == "" {
			models.agents = make(map[string]*SubagentModelRequest)
			models.actualCounts = make(map[string]map[string]int)
		}
		models.found = true
	}
	if event.Type == "session.start" {
		models.found = true
	}
	if err := models.observeMetrics(data); err != nil {
		return err
	}
	if strings.HasPrefix(event.Type, "subagent.") || (event.Type == "session.model_change" && event.AgentID != "") {
		return models.observeLifecycle(event, data)
	}
	return nil
}

func (models *subagentSessionModels) observeMetrics(data sessionSubagentData) error {
	if data.AgentMetrics != nil {
		models.found = true
		models.actualCounts = make(map[string]map[string]int)
	}
	for agentID, metric := range data.AgentMetrics {
		if agentID == "main" {
			continue
		}
		if models.agents[agentID] == nil && metric.AgentName != "" {
			name := metric.AgentDisplayName
			if name == "" {
				name = metric.AgentName
			}
			models.agents[agentID] = &SubagentModelRequest{AgentName: name, InvocationCount: 1}
		}
		counts := make(map[string]int)
		for model, usage := range metric.ModelMetrics {
			if usage.Requests.Count == nil {
				continue
			}
			if *usage.Requests.Count < 0 {
				return errors.New("negative agent request count")
			}
			counts[model] = *usage.Requests.Count
		}
		models.actualCounts[agentID] = counts
	}
	return nil
}

func (models *subagentSessionModels) observeLifecycle(event sessionSubagentEvent, data sessionSubagentData) error {
	models.found = true
	identity := event.AgentID
	if identity == "" {
		identity = data.ToolCallID
	}
	if identity == "" {
		return errors.New("missing agent or tool call identity")
	}
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{InvocationCount: 1}
		models.agents[identity] = row
	}
	if data.AgentDisplayName != "" {
		row.AgentName = data.AgentDisplayName
	} else if row.AgentName == "" && data.AgentName != "" {
		row.AgentName = data.AgentName
	}
	if event.Type == "subagent.started" {
		row.RequestedModel = data.Model
	}
	if data.Model != "" {
		row.EffectiveModel = data.Model
	} else if event.Type == "session.model_change" && data.NewModel != "" {
		row.EffectiveModel = data.NewModel
	}
	return nil
}

func (models *subagentSessionModels) rows() ([]SubagentModelRequest, []SubagentModelActual) {
	grouped := make(map[subagentModelKey]SubagentModelRequest)
	for _, row := range models.agents {
		if row.AgentName != "" {
			key := subagentModelKey{agent: row.AgentName, model: row.RequestedModel}
			combined := grouped[key]
			if combined.InvocationCount == 0 {
				combined = *row
			} else {
				combined.InvocationCount += row.InvocationCount
				if combined.EffectiveModel != row.EffectiveModel {
					combined.EffectiveModel = ""
				}
			}
			grouped[key] = combined
		}
	}
	requests := make([]SubagentModelRequest, 0, len(models.agents))
	for _, row := range grouped {
		requests = append(requests, row)
	}
	slices.SortFunc(requests, func(a, b SubagentModelRequest) int {
		if order := strings.Compare(a.AgentName, b.AgentName); order != 0 {
			return order
		}
		if order := strings.Compare(a.RequestedModel, b.RequestedModel); order != 0 {
			return order
		}
		return strings.Compare(a.EffectiveModel, b.EffectiveModel)
	})
	counts := make(map[string]int)
	for _, byModel := range models.actualCounts {
		for model, count := range byModel {
			counts[model] += count
		}
	}
	actuals := make([]SubagentModelActual, 0, len(counts))
	for model, count := range counts {
		actuals = append(actuals, SubagentModelActual{Model: model, Requests: count})
	}
	slices.SortFunc(actuals, func(a, b SubagentModelActual) int { return strings.Compare(a.Model, b.Model) })
	return requests, actuals
}
