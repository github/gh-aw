//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunStackGateCompilation(t *testing.T) {
	for _, event := range []string{"pull_request", "pull_request_review"} {
		for _, dryRun := range []bool{false, true} {
			for _, steps := range []string{"none", "authored", "imported"} {
				t.Run(fmt.Sprintf("%s/dry-run=%t/%s", event, dryRun, steps), func(t *testing.T) {
					dir := t.TempDir()
					path := filepath.Join(dir, "workflow.md")
					declaration := ""
					preSteps := "pre-steps:\n  - name: Authored host step\n    run: echo host-effect\n"
					switch steps {
					case "authored":
						declaration = preSteps
					case "imported":
						declaration = "imports: [shared.md]\n"
						require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\n"+preSteps+"---\nShared configuration.\n"), 0600))
					}
					eventType := "opened"
					if event == "pull_request_review" {
						eventType = "submitted"
					}
					source := fmt.Sprintf(`---
on:
  %s:
    types: [%s]
    max-stack: 2
engine: copilot
permissions:
  contents: read
%s---
Analyze the repository.
`, event, eventType, declaration)
					require.NoError(t, os.WriteFile(path, []byte(source), 0600))
					c := NewCompiler()
					c.SetSkipValidation(true)
					c.SetApprove(true)
					c.SetDryRun(dryRun)
					err := c.CompileWorkflow(path)
					lockPath := strings.TrimSuffix(path, ".md") + ".lock.yml"
					require.NoError(t, err)
					lock, err := os.ReadFile(lockPath)
					require.NoError(t, err)
					assert.Equal(t, 1, strings.Count(string(lock), "Stack position gate (max-stack: 2)"))
					assert.Contains(t, string(lock), "STACK_POSITION + max_stack <= STACK_SIZE")
					if dryRun {
						assert.NotContains(t, string(lock), "echo host-effect", "dry-run must omit pre-steps")
					} else if steps != "none" {
						assert.Contains(t, string(lock), "echo host-effect")
						assert.Less(t, strings.Index(string(lock), "name: Authored host step"), strings.Index(string(lock), "Stack position gate"))
					}
				})
			}
		}
	}
}
