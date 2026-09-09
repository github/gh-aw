package parser

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var importErrorLog = logger.New("parser:import_error")

// ImportError represents an error that occurred during import resolution
type ImportError struct {
	ImportPath string // The import path that failed (e.g., "nonexistent.md")
	FilePath   string // The workflow file containing the import
	Line       int    // Line number where the import is defined
	Column     int    // Column number where the import is defined
	Cause      error  // The underlying error
}

// ImportCycleError represents a circular import dependency
type ImportCycleError struct {
	Chain        []string // Full import chain showing the cycle (e.g., ["a.md", "b.md", "c.md", "d.md", "b.md"])
	WorkflowFile string   // The main workflow file being compiled
}

// Error returns the error message for ImportCycleError
func (e *ImportCycleError) Error() string {
	if len(e.Chain) < 2 {
		return "circular import detected. Imports must form a directed acyclic graph. Example: remove an import that completes the cycle"
	}
	importer := e.Chain[len(e.Chain)-2]
	imported := e.Chain[len(e.Chain)-1]
	return fmt.Sprintf(
		"circular import detected: %s. Imports must form a directed acyclic graph. Example: remove the import of %q from %q",
		strings.Join(e.Chain, " → "), imported, importer)
}

// FormatImportCycleError marks an import cycle error as ready for CLI display.
func FormatImportCycleError(err *ImportCycleError) error {
	importErrorLog.Printf("Formatting import cycle error: chain=%v, workflow=%s", err.Chain, err.WorkflowFile)
	return &FormattedParserError{formatted: err.Error(), cause: err}
}

// FormattedParserError is a sentinel error type returned by FormatImportError (and similar
// parser-level formatters) to signal that the error message is already console-formatted
// with source location.  Callers that detect this type must NOT re-wrap it, otherwise the
// user would see a double-formatted error message (e.g. one location at "engine:" wrapping
// another at the actual import line).
//
// This type is intentionally exported so that the workflow package's isFormattedCompilerError
// helper can detect it via errors.As without creating a circular import.
type FormattedParserError struct {
	formatted string // The complete console-formatted error string ready for display.
	cause     error  // The underlying error (e.g. ImportError.Cause) for errors.Is/As traversal.
}

func (e *FormattedParserError) Error() string { return e.formatted }
func (e *FormattedParserError) Unwrap() error { return e.cause }

// NewFormattedParserError creates a FormattedParserError with the given pre-formatted
// message string. Use this in external packages (e.g. pkg/workflow) to return an error
// that isFormattedCompilerError can detect without double-wrapping.
func NewFormattedParserError(formatted string) *FormattedParserError {
	return &FormattedParserError{formatted: formatted}
}

// FormatImportError formats an import error as a compilation error with source location
func FormatImportError(err *ImportError, yamlContent string) error {
	importErrorLog.Printf("Formatting import error: path=%s, file=%s, line=%d", err.ImportPath, err.FilePath, err.Line)

	lines := strings.Split(yamlContent, "\n")

	// Create context lines around the error
	var context []string
	startLine := max(1, err.Line-2)
	endLine := min(len(lines), err.Line+2)

	for i := startLine; i <= endLine; i++ {
		if i-1 < len(lines) {
			context = append(context, lines[i-1])
		}
	}

	// Determine the error message based on the cause
	message := "failed to resolve import"
	if err.Cause != nil {
		causeMsg := err.Cause.Error()
		if strings.Contains(causeMsg, "file not found") {
			message = "import file not found"
		} else if strings.Contains(causeMsg, "failed to download") {
			message = "failed to download import file"
		} else if strings.Contains(causeMsg, "failed to resolve ref") {
			message = "failed to resolve import reference"
		} else if strings.Contains(causeMsg, "invalid workflowspec") {
			message = "invalid import specification"
		} else {
			message = causeMsg
		}
	}

	hint := buildImportErrorHint(message, err.ImportPath)
	compilerErr := console.CompilerError{
		Position: console.ErrorPosition{
			File:   err.FilePath,
			Line:   err.Line,
			Column: err.Column,
		},
		Type:    "error",
		Message: message,
		Context: context,
		Hint:    hint,
	}

	formattedErr := console.FormatError(compilerErr)
	// Return a FormattedParserError so callers can detect that this error is already
	// console-formatted and must not be re-wrapped with additional location context.
	return &FormattedParserError{formatted: formattedErr, cause: err.Cause}
}

// buildImportErrorHint returns a tailored fix hint for an import error based on its message and path.
func buildImportErrorHint(message, importPath string) string {
	switch {
	case message == "failed to resolve import reference":
		return fmt.Sprintf("Verify the ref (branch, tag, or SHA) in `%s` is valid and accessible.", importPath)
	case strings.Contains(message, "file not found") || strings.Contains(message, "failed to resolve import"):
		return fmt.Sprintf("Ensure `%s` exists relative to the workflow directory, or check the `imports:` path.", importPath)
	case strings.Contains(message, "failed to download"):
		return fmt.Sprintf("Check that the remote import `%s` is accessible and the path is correct.", importPath)
	case strings.Contains(message, "invalid import specification"):
		return "Use the format `owner/repo/path@ref` for remote imports (e.g. `github/my-org/shared.md@main`)."
	default:
		return fmt.Sprintf("Check the `imports:` configuration for `%s` and review the error details.", importPath)
	}
}

// findImportsFieldLocation finds the line and column number of the imports field in YAML content
func findImportsFieldLocation(yamlContent string) (line int, column int) {
	lines := strings.Split(yamlContent, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Look for "imports:" at the start of a line (accounting for indentation)
		if strings.HasPrefix(trimmed, "imports:") {
			// Find the column where "imports:" starts
			col := strings.Index(line, "imports:") + 1 // +1 for 1-based indexing
			importErrorLog.Printf("Found imports field at line=%d, col=%d", i+1, col)
			return i + 1, col // +1 for 1-based line indexing
		}
	}
	// Default to line 1, column 1 if not found
	importErrorLog.Print("imports field not found in YAML content, defaulting to line=1, col=1")
	return 1, 1
}

// findImportItemLocation finds the line and column number of a specific import item in YAML content
func findImportItemLocation(yamlContent string, importPath string) (line int, column int) {
	importErrorLog.Printf("Locating import item in YAML: path=%s", importPath)
	lines := strings.Split(yamlContent, "\n")
	inImportsSection := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if we're entering the imports section
		if strings.HasPrefix(trimmed, "imports:") {
			inImportsSection = true
			continue
		}

		// If we're in the imports section and find a line with our import path
		if inImportsSection {
			// Check if this line exits the imports section (new top-level key)
			if line != "" && line[0] != ' ' && line[0] != '-' && line[0] != '\t' {
				break
			}

			// Check for the import path in this line
			if strings.Contains(line, importPath) {
				// Find the column where the import path starts
				col := strings.Index(line, importPath) + 1 // +1 for 1-based indexing
				importErrorLog.Printf("Located import item at line=%d, col=%d", i+1, col)
				return i + 1, col // +1 for 1-based line indexing
			}
		}
	}

	// Fallback to imports field location
	importErrorLog.Printf("Import item %q not found, falling back to imports field location", importPath)
	return findImportsFieldLocation(yamlContent)
}
