// Package scanfindings provides a shared representation of the findings reported
// by the scanner integrations (zizmor, poutine, grype, grant, runner-guard,
// yamllint, ...).
//
// Each scanner speaks its own native JSON dialect, with severities spelled in a
// different vocabulary ("High", "error", "Negligible", "note", ...) and locations
// shaped differently. Integrations decode their native output into their own
// structs and then map those structs onto the shared Finding type declared here,
// so that severity classification, ordering and rendering are implemented once
// instead of once per tool.
package scanfindings

import (
	"fmt"
	"io"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

// SeverityLevel is the shared severity vocabulary used by every scanner
// integration. Native severity labels are normalized with ParseSeverity.
type SeverityLevel string

const (
	// SeverityUnknown is used when a tool reports no severity, or one that
	// cannot be mapped onto the shared vocabulary.
	SeverityUnknown SeverityLevel = "unknown"
	// SeverityInfo covers informational findings ("info", "note", "notice").
	SeverityInfo SeverityLevel = "info"
	// SeverityLow covers low impact findings ("low", "negligible", "minor").
	SeverityLow SeverityLevel = "low"
	// SeverityMedium covers medium impact findings ("medium", "moderate", "warning").
	SeverityMedium SeverityLevel = "medium"
	// SeverityHigh covers high impact findings ("high", "error").
	SeverityHigh SeverityLevel = "high"
	// SeverityCritical covers the most severe findings ("critical").
	SeverityCritical SeverityLevel = "critical"
)

// ParseSeverity normalizes a native scanner severity label onto the shared
// vocabulary. Comparison is case-insensitive and unrecognized labels (including
// the empty string) map to SeverityUnknown.
func ParseSeverity(raw string) SeverityLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical", "crit":
		return SeverityCritical
	case "high", "error", "err":
		return SeverityHigh
	case "medium", "moderate", "warning", "warn":
		return SeverityMedium
	case "low", "negligible", "minor":
		return SeverityLow
	case "info", "information", "informational", "note", "notice":
		return SeverityInfo
	default:
		return SeverityUnknown
	}
}

// String returns the canonical lowercase name of the severity.
func (s SeverityLevel) String() string {
	return string(s)
}

// Rank returns the relative ordering of a severity, with higher values meaning
// more severe. Unknown severities rank lowest.
func (s SeverityLevel) Rank() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// AtLeast reports whether the severity is at least as severe as min.
func (s SeverityLevel) AtLeast(min SeverityLevel) bool {
	return s.Rank() >= min.Rank()
}

// ErrorType maps the severity onto the console error type used when rendering a
// finding as a console.CompilerError. Unknown severities are rendered as
// warnings so that unclassified findings remain visible.
func (s SeverityLevel) ErrorType() string {
	switch s {
	case SeverityCritical, SeverityHigh:
		return "error"
	case SeverityLow, SeverityInfo:
		return "info"
	default:
		return "warning"
	}
}

// Finding is the shared, tool-independent representation of a single scanner
// finding. Message holds the already-formatted, human readable description of
// the finding as produced by the owning integration.
type Finding struct {
	RuleID   string        `json:"rule_id,omitempty"`
	Severity SeverityLevel `json:"severity,omitempty"`
	Message  string        `json:"message"`
	File     string        `json:"file,omitempty"`
	Line     int           `json:"line,omitempty"`
	Column   int           `json:"column,omitempty"`
	// Context holds the source lines surrounding the finding, used when
	// rendering the finding to a terminal. It is optional.
	Context []string `json:"-"`
}

// CompilerError converts the finding into the console error format shared by all
// scanner output. Missing line and column values default to 1 so that the
// rendered position stays well formed.
func (f Finding) CompilerError() console.CompilerError {
	line := f.Line
	if line <= 0 {
		line = 1
	}
	column := f.Column
	if column <= 0 {
		column = 1
	}

	return console.CompilerError{
		Position: console.ErrorPosition{
			File:   f.File,
			Line:   line,
			Column: column,
		},
		Type:    f.Severity.ErrorType(),
		Message: f.Message,
		Context: f.Context,
	}
}

// FormatMessage builds the standard "[severity] rule: description" message used
// by the scanner integrations. Empty parts are omitted.
func FormatMessage(severityLabel, ruleID, description string) string {
	severityPart := ""
	if severityLabel != "" {
		severityPart = fmt.Sprintf("[%s]", severityLabel)
	}

	bodyPart := ""
	switch {
	case ruleID != "" && description != "":
		bodyPart = fmt.Sprintf("%s: %s", ruleID, description)
	case ruleID != "":
		bodyPart = ruleID
	case description != "":
		bodyPart = description
	}

	return strings.TrimSpace(strings.Join([]string{severityPart, bodyPart}, " "))
}

// Render writes the findings to w using the shared console error format.
func Render(w io.Writer, findings []Finding) {
	for _, finding := range findings {
		fmt.Fprint(w, console.FormatError(finding.CompilerError()))
	}
}

// ContextLines returns a symmetric window of up to two source lines before and
// after the 1-based line number. The window shrinks at file boundaries to keep
// the target line at its midpoint for context rendering. It returns nil when
// the line is out of range for the provided file lines.
func ContextLines(fileLines []string, line int) []string {
	if len(fileLines) == 0 || line <= 0 || line > len(fileLines) {
		return nil
	}

	window := min(2, line-1, len(fileLines)-line)
	start := line - window
	end := line + window

	context := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		context = append(context, fileLines[i-1])
	}
	return context
}
