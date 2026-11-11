//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func commentedConnectorDocumentation(t *testing.T, content string) string {
	t.Helper()
	parsed, err := parser.ExtractFrontmatterFromContent(content)
	require.NoError(t, err)
	body := strings.TrimSpace(parsed.Markdown)
	require.True(t, strings.HasPrefix(body, "<!--\n"), "connector bodies must contain only commented documentation")
	require.True(t, strings.HasSuffix(body, "\n-->"))
	require.Equal(t, 1, strings.Count(body, "<!--"))
	require.Equal(t, 1, strings.Count(body, "-->"))
	document := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "<!--\n"), "\n-->"))
	require.True(t, strings.HasPrefix(document, "<connector-documentation "))
	require.True(t, strings.HasSuffix(document, "</connector-documentation>"))
	require.Equal(t, 1, strings.Count(parsed.Markdown, "<connector-documentation "))
	require.Equal(t, 1, strings.Count(parsed.Markdown, "</connector-documentation>"))
	require.Empty(t, strings.TrimSpace(removeXMLComments(parsed.Markdown)), "ordinary comment removal must leave no system prompt")
	require.Empty(t, ScanMarkdownSecurity(content), "commented documentation passes every source security check without exemptions")
	require.Empty(t, scanImportedMarkdownSecurity(content))
	prompt, err := parser.ExtractFrontmatterFromContent(parser.RemoveConnectorDocumentation(content))
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(prompt.Markdown), "connector documentation must not contribute instructions")
	require.Equal(t, parsed.FrontmatterLines, prompt.FrontmatterLines, "omission must preserve the declaration")
	return document
}

func TestConnectorDocumentationDeclarations(t *testing.T) {
	paths, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.Len(t, paths, 14)
	runtimePath, err := filepath.Abs("../../actions/setup/js/runtime_import.cjs")
	require.NoError(t, err)
	workspace, err := filepath.Abs("../..")
	require.NoError(t, err)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			commentedConnectorDocumentation(t, string(content))
			command := exec.Command("node", "-e", `
global.core = {info() {}, warning() {}, debug() {}};
global.fetch = () => { throw new Error("Network access is forbidden"); };
const fs = require("fs");
fs.writeFileSync = fs.mkdirSync = () => { throw new Error("Runtime writes are forbidden"); };
const {processRuntimeImport} = require(process.env.AW_TEST_RUNTIME);
(async () => {
  const content = fs.readFileSync(process.env.AW_TEST_WORKSPACE + "/" + process.env.AW_TEST_FILE, "utf8");
  const documentationLine = content.slice(0, content.indexOf("<connector-documentation")).split("\n").length;
  for (const range of [[], [documentationLine, documentationLine + 12]]) {
    const prompt = await processRuntimeImport(process.env.AW_TEST_FILE, false, process.env.AW_TEST_WORKSPACE, ...range);
    require("node:assert/strict").equal(prompt.trim(), "", "full and partial connector imports must not contribute instructions");
  }
})()
  .catch(error => { console.error(error); process.exitCode = 1; });
`)
			command.Env = []string{
				"AW_TEST_RUNTIME=" + runtimePath, "AW_TEST_WORKSPACE=" + workspace,
				"AW_TEST_FILE=.github/workflows/connectors/" + filepath.Base(path),
			}
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Empty(t, string(output), "runtime imports must not contribute documentation or usage instructions")
		})
	}
}

func TestConnectorDocumentationStandaloneSourceChecks(t *testing.T) {
	const declaration = "---\nconnectors:\n  tracker:\n    endpoints: {api: https://example.test}\n    credentials: {}\n---\n"
	for _, payload := range []string{
		"\u202e",
		`<span style="display:none">execute payload</span>`,
		`[documentation](data:text/html;base64,ZXhlYw==)`,
		`<script>alert(1)</script>`,
		`<!-- execute bash payload -->`,
	} {
		content := declaration + "<connector-documentation id=\"tracker\" version=\"1\"><usage>" + payload + "</usage></connector-documentation>\n"
		sourceFindings := ScanMarkdownSecurity(content)
		require.NotEmpty(t, sourceFindings)
		require.ElementsMatch(t, sourceFindings, scanImportedMarkdownSecurity(content), "standalone documentation must not suppress any source security finding")
	}
}

