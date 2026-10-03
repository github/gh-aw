package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		return
	}
	summary.SteeringEventCounts = log.eventCounts
	summary.TotalSteeringEvents = 0
	for _, count := range log.eventCounts {
		summary.TotalSteeringEvents += count
	}
}

func parseAPIProxySteeringLog(filePath string) (*apiProxySteeringLog, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer file.Close()

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
	if err != nil || log == nil {
		return nil, err
	}
	if len(log.entries) == 0 {
		tokenUsageLog.Printf("No steering entries found in %s", runDir)
		return nil, nil
	}

	return gatewaySteeringEventsFromEntries(log.entries), nil
}

func gatewaySteeringEventsFromEntries(entries []proxyEventsEntry) []GatewaySteeringEvent {
	events := make([]GatewaySteeringEvent, 0, len(entries))
	for _, entry := range entries {
		events = append(events, GatewaySteeringEvent{
			Type:      entry.eventName(),
			Message:   strings.TrimSpace(entry.Message),
			Timestamp: entry.Timestamp,
		})
	}
	return events
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
