//go:build !integration

package aw_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/aw"
)

// TestSpec_PublicAPI_ParseEmit preserves unchanged source bytes as promised in
// README.md / Overview, including comments, line endings, and prompt whitespace.
func TestSpec_PublicAPI_ParseEmit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "comments and prompt whitespace",
			source: "---\n# authored comment\nname: Example # inline comment\n---\n\n  Keep this indentation.  \n\n",
		},
		{
			name:   "CRLF line endings",
			source: "---\r\n# authored comment\r\nname: Example\r\n---\r\n\r\n  Keep this indentation.  \r\n",
		},
		{
			name:   "no final newline",
			source: "---\nname: Example\n---\n\nPrompt without a final newline.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			document, err := aw.Parse(tt.source)
			require.NoError(t, err, "authored Markdown with YAML frontmatter should parse")
			require.NotNil(t, document, "parsing should return a document")
			emitted, err := aw.Emit(document)
			require.NoError(t, err, "an unchanged document should emit successfully")
			assert.Equal(t, tt.source, emitted, "emission should preserve every unchanged source byte")
		})
	}
}

// TestSpec_Types_Document checks README.md / Public API: the document exposes
// frontmatter, prompt text, and source locations.
func TestSpec_Types_Document(t *testing.T) {
	t.Parallel()

	document, err := aw.Parse("---\nname: Example\n---\n\nThe authored prompt.\n")
	require.NoError(t, err, "a document containing frontmatter and prompt text should parse")
	require.NotNil(t, document, "parsing should return a document")
	assert.Equal(t, "Example", document.Frontmatter["name"], "frontmatter should retain the authored value")
	assert.Equal(t, "\nThe authored prompt.\n", document.Markdown, "the prompt should retain its authored whitespace")
	location := document.Location
	assert.NotEmpty(t, location.FrontmatterLines, "source locations should retain frontmatter lines")
	assert.NotEmpty(t, location.FieldLines, "source locations should retain field locations")
	// SPEC_AMBIGUITY: The README does not define line numbering or field-path
	// syntax, so no exact location offsets or keys are asserted.
}

// TestSpec_PublicAPI_EmitEdits checks README.md / Public API: edits serialize
// deterministically while unchanged prompt bytes remain intact.
func TestSpec_PublicAPI_EmitEdits(t *testing.T) {
	t.Parallel()

	const source = "---\nname: Original\n---\n\n  Preserve this prompt.  \n"
	tests := []struct {
		name  string
		value any
	}{
		{name: "string edit", value: "Updated"},
		{name: "mapping edit", value: map[string]any{"z": "last", "a": "first"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var first string
			for attempt := range 3 {
				document, err := aw.Parse(source)
				require.NoError(t, err, "the original document should parse before an edit")
				document.Frontmatter["name"] = tt.value
				emitted, err := aw.Emit(document)
				require.NoError(t, err, "an edited document should emit successfully")
				if attempt == 0 {
					first = emitted
				} else {
					assert.Equal(t, first, emitted, "identical edits should produce identical source bytes")
				}
				reparsed, err := aw.Parse(emitted)
				require.NoError(t, err, "emitted edits should remain parseable")
				assert.Equal(t, tt.value, reparsed.Frontmatter["name"], "emission should retain the edited value")
				assert.Equal(t, document.Markdown, reparsed.Markdown, "frontmatter edits should preserve the prompt")
			}
		})
	}
}

// TestSpec_DesignDecision_CompilerResponsibilities checks README.md / Overview
// and Dependencies: schema validation and import resolution belong to the parent
// compiler, so parsing alone retains authored configuration.
func TestSpec_DesignDecision_CompilerResponsibilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		frontmatter string
		key         string
		expected    any
	}{
		{
			name:        "unresolved import",
			frontmatter: "imports:\n  - ./spec-enforcer-unresolved-import.md\n",
			key:         "imports",
			expected:    []any{"./spec-enforcer-unresolved-import.md"},
		},
		{
			name:        "configuration outside the workflow schema",
			frontmatter: "spec_enforcer_unknown_field: true\n",
			key:         "spec_enforcer_unknown_field",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := strings.Join([]string{"---\n", tt.frontmatter, "---\nPrompt.\n"}, "")
			document, err := aw.Parse(source)
			require.NoError(t, err, "AST parsing should leave compiler responsibilities to the compiler")
			assert.Equal(t, tt.expected, document.Frontmatter[tt.key], "parsing should retain the authored configuration")
		})
	}
}

// SPEC_AMBIGUITY: README.md names ExecutionBlocks, AuthoredJobDependencies, and
// ValidateStatusOnlyJob without defining execution-block forms, dependency input
// forms, or status-only requirements. Their behavior needs a fuller specification
// before it can be asserted without assumptions.
