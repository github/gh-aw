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
	SourceEngine     string          `json:"sourceEngine"`
	SessionID        string          `json:"sessionId"`
	InvocationID     string          `json:"invocationId"`
	ToolCallID       string          `json:"toolCallId"`
	AgentName        string          `json:"agentName"`
	AgentDisplayName string          `json:"agentDisplayName"`
	Model            string          `json:"model"`
	NewModel         string          `json:"newModel"`
	RequestedModel   string          `json:"requestedModel"`
	ResolvedModel    string          `json:"resolvedModel"`
	Outcome          string          `json:"outcome"`
	ErrorMessage     string          `json:"errorMessage"`
	ReasoningEffort  string          `json:"reasoningEffort"`
	Error            string          `json:"error"`
	Agent            string          `json:"agent"`
	Event            json.RawMessage `json:"event"`
	AgentMetrics     map[string]struct {
		AgentName          string `json:"agentName"`
		AgentDisplayName   string `json:"agentDisplayName"`
		TotalNanoAiu       int64  `json:"totalNanoAiu"`
		TotalApiDurationMs int    `json:"totalApiDurationMs"`
		ModelMetrics       map[string]struct {
			Requests struct {
				Count *int `json:"count"`
			} `json:"requests"`
			Usage struct {
				InputTokens      int `json:"inputTokens"`
				OutputTokens     int `json:"outputTokens"`
				CacheReadTokens  int `json:"cacheReadTokens"`
				CacheWriteTokens int `json:"cacheWriteTokens"`
				ReasoningTokens  int `json:"reasoningTokens"`
			} `json:"usage"`
			TotalNanoAiu int64 `json:"totalNanoAiu"`
		} `json:"modelMetrics"`
	} `json:"agentMetrics"`
}

// Unified conclusion evidence precedes the canonical bootstrap trace. Neither
// requires downloading the original engine logs to identify subagents.
func readSessionSubagentModels(runDir string) ([]SubagentModelRequest, []SubagentModelActual, bool, error) {
	requests, actuals, _, found, err := readSessionSubagentModelsDetailed(runDir)
	return requests, actuals, found, err
}

func readSessionSubagentModelsDetailed(runDir string) ([]SubagentModelRequest, []SubagentModelActual, []AgentUsageBreakdown, bool, error) {
	var diagnostics error
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl", "agent-session.jsonl"} {
		path := filepath.Join(runDir, filepath.FromSlash(relative))
		if err := validateSubagentSessionSource(path); err != nil {
			diagnostics = errors.Join(diagnostics, err)
			continue
		}
		requests, actuals, agentUsage, found, parseErr := parseSessionSubagentFileDetailed(path, relative != "agent-session.jsonl")
		if errors.Is(parseErr, os.ErrNotExist) {
			continue
		}
		if parseErr != nil {
			tokenUsageSubagentLog.Printf("failed to parse %s: %v", relative, parseErr)
			diagnostics = errors.Join(diagnostics, fmt.Errorf("%s: %w", relative, parseErr))
			continue
		}
		if found {
			return requests, actuals, agentUsage, true, nil
		}
	}
	return nil, nil, nil, false, diagnostics
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
	requests, actuals, _, found, err = parseSessionSubagentFileDetailed(path, unified)
	return
}

func parseSessionSubagentFileDetailed(path string, unified bool) (requests []SubagentModelRequest, actuals []SubagentModelActual, agentUsage []AgentUsageBreakdown, found bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, false, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close session: %w", closeErr))
		}
	}()
	return parseSessionSubagentModelsDetailed(file, unified)
}

type subagentSessionModels struct {
	agents       map[string]*SubagentModelRequest
	actualCounts map[string]map[string]int
	actualModels map[string]map[string]SubagentModelActual
	agentUsage   map[string]*AgentUsageBreakdown
	sourceEngine string
	found        bool
}

func parseSessionSubagentModels(input io.Reader, unified bool) ([]SubagentModelRequest, []SubagentModelActual, bool, error) {
	requests, actuals, _, found, err := parseSessionSubagentModelsDetailed(input, unified)
	return requests, actuals, found, err
}

