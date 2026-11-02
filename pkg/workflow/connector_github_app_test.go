//go:build !integration

package workflow

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func connectorAppSource(t *testing.T) string {
	t.Helper()
	doc, err := parser.ExtractFrontmatterFromContent("---\n" + awConnectorSource(t) + "---\n")
	require.NoError(t, err)
	declarations := doc.Frontmatter["connectors"].(map[string]any)
	credentials := declarations["tracker"].(map[string]any)["credentials"].(map[string]any)
	for _, name := range []string{"reader", "writer"} {
		credential := credentials[name].(map[string]any)
		delete(credential, "secret")
		level := "read"
		if name == "writer" {
			level = "write"
		}
		credential["github-app"] = map[string]any{
			"client-id": "${{ vars.CONNECTOR_APP_ID }}", "private-key": "${{ secrets.CONNECTOR_APP_KEY }}",
			"owner": "example", "repositories": []string{"issues"},
			"permissions": map[string]string{"issues": level},
		}
	}
	content, err := yaml.Marshal(doc.Frontmatter)
	require.NoError(t, err)
	return string(content)
}

func TestConnectorGitHubAppPhaseTokens(t *testing.T) {
	for _, test := range []struct {
		name, selection string
		read, write     bool
	}{
		{"unselected", "", false, false},
		{"agent", "tools:\n  tracker: [get-issue]\n", true, false},
		{"safe-output", "safe-outputs:\n  tracker-add-comment:\n", false, true},
		{"combined", "tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n", true, true},
		{"staged", "safe-outputs:\n  staged: true\n  tracker-add-comment:\n", false, false},
		{"dynamic-staging", "safe-outputs:\n  staged: ${{ inputs.stage }}\n  tracker-add-comment:\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAppSource(t)+test.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
			assert.Equal(t, test.read, strings.Contains(output, "Mint connector GitHub App token tracker/reader"))
			assert.Equal(t, test.write, strings.Contains(output, "Mint connector GitHub App token tracker/writer"))
			assert.NotContains(t, output, "${{ secrets."+connectorAppSecret("tracker", "reader")+" }}")
			assert.NotContains(t, output, "${{ secrets."+connectorAppSecret("tracker", "writer")+" }}")
			if test.read || test.write {
				assert.Contains(t, output, "skip-token-revoke: false")
				assert.Contains(t, output, "repositories: issues")
				assert.Contains(t, output, "actions/create-github-app-token@")
			}
			if test.name == "dynamic-staging" {
				assert.Contains(t, output, "if: ${{ format('{0}', inputs.stage) != 'true' }}")
			}
			if test.read {
				encoded, err := data.Connectors["tracker"].Agent.Encode()
				require.NoError(t, err)
				plan, err := base64.StdEncoding.DecodeString(encoded)
				require.NoError(t, err)
				assert.NotContains(t, string(plan), "CONNECTOR_APP_KEY")
				assert.NotContains(t, string(plan), "CONNECTOR_APP_ID")
			}
		})
	}
}

func TestConnectorGitHubAppRejectsUnboundedCredentials(t *testing.T) {
	for _, test := range []struct{ name, old, replacement, message string }{
		{"agent-write", "issues: read", "issues: write", "read-only"},
		{"missing-key", "${{ secrets.CONNECTOR_APP_KEY }}", "inline-private-key", "private-key secrets reference"},
		{"event-id", "${{ vars.CONNECTOR_APP_ID }}", "${{ github.event.issue.title }}", "literal client-id"},
		{"wildcard-repo", "- issues", "- '*'", "exact repository names"},
		{"expression-owner", "owner: example", "owner: '${{ github.actor }}'", "literal installation owner"},
		{"no-permissions", "permissions:\n                        issues: read", "permissions: {}", "permissions"},
		{"both-sources", "                github-app:", "                secret: OTHER_TOKEN\n                github-app:", "exactly one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := connectorAppSource(t)
			require.Contains(t, source, test.old)
			source = strings.Replace(source, test.old, test.replacement, 1)
			_, err := c.ParseWorkflowString(awConnectorHeader+source+"tools:\n  tracker: [get-issue]\n---\nReview.\n", root+"/proof.md")
			require.ErrorContains(t, err, test.message)
		})
	}
}
