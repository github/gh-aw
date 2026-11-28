package codemod

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRename(t *testing.T) {
	for _, source := range []string{
		"tools:\n  github:\n    repos: [owner/repo] # keep\n",
		"tools: {github: {repos: [owner/repo]}} # keep\n",
		"'tools': {\"github\": {'repos': [owner/repo]}} # keep\n",
		"other: é\ntools: {github: {\"repos\": [owner/repo]}} # keep\n",
	} {
		t.Run(source, func(t *testing.T) {
			content := "---\n" + source + "---\n\n# Body\nrepos: untouched\n"
			updated, applied, err := Apply(content, func(f *Frontmatter) error {
				return f.Rename([]string{"tools", "github", "repos"}, "allowed-repos")
			})
			require.NoError(t, err)
			assert.True(t, applied)
			assert.Contains(t, updated, "# keep")
			assert.Contains(t, updated, "\n\n# Body\nrepos: untouched\n")
			document, err := aw.Parse(updated)
			require.NoError(t, err)
			tools := document.Frontmatter["tools"].(map[string]any)
			github := tools["github"].(map[string]any)
			assert.Contains(t, github, "allowed-repos")
			assert.NotContains(t, github, "repos")
			repeated, applied, err := Apply(updated, func(f *Frontmatter) error {
				return f.Rename([]string{"tools", "github", "repos"}, "allowed-repos")
			})
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, updated, repeated)
		})
	}
}

func TestFrontmatterNoOp(t *testing.T) {
	for _, content := range []string{
		"# Plain Markdown\n",
		"---\n# comment\n---\n",
		"---\ntools: {github: {repos: [], allowed-repos: null}}\n---\n",
	} {
		updated, applied, err := Apply(content, func(f *Frontmatter) error {
			return f.Rename([]string{"tools", "github", "repos"}, "allowed-repos")
		})
		require.NoError(t, err)
		assert.False(t, applied)
		assert.Equal(t, content, updated)
	}
}

func TestTransferInlineComment(t *testing.T) {
	content := "---\nimports:\n  - uses: .github/workflows/connectors/linear.md\n    with:\n      writer-secret: CUSTOM\nsafe-outputs:\n  linear-token: ${{ secrets.CUSTOM }} # rotated quarterly\n---\n# Body\n"
	updated, applied, err := Apply(content, func(f *Frontmatter) error {
		if err := f.TransferInlineComment(
			[]string{"safe-outputs", "linear-token"},
			[]any{"imports", 0, "with", "writer-secret"},
		); err != nil {
			return err
		}
		return f.Remove("safe-outputs", "linear-token")
	})
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, updated, "writer-secret: CUSTOM # rotated quarterly")
	assert.NotContains(t, updated, "linear-token:")
	assert.Contains(t, updated, "---\n# Body\n")
}

func TestFrontmatterInvalidBodyBytes(t *testing.T) {
	body := "\n# Body \x93quoted\x94 \xff\r\n"
	for _, header := range []string{"", "---\nold: true\n---\n"} {
		for _, migrate := range []bool{false, true} {
			content := header + body
			updated, applied, err := Apply(content, func(f *Frontmatter) error {
				if migrate {
					return f.Set([]string{"old"}, false)
				}
				return nil
			})
			require.NoError(t, err)
			assert.Equal(t, migrate, applied)
			document, err := aw.Parse(updated)
			require.NoError(t, err)
			assert.Equal(t, body, document.Markdown)
			if migrate {
				assert.Equal(t, false, document.Frontmatter["old"])
			} else {
				assert.Equal(t, content, updated)
			}
		}
	}
}

func TestFrontmatterRejectsInvalidUTF8(t *testing.T) {
	for _, source := range []string{"old: \xff\n", "# \xff\nold: true\n"} {
		content := "---\n" + source + "---\n# Body\n"
		called := false
		updated, applied, err := Apply(content, func(f *Frontmatter) error {
			called = true
			return f.Set([]string{"old"}, false)
		})
		require.Error(t, err)
		assert.False(t, called)
		assert.False(t, applied)
		assert.Equal(t, content, updated)
	}
}

