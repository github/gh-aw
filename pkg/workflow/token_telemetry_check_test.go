//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"
)

func TestSmokeTokenTelemetryCheckIsEngineAware(t *testing.T) {
	for _, engine := range []string{"kiro", "goose", "cursor", "copilot"} {
		t.Run(engine, func(t *testing.T) {
			content, err := os.ReadFile("../../.github/workflows/smoke-" + engine + ".lock.yml")
			if err != nil {
				t.Fatal(err)
			}
			_, check, ok := strings.Cut(string(content), "\n  check_token_telemetry:\n")
			if !ok {
				t.Fatal("missing token telemetry check")
			}
			check = strings.SplitN(check, "\n  conclusion:\n", 2)[0]
			for _, expected := range []string{
				"      - activation\n      - agent",
				"Assert token_usage.jsonl is non-empty",
				"Assert agent_usage.json has non-zero token counts",
				"Report engines without proxy token telemetry",
			} {
				if !strings.Contains(check, expected) {
					t.Errorf("token telemetry check missing %q", expected)
				}
			}
			unsupported := `contains(fromJSON('["kiro","goose","cursor"]'), needs.activation.outputs.engine_id)`
			if strings.Count(check, "!"+unsupported) != 2 || strings.Count(check, "&& "+unsupported) != 1 {
				t.Error("proxy assertions must run only for supported engines; unsupported engines must receive a notice")
			}
			if strings.Contains(check, "continue-on-error: true") {
				t.Error("agent artifact download must not silently fail")
			}
		})
	}
}
