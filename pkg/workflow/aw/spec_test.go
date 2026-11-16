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

// SPEC_AMBIGUITY: README.md does not define execution-block forms, authored
// dependency input forms, status-only opt-in syntax, or prohibited agent-data
// references. Those cases need a fuller specification before they can be asserted
// without assumptions. The explicit graph and empty-condition contracts are
// tested below.

// TestSpec_PublicAPI_ParseActionsExpression checks README.md / Public API:
// accept one nonempty expression using syntax alone, without semantic validation.
func TestSpec_PublicAPI_ParseActionsExpression(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		expression string
		wantErr    bool
	}{
		{name: "context reference", expression: "github.ref"},
		{name: "comparison", expression: "github.ref == 'refs/heads/main'"},
		{name: "unvalidated context", expression: "spec_unknown_context.value"},
		{name: "unvalidated function", expression: "spec_unknown_function('value')"},
		{name: "empty expression", wantErr: true},
		{name: "incomplete comparison", expression: "github.ref ==", wantErr: true},
		{name: "two expressions", expression: "github.ref github.sha", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, tree, err := aw.ParseActionsExpression(tt.expression)
			if tt.wantErr {
				require.Error(t, err, "input must contain exactly one complete nonempty expression: %s", tt.name)
				return
			}
			require.NoError(t, err, "syntactically valid expressions must parse without semantic validation: %s", tt.name)
			assert.NotNil(t, tree, "parsing a nonempty expression must return its syntax tree")
		})
	}
}

// TestSpec_PublicAPI_JobDependencyGraphDependsOn checks README.md / Public API:
// dependency queries are transitive and safe before cycle validation.
func TestSpec_PublicAPI_JobDependencyGraphDependsOn(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		graph      aw.JobDependencyGraph
		job        string
		dependency string
		expected   bool
	}{
		{name: "direct dependency", graph: aw.JobDependencyGraph{"publish": {"build"}, "build": {}}, job: "publish", dependency: "build", expected: true},
		{name: "transitive dependency", graph: aw.JobDependencyGraph{"publish": {"test"}, "test": {"build"}, "build": {}}, job: "publish", dependency: "build", expected: true},
		{name: "disconnected jobs", graph: aw.JobDependencyGraph{"publish": {}, "build": {}}, job: "publish", dependency: "build"},
		{name: "dependency is directional", graph: aw.JobDependencyGraph{"publish": {"build"}, "build": {}}, job: "build", dependency: "publish"},
		{name: "reachable through a cycle", graph: aw.JobDependencyGraph{"first": {"second"}, "second": {"first", "build"}, "build": {}}, job: "first", dependency: "build", expected: true},
		{name: "cycle disconnected from target", graph: aw.JobDependencyGraph{"first": {"second"}, "second": {"first"}, "build": {}}, job: "first", dependency: "build"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.graph.DependsOn(tt.job, tt.dependency), "dependency queries must follow graph edges even before cycle validation: %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_HasSuccessfulDetectionCondition checks README.md / Public
// API's explicit rule that an empty condition is considered successful.
func TestSpec_PublicAPI_HasSuccessfulDetectionCondition(t *testing.T) {
	t.Parallel()
	assert.True(t, aw.HasSuccessfulDetectionCondition(""), "an empty job condition must be considered successful")
}

// TestSpec_PublicAPI_FrontmatterResult follows README.md / Public API: callers
// can obtain the parser's frontmatter result and its source locations.
// SPEC_AMBIGUITY: prompt whitespace in the shared parser result is unspecified;
// byte preservation is tested through Parse/Emit, whose contract promises it.
func TestSpec_PublicAPI_FrontmatterResult(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		source string
	}{
		{name: "LF", source: "---\n# authored comment\nname: Example\n---\n\nPrompt.\n"},
		{name: "CRLF", source: "---\r\n# authored comment\r\nname: Example\r\n---\r\n\r\nPrompt.\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			document, err := aw.Parse(tt.source)
			require.NoError(t, err, "a workflow must parse before requesting its frontmatter result")
			result := document.FrontmatterResult()
			require.NotNil(t, result, "a parsed document must expose its frontmatter result")
			assert.Equal(t, document.Frontmatter, result.Frontmatter, "the parser result must retain the document's frontmatter")
			assert.Equal(t, document.Location.FrontmatterLines, result.FrontmatterLines, "the parser result must retain original frontmatter lines")
			assert.Equal(t, document.Location.FrontmatterStart, result.FrontmatterStart, "the parser result must retain the frontmatter source location")
			assert.Equal(t, document.Location.FieldLines, result.FieldLines, "the parser result must retain field source locations")
		})
	}
}

// TestSpec_PublicAPI_ExecutionBlockFieldPath follows README.md / Public API:
// FieldPath joins the field name to the execution block's source path.
// SPEC_AMBIGUITY: the path notation is unspecified; only retention of the block
// path and field name is asserted.
func TestSpec_PublicAPI_ExecutionBlockFieldPath(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		path  string
		field string
	}{
		{name: "engine field", path: "engine", field: "steps"},
		{name: "custom job field", path: "jobs.build", field: "if"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			block := aw.ExecutionBlock{Path: tt.path}
			path := block.FieldPath(tt.field)
			assert.Contains(t, path, tt.path, "the joined field path must retain its execution block's source path")
			assert.Contains(t, path, tt.field, "the joined field path must retain the requested field name")
		})
	}
}

// TestSpec_PublicAPI_IsPreAgentCustomJobRequiresProof follows README.md /
// Public API: graph edges must prove that a custom job precedes the agent.
// SPEC_AMBIGUITY: built-in job identifiers and detection-data edges are not
// specified, so these cases only assert the absence of proof without edges.
func TestSpec_PublicAPI_IsPreAgentCustomJobRequiresProof(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		graph aw.JobDependencyGraph
	}{
		{name: "nil graph"},
		{name: "empty graph", graph: aw.JobDependencyGraph{}},
		{name: "isolated custom job", graph: aw.JobDependencyGraph{"prepare": {}}},
		{name: "disconnected custom jobs", graph: aw.JobDependencyGraph{"prepare": {}, "check": {}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, tt.graph.IsPreAgentCustomJob("prepare"), "a graph without edges cannot prove a custom job precedes the agent")
		})
	}
}