func TestFrontmatterSetStringContexts(t *testing.T) {
	for _, source := range []string{
		"tools: {github: {allowed-repos: current}}\n",
		"tools: {github: {allowed-repos: 'current'}}\n",
		"tools: {github: {allowed-repos: \"current\"}}\n",
		"tools:\n  github:\n    allowed-repos: current\n",
	} {
		for _, value := range []string{"${{ github.repository }}", "one,two", "one[two]", "one{two}", "normal", "true"} {
			t.Run(source+value, func(t *testing.T) {
				content := "---\n" + source + "---\n# Body\n"
				updated, applied, err := Apply(content, func(f *Frontmatter) error {
					return f.Set([]string{"tools", "github", "allowed-repos"}, value)
				})
				require.NoError(t, err)
				assert.True(t, applied)
				document, err := aw.Parse(updated)
				require.NoError(t, err)
				tools := document.Frontmatter["tools"].(map[string]any)
				github := tools["github"].(map[string]any)
				assert.Equal(t, value, github["allowed-repos"])
				assert.Equal(t, "# Body\n", document.Markdown)
				if !strings.Contains(source, "{") && value == "normal" {
					assert.Contains(t, updated, "allowed-repos: normal\n")
				}
			})
		}
	}
}

func TestFrontmatterAtomicity(t *testing.T) {
	content := "---\nold: true\n---\n\n# Body\n"
	failure := errors.New("transform failed")
	for _, transform := range []func(*Frontmatter) error{
		func(f *Frontmatter) error {
			require.NoError(t, f.Rename([]string{"old"}, "new"))
			return failure
		},
		func(f *Frontmatter) error {
			return f.Edit(func(document *aw.Document) error {
				document.Markdown = "changed"
				return nil
			})
		},
		func(f *Frontmatter) error {
			return f.Set([]string{"old", "child"}, true)
		},
	} {
		updated, applied, err := Apply(content, transform)
		require.Error(t, err)
		assert.False(t, applied)
		assert.Equal(t, content, updated)
	}
}

func TestFrontmatterSemanticEdit(t *testing.T) {
	content := "---\n# settings\nengine: {id: copilot, old: true}\nsteps:\n  - run: |\n      echo hello\n      echo world\n---\n\n# Body  \r\n"
	updated, applied, err := Apply(content, func(f *Frontmatter) error {
		require.NoError(t, f.Remove("engine", "old"))
		require.NoError(t, f.Set([]string{"engine", "model"}, "test-model"))
		value, found := f.Lookup("engine", "model")
		assert.True(t, found)
		assert.Equal(t, "test-model", value)
		return nil
	})
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, updated, "# settings")
	assert.Contains(t, updated, "run: |\n      echo hello\n      echo world")
	assert.Contains(t, updated, "---\n\n# Body  \r\n")
}

func TestFrontmatterCRLF(t *testing.T) {
	content := "---\r\ntools: {github: {repos: []}}\r\n---\r\n\r\n# Body\r\n"
	updated, applied, err := Apply(content, func(f *Frontmatter) error {
		return f.Rename([]string{"tools", "github", "repos"}, "allowed-repos")
	})
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, "---\r\ntools: {github: {allowed-repos: []}}\r\n---\r\n\r\n# Body\r\n", updated)
}

func TestFrontmatterCRLFScalarSpan(t *testing.T) {
	content := "---\r\nbots:\r\n  - dependabot # duplicate\r\n  # Unique bot\r\n  # Keep this explanation\r\n  - renovate # source inline\r\n  # Another unique bot\r\n  - other\r\non:\r\n  bots:\r\n    # Destination explanation\r\n    - dependabot # destination inline\r\n---\r\n\r\n# Body\r\n"
	updated, applied, err := Apply(content, func(f *Frontmatter) error {
		_, node, err := f.entry([]string{"on", "bots"})
		require.NoError(t, err)
		start, end, found := f.scalarSpan(node.Content[0])
		require.True(t, found)
		assert.Equal(t, "dependabot", content[start:end])
		return f.MergeSequence([]string{"bots"}, []string{"on", "bots"})
	})
	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, strings.ReplaceAll(updated, "\r\n", ""), "\n")
	document, err := aw.Parse(updated)
	require.NoError(t, err)
	assert.NotContains(t, document.Frontmatter, "bots")
	assert.Equal(t, []any{"dependabot", "renovate", "other"}, document.Frontmatter["on"].(map[string]any)["bots"])
	assert.Equal(t, "\r\n# Body\r\n", document.Markdown)
}

