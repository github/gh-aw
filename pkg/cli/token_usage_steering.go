package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func countAPIProxySteeringEvents(runDir string) int {
	events, err := extractGatewaySteeringEvents(runDir)
	if err != nil {
		tokenUsageLog.Printf("Failed to parse API proxy steering events in %s: %v", runDir, err)
		return 0
	}
	return len(events)
}

// scanSteeringEntries reads all valid steering proxyEventsEntry records from r.
// Lines that fail the quick-keyword check or JSON decoding are silently skipped.
// The caller is responsible for the lifetime of r.
func scanSteeringEntries(r io.Reader) ([]proxyEventsEntry, error) {
	var entries []proxyEventsEntry
	scanner := bufio.NewScanner(r)
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
		if isSteeringEvent(entry.eventName(), strings.TrimSpace(entry.Message)) {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

func parseAPIProxySteeringEvents(filePath string) ([]GatewaySteeringEvent, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := scanSteeringEntries(file)
	if err != nil {
		return nil, err
	}
	return gatewaySteeringEventsFromEntries(entries), nil
}

func extractGatewaySteeringEvents(runDir string) ([]GatewaySteeringEvent, error) {
	eventsPath := findAPIProxyEventsFile(runDir)
	if eventsPath == "" {
		return nil, nil
	}

	return parseAPIProxySteeringEvents(eventsPath)
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
	switch eventName {
	case tokenSteeringEventName:
		return strings.HasPrefix(message, awfTokenWarningPrefix)
	case timeoutSteeringEventName:
		return strings.HasPrefix(message, awfTimeWarningPrefix)
	default:
		return false
	}
}

// eventName returns the normalised event name from whichever field is populated.
func (e proxyEventsEntry) eventName() string {
	for _, v := range []string{e.Event, e.Type, e.EventNameSnake, e.EventNameCamel} {
		if v = strings.TrimSpace(v); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}
