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
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
)

type apiProxySteeringLog struct {
	path        string
	eventCounts map[string]int
	entries     []proxyEventsEntry
}

func applyGatewaySteeringSummary(summary *TokenUsageSummary, runDir string) {
	if summary == nil {
		return
	}
	log, err := findAPIProxyEventsLog(runDir)
	if err != nil {
		tokenUsageLog.Printf("Failed to parse API proxy steering events in %s: %v", runDir, err)
		return
	}
	if log == nil {
		log, err = readUnifiedSteeringLog(runDir)
		if err != nil {
			tokenUsageLog.Printf("Failed to read unified steering events in %s: %v", runDir, err)
			return
		}
	}
	if log == nil {
		return
	}
	summary.SteeringEventCounts = log.eventCounts
	summary.TotalSteeringEvents = 0
	for _, count := range log.eventCounts {
		summary.TotalSteeringEvents += count
	}
}

// applySteeringNotices records the AWF steering notices delivered on model
// requests and every per-conversation delivery of them. The unified session is
// the source of truth: notices are read from the steering field, and deliveries
// from the steeringDelivery field, of firewall.token_usage events. Runs without a
// unified session or from AWF versions that do not record the fields leave the
// lists empty. When the unified session cannot be read, the notices are marked
// unavailable and a warning is recorded so that an empty list keeps meaning "no
// notices".
func applySteeringNotices(summary *TokenUsageSummary, runDir string) {
	if summary == nil {
		return
	}
	collected, err := readUnifiedTokenUsage(runDir)
	if err != nil {
		tokenUsageLog.Printf("Failed to read steering notices from unified session in %s: %v", runDir, err)
		summary.SteeringNotices = nil
		summary.SteeringDeliveries = nil
		summary.SteeringNoticesUnavailable = true
		addTokenUsageWarning(summary, "steering notices unavailable: failed to read unified session: "+err.Error())
		return
	}
	if len(collected.notices) > 0 || len(collected.deliveries) > 0 {
		tokenUsageLog.Printf("Found %d steering notice(s) and %d delivery(ies) in unified session", len(collected.notices), len(collected.deliveries))
	}
	summary.SteeringNotices = collected.notices
	summary.SteeringDeliveries = collected.deliveries
}

// tokenUsageSteeringDeliveries returns the steering notice deliveries recorded on
// a token usage summary, or nil when there is no summary.
func tokenUsageSteeringDeliveries(summary *TokenUsageSummary) []SteeringDelivery {
	if summary == nil {
		return nil
	}
	return summary.SteeringDeliveries
}

// tokenUsageSteeringNotices returns the steering notices recorded on a token
// usage summary, or nil when there is no summary.
func tokenUsageSteeringNotices(summary *TokenUsageSummary) []SteeringNotice {
	if summary == nil {
		return nil
	}
	return summary.SteeringNotices
}

// tokenUsageSteeringNoticesUnavailable reports whether the steering notices of a
// token usage summary could not be read.
func tokenUsageSteeringNoticesUnavailable(summary *TokenUsageSummary) bool {
	return summary != nil && summary.SteeringNoticesUnavailable
}

func parseAPIProxySteeringLog(filePath string) (*apiProxySteeringLog, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			tokenUsageLog.Printf("Failed to close steering log: %v", err)
		}
	}()

	log := &apiProxySteeringLog{
		path:        filePath,
		eventCounts: make(map[string]int),
	}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !containsSteeringKeyword(line) {
			continue
		}
		var entry proxyEventsEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		eventName := entry.eventName()
		if isSteeringEventName(eventName) {
			log.eventCounts[eventName]++
		}
		if isSteeringEvent(eventName, strings.TrimSpace(entry.Message)) {
			log.entries = append(log.entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	tokenUsageLog.Printf("Parsed %d steering event type(s) from %s", len(log.eventCounts), filePath)
	return log, nil
}

func extractGatewaySteeringEvents(runDir string) ([]GatewaySteeringEvent, error) {
	log, err := findAPIProxyEventsLog(runDir)
	if err == nil && log == nil {
		log, err = readUnifiedSteeringLog(runDir)
	}
	if err != nil || log == nil {
		return nil, err
	}
	if len(log.entries) == 0 {
		tokenUsageLog.Printf("No steering entries found in %s", runDir)
		return nil, nil
	}

	return gatewaySteeringEventsFromEntries(log.entries), nil
}

// readUnifiedSteeringLog uses the published session when the raw proxy event log
// is absent. Never merge the two sources: they describe the same proxy events.
// Cache successful results, including absent events, across summary and timeline
// consumers of the same run.
func readUnifiedSteeringLog(runDir string) (*apiProxySteeringLog, error) {
	cacheKey := filepath.Clean(runDir)
	if cached, ok := loadUnifiedSteeringLogCache(cacheKey); ok {
		return cached.log, nil
	}
	result, err, _ := unifiedSteeringLogGroup.Do(cacheKey, func() (any, error) {
		if cached, ok := loadUnifiedSteeringLogCache(cacheKey); ok {
			return cached, nil
		}
		log, err := parseUnifiedSteeringLog(cacheKey)
		if err != nil {
			return nil, err
		}
		entry := apiProxySteeringLogCacheEntry{log: log}
		unifiedSteeringLogCache.Store(cacheKey, entry)
		return entry, nil
	})
	if err != nil {
		return nil, err
	}
	entry, ok := result.(apiProxySteeringLogCacheEntry)
	if !ok {
		return nil, fmt.Errorf("unexpected unified steering log cache result type %T", result)
	}
	return entry.log, nil
}

var (
	unifiedSteeringLogCache sync.Map
	unifiedSteeringLogGroup singleflight.Group
)

func loadUnifiedSteeringLogCache(cacheKey string) (apiProxySteeringLogCacheEntry, bool) {
	cached, ok := unifiedSteeringLogCache.Load(cacheKey)
	if !ok {
		return apiProxySteeringLogCacheEntry{}, false
	}
	entry, ok := cached.(apiProxySteeringLogCacheEntry)
	if !ok {
		unifiedSteeringLogCache.Delete(cacheKey)
		return apiProxySteeringLogCacheEntry{}, false
	}
	return entry, true
}

func parseUnifiedSteeringLog(runDir string) (*apiProxySteeringLog, error) {
	path, err := findUnifiedSessionPath(runDir)
	if err != nil || path == "" {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			tokenUsageLog.Printf("Failed to close unified session: %v", err)
		}
	}()

	log := &apiProxySteeringLog{path: path, eventCounts: make(map[string]int)}
	reader := bufio.NewReader(file)
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			return nil, fmt.Errorf("oversized session record on line %d", lineNumber)
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var event sessionSubagentEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, fmt.Errorf("invalid session event on line %d: %w", lineNumber, err)
			}
			collectUnifiedSteeringEvent(log, event)
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("failed to read session: %w", readErr)
			}
			break
		}
	}
	if len(log.eventCounts) == 0 {
		return nil, nil
	}
	return log, nil
}

