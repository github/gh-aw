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

func readUnifiedTokenUsageEntries(runDir string) ([]TokenUsageEntry, error) {
	collected, err := readUnifiedTokenUsage(runDir)
	return collected.entries, err
}

// readUnifiedTokenUsage returns the agent-phase firewall.token_usage entries and
// the AWF steering notices and notice deliveries recorded on
// firewall.token_usage events of the agent and threat-detection phases, in
// session order.
func readUnifiedTokenUsage(runDir string) (collected unifiedTokenUsageCollection, err error) {
	path, err := findUnifiedSessionPath(runDir)
	if err != nil || path == "" {
		return unifiedTokenUsageCollection{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return unifiedTokenUsageCollection{}, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close session: %w", closeErr))
		}
	}()

	collected = unifiedTokenUsageCollection{entries: make([]TokenUsageEntry, 0)}
	var routingEvents []unifiedRoutingEvent
	reader := bufio.NewReader(file)
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return unifiedTokenUsageCollection{}, fmt.Errorf("oversized session record on line %d", lineNumber)
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var event sessionSubagentEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return unifiedTokenUsageCollection{}, fmt.Errorf("invalid session event on line %d: %w", lineNumber, err)
			}
			if event.Type == "firewall.model_routing" && event.Provenance.Component == "firewall" && event.Provenance.Phase == "agent" {
				var data unifiedModelRoutingData
				if err := json.Unmarshal(event.Data, &data); err == nil {
					routingEvents = append(routingEvents, unifiedRoutingEvent{event: event, data: data})
				}
			}
			if err := collected.add(event, lineNumber); err != nil {
				return unifiedTokenUsageCollection{}, err
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return unifiedTokenUsageCollection{}, fmt.Errorf("failed to read session: %w", readErr)
			}
			break
		}
	}
	enrichUnifiedTokenUsageEntries(runDir, collected.entries, routingEvents)
	return collected, nil
}

// findUnifiedSessionPath returns the unified session file of a run directory, or
// an empty path when the run has none.
func findUnifiedSessionPath(runDir string) (string, error) {
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		candidate := filepath.Join(runDir, filepath.FromSlash(relative))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("%s: %w", relative, err)
		}
		if err := validateSubagentSessionSource(candidate); err != nil {
			return "", fmt.Errorf("%s: %w", relative, err)
		}
		return candidate, nil
	}
	return "", nil
}

// unifiedTokenUsageCollection accumulates agent-phase token usage entries and the
// steering notices and notice deliveries of agent and threat-detection
// firewall.token_usage events.
type unifiedTokenUsageCollection struct {
	entries    []TokenUsageEntry
	notices    []SteeringNotice
	deliveries []SteeringDelivery
}

func (c *unifiedTokenUsageCollection) add(event sessionSubagentEvent, lineNumber int) error {
	phase := event.Provenance.Phase
	if event.Type != "firewall.token_usage" || event.Provenance.Component != "firewall" || (phase != "agent" && phase != "detection") {
		return nil
	}
	entry, err := unifiedTokenUsageEntry(event)
	if err != nil {
		if phase == "agent" {
			return fmt.Errorf("invalid firewall.token_usage on line %d: %w", lineNumber, err)
		}
		// Detection-phase events only contribute steering notices; skip malformed
		// ones rather than failing agent token usage.
		tokenUsageSubagentLog.Printf("skipping invalid detection firewall.token_usage on line %d: %v", lineNumber, err)
		return nil
	}
	if entry.Steering.valid() {
		c.notices = append(c.notices, SteeringNotice{
			Type:      entry.Steering.Type,
			Threshold: int(entry.Steering.Threshold),
			RequestID: entry.RequestID,
			Phase:     phase,
		})
	}
	if entry.SteeringDelivery.valid() {
		threshold, conversation := entry.SteeringDelivery.thresholdAndConversation()
		c.deliveries = append(c.deliveries, SteeringDelivery{
			Type:         entry.SteeringDelivery.Type,
			Trigger:      entry.SteeringDelivery.Trigger,
			Threshold:    threshold,
			Conversation: conversation,
			First:        entry.SteeringDelivery.First,
			RequestID:    entry.RequestID,
			Phase:        phase,
		})
	}
	if phase == "agent" {
		c.entries = append(c.entries, entry)
	}
	return nil
}

