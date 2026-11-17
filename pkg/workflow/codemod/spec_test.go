//go:build !integration

package codemod_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

const specMarkdown = "\n# Authored workflow\n\nThe word before belongs to Markdown.\n"

const specFrontmatter = "---\n" +
	"# authored comment\n" +
	"keep: \"quoted\" # keep comment\n" +
	"source: before # original source comment\n" +
	"destination: authoritative\n" +
	"empty: {}\n" +
	"list-source: [alpha]\n" +
	"list-target: [beta]\n" +
	"---\n"

// TestSpec_PublicAPI_Apply checks README / Public API: no-op transformations
// preserve every byte, and errors roll back the entire transformation.
func TestSpec_PublicAPI_Apply(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		content string
		edit    bool
	}{
		{name: "LF no-op", content: specFrontmatter + specMarkdown},
		{name: "CRLF no-op", content: strings.ReplaceAll(specFrontmatter+specMarkdown, "\n", "\r\n")},
		{name: "rollback after an edit", content: specFrontmatter + specMarkdown, edit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, _, err := codemod.Apply(tt.content, func(frontmatter *codemod.Frontmatter) error {
				if !tt.edit {
					return nil
				}
				if err := frontmatter.Set([]string{"source"}, "after"); err != nil {
					t.Errorf("the valid edit preceding rollback should succeed: %v", err)
					return err
				}
				return errors.New("spec transformation failed")
			})
			if tt.edit {
				require.Error(t, err, "a failed transformation should return an error")
			} else {
				require.NoError(t, err, "a no-op transformation should succeed")
			}
			assert.Equal(t, tt.content, result, "no-op and failed transformations should return byte-identical input")
		})
	}
}

// TestSpec_PublicAPI_Lookup checks README / Public API: paths are exact and
// present null values remain distinguishable from absent fields.
func TestSpec_PublicAPI_Lookup(t *testing.T) {
	t.Parallel()
	content := "---\nnested:\n  value: nested-value\nnested.value: dotted-value\noptional: null\n---\n" + specMarkdown

	for _, tt := range []struct {
		name    string
		path    []string
		present bool
		value   any
	}{
		{name: "nested mapping", path: []string{"nested", "value"}, present: true, value: "nested-value"},
		{name: "literal dotted key", path: []string{"nested.value"}, present: true, value: "dotted-value"},
		{name: "present null", path: []string{"optional"}, present: true},
		{name: "absent field", path: []string{"missing"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, _, err := codemod.Apply(content, func(frontmatter *codemod.Frontmatter) error {
				value, present := frontmatter.Lookup(tt.path...)
				assert.Equal(t, tt.present, present, "Lookup should report presence for the exact authored path")
				if tt.present {
					assert.Equal(t, tt.value, value, "Lookup should return the authored value, including null")
				}
				return nil
			})
			require.NoError(t, err, "looking up authored fields should succeed")
			assert.Equal(t, content, result, "Lookup should leave the source byte-identical")
		})
	}
}

// TestSpec_PublicAPI_MappingEdits checks README / Overview and Public API:
// authored paths change while unrelated syntax and Markdown remain intact.
// SPEC_AMBIGUITY: The README does not define the exact formatting of changed
// fields, Move collisions, or MergeSequence ordering and duplicate handling.
func TestSpec_PublicAPI_MappingEdits(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		transform func(*codemod.Frontmatter) error
		path      []string
		present   bool
		value     any
		removed   []string
		sequence  bool
	}{
		{
			name: "Set",
			transform: func(f *codemod.Frontmatter) error {
				return f.Set([]string{"source"}, "after")
			},
			path: []string{"source"}, present: true, value: "after",
		},
		{
			name: "Remove",
			transform: func(f *codemod.Frontmatter) error {
				return f.Remove("source")
			},
			path: []string{"source"},
		},
		{
			name: "Rename",
			transform: func(f *codemod.Frontmatter) error {
				return f.Rename([]string{"source"}, "renamed")
			},
			path: []string{"renamed"}, present: true, value: "before", removed: []string{"source"},
		},
		{
			name: "Rename retains authoritative destination",
			transform: func(f *codemod.Frontmatter) error {
				return f.Rename([]string{"source"}, "destination")
			},
			path: []string{"destination"}, present: true, value: "authoritative",
		},
		{
			name: "PruneEmpty",
			transform: func(f *codemod.Frontmatter) error {
				return f.PruneEmpty("empty")
			},
			path: []string{"empty"},
		},
		{
			name: "Move",
			transform: func(f *codemod.Frontmatter) error {
				return f.Move([]string{"source"}, []string{"moved"})
			},
			path: []string{"moved"}, present: true, value: "before", removed: []string{"source"},
		},
		{
			name: "MergeSequence",
			transform: func(f *codemod.Frontmatter) error {
				return f.MergeSequence([]string{"list-source"}, []string{"list-target"})
			},
			path: []string{"list-target"}, present: true, value: []any{"alpha", "beta"}, sequence: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, _, err := codemod.Apply(specFrontmatter+specMarkdown, tt.transform)
			require.NoError(t, err, "the documented %s edit should succeed", tt.name)
			assert.Contains(t, result, "# authored comment\n", "edits should preserve unrelated comments")
			assert.Contains(t, result, "keep: \"quoted\" # keep comment\n", "edits should preserve unrelated quoting and inline comments")
			assert.True(t, strings.HasSuffix(result, "---\n"+specMarkdown), "edits should preserve the complete Markdown body")

			_, _, err = codemod.Apply(result, func(frontmatter *codemod.Frontmatter) error {
				value, present := frontmatter.Lookup(tt.path...)
				assert.Equal(t, tt.present, present, "%s should update the selected mapping path", tt.name)
				if present {
					if tt.sequence {
						assert.ElementsMatch(t, tt.value, value, "merging should retain both disjoint sequence values")
					} else {
						assert.Equal(t, tt.value, value, "%s should expose the documented value", tt.name)
					}
				}
				if tt.removed != nil {
					_, remains := frontmatter.Lookup(tt.removed...)
					assert.False(t, remains, "%s should remove the original mapping path", tt.name)
				}
				return nil
			})
			require.NoError(t, err, "the edited frontmatter should remain readable")
		})
	}
}

