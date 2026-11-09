//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestSemanticFunctionRefactorWorkflowCostGuardrails(t *testing.T) {
	t.Parallel()

	repoWorkflowDir := filepath.Clean(filepath.Join("..", "..", ".github", "workflows"))
	sourcePath := filepath.Join(repoWorkflowDir, "semantic-function-refactor.md")
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("failed to read workflow source: %v", err)
	}

	testDir := testutil.TempDir(t, "semantic-function-refactor-*")
	workflowDir := filepath.Join(testDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow fixture directories: %v", err)
	}
	sharedSourceDir := filepath.Join(repoWorkflowDir, "shared")
	if err := filepath.Walk(sharedSourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(repoWorkflowDir, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(workflowDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, content, 0o644)
	}); err != nil {
		t.Fatalf("failed to copy shared imports: %v", err)
	}

	workflowFile := filepath.Join(workflowDir, "semantic-function-refactor.md")
	if err := os.WriteFile(workflowFile, content, 0o644); err != nil {
		t.Fatalf("failed to copy workflow source: %v", err)
	}

	compiler := NewCompiler()
	compiler.SetWorkflowIdentifier("semantic-function-refactor.md")
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	for _, snippet := range []string{
		`GH_AW_MAX_DAILY_AI_CREDITS: "300"`,
		`GH_AW_MAX_AI_CREDITS: "300"`,
		`\"maxAiCredits\":${GH_AW_MAX_AI_CREDITS}`,
		`claude-sonnet-5`,
		`name: Pre-compute semantic refactor slice`,
		`/tmp/gh-aw/agent/semantic-function-refactor/analysis.json`,
		`go run ./scripts/semantic-refactor-slice`,
		`--max-turns 50`,
	} {
		if !strings.Contains(lockStr, snippet) {
			t.Fatalf("expected compiled workflow to contain %q", snippet)
		}
		for _, snippet := range []string{`Bash(awk)`, `Bash(find`, `Bash(serena:*)`, "# - Edit\n", "# - Write\n", `"serena":`} {
			if strings.Contains(lockStr, snippet) {
				t.Fatalf("analysis workflow must not expose broad discovery/edit tools: %q", snippet)
			}
		}
		for _, snippet := range []string{
			"Do not implement refactorings", "48 KiB", "16 functions", "600 source",
			"turn 20", "turn 25", "8 analysis tool calls", "3 promising candidate",
			"gh CLI proxy", "safeoutputs --help", "Only **after** submitting",
			"Do not close existing reports", "report_incomplete", "truncated: true",
			"schedule: daily around 02:17", "edit: false",
		} {
			if !strings.Contains(string(content), snippet) {
				t.Fatalf("missing prompt/tool/budget guardrail: %q", snippet)
			}
		}
	}
}
