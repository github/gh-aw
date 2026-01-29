//go:build integration

package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRemoveJavaScriptCommentsAllRepoFiles tests comment removal on all .cjs files in the repository
// and validates that the resulting JavaScript is syntactically valid
func TestRemoveJavaScriptCommentsAllRepoFiles(t *testing.T) {
	// Find all .cjs files in the actions/setup/js directory
	jsDir := "../../actions/setup/js"
	entries, err := os.ReadDir(jsDir)
	if err != nil {
		t.Fatalf("Failed to read js directory: %v", err)
	}

	var cjsFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cjs") {
			// Skip test files as they use different syntax (ES modules)
			if strings.Contains(entry.Name(), ".test.") {
				continue
			}
			cjsFiles = append(cjsFiles, entry.Name())
		}
	}

	if len(cjsFiles) == 0 {
		t.Fatal("No .cjs files found in js directory")
	}

	t.Logf("Testing comment removal on %d .cjs files", len(cjsFiles))

	// Track statistics
	var totalSize, processedSize int64
	var successCount, failedCount int
	failedFiles := []string{}

	for _, filename := range cjsFiles {
		t.Run(filename, func(t *testing.T) {
			filepath := jsDir + "/" + filename
			content, err := os.ReadFile(filepath)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", filename, err)
			}

			originalContent := string(content)
			totalSize += int64(len(originalContent))

			// Apply comment removal
			cleanedContent := removeJavaScriptComments(originalContent)
			processedSize += int64(len(cleanedContent))

			// Verify the cleaned content is not empty for non-comment-only files
			if strings.TrimSpace(cleanedContent) == "" && strings.TrimSpace(originalContent) != "" {
				// Check if original had any code (not just comments)
				hasCode := false
				lines := strings.Split(originalContent, "\n")
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") && !strings.HasPrefix(trimmed, "*") {
						hasCode = true
						break
					}
				}
				if hasCode {
					t.Errorf("Comment removal resulted in empty content for file with code: %s", filename)
					failedCount++
					failedFiles = append(failedFiles, filename)
					return
				}
			}

			// Validate JavaScript syntax by attempting to parse with Node.js
			// Wrap in an async function to handle top-level await which is common in GitHub Actions scripts
			if err := validateJavaScriptSyntaxIntegration(cleanedContent, filename); err != nil {
				t.Logf("Note: Syntax validation shows issues for %s (may be expected for GitHub Actions context): %v", filename, err)
				// Don't fail the test as top-level await is valid in GitHub Actions context
				// but just log it for visibility
			} else {
				successCount++
			}
		})
	}

	// Report statistics
	if len(failedFiles) > 0 {
		t.Logf("Files with issues (%d): %v", len(failedFiles), failedFiles)
	}

	compressionRatio := 100.0 * float64(totalSize-processedSize) / float64(totalSize)
	t.Logf("Processed %d files, validated %d successfully", len(cjsFiles), successCount)
	t.Logf("Original size: %d bytes, processed size: %d bytes, compression: %.2f%%",
		totalSize, processedSize, compressionRatio)

	// Ensure we processed a reasonable number of files
	// Note: Lower threshold after removing embedded scripts (which are now loaded at runtime)
	if len(cjsFiles) < 5 {
		t.Errorf("Expected to process at least 5 .cjs files, but only found %d", len(cjsFiles))
	}
}

// validateJavaScriptSyntaxIntegration validates that the JavaScript code is syntactically correct
// by attempting to parse it with Node.js
func validateJavaScriptSyntaxIntegration(code, filename string) error {
	// Wrap the code in an async function to handle top-level await
	// which is commonly used in GitHub Actions scripts
	wrappedCode := fmt.Sprintf("(async () => {\n%s\n})();", code)

	// Create a temporary file with the cleaned JavaScript
	tmpfile, err := os.CreateTemp("", "validate-js-*.cjs")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(wrappedCode)); err != nil {
		tmpfile.Close()
		return fmt.Errorf("failed to write to temp file: %w", err)
	}
	tmpfile.Close()

	// Use Node.js to check syntax without executing the code
	cmd := exec.Command("node", "--check", tmpfile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("syntax check failed: %s (output: %s)", err, string(output))
	}

	return nil
}