type unifiedModelRoutingData struct {
	Stage              string `json:"stage"`
	Attempt            int    `json:"attempt"`
	ClassifierModel    string `json:"classifier_model"`
	ClassifierAttempts int    `json:"classifier_attempts"`
	RequestID          string `json:"request_id"`
	Endpoint           string `json:"endpoint"`
	Path               string `json:"path"`
	XInitiator         string `json:"x_initiator"`
	XInitiatorCamel    string `json:"xInitiator"`
}

type unifiedRoutingEvent struct {
	event sessionSubagentEvent
	data  unifiedModelRoutingData
}

func enrichUnifiedTokenUsageEntries(runDir string, entries []TokenUsageEntry, routingEvents []unifiedRoutingEvent) {
	rawByRequestID := make(map[string]TokenUsageEntry)
	if rawPath := findTokenUsageFile(runDir); rawPath != "" {
		rawEntries, _, err := scanTokenUsageEntries(rawPath)
		if err == nil {
			for _, entry := range rawEntries {
				if entry.RequestID != "" {
					rawByRequestID[entry.RequestID] = entry
				}
			}
		} else {
			tokenUsageSubagentLog.Printf("failed to read raw token usage for unified metadata fallback: %v", err)
		}
	}

	for i := range entries {
		entry := &entries[i]
		if raw, ok := rawByRequestID[entry.RequestID]; ok {
			if fillTokenUsageMetadata(entry, raw) {
				tokenUsageSubagentLog.Printf("filled unified token usage metadata from raw proxy log for request %s", entry.RequestID)
			}
		}
	}

	for _, classifier := range unifiedRoutingClassifierRequests(routingEvents, entries) {
		if classifier.index < 0 || classifier.index >= len(entries) {
			continue
		}
		entry := &entries[classifier.index]
		changed := false
		if entry.Purpose == "" {
			entry.Purpose = "routing_classification"
			changed = true
		}
		if entry.Path == "" {
			entry.Path = firstNonEmptyModel(classifier.data.Path, classifier.data.Endpoint)
			changed = changed || entry.Path != ""
		}
		if entry.XInitiator == "" {
			entry.XInitiator = firstNonEmptyModel(classifier.data.XInitiator, classifier.data.XInitiatorCamel)
			changed = changed || entry.XInitiator != ""
		}
		if changed {
			tokenUsageSubagentLog.Printf("filled unified token usage metadata from firewall.model_routing timing join for request %s", entry.RequestID)
		}
	}
}

func fillTokenUsageMetadata(entry *TokenUsageEntry, source TokenUsageEntry) bool {
	changed := false
	if entry.Purpose == "" && source.Purpose != "" {
		entry.Purpose = source.Purpose
		changed = true
	}
	if entry.Path == "" && source.Path != "" {
		entry.Path = source.Path
		changed = true
	}
	if entry.XInitiator == "" && source.XInitiator != "" {
		entry.XInitiator = source.XInitiator
		changed = true
	}
	return changed
}

type routedClassifierRequest struct {
	index int
	data  unifiedModelRoutingData
}

func unifiedRoutingClassifierRequests(events []unifiedRoutingEvent, entries []TokenUsageEntry) []routedClassifierRequest {
	requestIDs := make(map[string]struct{})
	for _, event := range events {
		if event.data.Stage == "request" && event.data.RequestID != "" {
			requestIDs[event.data.RequestID] = struct{}{}
		}
	}
	classifiers := make([]routedClassifierRequest, 0)
	usedEntries := make(map[int]struct{})
	for i, event := range events {
		if event.data.Stage != "classification" || event.data.ClassifierModel == "" {
			continue
		}
		start, err := subagentSessionStart(event.event)
		if err != nil || start.IsZero() {
			continue
		}
		var selection time.Time
		for _, candidate := range events[i+1:] {
			if candidate.data.Stage != "selection" {
				continue
			}
			selection, err = subagentSessionStart(candidate.event)
			if err == nil && selection.After(start) {
				break
			}
			selection = time.Time{}
		}
		if selection.IsZero() {
			continue
		}
		for entryIndex, entry := range entries {
			if entry.RequestID == "" || entry.Purpose != "" || entry.DurationMs < 0 ||
				!unifiedModelMatches(entry.Model, event.data.ClassifierModel) {
				continue
			}
			if _, used := usedEntries[entryIndex]; used {
				continue
			}
			if _, isRoutedRequest := requestIDs[entry.RequestID]; isRoutedRequest {
				continue
			}
			timestamp, ok := parseTokenUsageTimestamp(entry.Timestamp)
			if !ok || timestamp.Before(start) {
				continue
			}
			end := timestamp.Add(time.Duration(entry.DurationMs) * time.Millisecond)
			if end.After(selection) {
				continue
			}
			classifiers = append(classifiers, routedClassifierRequest{index: entryIndex, data: event.data})
			usedEntries[entryIndex] = struct{}{}
		}
	}
	return classifiers
}

