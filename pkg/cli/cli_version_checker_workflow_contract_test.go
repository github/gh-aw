//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/require"
)

func TestCLIVersionCheckerUsesCopilotModel(t *testing.T) {
	t.Parallel()

	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "cli-version-checker.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "\nmodel: copilot/gpt-5\n")
}

func TestCLIVersionCheckerReleaseResolutionContract(t *testing.T) {
	t.Parallel()

	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "cli-version-checker.md"))
	require.NoError(t, err)
	workflow := string(content)

	for _, instruction := range []string{
		"Apply this policy to **all release resolution**",
		"cutoff = check_time - 259200 seconds",
		"at or before the cutoff",
		"A release exactly 72 hours old is eligible; one 71 hours 59 minutes old is not",
		"search older stable releases",
		"Never downgrade an existing pin",
		"cached \"latest\" values never bypass the cooldown",
		"publication timestamp is missing, invalid, or in the future, do not select it",
		"Report unavailable metadata with `report_incomplete`",
		"security fixes do not bypass the cooldown",
		"including digest-only updates",
	} {
		require.Contains(t, workflow, instruction)
	}

	for _, pkg := range []string{
		"@anthropic-ai/claude-code", "@github/copilot", "@openai/codex",
		"@playwright/cli", "@earendil-works/pi-coding-agent",
	} {
		require.Contains(t, workflow, "npm view "+pkg+" versions time --json")
		require.NotContains(t, workflow, "npm view "+pkg+" version`")
	}

	for _, repo := range []string{
		"github/github-mcp-server", "github/gh-aw-mcpg", "github/gh-aw-threat-detection",
		"rhysd/actionlint", "anchore/syft", "anchore/grype", "anchore/grant",
		"zizmorcore/zizmor", "boostsecurityio/poutine", "vigilant-llc/runner-guard",
		"PipelineComponents/yamllint",
	} {
		require.Contains(t, workflow, "https://api.github.com/repos/"+repo+"/releases?per_page=100")
		require.NotContains(t, workflow, "https://api.github.com/repos/"+repo+"/releases/latest")
	}

	require.Contains(t, workflow, "bash scripts/update-copilot-version.sh VERSION")
	require.Contains(t, workflow, "bash scripts/update-release-pins.sh github/copilot-cli VERSION SHA256SUMS.txt TRUSTED_MANIFEST_SHA256")
	require.Contains(t, workflow, "updates `DefaultCopilotVersion` and `DefaultCopilotChecksumsSHA256` together")
	require.Contains(t, workflow, "If independent authentication is unavailable, leave the Copilot pins unchanged")
}
