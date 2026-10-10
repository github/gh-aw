package workqueue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// IssuesSettings describes optional Issues projection, not queue authority.
// Nil Settings.Issues means disabled; true or {} uses the default "work" label.
type IssuesSettings struct {
	Label string `json:"label"`
}

func parseIssuesSettings(data []byte) (*IssuesSettings, error) {
	switch string(bytes.TrimSpace(data)) {
	case "false":
		return nil, nil
	case "true":
		return &IssuesSettings{Label: "work"}, nil
	}
	fields, err := settingsObject(data, "work_queue.issues", []string{"label"})
	if err != nil {
		return nil, fmt.Errorf("work_queue.issues: use true, false, or an object with an optional literal label: %w", err)
	}
	issues := &IssuesSettings{Label: "work"}
	if data, exists := fields["label"]; exists {
		var label string
		if json.Unmarshal(data, &label) != nil || strings.TrimSpace(label) == "" || len(label) > 33 ||
			strings.Contains(label, "${{") || strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return nil, settingsError("work_queue.issues.label", "must be a nonblank literal of at most 33 UTF-8 bytes without control characters or expressions")
		}
		issues.Label = label
	}
	return issues, nil
}
