//go:build !integration

package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
)

func TestEmitTipOnce(t *testing.T) {
	compiler := NewCompiler()
	const firstTip = "Tip: first suggestion"
	const secondTip = "Tip: second suggestion"

	compiler.SetQuiet(true)
	stderr := testutil.CaptureStderr(t, func() {
		compiler.emitTipOnce("quiet.md", firstTip)
	})
	assert.Empty(t, stderr)

	compiler.SetQuiet(false)
	stderr = testutil.CaptureStderr(t, func() {
		compiler.emitTipOnce("first.md", firstTip)
		compiler.emitTipOnce("first.md", firstTip)
		compiler.ResetWarningCount()
		compiler.emitTipOnce("second.md", firstTip)
		compiler.emitTipOnce("second.md", secondTip)
	})

	assert.Equal(t, 1, strings.Count(stderr, firstTip))
	assert.Equal(t, 1, strings.Count(stderr, secondTip))
	assert.Contains(t, stderr, "first.md")
	assert.Zero(t, compiler.GetWarningCount(), "tips should not count as warnings")
}

// TestIsFormattedCompilerError verifies that the helper correctly identifies
// errors produced by formatCompilerError / formatCompilerErrorWithPosition and
// returns false for other error types.
func TestIsFormattedCompilerError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "error from formatCompilerError with nil cause",
			err:      formatCompilerError("file.md", "error", "something went wrong", nil),
			expected: true,
		},
		{
			name:     "error from formatCompilerError with non-nil cause",
			err:      formatCompilerError("file.md", "error", "something went wrong", errors.New("root cause")),
			expected: true,
		},
		{
			name:     "error from formatCompilerErrorWithPosition",
			err:      formatCompilerErrorWithPosition("file.md", 5, 3, "error", "bad value", nil),
			expected: true,
		},
		{
			name: "error from parser.FormatImportError is detected as formatted",
			err: parser.FormatImportError(&parser.ImportError{
				ImportPath: "missing.md",
				FilePath:   "workflow.md",
				Line:       10,
				Column:     5,
				Cause:      errors.New("file not found: missing.md"),
			}, "imports:\n  - missing.md"),
			expected: true,
		},
		{
			name:     "error from parser.NewFormattedParserError is detected as formatted",
			err:      parser.NewFormattedParserError("workflow.md:5:3: error: bad value"),
			expected: true,
		},
		{
			name:     "plain error is not formatted",
			err:      errors.New("plain error"),
			expected: false,
		},
		{
			name:     "fmt.Errorf error is not formatted",
			err:      errors.New("wrapped: plain error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFormattedCompilerError(tt.err)
			assert.Equal(t, tt.expected, got,
				"isFormattedCompilerError(%v) should be %v", tt.err, tt.expected)
		})
	}
}
