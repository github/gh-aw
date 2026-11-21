package workflowcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDailyCLIToolsTesterCoverageContract(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "daily-cli-tools-tester.md"))
	if err != nil {
		t.Fatalf("failed to read CLI tester workflow source: %v", err)
	}
	text := string(content)
	for _, token := range []string{
		"If any required check is blocked or unfinished, use `report_incomplete`",
		"3 targeted trusted-workflow compiles with `dry_run: true` and `strict: true`",
		"do not count exclusions as passed tests or emit `report_incomplete` solely for them",
		"Never claim full coverage or repository-wide health",
		"Record these three skipped edge cases as **not tested (expected exclusion)**",
		"These exclusions alone do not require `report_incomplete`",
		"Do not emit a PR. A later `noop` must not override that incomplete status or claim success",
		"Do not relabel a failed or blocked required check as an expected exclusion",
		"Coverage is partial by design",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("missing CLI tester coverage contract: %q", token)
		}
	}
	for _, obsolete := range []string{
		"Use `report_incomplete` to record these three skipped edge cases",
		"All audit, logs, and compile commands functioning correctly",
		"Error detection (should be none for successful run)",
	} {
		if strings.Contains(text, obsolete) {
			t.Errorf("misleading CLI tester reporting instruction: %q", obsolete)
		}
	}
}
