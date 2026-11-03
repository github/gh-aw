//go:build !integration

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func writeConnectorSetupFixture(t *testing.T, root, path, content string) string {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0700))
	require.NoError(t, os.WriteFile(target, []byte(content), 0600))
	return target
}

func TestConnectorSetupRepositoryDiscovery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	declaration := strings.Replace(developmentConnectorFixture, "default: DEV_READ_TOKEN", "required: true", 1)
	writeConnectorSetupFixture(t, root, ".github/workflows/shared/custom.md", declaration)
	writeConnectorSetupFixture(t, root, ".github/workflows/connectors/local.md", "---\nimports: [../shared/custom.md]\n---\n")
	writeConnectorSetupFixture(t, root, ".github/workflows/review-golden.md", `---
on: workflow_dispatch
imports:
  aw:
    - uses: connectors/local.md
      with:
        reader-secret: NEVER_DISPLAY_BINDING_VALUES
---
`)
	writeConnectorSetupFixture(t, root, ".github/workflows/legacy.md", "---\non: workflow_dispatch\nimports:\n  - path: /.github/workflows/shared/custom.md\n---\n")
	writeConnectorSetupFixture(t, root, ".github/workflows/pinned.md", "---\non: workflow_dispatch\nimports: [owner/repo/.github/workflows/connectors/remote.md@"+strings.Repeat("a", 40)+"]\n---\n")
	writeConnectorSetupFixture(t, root, ".github/workflows/unrelated-golden.md", "---\non: workflow_dispatch\n---\n")
	writeConnectorSetupFixture(t, root, ".github/workflows/cycle.md", "---\non: workflow_dispatch\nimports: [cycle.md]\n---\n")
	var output bytes.Buffer
	require.NoError(t, writeRepositoryConnectorGuidance(&output, root))
	for _, expected := range []string{
		"shared/custom.md (ids: demo)", "connectors/local.md", "review-golden.md (golden example",
		"legacy.md", "pinned.md", "Importing enables nothing", "Secret names only",
		"did not read/write credentials",
	} {
		require.Contains(t, output.String(), expected)
	}
	for _, excluded := range []string{"unrelated-golden.md", "cycle.md", "NEVER_DISPLAY_BINDING_VALUES", "DEV_WRITE_TOKEN"} {
		require.NotContains(t, output.String(), excluded)
	}
	require.NoDirExists(t, filepath.Join(root, ".github", "aw", "imports"))
}

func TestConnectorSetupDiscoveryBoundsAndNoFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var output bytes.Buffer
	require.NoError(t, writeRepositoryConnectorGuidance(&output, root))
	require.Empty(t, output.String())
	outside := writeConnectorSetupFixture(t, t.TempDir(), "outside.md", developmentConnectorFixture)
	dir := filepath.Join(root, ".github", "workflows", "connectors")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "outside.md")))
	require.NoError(t, writeRepositoryConnectorGuidance(&output, root))
	require.Empty(t, output.String(), "discovery must not follow symlink declarations")
	path := writeConnectorSetupFixture(t, root, ".github/workflows/connectors/bad.md", "---\nconnectors: [unclosed\n---\n")
	err := writeRepositoryConnectorGuidance(&output, root)
	require.ErrorContains(t, err, "correct its frontmatter")
	require.Empty(t, output.String(), "incomplete discovery must not produce a success-shaped inventory")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), connectors.MaxDocumentBytes+1), 0600))
	require.ErrorContains(t, writeRepositoryConnectorGuidance(&output, root), "exceeds 256 KiB")
}

func TestConnectorSetupAuthenticationAndBindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	content := strings.ReplaceAll(developmentConnectorFixture, "credentials>", "authentication>")
	content = strings.Replace(content, "<authentication>", `<authentication audience="https://api.example.test" renewal="external">
    <acquisition>Use a tenant-bound OAuth access token; renew externally.</acquisition>`, 1)
	writeDevelopmentConnector(t, root, content)
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root)
	require.NoError(t, err)
	for _, expected := range []string{
		"audience=https://api.example.test", "renewal=external", "tenant-bound OAuth",
		"reader-secret: string", "writer-secret: string", "--grant issues=",
		"api: https://api.example.test", "not fetched or added to network.allowed",
		"Importing selects nothing", "exact declared native methods",
		"token acquisition, consent, refresh and provider permissions remain external",
		"No capabilities, native methods, write credentials or webhook ingress were enabled",
	} {
		require.Contains(t, output, expected)
	}
	require.NotContains(t, output, "${{ github.aw.import-inputs")
}

func TestConnectorSetupConfluenceAuthenticationMetadata(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("../../.github/workflows/connectors/confluence.md")
	require.NoError(t, err)
	root := t.TempDir()
	writeConnectorSetupFixture(t, root, ".github/workflows/connectors/confluence.md", string(content))
	output, err := executeDevelopmentCommand("confluence", "setup", "--root", root)
	require.NoError(t, err)
	for _, expected := range []string{
		"Atlassian OAuth", "audience=api.atlassian.com", "expiry and renewal",
		"CONFLUENCE_READ_ACCESS_TOKEN", "CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN",
	} {
		require.Contains(t, connectorConsoleText(output), expected)
	}
}

