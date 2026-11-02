//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConnectorEmptyPublicResourceGrants(t *testing.T) {
	for _, test := range []struct {
		name, selection string
		emptyIssues     bool
		reject          bool
	}{
		{"unused-empty-grant", "tools:\n  tracker: [get-issue]\n", false, false},
		{"selected-empty-grant", "tools:\n  tracker: [get-issue]\n", true, true},
		{"registration-without-grants", "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var declaration map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(awConnectorSource(t)), &declaration))
			config := declaration["connectors"].(map[string]any)["tracker"].(map[string]any)
			resources := config["resources"].(map[string]any)
			resources["unused"] = []string{}
			if test.emptyIssues {
				resources["issues"] = []string{}
			}
			content, err := yaml.Marshal(declaration)
			require.NoError(t, err)
			root := t.TempDir()
			path := filepath.Join(root, ".github", "workflows", "grants.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte(awConnectorHeader+string(content)+test.selection+"---\nRead approved references.\n"), 0600))
			compiler := NewCompiler(WithNoEmit(true))
			compiler.gitRoot = root
			data, err := compiler.ParseWorkflowFile(path)
			if test.reject {
				require.ErrorContains(t, err, "resource allowlist")
				return
			}
			require.NoError(t, err)
			if test.selection == "" {
				require.Empty(t, data.Connectors)
			} else {
				require.NotNil(t, data.Connectors["tracker"].Agent)
			}
		})
	}
}
