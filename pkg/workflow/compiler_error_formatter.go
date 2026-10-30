package workflow

import (
	"errors"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var compilerErrorLog = logger.New("workflow:compiler_error_formatter")

// wrappedCompilerError carries the formatted diagnostic string (returned by Error())
// and the original underlying error (returned by Unwrap()), preserving the error chain
// for errors.Is/As callers while keeping the displayed string free of duplication.
type wrappedCompilerError struct {
	formatted string
	cause     error
}

func (e *wrappedCompilerError) Error() string { return e.formatted }
func (e *wrappedCompilerError) Unwrap() error { return e.cause }

// formatCompilerError creates a formatted compiler error at line 1, column 1.
// Use this when the exact source position is unknown; IDE tooling can still navigate to the file.
// Use formatCompilerErrorWithPosition when a specific line/column is available.
// When cause is a *WorkflowValidationError with Line > 0, the error's own position is used
// instead of the default 1:1, giving precise IDE-navigable locations.
//
// filePath: the file path to include in the error (typically markdownPath or lockFile)
// errType: the error type ("error" or "warning")
// message: the error message text
// cause: optional underlying error to wrap (use nil for validation errors)
func formatCompilerError(filePath string, errType string, message string, cause error) error {
	line, column := 1, 1
	// Promote precise source location from WorkflowValidationError when available so that
	// the emitted "file:line:col: error:" prefix points directly at the problematic field
	// rather than always defaulting to line 1, column 1.
	var vErr *WorkflowValidationError
	if errors.As(cause, &vErr) && vErr.Line > 0 {
		line = vErr.Line
		if vErr.Column > 0 {
			column = vErr.Column
		}
		if vErr.File != "" {
			filePath = vErr.File
		}
	}
	return formatCompilerErrorWithPosition(filePath, line, column, errType, message, cause)
}

// isFormattedCompilerError reports whether err is already a console-formatted compiler error
// produced by formatCompilerError, formatCompilerErrorWithPosition, or parser.FormatImportError.
// Use this instead of fragile string-contains checks to avoid double-wrapping.
func isFormattedCompilerError(err error) bool {
	var wce *wrappedCompilerError
	if errors.As(err, &wce) {
		return true
	}
	// Also detect errors from the parser package (e.g. FormatImportError) which are already
	// console-formatted with source location and must not be re-wrapped.
	var fpe *parser.FormattedParserError
	return errors.As(err, &fpe)
}

// formatCompilerErrorWithPosition creates a formatted compiler error with specific line/column position.
//
// filePath: the file path to include in the error
// line: the line number where the error occurred
// column: the column number where the error occurred
// errType: the error type ("error" or "warning")
// message: the error message text
// cause: optional underlying error to wrap (use nil for validation errors)
func formatCompilerErrorWithPosition(filePath string, line int, column int, errType string, message string, cause error) error {
	compilerErrorLog.Printf("Formatting compiler error: file=%s, line=%d, column=%d, type=%s, message=%s", filePath, line, column, errType, message)
	formattedErr := console.FormatErrorStderr(console.CompilerError{
		Position: console.ErrorPosition{
			File:   filePath,
			Line:   line,
			Column: column,
		},
		Type:    errType,
		Message: message,
	})

	// Always return a *wrappedCompilerError so isFormattedCompilerError can detect it.
	// cause may be nil for validation errors that have no underlying cause.
	return &wrappedCompilerError{formatted: formattedErr, cause: cause}
}

// formatCompilerErrorWithContext creates a formatted compiler error with specific
// line/column position and source-code context lines for Rust-style rendering.
// Use this when source context is available; otherwise use formatCompilerErrorWithPosition.
//
// filePath: the file path to include in the error
// line: the line number where the error occurred
// column: the column number where the error occurred
// errType: the error type ("error" or "warning")
// message: the error message text
// cause: optional underlying error to wrap (use nil for validation errors)
// context: source code lines around the error for Rust-like snippet rendering
func formatCompilerErrorWithContext(filePath string, line int, column int, errType string, message string, cause error, context []string) error {
	compilerErrorLog.Printf("Formatting compiler error with context: file=%s, line=%d, column=%d, type=%s, context=%d lines", filePath, line, column, errType, len(context))
	formattedErr := console.FormatErrorStderr(console.CompilerError{
		Position: console.ErrorPosition{
			File:   filePath,
			Line:   line,
			Column: column,
		},
		Type:    errType,
		Message: message,
		Context: context,
	})

	return &wrappedCompilerError{formatted: formattedErr, cause: cause}
}

// formatCompilerMessage creates a formatted compiler message string (for warnings printed to stderr)
// filePath: the file path to include in the message (typically markdownPath or lockFile)
// msgType: the message type ("error" or "warning")
// message: the message text
func formatCompilerMessage(filePath string, msgType string, message string) string {
	return console.FormatErrorStderr(console.CompilerError{
		Position: console.ErrorPosition{
			File:   filePath,
			Line:   0,
			Column: 0,
		},
		Type:    msgType,
		Message: message,
	})
}

// emitTipOnce prints each distinct tip only once across workflows compiled by c.
func (c *Compiler) emitTipOnce(filePath, message string) {
	if c.quiet || c.tipsShown[message] {
		return
	}
	fmt.Fprintln(os.Stderr, formatCompilerMessage(filePath, "info", message))
	c.tipsShown[message] = true
}
