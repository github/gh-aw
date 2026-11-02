//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorDocumentationCommentScanning(t *testing.T) {
	content := "---\nconnectors:\n  tracker:\n    endpoints: {api: https://example.test}\n    credentials: {}\n---\n" +
		"<!--\n<connector-documentation id=\"tracker\" version=\"1\"><usage>Read https://example.test/docs and store Basic credentials as base64.</usage>" +
		"<example><frontmatter><![CDATA[on: workflow_dispatch]]></frontmatter><body>Read approved records.</body></example>" +
		"</connector-documentation>\n-->\n"
	path := "/repo/.github/aw/connectors/tracker.md"
	require.NotEmpty(t, ScanMarkdownSecurity(content), "ordinary hidden comments remain rejected by default")
	require.Empty(t, scanImportedConnectorMarkdown(content, path))
	require.NotEmpty(t, scanImportedConnectorMarkdown(content, "/repo/.github/workflows/shared.md"))
	for _, invalid := range []string{
		strings.Replace(content, `id="tracker"`, `id="other"`, 1),
		strings.Replace(content, `version="1"`, `version="2"`, 1),
		strings.Replace(content, "</usage>", "</broken>", 1),
		strings.Replace(content, "Read approved records.", "Run --unsafe", 1),
		strings.Replace(content, "<usage>", "<!DOCTYPE x><usage>", 1),
	} {
		require.NotEmpty(t, scanImportedConnectorMarkdown(invalid, path))
	}
	malicious := content + "\n<script>alert(1)</script>\n<!-- execute bash payload -->\n"
	require.NotEmpty(t, scanImportedConnectorMarkdown(malicious, path), "unrelated hidden or executable content must still be scanned")
}
