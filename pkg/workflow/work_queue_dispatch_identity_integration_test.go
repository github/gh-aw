//go:build integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueCompiledPolicyAndLaunchIdentity(t *testing.T) {
	for _, entry := range []struct {
		name      string
		dispatch  string
		principal string
		kind      string
	}{
		{"default-token", "", "41898282", "github_token"},
		{"default-override-pat", "", "999", "authenticated"},
		{"app", "    github-app:\n      app-id: ${{ vars.WORKER_APP_ID }}\n      private-key: ${{ secrets.WORKER_APP_KEY }}\n", "888", "github_app"},
		{"pat", "    github-token: ${{ secrets.WORKER_PAT }}\n", "999", "authenticated"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			dir := filepath.Join(testutil.TempDir(t, "compiled-queue-identity-"), ".github", "workflows")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "worker.md"), []byte("---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\n---\nProcess the original immutable assignment.\n"), 0o600))
			source := fmt.Sprintf(`---
on: workflow_dispatch
engine: claude
tools:
  work-queue: true
work-queue-policy:
  producers:
    '11':
      pools: [default]
      priorities: [1, 2, 3, 4, 5]
      fairness-keys: ['']
    '12':
      pools: [default]
      priorities: [1, 2, 3, 4, 5]
      fairness-keys: ['']
  worker-profiles:
    default:
      workflow: .github/workflows/worker.lock.yml
      ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      principal: '%s'
      trust-domain: default
      credential-scope: repository
      effect-scope: owner/repo
safe-outputs:
  dispatch-workflow:
    workflows: [worker]
%s---
Dispatch only approved workers using the protected launch credential.
`, entry.principal, entry.dispatch)
			filename := filepath.Join(dir, "dispatcher.md")
			require.NoError(t, os.WriteFile(filename, []byte(source), 0o600))
			compiler := NewCompiler(WithVersion("integration"))
			compiler.SetApprove(true)
			require.NoError(t, compiler.CompileWorkflow(filename))
			content, err := os.ReadFile(filepath.Join(dir, "dispatcher.lock.yml"))
			require.NoError(t, err)
			assertWorkQueueProtectedOriginTransport(t, content)
			var compiled struct {
				Jobs map[string]struct {
					Steps []struct {
						ID   string            `yaml:"id"`
						Env  map[string]string `yaml:"env"`
						With map[string]string `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			found := false
			for _, step := range compiled.Jobs["safe_outputs"].Steps {
				if step.ID != "work_queue_controls" {
					continue
				}
				found = true
				require.Equal(t, "dispatcher", step.Env["GH_AW_WORK_QUEUE_ROLE"])
				proposal := strings.ReplaceAll(step.Env["GH_AW_WORK_QUEUE_POLICY"], "${{ github.repository }}", "owner/repo")
				require.NotEmpty(t, proposal)
				require.NotContains(t, proposal, "${{")
				var policy workqueue.Policy
				require.NoError(t, json.Unmarshal([]byte(proposal), &policy))
				require.NoError(t, workqueue.ValidatePolicy(policy))
				profile := policy.Pools["default"].Profiles["default"]
				require.Equal(t, entry.principal, profile.Principal)
				require.Equal(t, ".github/workflows/worker.lock.yml", profile.Workflow)
				require.Equal(t, strings.Repeat("a", 40), profile.Ref)
				require.Equal(t, "default", profile.TrustDomain)
				require.Equal(t, "repository", profile.CredentialScope)
				require.Equal(t, "owner/repo", profile.EffectScope)
				require.NotContains(t, proposal, "github.sha")
				require.Contains(t, policy.Producers, "11")
				require.Contains(t, policy.Producers, "12")
				configuration := step.Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"]
				for _, expression := range []string{
					"${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
					"${{ secrets.WORKER_PAT }}",
					"${{ steps.work-queue-dispatch-app-token.outputs.token }}",
				} {
					configuration = strings.ReplaceAll(configuration, expression, "protected-selected-token")
				}
				configuration = strings.ReplaceAll(configuration, "${{ secrets.GH_AW_GITHUB_TOKEN != '' && 'authenticated' || 'github_token' }}", entry.kind)
				configuration = strings.ReplaceAll(configuration, "${{ steps.work-queue-dispatch-app-token.outputs.app-slug }}", "approved-worker")
				configuration = strings.ReplaceAll(configuration, "${{ github.event.repository.default_branch }}", "main")
				require.NotContains(t, configuration, "${{")
				var config map[string]any
				require.NoError(t, json.Unmarshal([]byte(configuration), &config))
				credential := config["work_queue_dispatch_credential"].(map[string]any)
				require.Equal(t, entry.kind, credential["kind"])
				require.NotContains(t, credential, "principal")
				require.Equal(t, []any{"worker"}, config["work_queue_workflows"])
				require.Equal(t, []any{"worker"}, config["aw_context_workflows"])
				input, err := json.Marshal(map[string]any{"policy": policy, "config": config, "principal": entry.principal, "kind": entry.kind, "script": step.With["script"]})
				require.NoError(t, err)
				command := exec.Command("node", "-e", `const fs = require("node:fs"); const { verifyCompiledDispatchIdentity } = require("./actions/setup/js/work_queue_control_adapter_checks.cjs"); verifyCompiledDispatchIdentity(JSON.parse(fs.readFileSync(0, "utf8"))).catch(error => { console.error(error); process.exitCode = 1; });`)
				command.Dir = filepath.Join("..", "..")
				command.Stdin = strings.NewReader(string(input))
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
			}
			require.True(t, found, "actual compiler must emit the trusted control runtime")
		})
	}
}
