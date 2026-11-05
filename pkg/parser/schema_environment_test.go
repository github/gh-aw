//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchemaEnvironmentDeployment(t *testing.T) {
	for _, location := range []string{"environment", "safe-outputs", "jobs"} {
		for _, tc := range []struct {
			name  string
			value any
			valid bool
		}{
			{"false", false, true},
			{"true", true, true},
			{"expression", "${{ github.ref_name == 'main' }}", true},
			{"non-expression", "no", false},
			{"string-boolean", "false", false},
			{"embedded-expression", "prefix ${{ true }}", false},
			{"number", 0, false},
			{"null", nil, false},
		} {
			t.Run(location+"/"+tc.name, func(t *testing.T) {
				environment := map[string]any{"name": "protected", "deployment": tc.value}
				data := map[string]any{"on": "push"}
				switch location {
				case "environment":
					data["environment"] = environment
				case "safe-outputs":
					data["safe-outputs"] = map[string]any{"environment": environment}
				case "jobs":
					data["jobs"] = map[string]any{
						"custom": map[string]any{
							"runs-on":     "ubuntu-latest",
							"environment": environment,
							"steps":       []any{map[string]any{"run": "echo testing"}},
						},
					}
				}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(data, "environment.md")
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}
