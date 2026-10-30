package actionslanguageservice

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPatternInvalidExpressionDebugLogRedactsSource(t *testing.T) {
	const marker = "sensitive-pattern-regression-marker"
	tests := []struct {
		name      string
		source    string
		unwrapped bool
	}{
		{name: "parser", source: "prefix ${{ '" + marker + "' + }}"},
		{name: "lexer", source: "prefix ${{ '" + marker + "' @@ }}"},
		{name: "unwrapped", source: "'" + marker + "' +", unwrapped: true},
	}

	if os.Getenv("GH_AW_PATTERN_LOG_TEST_HELPER") == "1" {
		for _, test := range tests {
			pattern := Pattern{
				Unwrapped: test.unwrapped,
				Filter: func(string, *Expression) []string {
					t.Fatal("filter called for invalid expression")
					return nil
				},
			}
			if pattern.MatchString(test.source) {
				t.Fatal("invalid expression matched")
			}
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestPatternInvalidExpressionDebugLogRedactsSource$")
	cmd.Env = append(os.Environ(),
		"GH_AW_PATTERN_LOG_TEST_HELPER=1",
		"DEBUG=workflow:actions_expression_matches",
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
	if !strings.Contains(logs, "workflow:actions_expression_matches") {
		t.Fatalf("debug logger was not enabled:\n%s", logs)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, length := 0, len(test.source), len(test.source)
			if !test.unwrapped {
				start = strings.Index(test.source, "${{")
				length = end - start - len("${{") - len("}}")
			}
			metadata := fmt.Sprintf("Cannot match invalid expression: start=%d end=%d length=%d", start, end, length)
			if !strings.Contains(logs, metadata) {
				t.Errorf("missing location metadata %q in debug output:\n%s", metadata, logs)
			}
		})
	}
}
