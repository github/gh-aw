//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestConnectorDocumentationCommentScanning(t *testing.T) {
	content := "---\nconnectors:\n  tracker:\n    endpoints: {api: https://example.test}\n    credentials: {}\n---\n" +
		"<!--\n<connector-documentation id=\"tracker\" version=\"1\"><usage>Read https://example.test/docs and store Basic credentials as base64.</usage>" +
		"<example><frontmatter><![CDATA[on: workflow_dispatch]]></frontmatter><body>Read approved records.</body></example>" +
		"</connector-documentation>\n-->\n"
	require.NotEmpty(t, ScanMarkdownSecurity(content), "ordinary hidden comments remain rejected by default")
	require.Empty(t, scanImportedMarkdownSecurity(content), "strict scanning follows deterministic omission, without a path-based exemption")
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
		require.NotEmpty(t, scanImportedMarkdownSecurity(content+"\n"+payload), "all scanner categories remain active outside omitted documentation")
		inside := strings.Replace(content, "Read approved records.", payload, 1)
		require.NotEmpty(t, scanImportedMarkdownSecurity(inside), "other source security checks also remain active inside omitted documentation")
	}
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
				if name == "matching" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "failed security scan")
					require.ErrorContains(t, err, spec)
				}
			})
		}
	}
}