func parseSessionSubagentModelsDetailed(input io.Reader, unified bool) ([]SubagentModelRequest, []SubagentModelActual, []AgentUsageBreakdown, bool, error) {
	reader := bufio.NewReader(input)
	headerSeen := !unified
	sessions := subagentSessionSelection{scopes: make(map[string]*subagentSessionModels), sources: make(map[string]string)}
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return nil, nil, nil, false, fmt.Errorf("oversized session record on line %d", lineNumber)
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var event sessionSubagentEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, nil, nil, false, fmt.Errorf("invalid session event on line %d: %w", lineNumber, err)
			}
			if !headerSeen {
				metadata := event.unifiedDetectionEvent
				metadata.Provenance.Component = event.Provenance.Component
				if err := validateUnifiedDetectionHeader(metadata); err != nil {
					return nil, nil, nil, false, err
				}
				headerSeen = true
			} else if unified && event.Type == "session.format" && event.Provenance.Component == "collector" {
				return nil, nil, nil, false, errors.New("session contains multiple collector format headers")
			} else if !unified || (event.Provenance.Component == "agent" && event.Provenance.Phase == "agent") {
				if err := sessions.observe(event); err != nil {
					return nil, nil, nil, false, fmt.Errorf("invalid %s on line %d: %w", event.Type, lineNumber, err)
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, nil, nil, false, fmt.Errorf("failed to read session: %w", readErr)
			}
			break
		}
	}
	if !headerSeen {
		return nil, nil, nil, false, errors.New("session is missing its leading session.format header")
	}
	models := sessions.scopes[sessions.final]
	if models == nil {
		return nil, nil, nil, false, nil
	}
	requests, actuals := models.rows()
	return requests, actuals, models.agentRows(), models.found, nil
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
			actualModels: make(map[string]map[string]SubagentModelActual),
			agentUsage:   make(map[string]*AgentUsageBreakdown),
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