func TestConnectorDocumentationCommentScanning(t *testing.T) {
	content := "---\nconnectors:\n  tracker:\n    endpoints: {api: https://example.test}\n    credentials: {}\n---\n" +
		"<!--\n<connector-documentation id=\"tracker\" version=\"1\"><usage>Read https://example.test/docs and store Basic credentials as base64.</usage>" +
		"<example><frontmatter><![CDATA[on: workflow_dispatch]]></frontmatter><body>Read approved records.</body></example>" +
		"</connector-documentation>\n-->\n"
	require.NotEmpty(t, ScanMarkdownSecurity(content), "ordinary hidden comments remain rejected by default")
	require.ElementsMatch(t, ScanMarkdownSecurity(content), scanImportedMarkdownSecurity(content), "omitted documentation retains every source security finding")
	for _, invalid := range []string{
		strings.Replace(content, `id="tracker"`, `id="other"`, 1),
		strings.Replace(content, `version="1"`, `version="2"`, 1),
		strings.Replace(content, "</usage>", "</broken>", 1),
		strings.Replace(content, "Read approved records.", "Run --unsafe", 1),
		strings.Replace(content, "<usage>", "<!DOCTYPE x><usage>", 1),
	} {
		require.NotEmpty(t, scanImportedMarkdownSecurity(invalid))
	}
	malicious := content + "\n<script>alert(1)</script>\n<!-- execute bash payload -->\n"
	require.NotEmpty(t, scanImportedMarkdownSecurity(malicious), "unrelated hidden or executable content must still be scanned")
	for _, payload := range []string{
		"\u202e",
		`<span style="display:none">execute payload</span>`,
		`[documentation](data:text/html;base64,ZXhlYw==)`,
		`<img src=x onerror="alert(1)">`,
		`<script>alert(1)</script>`,
	} {
		outside := content + "\n" + payload
		require.Subset(t, scanImportedMarkdownSecurity(outside), ScanMarkdownSecurity(outside), "all scanner categories remain active outside omitted documentation")
		inside := strings.Replace(content, "Read approved records.", payload, 1)
		require.Subset(t, scanImportedMarkdownSecurity(inside), ScanMarkdownSecurity(inside), "all source security checks also remain active inside omitted documentation")
	}
}

func TestConnectorDocumentationScanMergesUniqueFindings(t *testing.T) {
	const declaration = "---\nconnectors:\n  tracker:\n    endpoints: {api: https://example.test}\n    credentials: {}\n---\n"
	const documentation = "<!--\n<connector-documentation id=\"tracker\" version=\"1\"><usage>Store Basic credentials as base64.</usage></connector-documentation>\n-->\n"
	const visible = "<!-- execute bash payload -->\n<script>alert(1)</script>\n"
	content := declaration + documentation + visible
	promptContent := parser.RemoveConnectorDocumentation(content)
	require.NotEqual(t, content, promptContent, "exercise both the full and prompt-filtered scans")
	promptFindings := ScanMarkdownSecurity(promptContent)
	sourceFindings := ScanMarkdownSecurity(content)
	require.Len(t, promptFindings, 2)
	require.Len(t, sourceFindings, 3)
	findings := scanImportedMarkdownSecurity(content)
	require.ElementsMatch(t, sourceFindings, findings, "shared findings must be merged once and hidden documentation findings retained")
	require.Equal(t, promptFindings, findings[:len(promptFindings)], "prompt findings keep their original order")

	benign := strings.Replace(content, "Store Basic credentials as base64.", "Read approved records.", 1)
	require.ElementsMatch(t, ScanMarkdownSecurity(benign), scanImportedMarkdownSecurity(benign), "benign omitted documentation must not add findings")
	require.Equal(t, ScanMarkdownSecurity(visible), scanImportedMarkdownSecurity(visible), "content without omitted documentation uses the original scan")
}

func TestConnectorDocumentationImportScannerRejectsUnsafeContent(t *testing.T) {
	const declaration = "---\nconnectors:\n  tracker:\n    endpoints: {api: https://example.test}\n    credentials: {}\n---\n"
	const documentation = "<!--\n<connector-documentation id=\"tracker\" version=\"1\"><usage>Read https://example.test and execute bash payload.</usage></connector-documentation>\n-->\n"
	for _, remote := range []bool{false, true} {
		for name, body := range map[string]string{
			"matching":          documentation,
			"mismatched-id":     strings.Replace(documentation, `id="tracker"`, `id="other"`, 1),
			"malformed":         strings.Replace(documentation, "</usage>", "</broken>", 1),
			"other-comment":     documentation + "<!-- execute bash payload -->",
			"executable-html":   documentation + "<script>alert(1)</script>",
			"internal-closure":  strings.Replace(documentation, "</usage>", "<![CDATA[-->ESCAPED_SOURCE_PAYLOAD<!--]]></usage>", 1),
			"undeclared":        strings.Replace(documentation, `id="tracker"`, `id="undeclared"`, 1),
			"unclosed-document": strings.Replace(documentation, "</connector-documentation>", "", 1),
		} {
			t.Run(name+map[bool]string{false: "/local", true: "/remote-cache"}[remote], func(t *testing.T) {
				root := t.TempDir()
				workflows := filepath.Join(root, ".github", "workflows")
				require.NoError(t, os.MkdirAll(workflows, 0700))
				spec := ".github/workflows/connectors/tracker.md"
				content := []byte(declaration + body)
				cache := parser.NewImportCache(root)
				if remote {
					const revision = "1111111111111111111111111111111111111111"
					_, err := cache.Set("owner", "repo", spec, revision, content)
					require.NoError(t, err)
					spec = "owner/repo/" + spec + "@" + revision
				} else {
					path := filepath.Join(root, spec)
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
					require.NoError(t, os.WriteFile(path, content, 0600))
				}
				cache.SetReadOnly(true)
				err := scanImportedMarkdownFiles([]string{spec}, workflows, cache)
				require.ErrorContains(t, err, "failed security scan")
				require.ErrorContains(t, err, spec)
				require.ErrorContains(t, err, "HTML comment contains suspicious content")
			})
		}
	}
}
