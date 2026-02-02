package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var frontmatterErrorLog = logger.New("workflow:frontmatter_error")

// createFrontmatterError creates a detailed error for frontmatter parsing issues
// frontmatterLineOffset is the line number where the frontmatter content begins (1-based)
func (c *Compiler) createFrontmatterError(filePath, content string, err error, frontmatterLineOffset int) error {
	frontmatterErrorLog.Printf("Creating frontmatter error for file: %s, offset: %d", filePath, frontmatterLineOffset)
	lines := strings.Split(content, "\n")

	// Check if error already contains formatted yaml.FormatError() output
	// yaml.FormatError() produces output like "\n[line:col] message\n  line | content..."
	errorStr := err.Error()
	if strings.Contains(errorStr, "failed to parse frontmatter:\n[") && strings.Contains(errorStr, "\n>") {
		// This is already formatted by yaml.FormatError(), return as-is with filename prefix
		frontmatterErrorLog.Print("Detected yaml.FormatError() formatted output, using as-is")
		return fmt.Errorf("%s: %v", filePath, err)
	}

	// Check if this is a YAML parsing error that we can enhance
	if strings.Contains(errorStr, "failed to parse frontmatter:") {
		frontmatterErrorLog.Print("Detected wrapped YAML parsing error")
		// Extract the inner YAML error
		parts := strings.SplitN(errorStr, "failed to parse frontmatter: ", 2)
		if len(parts) > 1 {
			yamlErr := errors.New(parts[1])
			line, column, message := parser.ExtractYAMLError(yamlErr, frontmatterLineOffset)

			if line > 0 || column > 0 {
				frontmatterErrorLog.Printf("Extracted YAML error at line %d, column %d", line, column)
				// Create context lines around the error
				var context []string
				startLine := max(1, line-2)
				endLine := min(len(lines), line+2)

				for i := startLine; i <= endLine; i++ {
					if i-1 < len(lines) {
						context = append(context, lines[i-1])
					}
				}

				compilerErr := console.CompilerError{
					Position: console.ErrorPosition{
						File:   filePath,
						Line:   line,
						Column: column,
					},
					Type:    "error",
					Message: fmt.Sprintf("frontmatter parsing failed: %s", message),
					Context: context,
					Hint:    "check YAML syntax in frontmatter section",
				}

				// Format and return the error
				formattedErr := console.FormatError(compilerErr)
				return errors.New(formattedErr)
			}
		}
	} else {
		frontmatterErrorLog.Print("Attempting direct YAML error extraction")
		// Try to extract YAML error directly from the original error
		line, column, message := parser.ExtractYAMLError(err, frontmatterLineOffset)

		if line > 0 || column > 0 {
			frontmatterErrorLog.Printf("Extracted YAML error at line %d, column %d", line, column)
			// Create context lines around the error
			var context []string
			startLine := max(1, line-2)
			endLine := min(len(lines), line+2)

			for i := startLine; i <= endLine; i++ {
				if i-1 < len(lines) {
					context = append(context, lines[i-1])
				}
			}

			compilerErr := console.CompilerError{
				Position: console.ErrorPosition{
					File:   filePath,
					Line:   line,
					Column: column, // Use original column, we'll extend to word in console rendering
				},
				Type:    "error",
				Message: fmt.Sprintf("frontmatter parsing failed: %s", message),
				Context: context,
				// Hints removed as per requirements
			}

			// Format and return the error
			formattedErr := console.FormatError(compilerErr)
			return errors.New(formattedErr)
		}
	}

	// Fallback to original error
	frontmatterErrorLog.Printf("Using fallback error message: %v", err)
	return fmt.Errorf("failed to extract frontmatter: %w", err)
}
