//go:build !integration

package scanfindings_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/scanfindings"
)

// TestSpec_Types_SeverityLevel validates the documented severity vocabulary.
func TestSpec_Types_SeverityLevel(t *testing.T) {
	assert.Equal(t, "unknown", scanfindings.SeverityUnknown.String(), "unknown severity should use the documented canonical name")
	assert.Equal(t, "info", scanfindings.SeverityInfo.String(), "info severity should use the documented canonical name")
	assert.Equal(t, "low", scanfindings.SeverityLow.String(), "low severity should use the documented canonical name")
	assert.Equal(t, "medium", scanfindings.SeverityMedium.String(), "medium severity should use the documented canonical name")
	assert.Equal(t, "high", scanfindings.SeverityHigh.String(), "high severity should use the documented canonical name")
	assert.Equal(t, "critical", scanfindings.SeverityCritical.String(), "critical severity should use the documented canonical name")
}

// TestSpec_PublicAPI_ParseSeverity validates the documented behavior of ParseSeverity.
func TestSpec_PublicAPI_ParseSeverity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected scanfindings.SeverityLevel
	}{
		{name: "high is case insensitive", input: "High", expected: scanfindings.SeverityHigh},
		{name: "critical is case insensitive", input: "CRITICAL", expected: scanfindings.SeverityCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanfindings.ParseSeverity(tt.input)
			assert.Equal(t, tt.expected, result, "severity mismatch for: %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_SeverityOrdering validates the documented ordering helpers.
func TestSpec_PublicAPI_SeverityOrdering(t *testing.T) {
	assert.Greater(t, scanfindings.SeverityCritical.Rank(), scanfindings.SeverityHigh.Rank(), "critical should rank above high")
	assert.Greater(t, scanfindings.SeverityHigh.Rank(), scanfindings.SeverityMedium.Rank(), "high should rank above medium")
	assert.Greater(t, scanfindings.SeverityMedium.Rank(), scanfindings.SeverityLow.Rank(), "medium should rank above low")
	assert.Greater(t, scanfindings.SeverityLow.Rank(), scanfindings.SeverityInfo.Rank(), "low should rank above info")
	assert.Greater(t, scanfindings.SeverityInfo.Rank(), scanfindings.SeverityUnknown.Rank(), "info should rank above unknown")

	assert.True(t, scanfindings.SeverityHigh.AtLeast(scanfindings.SeverityMedium), "high should satisfy a medium threshold")
	assert.True(t, scanfindings.SeverityMedium.AtLeast(scanfindings.SeverityMedium), "equal severities should satisfy the threshold")
	assert.False(t, scanfindings.SeverityLow.AtLeast(scanfindings.SeverityHigh), "low should not satisfy a high threshold")
}

// TestSpec_PublicAPI_ErrorType validates the documented console error type mapping.
func TestSpec_PublicAPI_ErrorType(t *testing.T) {
	for _, severity := range []scanfindings.SeverityLevel{
		scanfindings.SeverityUnknown,
		scanfindings.SeverityInfo,
		scanfindings.SeverityLow,
		scanfindings.SeverityMedium,
		scanfindings.SeverityHigh,
		scanfindings.SeverityCritical,
	} {
		assert.Contains(t, []string{"error", "warning", "info"}, severity.ErrorType(),
			"ErrorType should return one of the documented console error types for %s", severity)
	}
}

// TestSpec_PublicAPI_NewFinding validates the documented conversion behavior.
func TestSpec_PublicAPI_NewFinding(t *testing.T) {
	params := scanfindings.NewFindingParams{
		RuleID:        "template-injection",
		SeverityLabel: "High",
		Message:       "supplied message",
		File:          ".github/workflows/demo.lock.yml",
		Line:          12,
		Column:        24,
	}

	finding := scanfindings.NewFinding(params)
	assert.Equal(t, params.RuleID, finding.RuleID, "NewFinding should preserve the supplied rule ID")
	assert.Equal(t, scanfindings.SeverityHigh, finding.Severity, "NewFinding should normalize severity case-insensitively")
	assert.Equal(t, params.Message, finding.Message, "NewFinding should preserve the supplied message")
	assert.Equal(t, params.File, finding.File, "NewFinding should preserve the supplied file")
	assert.Equal(t, params.Line, finding.Line, "NewFinding should preserve the supplied line")
	assert.Equal(t, params.Column, finding.Column, "NewFinding should preserve the supplied column")
}

// TestSpec_Types_Finding validates the documented Finding fields and conversion behavior.
func TestSpec_Types_Finding(t *testing.T) {
	finding := scanfindings.Finding{
		RuleID:   "template-injection",
		Severity: scanfindings.SeverityHigh,
		Message:  "[High] template-injection: template injection with untrusted input",
		File:     ".github/workflows/demo.lock.yml",
		Line:     12,
		Column:   24,
		Context:  []string{"line 11", "line 12", "line 13"},
	}

	err := finding.CompilerError()
	assert.Equal(t, console.CompilerError{
		Position: console.ErrorPosition{File: ".github/workflows/demo.lock.yml", Line: 12, Column: 24},
		Type:     "error",
		Message:  "[High] template-injection: template injection with untrusted input",
		Context:  []string{"line 11", "line 12", "line 13"},
	}, err, "compiler error should preserve the documented finding fields")
}

// TestSpec_PublicAPI_FormatMessage validates the documented message format.
func TestSpec_PublicAPI_FormatMessage(t *testing.T) {
	tests := []struct {
		name        string
		severity    string
		ruleID      string
		description string
		expected    string
	}{
		{name: "all parts included", severity: "High", ruleID: "template-injection", description: "template injection with untrusted input", expected: "[High] template-injection: template injection with untrusted input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanfindings.FormatMessage(tt.severity, tt.ruleID, tt.description)
			assert.Equal(t, tt.expected, result, "formatted message mismatch for: %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_Render validates the documented shared console rendering behavior.
func TestSpec_PublicAPI_Render(t *testing.T) {
	findings := []scanfindings.Finding{{
		Severity: scanfindings.SeverityHigh,
		Message:  scanfindings.FormatMessage("High", "template-injection", "template injection with untrusted input"),
		File:     ".github/workflows/demo.lock.yml",
		Line:     12,
		Column:   24,
	}}

	var buf bytes.Buffer
	scanfindings.Render(&buf, findings)

	expected := console.FormatError(findings[0].CompilerError())
	assert.Equal(t, expected, buf.String(), "rendered output should use the shared console format")
}

// TestSpec_PublicAPI_Sort validates the documented stable ordering criteria.
func TestSpec_PublicAPI_Sort(t *testing.T) {
	findings := []scanfindings.Finding{
		{File: "b.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh},
		{File: "a.yml", Line: 2, Column: 1, Severity: scanfindings.SeverityLow},
		{File: "a.yml", Line: 1, Column: 2, Severity: scanfindings.SeverityLow},
		{File: "a.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "b"},
		{File: "a.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityCritical, RuleID: "z"},
		{File: "a.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "a"},
	}

	scanfindings.Sort(findings)

	assert.Equal(t, []scanfindings.Finding{
		{File: "a.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityCritical, RuleID: "z"},
		{File: "a.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "a"},
		{File: "a.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "b"},
		{File: "a.yml", Line: 1, Column: 2, Severity: scanfindings.SeverityLow},
		{File: "a.yml", Line: 2, Column: 1, Severity: scanfindings.SeverityLow},
		{File: "b.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh},
	}, findings)

	tied := []scanfindings.Finding{
		{File: "same.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "same", Message: "first"},
		{File: "same.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "same", Message: "second"},
	}
	scanfindings.Sort(tied)
	assert.Equal(t, []scanfindings.Finding{
		{File: "same.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "same", Message: "first"},
		{File: "same.yml", Line: 1, Column: 1, Severity: scanfindings.SeverityHigh, RuleID: "same", Message: "second"},
	}, tied, "findings with equal sort keys should retain their input order")
}

// TestSpec_PublicAPI_CountAtLeast validates the documented severity threshold count.
func TestSpec_PublicAPI_CountAtLeast(t *testing.T) {
	findings := []scanfindings.Finding{
		{Severity: scanfindings.SeverityCritical},
		{Severity: scanfindings.SeverityHigh},
		{Severity: scanfindings.SeverityMedium},
		{Severity: scanfindings.SeverityUnknown},
	}

	assert.Equal(t, 2, scanfindings.CountAtLeast(findings, scanfindings.SeverityHigh))
}
