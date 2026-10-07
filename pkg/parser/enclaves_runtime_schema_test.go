package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnclavesRuntimeSchema(t *testing.T) {
	for _, executor := range []string{"script", "agent"} {
		for _, runtime := range []string{"", "docker", "cloud-hypervisor", "gvisor", "sbx", "nvx", "unknown"} {
			t.Run(executor+"/"+runtime, func(t *testing.T) {
				entry := map[string]any{
					executor: map[string]any{},
					"repos": []any{
						map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
					},
				}
				if executor == "agent" {
					entry[executor] = map[string]any{"model": "gpt-5"}
				}
				if runtime != "" {
					entry["runtime"] = runtime
				}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
					"on": "workflow_dispatch", "engine": "copilot", "enclaves": []any{entry},
				}, "workflow.md")
				if runtime == "" || runtime == "docker" || runtime == "cloud-hypervisor" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}
