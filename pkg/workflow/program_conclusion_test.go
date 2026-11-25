//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramConclusionFailureHandlerEngine(t *testing.T) {
	compiled, err := compileProgramWorkflows(t, map[string]string{
		"program.md": "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\nsafe-outputs:\n  noop:\n---\n",
		"p.ts":       "export default async () => {};\n",
	}, "program")
	require.NoError(t, err)

	for _, step := range compiled.Jobs["conclusion"].Steps {
		if step.ID == "handle_agent_failure" {
			require.Equal(t, "none", step.Env["GH_AW_ENGINE_ID"])
			return
		}
	}
	t.Fatal("conclusion is missing handle_agent_failure")
}
