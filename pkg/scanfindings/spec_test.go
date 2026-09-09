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
		{name: "warning maps to medium", input: "warning", expected: scanfindings.SeverityMedium},
		{name: "negligible maps to low", input: "Negligible", expected: scanfindings.SeverityLow},
		{name: "note maps to info", input: "note", expected: scanfindings.SeverityInfo},
		{name: "empty string maps to unknown", input: "", expected: scanfindings.SeverityUnknown},
		{name: "unknown label maps to unknown", input: "urgent", expected: scanfindings.SeverityUnknown},
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
	assert.Equal(t, "error", scanfindings.SeverityCritical.ErrorType(), "critical severities should render as console errors")
	assert.Equal(t, "error", scanfindings.SeverityHigh.ErrorType(), "high severities should render as console errors")
	assert.Equal(t, "warning", scanfindings.SeverityMedium.ErrorType(), "medium severities should render as console warnings")
	assert.Equal(t, "info", scanfindings.SeverityLow.ErrorType(), "low severities should render as console info")
	assert.Equal(t, "info", scanfindings.SeverityInfo.ErrorType(), "info severities should render as console info")
	assert.Equal(t, "warning", scanfindings.SeverityUnknown.ErrorType(), "unknown severities should remain visible as warnings")
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

// TestSpec_PublicAPI_FindingCompilerErrorDefaults validates the documented default position handling.
func TestSpec_PublicAPI_FindingCompilerErrorDefaults(t *testing.T) {
	err := (scanfindings.Finding{Message: "message"}).CompilerError()
	assert.Equal(t, 1, err.Position.Line, "missing line should default to 1")
	assert.Equal(t, 1, err.Position.Column, "missing column should default to 1")
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
		{name: "empty severity omitted", ruleID: "template-injection", description: "description", expected: "template-injection: description"},
		{name: "empty rule id omitted", severity: "note", description: "description", expected: "[note] description"},
		{name: "empty description omitted", severity: "warning", ruleID: "rule-1", expected: "[warning] rule-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanfindings.FormatMessage(tt.severity, tt.ruleID, tt.description)
			assert.Equal(t, tt.expected, result, "formatted message mismatch for: %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ContextLines validates the documented context window behavior.
func TestSpec_PublicAPI_ContextLines(t *testing.T) {
	lines := []string{"1", "2", "3", "4", "5", "6"}

	assert.Equal(t, []string{"2", "3", "4", "5", "6"}, scanfindings.ContextLines(lines, 4), "middle lines should include up to two lines before and after")
	assert.Equal(t, []string{"1"}, scanfindings.ContextLines(lines, 1), "first line should shrink the window at the start boundary")
	assert.Nil(t, scanfindings.ContextLines(lines, 0), "line zero should be out of range")
	assert.Nil(t, scanfindings.ContextLines(lines, 7), "lines beyond the file should be out of range")
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
