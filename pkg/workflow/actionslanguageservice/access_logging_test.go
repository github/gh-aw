package actionslanguageservice

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRewriteReferencesInvalidExpressionDebugLogRedactsSource(t *testing.T) {
	const marker = "sensitive-reference-regression-marker"
	sources := []string{
		"'" + marker + "' github.ref",
		"'" + marker + "' @@",
	}
	if os.Getenv("GH_AW_REFERENCE_LOG_TEST_HELPER") == "1" {
		for _, source := range sources {
			got := RewriteReferences(source, func([]string) (string, bool) {
				t.Fatal("replacement called for invalid expression")
				return "", false
			})
			if got != source {
				t.Fatalf("invalid source changed: got %q, want %q", got, source)
			}
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestRewriteReferencesInvalidExpressionDebugLogRedactsSource$")
	cmd.Env = append(os.Environ(),
		"GH_AW_REFERENCE_LOG_TEST_HELPER=1",
		"DEBUG=workflow:actions_expression_references",
		"DEBUG_COLORS=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("debug logging subprocess failed: %v\n%s", err, output)
	}
	logs := string(output)
	if strings.Contains(logs, marker) {
		t.Fatalf("debug output disclosed sensitive expression content:\n%s", logs)
	}
	if !strings.Contains(logs, "workflow:actions_expression_references") {
		t.Fatalf("debug logger was not enabled:\n%s", logs)
	}
	if got := strings.Count(logs, "Cannot rewrite invalid expression"); got != len(sources) {
		t.Fatalf("got %d sanitized diagnostics, want %d:\n%s", got, len(sources), logs)
	}
}
