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
		"Run `go test -race -json` once per unique modified package",
		"Do not run `go test ./...`",
		"Running the assigned `go test` commands is permitted validation",
		"Do not pre-screen packages for policy compatibility",
		"completed-analysis cursor, not an attempted-batch cursor",
		"Assess every selected file before calling analysis complete",
		"Use `noop` only when no safe edit is available",
		"create a draft PR only when edits pass all validation and diff checks",
		"Never write it or its directory; preserve the cursor unchanged on every outcome",
		"For any partial, blocked, failed, or uncertain result, use `report_incomplete`, never `noop`",
		"On failed, blocked, or out-of-scope validation, restore only your edits and report incomplete",
		"Partition the rotated list into non-overlapping batches of at most 10 files",
		"(github.run_number - 1) % number_of_batches",
		"${{ github.run_number }}",
		"zero consecutive retries after a block",
		"if there is only one batch, report that rotation cannot select a different batch",
		"State the batch and exact command, denial, or test error",
		"classify a permitted nonzero test as test failure",
		"For a budget stop without command failure, report budget-limited work",
		"Confirm candidate edits were restored without reverting pre-existing work, and the cursor is unchanged",
		"max-patch-files: 10",
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
		"then `go test -json ./...` once",
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
	if config.MaxAICredits != 25 || config.MaxTurns != 60 {
		t.Fatal("parallelizer must retain its per-run credit and turn limits")
	}
}
