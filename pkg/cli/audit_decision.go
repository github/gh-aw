package cli

import (
	"encoding/json"
	"strconv"
	"strings"
)

// AuditDecision is advisory invocation evidence, not an engine retry or authorization.
type AuditDecision struct {
	ProposedAttempt   int                     `json:"proposedAttempt"`
	StartupAttemptID  string                  `json:"startupAttemptId,omitempty"`
	Outcome           string                  `json:"outcome"`
	Questions         []AuditDecisionQuestion `json:"questions"`
	LatencyMs         int64                   `json:"latencyMs"`
	Timestamp         int64                   `json:"timestamp"`
	Error             string                  `json:"error,omitempty"`
	AvoidedInvocation bool                    `json:"avoidedInvocation"`
	Phase             string                  `json:"phase"`
	Path              string                  `json:"path"`
	Index             *int                    `json:"index,omitempty"`
}

type AuditDecisionQuestion struct {
	Question int    `json:"question"`
	Outcome  string `json:"outcome"`
	Error    string `json:"error,omitempty"`
}

func validDecisionError(value string) bool {
	switch value {
	case "", "invalid_configuration", "sandbox_unavailable", "proxy_unavailable",
		"invalid_proxy_endpoint", "model_unavailable", "timeout", "evaluation_failed", "file_reference_failed", "sensitive_content_blocked":
		return true
	default:
		return false
	}
}

func parseAuditDecision(event sessionModelRoutingEvent) *AuditDecision {
	if event.Provenance.Phase != "agent" {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(event.Data, &fields) != nil {
		return nil
	}
	for _, name := range []string{"proposedAttempt", "outcome", "questions", "latencyMs", "timestamp", "avoidedInvocation"} {
		value := fields[name]
		if len(value) == 0 || string(value) == "null" {
			return nil
		}
	}
	var decision AuditDecision
	if json.Unmarshal(event.Data, &decision) != nil || decision.ProposedAttempt < 1 || decision.ProposedAttempt > 101 ||
		decision.LatencyMs < 0 || decision.Timestamp < 0 || len(decision.Questions) > 400 || !validDecisionError(decision.Error) {
		return nil
	}
	if decision.StartupAttemptID != "" {
		raw, ok := strings.CutPrefix(decision.StartupAttemptID, "startup:")
		number, err := strconv.Atoi(raw)
		if !ok || err != nil || number < 1 || number > 101 || strconv.Itoa(number) != raw {
			return nil
		}
	}
	outcome := "unknown"
	for index, question := range decision.Questions {
		if question.Question != index+1 || !validDecisionError(question.Error) {
			return nil
		}
		switch question.Outcome {
		case "deny":
			outcome = "deny"
		case "allow":
			if outcome != "deny" {
				outcome = "allow"
			}
		case "unknown":
		default:
			return nil
		}
	}
	if decision.Outcome != outcome || decision.AvoidedInvocation != (outcome == "deny") {
		return nil
	}
	decision.Phase, decision.Path, decision.Index = event.Provenance.Phase, event.Provenance.Path, event.Provenance.Index
	return &decision
}
