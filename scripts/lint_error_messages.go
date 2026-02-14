package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// QualityIssue represents a quality issue with an error message.
type QualityIssue struct {
	File       string
	Line       int
	Issue      string
	Suggestion string
}

// FileStats tracks statistics for a single file.
type FileStats struct {
	Total     int
	Compliant int
	Issues    []QualityIssue
}

var (
	// Patterns to detect good error messages.
	hasExample  = regexp.MustCompile(`(?i)\bexample:\s`)
	hasExpected = regexp.MustCompile(`(?i)\b(expected|valid|must be|should be)\b`)

	// Patterns for error types that MUST have examples.
	isValidationError = regexp.MustCompile(`(?i)\b(invalid|must|cannot|missing|required|unknown|duplicate|unsupported)\b`)
	isFormatError     = regexp.MustCompile(`(?i)\bformat\b`)
	isTypeError       = regexp.MustCompile(`(?i)\b(must be|got %T|expected type)\b`)
	isEnumError       = regexp.MustCompile(`(?i)\b(valid (options|values|engines|modes|levels)|one of)\b`)

	// Patterns for errors that can skip examples.
	isWrappedError = regexp.MustCompile(`%w`)
	hasDocLink     = regexp.MustCompile(`https?://`)
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-test" {
		// Test mode - used by tests
		os.Exit(0)
	}

	fmt.Println("🔍 Error Message Quality Linter")
	fmt.Println()

	// Parse directories
	dirs := []string{"pkg/workflow", "pkg/cli"}

	allStats := make(map[string]*FileStats)
	totalMessages := 0
	totalCompliant := 0

	for _, dir := range dirs {
		fmt.Printf("Analyzing error messages in %s/...\n", dir)

		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				stats := analyzeFile(path)
				if stats.Total > 0 {
					allStats[path] = stats
					totalMessages += stats.Total
					totalCompliant += stats.Compliant
				}
			}

			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking directory %s: %v\n", dir, err)
			os.Exit(1)
		}
	}

	fmt.Println()

	// Print file-by-file results
	sortedFiles := make([]string, 0, len(allStats))
	for file := range allStats {
		sortedFiles = append(sortedFiles, file)
	}
	sort.Strings(sortedFiles)

	issueCount := 0
	for _, file := range sortedFiles {
		stats := allStats[file]
		compliance := 0
		if stats.Total > 0 {
			compliance = (stats.Compliant * 100) / stats.Total
		}

		if len(stats.Issues) == 0 {
			fmt.Printf("✓ %s: %d/%d compliant (100%%)\n", file, stats.Compliant, stats.Total)
		} else {
			fmt.Printf("✗ %s: %d/%d compliant (%d%%)\n", file, stats.Compliant, stats.Total, compliance)

			// Show first 3 issues per file to avoid overwhelming output
			maxIssues := 3
			for i, issue := range stats.Issues {
				if i >= maxIssues {
					remaining := len(stats.Issues) - maxIssues
					fmt.Printf("  ... and %d more issue(s)\n", remaining)
					break
				}
				fmt.Printf("  - Line %d: %s\n", issue.Line, issue.Issue)
				if issue.Suggestion != "" {
					fmt.Printf("    Suggestion: %s\n", issue.Suggestion)
				}
			}
			issueCount += len(stats.Issues)
		}
	}

	// Print summary
	fmt.Println()
	fmt.Println("📊 Summary:")
	fmt.Printf("  Total error messages: %d\n", totalMessages)
	fmt.Printf("  Compliant: %d (%d%%)\n", totalCompliant, (totalCompliant*100)/max(totalMessages, 1))
	fmt.Printf("  Non-compliant: %d (%d%%)\n", totalMessages-totalCompliant, ((totalMessages-totalCompliant)*100)/max(totalMessages, 1))
	fmt.Printf("  Total issues: %d\n", issueCount)
	fmt.Println()

	// Check threshold
	threshold := 80
	if len(os.Args) > 1 {
		_, err := fmt.Sscanf(os.Args[1], "%d", &threshold)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid threshold value '%s', using default 80%%\n", os.Args[1])
			threshold = 80
		}
	}

	compliancePercentage := (totalCompliant * 100) / max(totalMessages, 1)

	if compliancePercentage >= threshold {
		fmt.Printf("✅ Meets quality threshold (%d%%)\n", threshold)
		os.Exit(0)
	} else {
		fmt.Printf("❌ Below quality threshold (%d%% < %d%%)\n", compliancePercentage, threshold)
		os.Exit(1)
	}
}

