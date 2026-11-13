//go:build !integration

package console_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/console"
)

func TestSpec_PublicAPI_FormatMessagePrefixes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	tests := []struct {
		name   string
		format func(string) string
		input  string
		marker string
	}{
		{name: "command", format: console.FormatCommandMessage, input: "gh aw compile", marker: "$"},
		{name: "info", format: console.FormatInfoMessage, input: "details", marker: "i"},
		{name: "progress", format: console.FormatProgressMessage, input: "working", marker: "▸"},
		{name: "prompt", format: console.FormatPromptMessage, input: "continue", marker: "?"},
		{name: "success", format: console.FormatSuccessMessage, input: "complete", marker: "✓"},
		{name: "verbose", format: console.FormatVerboseMessage, input: "trace", marker: "»"},
		{name: "warning", format: console.FormatWarningMessage, input: "careful", marker: "⚠"},
		{name: "error", format: console.FormatErrorMessage, input: "failed", marker: "✗"},
		{name: "list item", format: console.FormatListItem, input: "entry", marker: "•"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.format(tt.input)
			assert.Contains(t, got, tt.marker, "formatted message should contain its documented prefix")
			assert.Contains(t, got, tt.input, "formatted message should retain its input")
		})
	}
}

func TestSpec_PublicAPI_FormatError(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := console.FormatError(console.CompilerError{
		Position: console.ErrorPosition{File: "workflow.md", Line: 12, Column: 3},
		Type:     "error",
		Message:  "invalid workflow",
		Context:  []string{"on: invalid"},
		Hint:     "use a supported trigger",
	})
	for _, expected := range []string{"workflow.md", "12", "3", "error", "invalid workflow", "on: invalid", "use a supported trigger"} {
		assert.Contains(t, got, expected, "formatted compiler error should retain documented content %q", expected)
	}
}

func TestSpec_PublicAPI_FormatErrorWithSuggestions(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := console.FormatErrorWithSuggestions("invalid configuration", []string{"check the schema", "review the example"})
	for _, expected := range []string{"invalid configuration", "check the schema", "review the example"} {
		assert.Contains(t, got, expected, "formatted suggestions should retain documented content %q", expected)
	}
}

func TestSpec_PublicAPI_FormatErrorChain(t *testing.T) {
	root := errors.New("root cause")
	wrapped := fmt.Errorf("outer context: %w", root)
	got := console.FormatErrorChain(wrapped)
	assert.Contains(t, got, "outer context", "formatted chain should include the outer error")
	assert.Contains(t, got, "root cause", "formatted chain should include the unwrapped error")
	assert.Contains(t, got, "\n", "formatted chain should be readable across multiple lines")
}

func TestSpec_DesignDecision_RenderStructTags(t *testing.T) {
	type Overview struct {
		Name     string `console:"header:Workflow"`
		Tokens   int    `console:"header:Token Count"`
		Optional string `console:"header:Optional,omitempty"`
		Internal string `console:"-"`
	}
	got := console.RenderStruct([]Overview{{Name: "agentic-token-audit", Tokens: 1200, Internal: "hidden"}})
	assert.Contains(t, got, "Workflow", "header tag should choose the documented heading")
	assert.Contains(t, got, "Token Count", "header tag should support multi-word headings")
	assert.Contains(t, got, "agentic-token-audit", "rendered struct should include field values")
	assert.NotContains(t, got, "Optional", "omitempty should omit a table column when every value is zero")
	assert.NotContains(t, got, "hidden", "dash tag should omit a field")

	mixed := console.RenderStruct([]Overview{
		{Name: "first", Optional: "visible"},
		{Name: "second"},
	})
	assert.Contains(t, mixed, "Optional", "omitempty should retain a table column when any row has a value")
	assert.Contains(t, mixed, "visible", "retained omitempty columns should render non-zero values")
}

func TestSpec_DesignDecision_RenderStructZeroNestedStruct(t *testing.T) {
	type Inner struct {
		Value int `console:"header:Value"`
	}
	type Row struct {
		Inner Inner `console:"title:Inner Section,omitempty"`
	}

	got := console.RenderStruct(Row{})
	assert.Contains(t, got, "Inner Section", "omitempty should retain zero-valued nested structs outside slice tables")
	assert.Contains(t, got, "Value", "zero-valued nested struct fields should still render")
}

func TestSpec_PublicAPI_RenderTableOptionalSections(t *testing.T) {
	got := console.RenderTable(console.TableConfig{
		Headers:   []string{"Name", "Status"},
		Rows:      [][]string{{"build", "success"}},
		Title:     "Job Results",
		ShowTotal: true,
		TotalRow:  []string{"Total", "1"},
		TTYFunc:   func() bool { return false },
	})
	for _, expected := range []string{"Name", "Status", "build", "success", "Job Results", "Total"} {
		assert.Contains(t, got, expected, "rendered table should contain configured value %q", expected)
	}
}

func TestSpec_PublicAPI_ToRelativePath(t *testing.T) {
	cwd, err := filepath.Abs(".")
	require.NoError(t, err, "test should resolve its working directory")
	got := console.ToRelativePath(filepath.Join(cwd, "documented", "workflow.md"))
	assert.Equal(t, filepath.Join("documented", "workflow.md"), got, "path beneath cwd should be displayed relatively")
}

func TestSpec_Types_DocumentedTypesExist(t *testing.T) {
	assert.Equal(t, 80, console.DefaultTableWidth, "DefaultTableWidth should match the documented display-column budget")
	values := []any{
		console.CompilerError{},
		console.ErrorPosition{},
		console.FormField{},
		console.SelectOption{},
		console.TableConfig{},
		console.RenderOptions{},
		console.TreeNode{},
		console.NewListItem("title", "description", "value"),
		console.NewProgressBar(1),
		console.NewSpinner("message"),
	}
	for _, value := range values {
		assert.NotNil(t, value, "documented type or constructor result should exist")
	}
}

func TestSpec_ThreadSafety_TimeLocation(t *testing.T) {
	locations := []*time.Location{time.UTC, time.FixedZone("documented", 3600)}
	var wait sync.WaitGroup
	for i := range 20 {
		wait.Add(1)
		go func(location *time.Location) {
			defer wait.Done()
			console.SetTimeLocation(location)
			_ = console.RenderStruct(struct{ At time.Time }{At: time.Unix(0, 0)})
			console.ResetTimeLocation()
		}(locations[i%len(locations)])
	}
	wait.Wait()
}

// SPEC_AMBIGUITY: The README does not specify exact whitespace, ANSI styling,
// unit rounding, or unsupported-interactivity error messages, so these tests
// intentionally assert only the documented observable contracts.