func TestConnectorSetupWrapperMetadataAndGoldenExamples(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDevelopmentConnector(t, root, developmentConnectorFixture)
	wrapper := `---
import-schema:
  custom-reader:
    type: string
    default: WRAPPER_READ_TOKEN
imports:
  - uses: demo.md
    with:
      reader-secret: ${{ github.aw.import-inputs.custom-reader }}
---
<!--
<connector-documentation id="demo"><authentication><setup>Wrapper-specific guidance.</setup></authentication></connector-documentation>
-->
`
	writeConnectorSetupFixture(t, root, ".github/workflows/connectors/wrapper.md", wrapper)
	writeConnectorSetupFixture(t, root, ".github/workflows/demo-golden.md", "---\non: workflow_dispatch\nimports: [connectors/wrapper.md]\n---\n")
	writeConnectorSetupFixture(t, root, ".github/workflows/other-golden.md", "---\non: workflow_dispatch\n---\n")
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root,
		"--file", ".github/workflows/connectors/wrapper.md", "--input", `custom-reader="OVERRIDE_READ_TOKEN"`)
	require.NoError(t, err)
	require.Contains(t, output, "OVERRIDE_READ_TOKEN")
	require.Contains(t, output, "Create a provider app")
	require.Contains(t, output, "Wrapper-specific guidance")
	require.Contains(t, output, "custom-reader: string")
	require.Contains(t, output, "demo-golden.md")
	require.NotContains(t, output, "other-golden.md")
	require.NotContains(t, output, "WRAPPER_READ_TOKEN")
}

func TestConnectorSetupPinnedImportMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sha := strings.Repeat("a", 40)
	cache := parser.NewImportCache(root)
	_, err := cache.Set("example", "connectors", ".github/workflows/connectors/demo.md", sha, []byte(developmentConnectorFixture))
	require.NoError(t, err)
	writeConnectorSetupFixture(t, root, ".github/workflows/connectors/pinned.md", "---\nimports: [example/connectors/.github/workflows/connectors/demo.md@"+sha+"]\n---\n")
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root, "--file", ".github/workflows/connectors/pinned.md")
	require.NoError(t, err)
	require.Contains(t, output, "Create a provider app")
	require.Contains(t, output, "DEV_READ_TOKEN")
}

func TestConnectorSetupInitAndNewAreNonactivating(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init", "--quiet", root)
	require.NoError(t, command.Run())
	t.Chdir(root)
	t.Setenv("CI", "true")
	path := writeDevelopmentConnector(t, root, developmentConnectorFixture)
	_, stderr := captureOutput(t, func() error {
		command := NewInitCommand()
		command.SetArgs([]string{"--no-skill", "--no-agent", "--no-mcp"})
		return command.Execute()
	})
	require.Contains(t, stderr, "connectors/demo.md (ids: demo)")
	require.Contains(t, stderr, "Importing enables nothing")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, developmentConnectorFixture, string(content))
	for _, absent := range []string{".github/workflows/aw.json", ".github/mcp.json", ".github/workflows/copilot-setup-steps.yml"} {
		require.NoFileExists(t, filepath.Join(root, absent))
	}
	_, stderr = captureOutput(t, func() error {
		return CreateWorkflowMarkdownFile("review", false, false, "copilot")
	})
	require.Contains(t, stderr, "connectors/demo.md (ids: demo)")
	created, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "review.md"))
	require.NoError(t, err)
	parsed, err := parser.ExtractFrontmatterFromContent(string(created))
	require.NoError(t, err)
	require.NotContains(t, parsed.Frontmatter, "imports")
	require.NotContains(t, parsed.Frontmatter, "connectors")
	require.Equal(t, "defaults", parsed.Frontmatter["network"])
	require.NotContains(t, parsed.Frontmatter, "tools")
}

func TestConnectorSetupGeneratedInstructions(t *testing.T) {
	t.Parallel()
	skill, err := renderAgenticWorkflowsSkillContent([]string{"connectors.md"})
	require.NoError(t, err)
	agent, err := buildAgenticWorkflowsAgentContent(t.TempDir())
	require.NoError(t, err)
	for _, content := range []string{skill, agent} {
		for _, expected := range []string{
			"phase-specific credential names/scopes", "local/pinned imports", "golden",
			"never", "Token acquisition/refresh", "provider permissions remain external",
		} {
			require.Contains(t, content, expected)
		}
	}
	bootstrap := newSecretsBootstrapSubcommand()
	require.Contains(t, bootstrap.Long, "Only required engine/system secrets")
	require.Contains(t, bootstrap.Long, "Presence of an Actions secret does not prove provider access")
}