func (models *subagentSessionModels) observe(event sessionSubagentEvent) error {
	switch event.Type {
	case "session.start", "session.init", "subagent.started", "subagent.configured", "subagent.completed", "subagent.failed", "session.model_change", "session.result", "session.shutdown", "pi.subagent_dispatch", "pi.subagent_event", "pi.subagent_result":
	default:
		return nil
	}
	var data sessionSubagentData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return err
	}
	if event.Type == "session.init" && (data.SourceEngine == "copilot" || data.SourceEngine == "pi") {
		models.sourceEngine = data.SourceEngine
		if data.SessionID == "" && event.AgentID == "" {
			models.agents = make(map[string]*SubagentModelRequest)
			models.actualCounts = make(map[string]map[string]int)
			models.actualModels = make(map[string]map[string]SubagentModelActual)
			models.agentUsage = make(map[string]*AgentUsageBreakdown)
		}
		models.found = true
	}
	if event.Type == "session.start" {
		models.found = true
	}
	if err := models.observeMetrics(data); err != nil {
		return err
	}
	switch event.Type {
	case "pi.subagent_dispatch":
		return models.observePiDispatch(event, data)
	case "pi.subagent_event":
		return models.observePiUsage(event, data)
	case "pi.subagent_result":
		return models.observePiResult(event, data)
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
		models.actualModels = make(map[string]map[string]SubagentModelActual)
		models.resetAgentUsageMetrics()
	}
	for agentID, metric := range data.AgentMetrics {
		if agentID != "main" && models.agents[agentID] == nil && metric.AgentName != "" {
			name := metric.AgentDisplayName
			if name == "" {
				name = metric.AgentName
			}
			models.agents[agentID] = &SubagentModelRequest{AgentName: name, InvocationCount: 1}
		}
		name := metric.AgentDisplayName
		if name == "" {
			name = metric.AgentName
		}
		role := "subagent"
		if agentID == "main" {
			name, role = "main", "main"
		} else if name == "" && models.agents[agentID] != nil {
			name = models.agents[agentID].AgentName
		}
		agentUsage := models.agentUsage[agentID]
		if agentUsage == nil {
			agentUsage = &AgentUsageBreakdown{}
			models.agentUsage[agentID] = agentUsage
		}
		if name == "" && models.agents[agentID] != nil {
			name = models.agents[agentID].AgentName
		}
		agentUsage.AgentName = name
		agentUsage.AgentType = role
		agentUsage.SourceEngine = models.sourceEngine
		agentUsage.InstanceCount = max(agentUsage.InstanceCount, 1)
		agentUsage.AIC = float64(metric.TotalNanoAiu) / 1e9
		agentUsage.TotalApiDurationMs = metric.TotalApiDurationMs
		agentUsage.Requests, agentUsage.InputTokens, agentUsage.OutputTokens = 0, 0, 0
		agentUsage.CacheReadTokens, agentUsage.CacheWriteTokens, agentUsage.ReasoningTokens = 0, 0, 0
		agentUsage.Models = nil
		counts := make(map[string]int)
		actuals := make(map[string]SubagentModelActual)
		for model, usage := range metric.ModelMetrics {
			requestCount := 0
			if usage.Requests.Count != nil {
				requestCount = *usage.Requests.Count
			}
			if requestCount < 0 {
				return errors.New("negative agent request count")
			}
			counts[model] = requestCount
			actual := SubagentModelActual{Model: model, Requests: requestCount}
			actual.TokenCoreMetrics = TokenCoreMetrics{
				InputTokens: usage.Usage.InputTokens, OutputTokens: usage.Usage.OutputTokens,
				CacheReadTokens: usage.Usage.CacheReadTokens, CacheWriteTokens: usage.Usage.CacheWriteTokens,
				ReasoningTokens: usage.Usage.ReasoningTokens,
			}
			actual.AIC = float64(usage.TotalNanoAiu) / 1e9
			if agentID != "main" {
				actuals[model] = actual
			}
			agentUsage.Models = append(agentUsage.Models, AgentModelUsage{
				Model: model, Requests: requestCount, TokenCoreMetrics: actual.TokenCoreMetrics, AIC: actual.AIC,
			})
			agentUsage.Requests += requestCount
			agentUsage.InputTokens += usage.Usage.InputTokens
			agentUsage.OutputTokens += usage.Usage.OutputTokens
			agentUsage.CacheReadTokens += usage.Usage.CacheReadTokens
			agentUsage.CacheWriteTokens += usage.Usage.CacheWriteTokens
			agentUsage.ReasoningTokens += usage.Usage.ReasoningTokens
		}
		if agentID != "main" {
			models.actualCounts[agentID] = counts
			models.actualModels[agentID] = actuals
		}
		models.agentUsage[agentID] = agentUsage
	}
	return nil
}

func (models *subagentSessionModels) resetAgentUsageMetrics() {
	reset := make(map[string]*AgentUsageBreakdown, len(models.agentUsage))
	for agentID, previous := range models.agentUsage {
		copy := *previous
		copy.Requests = 0
		copy.TokenCoreMetrics = TokenCoreMetrics{}
		copy.AIC = 0
		copy.TotalApiDurationMs = 0
		copy.Models = nil
		reset[agentID] = &copy
	}
	models.agentUsage = reset
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
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: row.AgentName, AgentType: "subagent"}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = row.AgentName
	agentUsage.AgentType = "subagent"
	if event.Type == "subagent.started" {
		row.RequestedModel = data.Model
		row.IncompleteCount = 1
		row.Effort = data.ReasoningEffort
		agentUsage := models.agentUsage[identity]
		if agentUsage == nil {
			agentUsage = &AgentUsageBreakdown{AgentName: row.AgentName, AgentType: "subagent"}
			models.agentUsage[identity] = agentUsage
		}
		agentUsage.InstanceCount = 1
		agentUsage.RequestedModels = appendUnique(agentUsage.RequestedModels, data.Model)
		agentUsage.Effort = data.ReasoningEffort
	}
	if event.Type == "subagent.configured" {
		if data.Model != "" {
			row.ResolvedModel = data.Model
		}
		if data.ReasoningEffort != "" {
			row.Effort = data.ReasoningEffort
			if agentUsage := models.agentUsage[identity]; agentUsage != nil {
				agentUsage.Effort = data.ReasoningEffort
			}
		}
	}
	if event.Type == "subagent.completed" {
		row.CompletedCount++
		row.IncompleteCount = max(0, row.IncompleteCount-1)
		agentUsage.CompletedCount++
		agentUsage.IncompleteCount = max(0, agentUsage.IncompleteCount-1)
	}
	if event.Type == "subagent.failed" {
		row.FailedCount++
		row.IncompleteCount = max(0, row.IncompleteCount-1)
		row.Error = sanitizeSubagentError(firstNonEmptyModel(data.Error, data.ErrorMessage))
		agentUsage.FailedCount++
		agentUsage.IncompleteCount = max(0, agentUsage.IncompleteCount-1)
	}
	return nil
}