func collectUnifiedSteeringEvent(log *apiProxySteeringLog, event sessionSubagentEvent) {
	if event.Type != "firewall.steering" || event.Provenance.Component != "firewall" ||
		(event.Provenance.Phase != "agent" && event.Provenance.Phase != "detection") {
		return
	}
	var data struct {
		proxyEventsEntry
		RequestIDCamel string `json:"requestId"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return
	}
	name := data.eventName()
	if !isSteeringEventName(name) {
		return
	}
	log.eventCounts[name]++
	if !isSteeringEvent(name, strings.TrimSpace(data.Message)) {
		return
	}
	if data.RequestID == "" {
		data.RequestID = data.RequestIDCamel
	}
	if timestamp := unifiedSteeringTimestamp(event); timestamp != "" {
		data.Timestamp = timestamp
	}
	log.entries = append(log.entries, data.proxyEventsEntry)
}

func gatewaySteeringEventsFromEntries(entries []proxyEventsEntry) []GatewaySteeringEvent {
	events := make([]GatewaySteeringEvent, 0, len(entries))
	for _, entry := range entries {
		events = append(events, GatewaySteeringEvent{
			Type:         entry.eventName(),
			Message:      strings.TrimSpace(entry.Message),
			Timestamp:    entry.Timestamp,
			Threshold:    int(entry.Threshold),
			RequestID:    strings.TrimSpace(entry.RequestID),
			Trigger:      steeringEventTrigger(entry),
			Conversation: steeringEventConversation(entry),
		})
	}
	return events
}

// steeringEventTrigger returns the AWF trigger of a steering event: "threshold"
// or "little_left". Events from AWF v0.29.11 and older carry none and return "".
func steeringEventTrigger(entry proxyEventsEntry) string {
	switch trigger := optionalJSONString(entry.Trigger); trigger {
	case steeringTriggerThreshold, steeringTriggerLittleLeft:
		return trigger
	default:
		return ""
	}
}

// steeringEventConversation returns the opaque conversation key of a steering
// event, or "" when it is absent, null or malformed.
func steeringEventConversation(entry proxyEventsEntry) string {
	conversation, ok := steeringConversation(entry.Conversation)
	if !ok {
		return ""
	}
	return conversation
}

func containsSteeringKeyword(line string) bool {
	return strings.Contains(line, "steering") ||
		strings.Contains(line, "STEERING") ||
		strings.Contains(line, "Steering")
}

// isSteeringEvent matches AWF proxy steering events using both event name and
// message format from the firewall specification.
func isSteeringEvent(eventName, message string) bool {
	if !isSteeringEventName(eventName) {
		return false
	}
	switch eventName {
	case tokenSteeringEventName:
		return strings.HasPrefix(message, awfTokenWarningPrefix)
	case timeoutSteeringEventName:
		return strings.HasPrefix(message, awfTimeWarningPrefix)
	case aiCreditSteeringEventName:
		return strings.HasPrefix(message, awfAICreditWarningPrefix)
	default:
		return false
	}
}

func isSteeringEventName(eventName string) bool {
	return eventName == "steering" || strings.HasSuffix(eventName, "_steering")
}

// eventName returns the normalised event name from whichever field is populated.
func (e proxyEventsEntry) eventName() string {
	for _, v := range []string{e.Event, e.Type, e.EventNameSnake, e.EventNameCamel} {
		if v = strings.TrimSpace(v); v != "" {
			return strings.ToLower(v)
		}
	}
	if len(e.Payload) > 0 {
		var payload struct {
			Event string `json:"event"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return ""
		}
		for _, v := range []string{payload.Event, payload.Type} {
			if v = strings.TrimSpace(v); v != "" {
				return strings.ToLower(v)
			}
		}
	}
	return ""
}
