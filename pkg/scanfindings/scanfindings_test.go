package scanfindings

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type failingFindingsWriter struct {
	writes int
}

func (w *failingFindingsWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("output unavailable")
}

func TestRenderStopsOnWriteError(t *testing.T) {
	w := &failingFindingsWriter{}
	Render(w, []Finding{{Message: "first"}, {Message: "second"}})
	if w.writes != 1 {
		t.Errorf("Render attempted %d writes after output failed, want 1", w.writes)
	}
}

func TestNewFinding(t *testing.T) {
	fileLines := []string{"one", "two", "three", "four", "five"}
	tests := []struct {
		name     string
		severity string
		line     int
		column   int
		lines    []string
		want     Finding
	}{
		{
			name: "normalizes severity and attaches context", severity: " High ",
			line: 3, column: 7, lines: fileLines,
			want: Finding{
				RuleID: "rule", Severity: SeverityHigh, Message: "native message",
				File: "workflow.yml", Line: 3, Column: 7, Context: fileLines,
			},
		},
		{
			name: "preserves absent coordinates", severity: "",
			want: Finding{
				RuleID: "rule", Severity: SeverityUnknown, Message: "native message",
				File: "workflow.yml",
			},
		},
		{
			name: "preserves negative coordinates", severity: "custom",
			line: -1, column: -2, lines: fileLines,
			want: Finding{
				RuleID: "rule", Severity: SeverityUnknown, Message: "native message",
				File: "workflow.yml", Line: -1, Column: -2,
			},
		},
		{
			name: "out of range has no context", severity: "note",
			line: 9, column: 1, lines: fileLines,
			want: Finding{
				RuleID: "rule", Severity: SeverityInfo, Message: "native message",
				File: "workflow.yml", Line: 9, Column: 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewFinding(NewFindingParams{
				RuleID: "rule", SeverityLabel: tt.severity, Message: "native message",
				File: "workflow.yml", Line: tt.line, Column: tt.column, FileLines: tt.lines,
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewFinding() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		raw  string
		want SeverityLevel
	}{
		{"Critical", SeverityCritical},
		{"critical", SeverityCritical},
		{"High", SeverityHigh},
		{"error", SeverityHigh},
		{"Medium", SeverityMedium},
		{"warning", SeverityMedium},
		{"moderate", SeverityMedium},
		{"Low", SeverityLow},
		{"Negligible", SeverityLow},
		{"Informational", SeverityInfo},
		{"note", SeverityInfo},
		{"info", SeverityInfo},
		{" High ", SeverityHigh},
		{"Unknown", SeverityUnknown},
		{"", SeverityUnknown},
		{"bogus", SeverityUnknown},
	}

	for _, tt := range tests {
		if got := ParseSeverity(tt.raw); got != tt.want {
			t.Errorf("ParseSeverity(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestSeverityErrorType(t *testing.T) {
	tests := []struct {
		severity SeverityLevel
		want     string
	}{
		{SeverityCritical, "error"},
		{SeverityHigh, "error"},
		{SeverityMedium, "warning"},
		{SeverityLow, "info"},
		{SeverityInfo, "info"},
		{SeverityUnknown, "warning"},
	}

	for _, tt := range tests {
		if got := tt.severity.ErrorType(); got != tt.want {
			t.Errorf("%q.ErrorType() = %q, want %q", tt.severity, got, tt.want)
		}
	}
}

func TestSeverityAtLeast(t *testing.T) {
	if !SeverityCritical.AtLeast(SeverityHigh) {
		t.Error("critical should be at least high")
	}
	if !SeverityHigh.AtLeast(SeverityHigh) {
		t.Error("high should be at least high")
	}
	if SeverityMedium.AtLeast(SeverityHigh) {
		t.Error("medium should not be at least high")
	}
	if SeverityUnknown.AtLeast(SeverityInfo) {
		t.Error("unknown should not be at least info")
	}
}

func TestFindingCompilerError(t *testing.T) {
	finding := Finding{
		RuleID:   "template-injection",
		Severity: SeverityHigh,
		Message:  "[High] template-injection: bad",
		File:     "workflow.lock.yml",
		Line:     12,
		Column:   4,
	}

	compilerErr := finding.CompilerError()
	if compilerErr.Type != "error" {
		t.Errorf("expected type error, got %q", compilerErr.Type)
	}
	if compilerErr.Position.File != "workflow.lock.yml" || compilerErr.Position.Line != 12 || compilerErr.Position.Column != 4 {
		t.Errorf("unexpected position: %+v", compilerErr.Position)
	}
	if compilerErr.Message != finding.Message {
		t.Errorf("unexpected message: %q", compilerErr.Message)
	}
}

func TestFindingCompilerErrorDefaultsPosition(t *testing.T) {
	compilerErr := Finding{Message: "no location"}.CompilerError()
	if compilerErr.Position.Line != 1 || compilerErr.Position.Column != 1 {
		t.Errorf("expected line and column to default to 1, got %+v", compilerErr.Position)
	}
}

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		severity, rule, description string
		want                        string
	}{
		{"High", "RGS-001", "Unsafe runner", "[High] RGS-001: Unsafe runner"},
		{"warning", "rule", "", "[warning] rule"},
		{"", "rule", "detail", "rule: detail"},
		{"error", "", "detail", "[error] detail"},
	}

	for _, tt := range tests {
		if got := FormatMessage(tt.severity, tt.rule, tt.description); got != tt.want {
			t.Errorf("FormatMessage(%q, %q, %q) = %q, want %q", tt.severity, tt.rule, tt.description, got, tt.want)
		}
	}
}

func TestContextLines(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five", "six"}

	got := ContextLines(lines, 4)
	want := []string{"two", "three", "four", "five", "six"}
	if len(got) != len(want) {
		t.Fatalf("ContextLines(4) returned %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ContextLines(4)[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := ContextLines(lines, 1); len(got) != 1 || got[0] != "one" {
		t.Errorf("ContextLines(1) = %v, want [one]", got)
	}
	if got := ContextLines(lines, 2); len(got) != 3 || got[1] != "two" {
		t.Errorf("ContextLines(2) = %v, want [one two three]", got)
	}
	if got := ContextLines(lines, len(lines)); len(got) != 1 || got[0] != "six" {
		t.Errorf("ContextLines(last) = %v, want [six]", got)
	}
	if got := ContextLines(lines, 0); got != nil {
		t.Errorf("ContextLines(0) = %v, want nil", got)
	}
	if got := ContextLines(lines, len(lines)+1); got != nil {
		t.Errorf("ContextLines(out of range) = %v, want nil", got)
	}
	if got := ContextLines(nil, 1); got != nil {
		t.Errorf("ContextLines(nil) = %v, want nil", got)
	}
}

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, []Finding{
		{Severity: SeverityHigh, Message: "boom", File: "a.yml", Line: 3, Column: 2},
	})

	output := buf.String()
	if !strings.Contains(output, "a.yml:3:2") {
		t.Errorf("expected rendered position in output, got %q", output)
	}
	if !strings.Contains(output, "boom") {
		t.Errorf("expected message in output, got %q", output)
	}
}