func unifiedModelMatches(model, classifierModel string) bool {
	model = modelNameWithoutProvider(model)
	classifierModel = modelNameWithoutProvider(classifierModel)
	return model != "" && classifierModel != "" && normalizeModelIdentity(model) == normalizeModelIdentity(classifierModel)
}

func unifiedTokenUsageEntry(event sessionSubagentEvent) (TokenUsageEntry, error) {
	var data struct {
		Provider        string              `json:"provider"`
		Model           string              `json:"model"`
		Purpose         string              `json:"purpose"`
		AgentName       json.RawMessage     `json:"agentName"`
		CallerSource    json.RawMessage     `json:"callerSource"`
		Path            string              `json:"path"`
		XInitiator      string              `json:"xInitiator"`
		XInitiatorSnake string              `json:"x_initiator"`
		RequestID       string              `json:"requestId"`
		AIC             json.RawMessage     `json:"aic"`
		TotalAIC        json.RawMessage     `json:"totalAic"`
		DurationMs      int                 `json:"durationMs"`
		Steering        *TokenUsageSteering `json:"steering"`
		// SteeringDelivery is absent on sessions from AWF v0.29.11 and older.
		SteeringDelivery *TokenUsageSteeringDelivery `json:"steeringDelivery"`
		Usage            struct {
			InputTokens              int             `json:"inputTokens"`
			OutputTokens             int             `json:"outputTokens"`
			CacheReadInputTokens     int             `json:"cacheReadInputTokens"`
			CacheCreationInputTokens int             `json:"cacheCreationInputTokens"`
			ReasoningOutputTokens    int             `json:"reasoningOutputTokens"`
			InputTokensIncludeCache  json.RawMessage `json:"inputTokensIncludeCache"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return TokenUsageEntry{}, err
	}
	timestamp, err := subagentSessionStart(event)
	if err != nil {
		return TokenUsageEntry{}, err
	}
	entry := TokenUsageEntry{
		Provider: data.Provider, Model: data.Model, Purpose: data.Purpose, Path: data.Path,
		AgentName: optionalJSONString(data.AgentName), CallerSource: optionalJSONString(data.CallerSource),
		XInitiator: firstNonEmptyModel(data.XInitiator, data.XInitiatorSnake),
		RequestID:  data.RequestID, Timestamp: timestamp.Format(time.RFC3339Nano), DurationMs: data.DurationMs,
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens: data.Usage.InputTokens, OutputTokens: data.Usage.OutputTokens,
			CacheReadTokens: data.Usage.CacheReadInputTokens, CacheWriteTokens: data.Usage.CacheCreationInputTokens,
			ReasoningTokens: data.Usage.ReasoningOutputTokens,
		},
		AICreditsThisResponse: data.AIC, AICreditsTotal: data.TotalAIC,
		InputTokensIncludeCache: data.Usage.InputTokensIncludeCache,
	}
	if data.Steering.valid() {
		entry.Steering = data.Steering
	}
	if data.SteeringDelivery.valid() {
		entry.SteeringDelivery = data.SteeringDelivery
	}
	return entry, nil
}

// optionalJSONString decodes an optional string field, treating any other JSON
// value as absent so a malformed caller field never rejects the usage record.
func optionalJSONString(raw json.RawMessage) string {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}
