//go:build integration

package workflow

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
)

var agenticsWorkflowDirectivePattern = regexp.MustCompile(`^<!-- agentics-workflow: ([a-z0-9][a-z0-9-]*\.md) -->$`)

func TestDocumentationGalleryWorkflowsCompile(t *testing.T) {
	galleryDir := filepath.Join("..", "..", "docs", "src", "content", "docs", "gallery")
	entries, err := os.ReadDir(galleryDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || entry.Name() == "index.md" || entry.Name() == "multi-repo.md" || entry.Name() == "maintaining-repos.md" || entry.Name() == "dependency-analysis.md" {
			continue
		}

		t.Run(strings.TrimSuffix(entry.Name(), ".md"), func(t *testing.T) {
			pagePath := filepath.Join(galleryDir, entry.Name())
			content, readErr := os.ReadFile(pagePath)
			if readErr != nil {
				t.Fatal(readErr)
			}

			workflowName, workflowContent, extractErr := extractPrimaryExampleWorkflow(string(content))
			directiveWorkflowName, directiveErr := extractAgenticsWorkflowName(string(content))
			if directiveErr != nil {
				t.Fatal(directiveErr)
			}
			if extractErr == nil && directiveWorkflowName != "" {
				t.Fatal("gallery page must use either an inline primary workflow or an agentics workflow directive, not both")
			}
			if extractErr == nil {
				compileExampleWorkflow(t, workflowName, []byte(workflowContent), pagePath)
				return
			}
			if directiveWorkflowName == "" {
				t.Fatal(extractErr)
			}

			const rawURLPrefix = "https://raw.githubusercontent.com/githubnext/agentics/main/workflows/"
			fetchAndCompileExampleWorkflow(t, directiveWorkflowName, rawURLPrefix+directiveWorkflowName)
		})
	}

	t.Run("repo-assist-upstream", func(t *testing.T) {
		const (
			rawURL = "https://raw.githubusercontent.com/githubnext/agentics/main/workflows/repo-assist.md"
		)

		fetchAndCompileExampleWorkflow(t, "repo-assist.md", rawURL)
	})
}

func fetchAndCompileExampleWorkflow(t *testing.T, workflowName, rawURL string) {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	response, fetchErr := client.Get(rawURL)
	if fetchErr != nil {
		t.Fatalf("fetch %s: %v", rawURL, fetchErr)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fetch %s: %s", rawURL, response.Status)
	}

	workflowContent, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		t.Fatalf("read %s: %v", rawURL, readErr)
	}
	compileExampleWorkflow(t, workflowName, workflowContent, rawURL)
}

func TestExtractAgenticsWorkflowName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr string
	}{
		{
			name:    "valid directive",
			content: "<!-- agentics-workflow: ci-doctor.md -->",
			want:    "ci-doctor.md",
		},
		{
			name:    "no directive",
			content: "# No workflow",
		},
		{
			name:    "multiple directives",
			content: "<!-- agentics-workflow: ci-doctor.md -->\n<!-- agentics-workflow: issue-triage.md -->",
			wantErr: "expected exactly one agentics workflow directive, found 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractAgenticsWorkflowName(tt.content)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("extractAgenticsWorkflowName() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("extractAgenticsWorkflowName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func extractAgenticsWorkflowName(content string) (string, error) {
	var workflowName string
	directiveCount := 0

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		match := agenticsWorkflowDirectivePattern.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}

		directiveCount++
		workflowName = match[1]
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", scanErr
	}
	if directiveCount > 1 {
		return "", fmt.Errorf("expected exactly one agentics workflow directive, found %d", directiveCount)
	}

	return workflowName, nil
}

func compileExampleWorkflow(t *testing.T, workflowName string, workflowContent []byte, source string) {
	t.Helper()
	tempDir := testutil.TempDir(t, "docs-example")
	workflowPath := filepath.Join(tempDir, workflowName)
	if writeErr := os.WriteFile(workflowPath, workflowContent, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	compiler := NewCompiler()
	compiler.SetWorkflowIdentifier(workflowName)
	if compileErr := compiler.CompileWorkflow(workflowPath); compileErr != nil {
		t.Fatalf("compile primary workflow from %s: %v", source, compileErr)
	}
}

func extractPrimaryExampleWorkflow(content string) (string, string, error) {
	const titlePrefix = `title=".github/workflows/`

	var workflowName string
	var workflowLines []string
	inPrimaryWorkflow := false
	primaryWorkflowCount := 0

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if !inPrimaryWorkflow {
			if !strings.HasPrefix(line, "```aw ") || !strings.Contains(line, titlePrefix) {
				continue
			}

			nameStart := strings.Index(line, titlePrefix) + len(titlePrefix)
			nameEnd := strings.Index(line[nameStart:], `"`)
			if nameEnd < 0 {
				return "", "", fmt.Errorf("primary workflow title is missing a closing quote")
			}
			workflowName = line[nameStart : nameStart+nameEnd]
			if filepath.Ext(workflowName) != ".md" || filepath.Base(workflowName) != workflowName {
				return "", "", fmt.Errorf("primary workflow title must name a .github/workflows/*.md file")
			}

			primaryWorkflowCount++
			inPrimaryWorkflow = true
			workflowLines = nil
			continue
		}

		if line == "```" {
			inPrimaryWorkflow = false
			continue
		}
		workflowLines = append(workflowLines, line)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", "", scanErr
	}
	if inPrimaryWorkflow {
		return "", "", fmt.Errorf("primary workflow code block is not closed")
	}
	if primaryWorkflowCount != 1 {
		return "", "", fmt.Errorf("expected exactly one primary workflow code block, found %d", primaryWorkflowCount)
	}

	return workflowName, strings.Join(workflowLines, "\n") + "\n", nil
}
