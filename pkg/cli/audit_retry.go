package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AuditRetrySummary separates main-agent retries from detection retries.
type AuditRetrySummary struct {
	AgentRetries           int                 `json:"agent_retries"`
	DetectionRetries       int                 `json:"detection_retries"`
	Attempts               []AuditRetryAttempt `json:"attempts"`
	EngineRetries          int                 `json:"engine_retries"`
	DetectionEngineRetries int                 `json:"detection_engine_retries"`
	EngineRetryHistory     []AuditEngineRetry  `json:"engine_retry_history,omitempty"`
}

// AuditEngineRetry is a native retry observation, not a harness invocation.
type AuditEngineRetry struct {
	Attempt          int      `json:"attempt"`
	MaxRetries       *int     `json:"maxRetries,omitempty"`
	BackoffMs        *float64 `json:"backoffMs,omitempty"`
	Phase            string   `json:"phase"`
	Path             string   `json:"path"`
	Index            *int     `json:"index,omitempty"`
	AttemptID        string   `json:"attemptId,omitempty"`
	StartupAttemptID string   `json:"startupAttemptId,omitempty"`
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

type auditRetryKey struct {
	phase, path, layer, startupID, id string
	attempt                           int
	startedAt                         float64
	timed                             bool
}

func auditRetrySequenceKey(event sessionModelRoutingEvent, attempt AuditRetryAttempt, layer string) auditRetryKey {
	return auditRetryKey{phase: event.Provenance.Phase, path: event.Provenance.Path, layer: layer, startupID: attempt.startupScope(), attempt: attempt.Attempt}
}

type auditRetryBuilder struct {
	report    *AuditRetrySummary
	canonical map[auditRetryKey]struct{}
	indices   map[auditRetryKey]int
}

func (builder *auditRetryBuilder) add(event sessionModelRoutingEvent, attempt AuditRetryAttempt, layer string) {
	if attempt.Attempt < 1 || (layer != "harness" && layer != "startup") {
		return
	}
	attempt.Layer, attempt.Phase, attempt.Path = layer, event.Provenance.Phase, event.Provenance.Path
	key := auditRetrySequenceKey(event, attempt, layer)
	key.id = attempt.AttemptID
	if attempt.StartedAt != nil {
		key.startedAt, key.timed = *attempt.StartedAt, true
	}
	if index, exists := builder.indices[key]; exists && index >= 0 && index < len(builder.report.Attempts) {
		builder.report.Attempts[index] = attempt
	} else {
		builder.indices[key] = len(builder.report.Attempts)
		builder.report.Attempts = append(builder.report.Attempts, attempt)
	}
}

func (builder *auditRetryBuilder) observeSummary(event sessionModelRoutingEvent) {
	var summary struct {
		Attempts []AuditRetryAttempt `json:"attemptResults"`
		Startup  []AuditRetryAttempt `json:"startupAttemptResults"`
	}
	if json.Unmarshal(event.Data, &summary) != nil {
		return
	}
	for _, group := range []struct {
		layer    string
		attempts []AuditRetryAttempt
	}{{"harness", summary.Attempts}, {"startup", summary.Startup}} {
		for _, attempt := range group.attempts {
			if _, exists := builder.canonical[auditRetrySequenceKey(event, attempt, group.layer)]; !exists {
				builder.add(event, attempt, group.layer)
			}
		}
	}
}

func (builder *auditRetryBuilder) observeEngine(event sessionModelRoutingEvent) {
	var payload struct {
		EngineRetry *AuditEngineRetry `json:"engineRetry"`
	}
	if json.Unmarshal(event.Data, &payload) != nil || payload.EngineRetry == nil || payload.EngineRetry.Attempt <= 0 {
		return
	}
	retry := *payload.EngineRetry
	retry.Phase, retry.Path, retry.Index = event.Provenance.Phase, event.Provenance.Path, event.Provenance.Index
	retry.AttemptID, retry.StartupAttemptID = event.Provenance.AttemptID, event.Provenance.StartupAttemptID
	builder.report.EngineRetryHistory = append(builder.report.EngineRetryHistory, retry)
	switch retry.Phase {
	case "agent":
		builder.report.EngineRetries++
	case "detection":
		builder.report.DetectionEngineRetries++
	}
}

func buildAuditRetrySummary(events []sessionModelRoutingEvent) *AuditRetrySummary {
	builder := auditRetryBuilder{report: &AuditRetrySummary{Attempts: []AuditRetryAttempt{}}, canonical: make(map[auditRetryKey]struct{}), indices: make(map[auditRetryKey]int)}
	for _, event := range events {
		var attempt AuditRetryAttempt
		if event.Type == "harness.attempt" && json.Unmarshal(event.Data, &attempt) == nil {
			builder.canonical[auditRetrySequenceKey(event, attempt, attempt.Layer)] = struct{}{}
		}
	}
	for _, event := range events {
		switch event.Type {
		case "session.result":
			builder.observeEngine(event)
		case "harness.attempt":
			var attempt AuditRetryAttempt
			if json.Unmarshal(event.Data, &attempt) == nil {
				builder.add(event, attempt, attempt.Layer)
			}
		case "harness.retry":
			builder.observeSummary(event)
		}
	}
	for _, attempt := range builder.report.Attempts {
		if attempt.Attempt <= 1 {
			continue
		}
		switch attempt.Phase {
		case "agent":
			builder.report.AgentRetries++
		case "detection":
			builder.report.DetectionRetries++
		}
	}
	if len(builder.report.Attempts) == 0 && len(builder.report.EngineRetryHistory) == 0 {
		return nil
	}
	return builder.report
}

func renderConsoleRetries(report *AuditRetrySummary) {
	if report != nil {
		fmt.Fprintf(os.Stderr, "  retries: agent=%d detection=%d attempts=%d engine=%d detection_engine=%d\n", report.AgentRetries, report.DetectionRetries, len(report.Attempts), report.EngineRetries, report.DetectionEngineRetries)
	}
}