// TestSpec_PublicAPI_Edit checks README / Public API: semantic edits operate
// on an independent document and preserve the Markdown body.
func TestSpec_PublicAPI_Edit(t *testing.T) {
	t.Parallel()
	result, _, err := codemod.Apply(specFrontmatter+specMarkdown, func(frontmatter *codemod.Frontmatter) error {
		original := frontmatter.Document
		return frontmatter.Edit(func(document *aw.Document) error {
			assert.NotSame(t, original, document, "Edit should provide an independent document")
			document.Frontmatter["source"] = "after"
			assert.Equal(t, "before", original.Frontmatter["source"], "semantic edits should not mutate the original document")
			return nil
		})
	})
	require.NoError(t, err, "a semantic frontmatter edit should succeed")
	assert.True(t, strings.HasSuffix(result, "---\n"+specMarkdown), "semantic editing should preserve the Markdown body")
	_, _, err = codemod.Apply(result, func(frontmatter *codemod.Frontmatter) error {
		value, present := frontmatter.Lookup("source")
		assert.True(t, present, "the semantically edited field should remain present")
		assert.Equal(t, "after", value, "the semantic edit should be reflected in the emitted frontmatter")
		return nil
	})
	require.NoError(t, err, "semantically edited frontmatter should remain readable")
}

// TestSpec_PublicAPI_Comment checks README / Public API: annotations preserve
// existing comments and the field's authored value.
func TestSpec_PublicAPI_Comment(t *testing.T) {
	t.Parallel()
	result, _, err := codemod.Apply(specFrontmatter+specMarkdown, func(frontmatter *codemod.Frontmatter) error {
		return frontmatter.Comment([]string{"source"}, "spec annotation")
	})
	require.NoError(t, err, "a single-line annotation should be accepted")
	assert.Contains(t, result, "original source comment", "adding an annotation should preserve the existing comment")
	assert.Contains(t, result, "spec annotation", "the new annotation should appear in frontmatter")
	assert.True(t, strings.HasSuffix(result, "---\n"+specMarkdown), "annotations should preserve the Markdown body")
	_, _, err = codemod.Apply(result, func(frontmatter *codemod.Frontmatter) error {
		value, present := frontmatter.Lookup("source")
		assert.True(t, present, "an annotated field should remain present")
		assert.Equal(t, "before", value, "an annotation should not replace the field value")
		return nil
	})
	require.NoError(t, err, "annotated frontmatter should remain readable")
}

// TestSpec_PublicAPI_RewriteText checks README / Public API: lexical
// transformations affect frontmatter while leaving Markdown outside the edit.
func TestSpec_PublicAPI_RewriteText(t *testing.T) {
	t.Parallel()
	result, _, err := codemod.Apply(specFrontmatter+specMarkdown, func(frontmatter *codemod.Frontmatter) error {
		return frontmatter.RewriteText(func(text string) string {
			return strings.ReplaceAll(text, "before", "after")
		})
	})
	require.NoError(t, err, "a valid lexical frontmatter transformation should succeed")
	assert.Contains(t, result, "source: after # original source comment\n", "the lexical replacement should affect frontmatter")
	assert.True(t, strings.HasSuffix(result, "---\n"+specMarkdown), "the same word in Markdown should remain unchanged")
}
