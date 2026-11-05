//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type compiledPRCredentialStep struct {
	ID   string         `yaml:"id"`
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

// TestCompileSafeOutputsPRCredentialsSpecificationIntegration checks GP5c and
// compiler threat specification section 7.10 at the CLI's generated-YAML boundary.
func TestCompileSafeOutputsPRCredentialsSpecificationIntegration(t *testing.T) {
	const appToken = "${{ steps.safe-outputs-app-token.outputs.token }}"
	const staticToken = "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
	tests := []struct {
		name          string
		fixture       string
		checkoutToken string
	}{
		{"host", "host", appToken},
		{"optional App", "optional", "${{ steps.safe-outputs-app-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"},
		{"optional App with custom fallback", "optional-custom", "${{ steps.safe-outputs-app-token.outputs.token || secrets.SO_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"},
		{"per-handler App", "handler-app", staticToken},
		{"global App not minted", "no-global-mint", staticToken},
		{"per-handler token", "handler-token", "${{ secrets.PR_TOKEN }}"},
		{"explicit checkout auth", "explicit", "${{ secrets.HOST_TOKEN }}"},
		{"explicit checkout App", "explicit-app", "${{ steps.checkout-app-token-0.outputs.token }}"},
		{"cross-organization target", "target", staticToken},
		{"distinct head repository", "head", staticToken},
		{"work-queue worker", "work-queue", appToken},
		{"work-queue dispatcher", "work-queue-dispatcher", appToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupIntegrationTest(t)
			defer setup.cleanup()

			filename := "test-safe-outputs-pr-credentials-" + tt.fixture + ".md"
			dstPath := filepath.Join(setup.workflowsDir, filename)
			copyWorkflowFile(t, filepath.Join(projectRoot, "pkg/cli/workflows", filename), dstPath)
			if tt.fixture == "work-queue-dispatcher" {
				const workerFilename = "test-safe-outputs-pr-credentials-work-queue.md"
				copyWorkflowFile(t, filepath.Join(projectRoot, "pkg/cli/workflows", workerFilename), filepath.Join(setup.workflowsDir, workerFilename))
			}
			cmd := exec.Command(setup.binaryPath, "compile", dstPath)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "compile should succeed:\n%s", output)

			lockContent, err := os.ReadFile(strings.TrimSuffix(dstPath, ".md") + ".lock.yml")
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]struct {
					Steps []compiledPRCredentialStep `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, goyaml.Unmarshal(lockContent, &compiled), "compiled workflow must be valid YAML")
			safeJob, ok := compiled.Jobs["safe_outputs"]
			require.True(t, ok, "safe_outputs job must exist")
			agentJob, ok := compiled.Jobs["agent"]
			require.True(t, ok, "agent job must exist")

			findStep := func(steps []compiledPRCredentialStep, nameOrID string) (int, compiledPRCredentialStep) {
				t.Helper()
				for index, step := range steps {
					if step.Name == nameOrID || step.ID == nameOrID {
						return index, step
					}
				}
				t.Fatalf("missing step %q", nameOrID)
				return -1, compiledPRCredentialStep{}
			}
			checkoutIndex, checkout := findStep(safeJob.Steps, "Checkout repository (gh-aw default)")
			assert.Equal(t, tt.checkoutToken, checkout.With["token"], "persisted token must match GP5c selection")
			assert.Equal(t, true, checkout.With["persist-credentials"])
			_, agentCheckout := findStep(agentJob.Steps, "Checkout repository (gh-aw default)")
			assert.Equal(t, false, agentCheckout.With["persist-credentials"])
			assert.NotContains(t, extractYAMLJobSection(string(lockContent), "agent"), "steps.safe-outputs-app-token",
				"privileged step output must not reach agent steps or the safe-outputs MCP server")
			for _, step := range agentJob.Steps {
				token, _ := step.With["token"].(string)
				assert.NotContains(t, token, "steps.safe-outputs-app-token", "agent must not consume privileged safe-output credentials")
				assert.NotEqual(t, "safe-outputs-app-token", step.ID, "agent must not mint the safe-output App token")
			}
			if tt.fixture == "no-global-mint" {
				for _, step := range safeJob.Steps {
					assert.NotEqual(t, "safe-outputs-app-token", step.ID, "unused global App must not be minted")
				}
				return
			}
			mintIndex, mint := findStep(safeJob.Steps, "safe-outputs-app-token")
			assert.Less(t, mintIndex, checkoutIndex, "global App mint must precede checkout")
			if tt.fixture == "explicit-app" {
				checkoutMintIndex, _ := findStep(safeJob.Steps, "checkout-app-token-0")
				assert.Less(t, checkoutMintIndex, checkoutIndex, "explicit checkout App mint must precede its consumer")
			}

			permissions := map[string]any{}
			for key, value := range mint.With {
				if strings.HasPrefix(key, "permission-") {
					permissions[key] = value
				}
			}
			expectedPermissions := map[string]any{
				"permission-contents":      "write",
				"permission-issues":        "write",
				"permission-pull-requests": "write",
			}
			if tt.fixture == "handler-app" || tt.fixture == "handler-token" {
				expectedPermissions = map[string]any{"permission-issues": "write"}
			}
			assert.Equal(t, expectedPermissions, permissions, "persistence must not expand global App permissions")
			if tt.fixture == "target" {
				assert.Equal(t, "OrgB", mint.With["owner"])
				assert.Equal(t, "target", mint.With["repositories"], "App scope must remain the configured target")
			} else {
				assert.Equal(t, "${{ github.event.repository.name }}", mint.With["repositories"], "App scope must remain the host repository")
			}

			if tt.fixture == "host" || tt.fixture == "explicit" {
				checkouts := map[string]compiledPRCredentialStep{}
				for _, step := range safeJob.Steps {
					if strings.HasPrefix(step.Uses, "actions/checkout@") {
						if repo, ok := step.With["repository"].(string); ok {
							checkouts[repo] = step
						}
					}
				}
				require.Contains(t, checkouts, "OrgA/static")
				require.Contains(t, checkouts, "OrgA/private")
				expectedStaticToken := staticToken
				expectedPrivateToken := "${{ secrets.STATIC_TOKEN }}"
				if tt.fixture == "explicit" {
					expectedStaticToken = "${{ secrets.HOST_TOKEN }}"
					expectedPrivateToken = "${{ steps.checkout-app-token-2.outputs.token }}"
					privateIndex, _ := findStep(safeJob.Steps, checkouts["OrgA/private"].Name)
					privateMintIndex, _ := findStep(safeJob.Steps, "checkout-app-token-2")
					assert.Less(t, privateMintIndex, privateIndex)
				}
				assert.Equal(t, expectedStaticToken, checkouts["OrgA/static"].With["token"])
				assert.Equal(t, expectedPrivateToken, checkouts["OrgA/private"].With["token"])
				assert.Equal(t, true, checkouts["OrgA/static"].With["persist-credentials"])
				assert.Equal(t, true, checkouts["OrgA/private"].With["persist-credentials"])
			}

			if tt.fixture == "host" {
				uploadJob, ok := compiled.Jobs["upload_code_scanning_sarif"]
				require.True(t, ok, "SARIF publication must remain in its separate job")
				foundUpload := false
				for _, step := range uploadJob.Steps {
					token, _ := step.With["token"].(string)
					assert.NotContains(t, token, "steps.safe-outputs-app-token")
					if strings.HasPrefix(step.Uses, "github/codeql-action/upload-sarif@") {
						foundUpload = true
						assert.Equal(t, "${{ secrets.SCAN_TOKEN }}", step.With["token"])
					}
				}
				assert.True(t, foundUpload, "SARIF upload action must exist")
			}
			if strings.HasPrefix(tt.fixture, "work-queue") {
				downloadIndex, _ := findStep(safeJob.Steps, "Download agent output artifact")
				reconcileIndex, _ := findStep(safeJob.Steps, "work_queue_claim_reconciliation")
				assert.Less(t, downloadIndex, reconcileIndex)
				assert.Less(t, reconcileIndex, mintIndex)
				for index, step := range safeJob.Steps {
					if strings.HasPrefix(step.Uses, "actions/download-artifact@") {
						assert.Less(t, index, reconcileIndex, "artifact downloads must precede claim reconciliation")
					}
				}
			}
		})
	}
}
