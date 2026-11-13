package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var threatDetectionSuppressionRulePattern = regexp.MustCompile(`^CTR-\d{3}$`)

type ThreatDetectionSuppression struct {
	Rule    string `json:"rule"`
	Reason  string `json:"reason"`
	Expires string `json:"expires,omitempty"`
}

type threatDetectionDiagnosticError struct {
	Rule string
	Err  error
}

func (e *threatDetectionDiagnosticError) Error() string {
	return fmt.Sprintf("%s: %v", e.Rule, e.Err)
}

func (e *threatDetectionDiagnosticError) Unwrap() error {
	return e.Err
}

func validateThreatDetectionSuppressions(suppressions []ThreatDetectionSuppression) error {
	for i, suppression := range suppressions {
		if !threatDetectionSuppressionRulePattern.MatchString(suppression.Rule) {
			return fmt.Errorf("threat-detection-suppress[%d].rule must be a CTR-* identifier. Example: rule: CTR-001", i)
		}
		if strings.TrimSpace(suppression.Reason) == "" {
			return fmt.Errorf("threat-detection-suppress[%d].reason must not be empty. Example: reason: \"accepted risk for internal tool\"", i)
		}
		if suppression.Expires != "" {
			expires, err := time.Parse("2006-01-02", suppression.Expires)
			if err != nil || expires.Format("2006-01-02") != suppression.Expires {
				return fmt.Errorf("threat-detection-suppress[%d].expires must be an ISO 8601 date. Example: expires: 2026-12-31", i)
			}
		}
	}
	return nil
}

func parseThreatDetectionSuppressions(value any) ([]ThreatDetectionSuppression, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal threat-detection-suppress: %w", err)
	}
	var suppressions []ThreatDetectionSuppression
	if err := json.Unmarshal(data, &suppressions); err != nil {
		return nil, fmt.Errorf("parse threat-detection-suppress: %w", err)
	}
	if err := validateThreatDetectionSuppressions(suppressions); err != nil {
		return nil, err
	}
	return suppressions, nil
}

// activeThreatDetectionSuppressions treats expires as the last active UTC
// calendar day; a suppression becomes inactive at 00:00 UTC the following day.
func activeThreatDetectionSuppressions(suppressions []ThreatDetectionSuppression, now time.Time) []ThreatDetectionSuppression {
	active := make([]ThreatDetectionSuppression, 0, len(suppressions))
	today := now.UTC().Format("2006-01-02")
	for _, suppression := range suppressions {
		if suppression.Expires == "" || suppression.Expires >= today {
			active = append(active, suppression)
		}
	}
	return active
}

func isThreatDetectionRuleSuppressed(suppressions []ThreatDetectionSuppression, rule string, now time.Time) bool {
	for _, suppression := range activeThreatDetectionSuppressions(suppressions, now) {
		if suppression.Rule == rule {
			return true
		}
	}
	return false
}

func isThreatDetectionDiagnosticSuppressed(err error, suppressions []ThreatDetectionSuppression, now time.Time) bool {
	var diagnosticErr *threatDetectionDiagnosticError
	return errors.As(err, &diagnosticErr) &&
		isThreatDetectionRuleSuppressed(suppressions, diagnosticErr.Rule, now)
}
