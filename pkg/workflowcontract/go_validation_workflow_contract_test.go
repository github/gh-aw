package workflowcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDailyCLIPerformanceRejectsIncompleteMeasurements(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "daily-cli-performance.md"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) != 3 {
		t.Fatal("workflow must have YAML frontmatter")
	}
	if !strings.Contains(parts[1], "strict: true") {
		t.Error("strict security controls must remain enabled")
	}
	text := strings.Join(strings.Fields(parts[2]), " ")
	for _, token := range []string{
		"python3 scripts/parse_benchmark_results.py",
		"finite metrics before updating history",
		"If the command fails, stop and call `report_incomplete`",
		"do not update historical metrics or publish a regression claim",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("missing benchmark acceptance contract: %q", token)
		}
	}
}

func TestGoTestParallelizerPreservesValidationAndCompletedCursor(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "daily-go-test-parallelizer.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(content)), " ")
	for _, token := range []string{
		"Run `go test -race` once per unique modified package",
		"Run `go test ./...` once after all candidate edits",
		"completed-analysis cursor, not an attempted-batch cursor",
		"all selected files to have a completed safety assessment",
		"No safe edit is available",
		"Candidate edits pass every required validation command and the diff check",
		"Preserve the previous cursor byte-for-byte",
		"Never advance the cursor immediately after selection",
		"Cursor persistence is temporarily disabled",
		"Do not write `state.json` or create its directory",
		"including successful validation and completed no-candidate analysis",
		"Do not create a pull request or use `noop` to imply validation succeeded",
		"use `report_incomplete` when analysis or required validation is blocked or fails",
		"Partition the rotated list into non-overlapping batches of at most 25 files",
		"(github.run_number - 1) % number_of_batches",
		"${{ github.run_number }}",
		"zero consecutive retries after a block",
		"if there is only one batch, report that rotation cannot select a different batch",
		"The blocked batch, the exact required command and its concrete error",
		"say it was not run and name the policy limitation instead of inventing an error",
		"environment/policy blocked, test failure, or agent failure",
		"Confirm candidate edits were restored by inspecting the diff",
		"Confirm the completed-analysis cursor remains unchanged and identify the next attempted batch",
		"draft: true",
		"strict: true",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("missing validation/progress contract: %q", token)
		}
	}

	for _, obsolete := range []string{
		"Always create `/tmp/gh-aw/cache-memory/go-test-parallelizer/`",
		"Revert the edit and use `noop` if any test command fails",
		"otherwise use `noop`",
	} {
		if strings.Contains(text, obsolete) {
			t.Errorf("obsolete success-shaped progress rule: %q", obsolete)
		}
	}
}

func TestGoTestParallelizerDisablesCursorPersistence(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "daily-go-test-parallelizer.md"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) != 3 {
		t.Fatal("workflow must have YAML frontmatter")
	}
	var config struct {
		MaxAICredits int `yaml:"max-ai-credits"`
		MaxTurns     int `yaml:"max-turns"`
		Tools        struct {
			CacheMemory struct {
				RestoreOnly bool `yaml:"restore-only"`
			} `yaml:"cache-memory"`
		}
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &config); err != nil {
		t.Fatal(err)
	}
	if !config.Tools.CacheMemory.RestoreOnly {
		t.Fatal("cursor must remain restore-only until trusted validation can authorize persistence")
	}
	if config.MaxAICredits != 15 || config.MaxTurns != 40 {
		t.Fatal("parallelizer must retain its per-run credit and turn limits")
	}
}