func analyzeFile(path string) *FileStats {
	stats := &FileStats{
		Total:     0,
		Compliant: 0,
		Issues:    []QualityIssue{},
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return stats
	}

	ast.Inspect(node, func(n ast.Node) bool {
		// Look for function calls
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check if it's fmt.Errorf or errors.New
		var isErrorCall bool
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			// fmt.Errorf, errors.New, etc.
			if ident, ok := fun.X.(*ast.Ident); ok {
				if (ident.Name == "fmt" && fun.Sel.Name == "Errorf") ||
					(ident.Name == "errors" && fun.Sel.Name == "New") {
					isErrorCall = true
				}
			}
		}

		if !isErrorCall || len(call.Args) == 0 {
			return true
		}

		// Extract the error message format string
		var messageStr string
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			// Remove quotes
			messageStr = lit.Value[1 : len(lit.Value)-1]
			// Unescape basic sequences
			messageStr = strings.ReplaceAll(messageStr, "\\n", "\n")
			messageStr = strings.ReplaceAll(messageStr, "\\t", "\t")
			messageStr = strings.ReplaceAll(messageStr, "\\\"", "\"")
		} else {
			// Skip non-literal strings (computed error messages)
			return true
		}

		stats.Total++

		// Check if this error should have an example
		pos := fset.Position(call.Pos())
		issue := checkErrorQuality(messageStr, pos.Line)

		if issue != nil {
			stats.Issues = append(stats.Issues, QualityIssue{
				File:       path,
				Line:       pos.Line,
				Issue:      issue.Issue,
				Suggestion: issue.Suggestion,
			})
		} else {
			stats.Compliant++
		}

		return true
	})

	return stats
}

func checkErrorQuality(message string, line int) *QualityIssue {
	// Check if this is an error that can skip quality checks
	if shouldSkipQualityCheck(message) {
		return nil
	}

	// Check if this is a validation/configuration error
	needsExample := isValidationError.MatchString(message) ||
		isFormatError.MatchString(message) ||
		isTypeError.MatchString(message) ||
		isEnumError.MatchString(message)

	if !needsExample {
		// Not a validation error, so it's compliant
		return nil
	}

	// Check for quality markers
	hasEx := hasExample.MatchString(message)
	hasExp := hasExpected.MatchString(message)

	// Validation errors should have examples
	if !hasEx {
		suggestion := suggestImprovement(message)
		return &QualityIssue{
			Issue:      "Missing example for validation error",
			Suggestion: suggestion,
		}
	}

	// If it has an example, check if it also explains what's expected
	if !hasExp && !hasExample.MatchString(message) {
		return &QualityIssue{
			Issue:      "Missing expected format/values explanation",
			Suggestion: "Add 'Expected:' or 'Valid values:' before the example",
		}
	}

	return nil
}

func shouldSkipQualityCheck(message string) bool {
	// Skip wrapped errors
	if isWrappedError.MatchString(message) {
		return true
	}

	// Skip errors with documentation links
	if hasDocLink.MatchString(message) {
		return true
	}

	// Skip very short errors (but not if they contain validation keywords)
	lowerMsg := strings.ToLower(message)
	if len(message) < 20 && !isValidationError.MatchString(message) {
		return true
	}

	// Skip errors that are self-explanatory (short ones only)
	selfExplanatoryPatterns := []string{
		"duplicate",
		"not found",
		"already exists",
	}

	for _, pattern := range selfExplanatoryPatterns {
		if strings.Contains(lowerMsg, pattern) && len(message) < 50 {
			return true
		}
	}

	// Skip simple "empty X" errors
	if strings.Contains(lowerMsg, "empty") && len(message) < 30 {
		return true
	}

	return false
}

func suggestImprovement(message string) string {
	lowerMsg := strings.ToLower(message)

	// Suggest based on error type
	if strings.Contains(lowerMsg, "invalid") && strings.Contains(lowerMsg, "format") {
		return "Add example of correct format: Example: field: \"value\""
	}

	if strings.Contains(lowerMsg, "must be") || strings.Contains(lowerMsg, "got %t") {
		return "Add example showing correct type: Example: field: 123"
	}

	if strings.Contains(lowerMsg, "invalid") && (strings.Contains(lowerMsg, "engine") ||
		strings.Contains(lowerMsg, "mode") || strings.Contains(lowerMsg, "level")) {
		return "List valid options and add example: Valid values: option1, option2. Example: field: option1"
	}

	if strings.Contains(lowerMsg, "missing") || strings.Contains(lowerMsg, "required") {
		return "Show example with required field: Example: field: \"value\""
	}

	return "Add 'Example:' section showing correct usage"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
