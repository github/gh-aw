//go:build !integration

package cli

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/codemod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyConnectorImportsCodemod(t *testing.T) {
	t.Parallel()
	mod := getLegacyConnectorImportsCodemod()
	assert.Equal(t, "legacy-connector-imports", mod.ID)
	assert.NotEmpty(t, mod.Name)
	assert.NotEmpty(t, mod.Description)
	assert.NotEmpty(t, mod.IntroducedIn)
	require.NotNil(t, mod.Apply)

	for _, test := range []struct {
		name, header string
		imports      []any
		outputs      map[string]any
	}{
		{"linear", "safe-outputs:\n  linear-create-issue: {max: 2} # keep selection\n  linear-token: ${{ secrets.LINEAR_CUSTOM }}\n",
			[]any{map[string]any{"uses": ".github/workflows/connectors/linear.md", "with": map[string]any{"writer-secret": "LINEAR_CUSTOM"}}},
			map[string]any{"linear-create-issue": map[string]any{"max": uint64(2)}}},
		{"jira", "safe-outputs:\n  jira-create-issue:\n  jira-update-issue: {}\n  jira-add-comment: {staged: true}\n  jira-add-label:\n",
			[]any{".github/workflows/connectors/jira.md"},
			map[string]any{"jira-create-issue": nil, "jira-update-issue": map[string]any{}, "jira-add-comment": map[string]any{"staged": true}, "jira-add-label": nil}},
		{"ado", "safe-outputs:\n  ado-create-work-item:\n  ado-update-work-item:\n  ado-comment-on-work-item: {max: 2} # keep selection\n  ado-assign-work-item:\n  ado-link-work-items:\n",
			[]any{map[string]any{"uses": ".github/workflows/connectors/azure-devops.md", "with": map[string]any{"organization": "", "project": "", "project-name": ""}}},
			map[string]any{"azure-devops-create-work-item": nil, "azure-devops-update-work-item": nil, "azure-devops-comment-on-work-item": map[string]any{"max": uint64(2)}, "azure-devops-assign-work-item": nil, "azure-devops-link-work-items": nil}},
		{"existing import", "imports:\n  - common.md # keep import\n  - uses: .github/workflows/connectors/linear.md\n    with:\n      teams: [approved]\nsafe-outputs:\n  linear-add-comment:\n  linear-token: ${{ secrets.LINEAR_CUSTOM }}\n",
			[]any{"common.md", map[string]any{"uses": ".github/workflows/connectors/linear.md", "with": map[string]any{"teams": []any{"approved"}, "writer-secret": "LINEAR_CUSTOM"}}},
			map[string]any{"linear-add-comment": nil}},
		{"remote import", "imports:\n  - uses: github/gh-aw/.github/workflows/connectors/jira.md@abcdef\n    with: {projects: [APP]}\nsafe-outputs:\n  jira-create-issue:\n",
			[]any{map[string]any{"uses": "github/gh-aw/.github/workflows/connectors/jira.md@abcdef", "with": map[string]any{"projects": []any{"APP"}}}},
			map[string]any{"jira-create-issue": nil}},
		{"mixed providers", "safe-outputs:\n  linear-add-comment:\n  jira-add-comment:\n  ado-link-work-items:\n  create-issue:\n",
			[]any{".github/workflows/connectors/linear.md", ".github/workflows/connectors/jira.md", map[string]any{"uses": ".github/workflows/connectors/azure-devops.md", "with": map[string]any{"organization": "", "project": "", "project-name": ""}}},
			map[string]any{"linear-add-comment": nil, "jira-add-comment": nil, "azure-devops-link-work-items": nil, "create-issue": nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := "---\nname: 'Keep quotes' # keep header\n" + test.header + "---\n\n# Body\nDo not rewrite ado-create-work-item here.\n"
			parsed, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)
			result, _, err := mod.Apply(content, parsed.Frontmatter)
			require.NoError(t, err)
			updated, err := parser.ExtractFrontmatterFromContent(result)
			require.NoError(t, err)
			assert.Equal(t, test.imports, updated.Frontmatter["imports"])
			assert.Equal(t, test.outputs, updated.Frontmatter["safe-outputs"])
			assert.Contains(t, result, "name: 'Keep quotes' # keep header")
			assert.True(t, strings.HasSuffix(result, "---\n\n# Body\nDo not rewrite ado-create-work-item here.\n"))
			if strings.Contains(content, "# keep selection") {
				assert.Contains(t, result, "# keep selection")
			}
			if strings.Contains(content, "# keep import") {
				assert.Contains(t, result, "# keep import")
			}
			repeated, applied, err := mod.Apply(result, updated.Frontmatter)
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, result, repeated)
		})
	}
}

