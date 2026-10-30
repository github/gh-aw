package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AuditRetrySummary separates main-agent retries from detection retries.
type AuditRetrySummary struct {
	AgentRetries     int                 `json:"agent_retries"`
	DetectionRetries int                 `json:"detection_retries"`
	Attempts         []AuditRetryAttempt `json:"attempts"`
}

// AuditRetryAttempt retains the observed retry history without changing accounting.
type AuditRetryAttempt struct {
	Attempt          int      `json:"attempt"`
	AttemptID        string   `json:"attemptId,omitempty"`
	StartupAttemptID string   `json:"startupAttemptId,omitempty"`
	ExitCode         *int     `json:"exitCode"`
	FailureClasses   []string `json:"failureClasses"`
	StartedAt        *float64 `json:"startedAt,omitempty"`
	FinishedAt       *float64 `json:"finishedAt,omitempty"`
	Strategy         string   `json:"strategy,omitempty"`
	BackoffMs        *float64 `json:"backoffMs,omitempty"`
	Outcome          string   `json:"outcome,omitempty"`
	Layer            string   `json:"layer"`
	Phase            string   `json:"phase"`
	Path             string   `json:"path"`
}

func (attempt AuditRetryAttempt) startupScope() string {
	if attempt.StartupAttemptID != "" {
		return attempt.StartupAttemptID
	}
	if prefix, _, ok := strings.Cut(attempt.AttemptID, ":harness:"); ok && strings.HasPrefix(prefix, "startup:") {
		return prefix
	}
	return ""
}

func readAuditRetrySummary(runDir string) *AuditRetrySummary {
	attribution, _, err := readSessionModelRouting(runDir)
	if err != nil {
		auditReportLog.Printf("Failed to read retry evidence from unified session: %v", err)
		return nil
	}
	if attribution == nil {
		return nil
	}
	return buildAuditRetrySummary(attribution.RetryEvents)
}

func buildAuditRetrySummary(events []sessionModelRoutingEvent) *AuditRetrySummary {
	if len(events) == 0 {
		return nil
	}
	invocation := func(event sessionModelRoutingEvent, layer, startupID string) string {
		return fmt.Sprintf("%q/%q/%q/%q", event.Provenance.Phase, event.Provenance.Path, layer, startupID)
	}
	canonical := make(map[string]bool)
	for _, event := range events {
		var attempt AuditRetryAttempt
		if event.Type == "harness.attempt" && json.Unmarshal(event.Data, &attempt) == nil {
			canonical[fmt.Sprintf("%s/%d", invocation(event, attempt.Layer, attempt.startupScope()), attempt.Attempt)] = true
		}
	}
	report := &AuditRetrySummary{}
	indices := make(map[string]int)
	add := func(event sessionModelRoutingEvent, attempt AuditRetryAttempt, layer string) {
		if attempt.Attempt < 1 || (layer != "harness" && layer != "startup") {
			return
		}
		attempt.Layer, attempt.Phase, attempt.Path = layer, event.Provenance.Phase, event.Provenance.Path
		id := fmt.Sprint(attempt.Attempt)
		if attempt.AttemptID != "" {
			id = attempt.AttemptID
		}
		key := invocation(event, layer, attempt.startupScope()) + "/" + id
		if index, exists := indices[key]; exists {
			report.Attempts[index] = attempt
		} else {
			indices[key] = len(report.Attempts)
			report.Attempts = append(report.Attempts, attempt)
		}
	}
	for _, event := range events {
		if event.Type == "harness.attempt" {
			var attempt AuditRetryAttempt
			if json.Unmarshal(event.Data, &attempt) == nil {
				add(event, attempt, attempt.Layer)
			}
			continue
		}
		var summary struct {
			Attempts []AuditRetryAttempt `json:"attemptResults"`
			Startup  []AuditRetryAttempt `json:"startupAttemptResults"`
		}
		if json.Unmarshal(event.Data, &summary) != nil {
			continue
		}
		for _, group := range []struct {
			layer    string
			attempts []AuditRetryAttempt
		}{{"harness", summary.Attempts}, {"startup", summary.Startup}} {
			for _, attempt := range group.attempts {
				if !canonical[fmt.Sprintf("%s/%d", invocation(event, group.layer, attempt.startupScope()), attempt.Attempt)] {
					add(event, attempt, group.layer)
				}
			}
		}
	}
	for _, attempt := range report.Attempts {
		if attempt.Attempt <= 1 {
			continue
		}
		switch attempt.Phase {
		case "agent":
			report.AgentRetries++
		case "detection":
			report.DetectionRetries++
		}
	}
	if len(report.Attempts) == 0 {
		return nil
	}
	return report
}

func renderConsoleRetries(report *AuditRetrySummary) {
	if report != nil {
		fmt.Fprintf(os.Stderr, "  retries: agent=%d detection=%d attempts=%d\n", report.AgentRetries, report.DetectionRetries, len(report.Attempts))
	}
}