func TestFrontmatterSetNewDocument(t *testing.T) {
	for _, content := range []string{"# Body\n", "---\n---\n# Body\n", "---\n# settings\n---\n# Body\n"} {
		updated, applied, err := Apply(content, func(f *Frontmatter) error {
			return f.Set([]string{"tools", "bash"}, false)
		})
		require.NoError(t, err)
		assert.True(t, applied)
		document, err := aw.Parse(updated)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"bash": false}, document.Frontmatter["tools"])
		assert.Equal(t, "# Body\n", document.Markdown)
		if strings.Contains(content, "# settings") {
			assert.Contains(t, updated, "# settings")
		}
	}
}

func TestFrontmatterAliasesRemainIndependent(t *testing.T) {
	content := "---\nengine: &engine {old: true}\nother: *engine\n---\n# Body\n"
	for _, operation := range []func(*Frontmatter) error{
		func(f *Frontmatter) error { return f.Rename([]string{"engine", "old"}, "new") },
		func(f *Frontmatter) error { return f.Set([]string{"engine", "old"}, false) },
		func(f *Frontmatter) error { return f.Remove("engine", "old") },
	} {
		updated, applied, err := Apply(content, operation)
		require.NoError(t, err)
		assert.True(t, applied)
		document, err := aw.Parse(updated)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"old": true}, document.Frontmatter["other"])
	}
}

func TestFrontmatterCRLFStructuralEdits(t *testing.T) {
	content := "---\r\non: push\r\nengine:\r\n  old: true # keep\r\n  id: copilot\r\n---\r\n# Body\r\n"
	updated, applied, err := Apply(content, func(f *Frontmatter) error {
		if err := f.Set([]string{"tools"}, map[string]any{"bash": false}); err != nil {
			return err
		}
		return f.Move([]string{"engine", "old"}, []string{"new"})
	})
	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, strings.ReplaceAll(updated, "\r\n", ""), "\n")
	assert.Contains(t, updated, "new: true # keep\r\n")
}

func TestFrontmatterInvalidOperations(t *testing.T) {
	content := "---\ntools: {bash: false}\n---\n"
	for _, operation := range []func(*Frontmatter) error{
		func(f *Frontmatter) error { return f.PruneEmpty() },
		func(f *Frontmatter) error { return f.Move([]string{"tools"}, []string{"tools", "nested"}) },
		func(f *Frontmatter) error {
			return f.RewriteText(func(string) string { return "invalid: [\n" })
		},
	} {
		updated, applied, err := Apply(content, operation)
		require.Error(t, err)
		assert.False(t, applied)
		assert.Equal(t, content, updated)
	}
}

func FuzzFrontmatterEdits(f *testing.F) {
	for _, value := range []string{"repo", "a: b", "one\n---\ntwo\n", "é # comment", "'quoted'", "${{ github.repository }}", "", "\t!è"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		original := map[string]any{
			"tools": map[string]any{"github": map[string]any{"repos": value}},
			"other": map[string]any{"repos": value},
		}
		encoded, err := yaml.MarshalWithOptions(original, yaml.JSON())
		require.NoError(t, err)
		body := "\n# Body\nrepos: untouched\n"
		content := "---\n" + string(encoded) + "---\n" + body
		if !utf8.ValidString(content) {
			updated, applied, err := Apply(content, func(*Frontmatter) error { return nil })
			require.Error(t, err)
			assert.False(t, applied)
			assert.Equal(t, content, updated)
			return
		}
		before, err := aw.Parse(content)
		require.NoError(t, err)
		updated, applied, err := Apply(content, func(f *Frontmatter) error {
			if err := f.Rename([]string{"tools", "github", "repos"}, "allowed-repos"); err != nil {
				return err
			}
			if err := f.Set([]string{"tools", "bash"}, false); err != nil {
				return err
			}
			return f.Remove("tools", "github", "allowed-repos")
		})
		require.NoError(t, err)
		assert.True(t, applied)
		document, err := aw.Parse(updated)
		require.NoError(t, err)
		assert.Equal(t, before.Frontmatter["other"], document.Frontmatter["other"])
		assert.Equal(t, body, document.Markdown)
		tools := document.Frontmatter["tools"].(map[string]any)
		assert.Equal(t, false, tools["bash"])
		assert.Equal(t, map[string]any{}, tools["github"])
	})
}