func (models *subagentSessionModels) observePiDispatch(event sessionSubagentEvent, data sessionSubagentData) error {
	identity := data.InvocationID
	if identity == "" {
		identity = event.AgentID
	}
	if identity == "" {
		return errors.New("missing pi sub-agent invocation identity")
	}
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{InvocationCount: 1, IncompleteCount: 1}
		models.agents[identity] = row
	}
	row.AgentName = data.Agent
	row.RequestedModel = data.RequestedModel
	row.ResolvedModel = data.ResolvedModel
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: data.Agent, AgentType: "subagent"}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = data.Agent
	agentUsage.AgentType = "subagent"
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.InstanceCount = 1
	agentUsage.RequestedModels = appendUnique(agentUsage.RequestedModels, data.RequestedModel)
	agentUsage.ResolvedModels = appendUnique(agentUsage.ResolvedModels, data.ResolvedModel)
	agentUsage.Effort = data.ReasoningEffort
	models.found = true
	return nil
}

func (models *subagentSessionModels) observePiUsage(event sessionSubagentEvent, data sessionSubagentData) error {
	var payload struct {
		Message struct {
			Model         string `json:"model"`
			ResponseModel string `json:"responseModel"`
			StopReason    string `json:"stopReason"`
			ErrorMessage  string `json:"errorMessage"`
			Usage         struct {
				Input      int `json:"input"`
				Output     int `json:"output"`
				CacheRead  int `json:"cacheRead"`
				CacheWrite int `json:"cacheWrite"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data.Event, &payload); err != nil {
		return err
	}
	identity := data.InvocationID
	if identity == "" {
		identity = event.AgentID
	}
	if identity == "" {
		return errors.New("missing pi sub-agent invocation identity")
	}
	model := firstNonEmptyModel(payload.Message.ResponseModel, payload.Message.Model)
	if model == "" {
		return nil
	}
	byModel := models.actualModels[identity]
	if byModel == nil {
		byModel = make(map[string]SubagentModelActual)
		models.actualModels[identity] = byModel
	}
	actual := byModel[model]
	actual.Model = model
	actual.Requests++
	actual.InputTokens += payload.Message.Usage.Input
	actual.OutputTokens += payload.Message.Usage.Output
	actual.CacheReadTokens += payload.Message.Usage.CacheRead
	actual.CacheWriteTokens += payload.Message.Usage.CacheWrite
	byModel[payload.Message.Model] = actual
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: data.Agent, AgentType: "subagent", InstanceCount: 1}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = firstNonEmptyModel(agentUsage.AgentName, data.Agent)
	agentUsage.AgentType = "subagent"
	agentUsage.SourceEngine = models.sourceEngine
	agentUsage.Requests++
	agentUsage.InputTokens += payload.Message.Usage.Input
	agentUsage.OutputTokens += payload.Message.Usage.Output
	agentUsage.CacheReadTokens += payload.Message.Usage.CacheRead
	agentUsage.CacheWriteTokens += payload.Message.Usage.CacheWrite
	agentUsage.Models = appendAgentModelUsage(agentUsage.Models, AgentModelUsage{
		Model: model, Requests: 1,
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens: payload.Message.Usage.Input, OutputTokens: payload.Message.Usage.Output,
			CacheReadTokens: payload.Message.Usage.CacheRead, CacheWriteTokens: payload.Message.Usage.CacheWrite,
		},
	})
	timestamp, _ := subagentSessionStart(event)
	agentUsage.requestUsages = append(agentUsage.requestUsages, agentRequestUsage{
		Model: model, Timestamp: timestamp,
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens: payload.Message.Usage.Input, OutputTokens: payload.Message.Usage.Output,
			CacheReadTokens: payload.Message.Usage.CacheRead, CacheWriteTokens: payload.Message.Usage.CacheWrite,
		},
	})
	counts := models.actualCounts[identity]
	if counts == nil {
		counts = make(map[string]int)
		models.actualCounts[identity] = counts
	}
	counts[model] = actual.Requests
	if payload.Message.StopReason == "error" || payload.Message.StopReason == "aborted" || payload.Message.ErrorMessage != "" {
		if row := models.agents[identity]; row != nil {
			row.Error = sanitizeSubagentError(firstNonEmptyModel(payload.Message.ErrorMessage, payload.Message.StopReason))
		}
	}
	models.found = true
	return nil
}

func (models *subagentSessionModels) observePiResult(event sessionSubagentEvent, data sessionSubagentData) error {
	identity := data.InvocationID
	if identity == "" {
		identity = event.AgentID
	}
	if identity == "" {
		return errors.New("missing pi sub-agent invocation identity")
	}
	row := models.agents[identity]
	if row == nil {
		row = &SubagentModelRequest{InvocationCount: 1}
		models.agents[identity] = row
	}
	row.IncompleteCount = 0
	if data.Outcome == "failed" || data.Error != "" || data.ErrorMessage != "" {
		row.FailedCount = 1
		row.Error = sanitizeSubagentError(firstNonEmptyModel(data.Error, data.ErrorMessage))
	} else {
		row.CompletedCount = 1
	}
	agentUsage := models.agentUsage[identity]
	if agentUsage == nil {
		agentUsage = &AgentUsageBreakdown{AgentName: data.Agent, AgentType: "subagent", InstanceCount: 1}
		models.agentUsage[identity] = agentUsage
	}
	agentUsage.AgentName = firstNonEmptyModel(agentUsage.AgentName, data.Agent)
	if row.FailedCount > 0 {
		agentUsage.FailedCount++
	} else {
		agentUsage.CompletedCount++
	}
	agentUsage.IncompleteCount = 0
	return nil
}

func sanitizeSubagentError(message string) string {
	message = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return ' '
		}
		return r
	}, message)), " ")
	const maxLength = 300
	if len(message) > maxLength {
		message = message[:maxLength] + "…"
	}
	return message
}

func (models *subagentSessionModels) rows() ([]SubagentModelRequest, []SubagentModelActual) {
	grouped := make(map[subagentModelKey]SubagentModelRequest)
	for agentID, row := range models.agents {
		if row.AgentName != "" {
			row.EffectiveModel = ""
			observedModels := make([]string, 0, len(models.actualCounts[agentID]))
			for model := range models.actualCounts[agentID] {
				observedModels = append(observedModels, model)
			}
			slices.Sort(observedModels)
			for _, model := range observedModels {
				if models.actualCounts[agentID][model] > 0 {
					row.EffectiveModel = model
					break
				}
			}
			key := subagentModelKey{agent: row.AgentName, model: row.RequestedModel}
			combined := grouped[key]
			if combined.InvocationCount == 0 {
				combined = *row
			} else {
				combined.InvocationCount += row.InvocationCount
				combined.CompletedCount += row.CompletedCount
				combined.FailedCount += row.FailedCount
				combined.IncompleteCount += row.IncompleteCount
				if combined.Effort != row.Effort {
					combined.Effort = ""
				}
				if combined.EffectiveModel != "" && row.EffectiveModel != "" && combined.EffectiveModel != row.EffectiveModel {
					combined.EffectiveModel = ""
				}
				if combined.Error == "" && row.Error != "" {
					combined.Error = row.Error
				}
			}
			if combined.EffectiveModel == "" && combined.FailedCount > 0 && combined.CompletedCount == 0 {
				combined.ReasonCode = modelMismatchReasonSubagentFailed
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
	return requests, models.actualRows()
}

func (models *subagentSessionModels) agentRows() []AgentUsageBreakdown {
	grouped := make(map[string]*AgentUsageBreakdown)
	for agentID, usage := range models.agentUsage {
		if usage.AgentName == "" {
			usage.AgentName = "subagent"
		}
		key := usage.AgentType + "\x00" + usage.AgentName
		row := grouped[key]
		if row == nil {
			row = &AgentUsageBreakdown{AgentName: usage.AgentName, AgentType: usage.AgentType, SourceEngine: usage.SourceEngine}
			grouped[key] = row
		}
		row.InstanceCount += usage.InstanceCount
		if row.SourceEngine == "" {
			row.SourceEngine = usage.SourceEngine
		}
		row.CompletedCount += usage.CompletedCount
		row.FailedCount += usage.FailedCount
		row.IncompleteCount += usage.IncompleteCount
		row.Requests += usage.Requests
		row.InputTokens += usage.InputTokens
		row.OutputTokens += usage.OutputTokens
		row.CacheReadTokens += usage.CacheReadTokens
		row.CacheWriteTokens += usage.CacheWriteTokens
		row.ReasoningTokens += usage.ReasoningTokens
		row.AIC += usage.AIC
		row.TotalApiDurationMs += usage.TotalApiDurationMs
		row.Effort = combineSubagentEffort(row.Effort, usage.Effort)
		row.requestUsages = append(row.requestUsages, usage.requestUsages...)
		for _, model := range usage.RequestedModels {
			row.RequestedModels = appendUnique(row.RequestedModels, model)
		}
		for _, model := range usage.ResolvedModels {
			row.ResolvedModels = appendUnique(row.ResolvedModels, model)
		}
		for _, model := range usage.Models {
			row.Models = appendAgentModelUsage(row.Models, model)
			if model.Requests > 0 {
				row.ServedModels = appendUnique(row.ServedModels, model.Model)
			}
		}
		if request := models.agents[agentID]; request != nil {
			row.RequestedModels = appendUnique(row.RequestedModels, request.RequestedModel)
			row.ResolvedModels = appendUnique(row.ResolvedModels, request.ResolvedModel)
			row.ServedModels = appendUnique(row.ServedModels, request.EffectiveModel)
			row.Effort = combineSubagentEffort(row.Effort, request.Effort)
		}
	}
	result := make([]AgentUsageBreakdown, 0, len(grouped))
	for _, row := range grouped {
		slices.Sort(row.RequestedModels)
		slices.Sort(row.ResolvedModels)
		slices.Sort(row.ServedModels)
		slices.SortFunc(row.Models, func(a, b AgentModelUsage) int { return strings.Compare(a.Model, b.Model) })
		result = append(result, *row)
	}
	slices.SortFunc(result, func(a, b AgentUsageBreakdown) int {
		if a.AgentType != b.AgentType {
			if a.AgentType == "main" {
				return -1
			}
			if b.AgentType == "main" {
				return 1
			}
		}
		return strings.Compare(a.AgentName, b.AgentName)
	})
	return result
}

func appendAgentModelUsage(models []AgentModelUsage, next AgentModelUsage) []AgentModelUsage {
	for i := range models {
		if models[i].Model != next.Model {
			continue
		}
		models[i].Requests += next.Requests
		models[i].InputTokens += next.InputTokens
		models[i].OutputTokens += next.OutputTokens
		models[i].CacheReadTokens += next.CacheReadTokens
		models[i].CacheWriteTokens += next.CacheWriteTokens
		models[i].ReasoningTokens += next.ReasoningTokens
		models[i].AIC += next.AIC
		return models
	}
	return append(models, next)
}

func (models *subagentSessionModels) actualRows() []SubagentModelActual {
	counts := make(map[string]SubagentModelActual)
	for _, byModel := range models.actualModels {
		for model, usage := range byModel {
			actual := counts[model]
			actual.Model = model
			actual.Requests += usage.Requests
			actual.InputTokens += usage.InputTokens
			actual.OutputTokens += usage.OutputTokens
			actual.CacheReadTokens += usage.CacheReadTokens
			actual.CacheWriteTokens += usage.CacheWriteTokens
			actual.ReasoningTokens += usage.ReasoningTokens
			actual.AIC += usage.AIC
			counts[model] = actual
		}
	}
	actuals := make([]SubagentModelActual, 0, len(counts))
	for _, actual := range counts {
		actuals = append(actuals, actual)
	}
	slices.SortFunc(actuals, func(a, b SubagentModelActual) int { return strings.Compare(a.Model, b.Model) })
	return actuals
}
