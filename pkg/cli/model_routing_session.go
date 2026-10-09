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
)

type sessionWorkflowInfo struct {
	EngineID       string                      `json:"engineId"`
	Model          string                      `json:"model"`
	RequestedModel string                      `json:"requestedModel"`
	CLIVersion     string                      `json:"cliVersion"`
	AWFVersion     string                      `json:"awfVersion"`
	MCPGVersion    string                      `json:"mcpgVersion"`
	AgentVersion   string                      `json:"agentVersion"`
	TriggerType    string                      `json:"triggerType"`
	Workflow       string                      `json:"workflow"`
	Repository     string                      `json:"repository"`
	RunID          NumericID                   `json:"runId"`
	DryRun         bool                        `json:"dryRun"`
	ModelRouting   *sessionModelRoutingPayload `json:"modelRouting"`
}

type sessionModelRoutingPayload struct {
	Status            string `json:"status"`
	Source            string `json:"source"`
	Provider          string `json:"provider"`
	WireModel         string `json:"wireModel"`
	Model             string `json:"model"`
	Effort            string `json:"effort"`
	AppliedEffort     string `json:"appliedEffort"`
	EffectiveEndpoint string `json:"effectiveEndpoint"`
	SelectedEndpoint  string `json:"selectedEndpoint"`
	Mode              string `json:"mode"`
	SelectedID        string `json:"selectedId"`
	RouterVersion     string `json:"routerVersion"`
	FailureCode       string `json:"failureCode"`
}

type sessionModelRoutingOutcome struct {
	Status            string `json:"status"`
	WireModel         string `json:"wireModel"`
	EffectiveEndpoint string `json:"effectiveEndpoint"`
	SelectedEndpoint  string `json:"selectedEndpoint"`
	Effort            string `json:"effort"`
	AppliedEffort     string `json:"appliedEffort"`
	FailureCode       string `json:"failureCode"`
}

type sessionModelRoutingEvent struct {
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	Provenance struct {
		Component string `json:"component"`
		Phase     string `json:"phase"`
	} `json:"provenance"`
}

type sessionModelRoutingAttribution struct {
	WorkflowInfo *sessionWorkflowInfo
	Outcome      *sessionModelRoutingOutcome
}

func readSessionModelRouting(runDir string) (*sessionModelRoutingAttribution, bool, error) {
	if runDir == "" {
		return nil, false, nil
	}
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		path := filepath.Join(runDir, filepath.FromSlash(relative))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, false, fmt.Errorf("%s: %w", relative, err)
		}
		if err := validateSubagentSessionSource(path); err != nil {
			return nil, false, fmt.Errorf("%s: %w", relative, err)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", relative, err)
		}
		attribution, err := readSessionModelRoutingEvents(file)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", relative, err)
		}
		found := attribution.WorkflowInfo != nil || attribution.Outcome != nil
		return attribution, found, nil
	}
	return nil, false, nil
}

func readSessionModelRoutingEvents(file *os.File) (*sessionModelRoutingAttribution, error) {
	defer file.Close()
	attribution := &sessionModelRoutingAttribution{}
	reader := bufio.NewReader(file)
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return nil, fmt.Errorf("oversized session record on line %d", lineNumber)
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			if err := decodeSessionModelRoutingEvent(line, lineNumber, attribution); err != nil {
				return nil, err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return attribution, nil
			}
			return nil, fmt.Errorf("failed to read session: %w", readErr)
		}
	}
}

func decodeSessionModelRoutingEvent(line []byte, lineNumber int, attribution *sessionModelRoutingAttribution) error {
	var event sessionModelRoutingEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return fmt.Errorf("invalid session event on line %d: %w", lineNumber, err)
	}
	switch {
	case event.Type == "workflow.info" && event.Provenance.Component == "workflow":
		var info sessionWorkflowInfo
		if err := json.Unmarshal(event.Data, &info); err != nil {
			return fmt.Errorf("invalid workflow.info on line %d: %w", lineNumber, err)
		}
		attribution.WorkflowInfo = &info
	case event.Type == "model_routing.outcome" && event.Provenance.Component == "agent" && event.Provenance.Phase == "agent":
		var outcome sessionModelRoutingOutcome
		if err := json.Unmarshal(event.Data, &outcome); err != nil {
			return fmt.Errorf("invalid model_routing.outcome on line %d: %w", lineNumber, err)
		}
		attribution.Outcome = &outcome
	}
	return nil
}

func (attribution *sessionModelRoutingAttribution) modelRouting() *AwInfoModelRouting {
	if attribution == nil {
		return nil
	}
	var routing AwInfoModelRouting
	found := false
	if info := attribution.WorkflowInfo; info != nil && info.ModelRouting != nil {
		data := info.ModelRouting
		routing = AwInfoModelRouting{
			Status: data.Status, Source: data.Source, Provider: data.Provider,
			Model: data.Model, WireModel: data.WireModel, Effort: data.Effort,
			AppliedEffort: data.AppliedEffort, Endpoint: data.EffectiveEndpoint,
			SelectedEndpoint: data.SelectedEndpoint, Mode: data.Mode,
			SelectedID: data.SelectedID, RouterVersion: data.RouterVersion,
			FailureCode: data.FailureCode,
		}
		found = true
	}
	if outcome := attribution.Outcome; outcome != nil {
		if outcome.Status != "" {
			routing.Status = outcome.Status
			found = true
		}
		if outcome.WireModel != "" {
			routing.WireModel = outcome.WireModel
			found = true
		}
		if outcome.EffectiveEndpoint != "" {
			routing.Endpoint = outcome.EffectiveEndpoint
			found = true
		}
		if outcome.SelectedEndpoint != "" {
			routing.SelectedEndpoint = outcome.SelectedEndpoint
			found = true
		}
		if outcome.Effort != "" {
			routing.Effort = outcome.Effort
			found = true
		}
		if outcome.AppliedEffort != "" {
			routing.AppliedEffort = outcome.AppliedEffort
			found = true
		}
		if outcome.FailureCode != "" {
			routing.FailureCode = outcome.FailureCode
			found = true
		}
	}
	if !found {
		return nil
	}
	return &routing
}

func (attribution *sessionModelRoutingAttribution) awInfo() *AwInfo {
	if attribution == nil || attribution.WorkflowInfo == nil {
		return nil
	}
	workflow := attribution.WorkflowInfo
	return &AwInfo{
		EngineID:       workflow.EngineID,
		Model:          workflow.Model,
		RequestedModel: workflow.RequestedModel,
		CLIVersion:     workflow.CLIVersion,
		AwfVersion:     workflow.AWFVersion,
		AwmgVersion:    workflow.MCPGVersion,
		Version:        workflow.AgentVersion,
		WorkflowName:   workflow.Workflow,
		Repository:     workflow.Repository,
		RunID:          workflow.RunID,
		DryRun:         workflow.DryRun,
		EventName:      workflow.TriggerType,
		ModelRouting:   attribution.modelRouting(),
	}
}