func TestLegacyConnectorImportsCodemodNoOpAndErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		header string
		fail   bool
	}{
		{"safe-outputs:\n  create-issue:\n", false},
		{"tools: {github: {}}\n", false},
		{"safe-outputs:\n  ado-upload-workitem-attachment:\n", false},
		{"imports: invalid\nsafe-outputs:\n  jira-add-comment:\n", true},
		{"safe-outputs:\n  linear-token: literal-token\n", true},
		{"safe-outputs:\n  linear-token: ${{ secrets.ONE || secrets.TWO }}\n", true},
		{"imports:\n  - uses: .github/workflows/connectors/linear.md\n    with: {writer-secret: OTHER}\nsafe-outputs:\n  linear-token: ${{ secrets.CUSTOM }}\n", true},
		{"imports:\n  - uses: .github/workflows/connectors/linear.md\n    with: invalid\nsafe-outputs:\n  linear-token: ${{ secrets.CUSTOM }}\n", true},
		{"safe-outputs:\n  jira-add-comment:\n  ado-create-work-item:\n  azure-devops-create-work-item:\n", true},
	} {
		t.Run(test.header, func(t *testing.T) {
			content := "---\n" + test.header + "---\nBody\n"
			parsed, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)
			result, applied, err := getLegacyConnectorImportsCodemod().Apply(content, parsed.Frontmatter)
			if test.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.False(t, applied)
			assert.Equal(t, content, result)
		})
	}
}

func TestLegacyConnectorImportsCodemodCompiler(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	path := filepath.Join(root, ".github", "workflows", "legacy-connector-test.md")
	for _, test := range []struct {
		provider, selection string
		grants              map[string]any
	}{
		{"linear", "linear-create-issue:\n  linear-token: ${{ secrets.CUSTOM_LINEAR }}",
			map[string]any{"teams": []any{"33333333-3333-4333-8333-333333333333"}, "projects": []any{"44444444-4444-4444-8444-444444444444"}}},
		{"jira", "jira-create-issue:",
			map[string]any{"site": "https://example.atlassian.net", "projects": []any{"APP"}}},
		{"azure-devops", "ado-create-work-item:",
			map[string]any{"organization": "contoso", "project": "33333333-3333-4333-8333-333333333333", "project-name": "Approved Project", "work-item-types": []any{"Task"}}},
	} {
		t.Run(test.provider, func(t *testing.T) {
			content := "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\nsafe-outputs:\n  " + test.selection + "\n---\nUse approved resources only.\n"
			_, err := workflow.NewCompiler(workflow.WithNoEmit(true)).ParseWorkflowString(content, path)
			require.Error(t, err)
			parsed, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)
			result, applied, err := getLegacyConnectorImportsCodemod().Apply(content, parsed.Frontmatter)
			require.NoError(t, err)
			require.True(t, applied)
			_, err = workflow.NewCompiler(workflow.WithNoEmit(true)).ParseWorkflowString(result, path)
			require.Error(t, err, "importing a connector must not authorize unscoped writes")

			result, _, err = codemod.Apply(result, func(f *codemod.Frontmatter) error {
				entries, _ := f.Lookup("imports")
				entry := entries.([]any)[0]
				if text, ok := entry.(string); ok {
					entry = map[string]any{"uses": text, "with": map[string]any{}}
				}
				mapping := entry.(map[string]any)
				inputs := maps.Clone(mapping["with"].(map[string]any))
				for key, value := range test.grants {
					inputs[key] = value
				}
				return f.Set([]string{"imports"}, []any{map[string]any{"uses": mapping["uses"], "with": inputs}})
			})
			require.NoError(t, err)
			data, err := workflow.NewCompiler(workflow.WithNoEmit(true)).ParseWorkflowString(result, path)
			require.NoError(t, err)
			require.NotNil(t, data.Connectors[test.provider].SafeOutputs)
		})
	}
}
