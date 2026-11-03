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
	"time"
)

// Unified conclusion evidence precedes the canonical bootstrap trace. Neither
// requires downloading the original engine logs to identify subagents.
func readSessionSubagentModelsDetailed(runDir string) ([]SubagentModelRequest, []SubagentModelActual, []AgentUsageBreakdown, bool, error) {
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		path := filepath.Join(runDir, filepath.FromSlash(relative))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, nil, nil, false, fmt.Errorf("%s: %w", relative, err)
		}
		if err := validateSubagentSessionSource(path); err != nil {
			return nil, nil, nil, false, fmt.Errorf("%s: %w", relative, err)
		}
		models, parseErr := parseSessionSubagentFile(path, true)
		if parseErr != nil {
			tokenUsageSubagentLog.Printf("failed to parse %s: %v", relative, parseErr)
			return nil, nil, nil, false, fmt.Errorf("%s: %w", relative, parseErr)
		}
		if models == nil {
			return nil, nil, nil, false, nil
		}
		if models.hasStartsWithoutAgentID() {
			models.reconcileSubagentIdentities(readNativeSubagentSession(runDir))
		}
		requests, actuals := models.rows()
		return requests, actuals, models.agentRows(), models.found, nil
	}

	path := filepath.Join(runDir, "agent-session.jsonl")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, false, nil
	} else if err != nil {
		return nil, nil, nil, false, fmt.Errorf("agent-session.jsonl: %w", err)
	}
	if err := validateSubagentSessionSource(path); err != nil {
		return nil, nil, nil, false, fmt.Errorf("agent-session.jsonl: %w", err)
	}
	models, err := parseSessionSubagentFile(path, false)
	if err != nil {
		tokenUsageSubagentLog.Printf("failed to parse agent-session.jsonl: %v", err)
		return nil, nil, nil, false, fmt.Errorf("agent-session.jsonl: %w", err)
	}
	if models != nil && models.found {
		requests, actuals := models.rows()
		return requests, actuals, models.agentRows(), true, nil
	}
	return nil, nil, nil, false, nil
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

func parseSessionSubagentFile(path string, unified bool) (models *subagentSessionModels, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close session: %w", closeErr))
		}
	}()
	return parseSessionSubagentSession(file, unified)
}

type subagentSessionModels struct {
	agents         map[string]*SubagentModelRequest
	actualCounts   map[string]map[string]int
	actualModels   map[string]map[string]SubagentModelActual
	agentUsage     map[string]*AgentUsageBreakdown
	legacyPiActive map[string]string
	legacyPiCount  map[string]int
	legacyPiTools  map[string]string
	sourceEngine   string
	found          bool
	// sessionID, starts, and metricNames are identity evidence used to
	// reconcile lifecycle instances that lack a top-level agentId.
	sessionID   string
	starts      []subagentStartEvidence
	metricNames map[string]subagentMetricNames
}

func parseSessionSubagentModels(input io.Reader, unified bool) ([]SubagentModelRequest, []SubagentModelActual, bool, error) {
	requests, actuals, _, found, err := parseSessionSubagentModelsDetailed(input, unified)
	return requests, actuals, found, err
}

func parseSessionSubagentModelsDetailed(input io.Reader, unified bool) ([]SubagentModelRequest, []SubagentModelActual, []AgentUsageBreakdown, bool, error) {
	models, err := parseSessionSubagentSession(input, unified)
	if err != nil || models == nil {
		return nil, nil, nil, false, err
	}
	requests, actuals := models.rows()
	return requests, actuals, models.agentRows(), models.found, nil
}

// parseSessionSubagentSession returns the per-instance models of the final
// session scope, or nil when the session has none.
func parseSessionSubagentSession(input io.Reader, unified bool) (*subagentSessionModels, error) {
	reader := bufio.NewReader(input)
	headerSeen := !unified
	sessions := subagentSessionSelection{scopes: make(map[string]*subagentSessionModels), sources: make(map[string]string)}
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return nil, fmt.Errorf("oversized session record on line %d", lineNumber)
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var event sessionSubagentEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, fmt.Errorf("invalid session event on line %d: %w", lineNumber, err)
			}
			if !headerSeen {
				metadata := event.unifiedDetectionEvent
				metadata.Provenance.Component = event.Provenance.Component
				if err := validateUnifiedDetectionHeader(metadata); err != nil {
					return nil, err
				}
				headerSeen = true
			} else if unified && event.Type == "session.format" && event.Provenance.Component == "collector" {
				return nil, errors.New("session contains multiple collector format headers")
			} else if !unified || (event.Provenance.Component == "agent" && event.Provenance.Phase == "agent") {
				if err := sessions.observe(event); err != nil {
					return nil, fmt.Errorf("invalid %s on line %d: %w", event.Type, lineNumber, err)
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("failed to read session: %w", readErr)
			}
			break
		}
	}
	if !headerSeen {
		return nil, errors.New("session is missing its leading session.format header")
	}
	return sessions.scopes[sessions.final], nil
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
		models = &subagentSessionModels{
			agents: make(map[string]*SubagentModelRequest), actualCounts: make(map[string]map[string]int),
			actualModels:   make(map[string]map[string]SubagentModelActual),
			agentUsage:     make(map[string]*AgentUsageBreakdown),
			legacyPiActive: make(map[string]string), legacyPiCount: make(map[string]int),
			legacyPiTools: make(map[string]string),
		}
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
