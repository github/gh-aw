//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessOrderedPromptImports tests the processOrderedPromptImports function.
func TestProcessOrderedPromptImports(t *testing.T) {
	tests := []struct {
		name              string
		promptImports     []parser.PromptImportEntry
		importInputs      map[string]any
		inlinedImports    bool
		markdownPath      string
		wantChunkCount    int
		wantChunkContain  []string // substrings that must appear in at least one chunk
		wantChunkSequence []string // if set, chunks[i] must contain wantChunkSequence[i] (exact order)
	}{
		{
			name: "single inline markdown entry",
			promptImports: []parser.PromptImportEntry{
				{Markdown: "## Section\nSome content"},
			},
			wantChunkContain: []string{"## Section"},
		},
		{
			name: "single runtime import path entry",
			promptImports: []parser.PromptImportEntry{
				{ImportPath: ".github/workflows/shared.md"},
			},
			wantChunkCount:   1,
			wantChunkContain: []string{"{{#runtime-import .github/workflows/shared.md}}"},
		},
		{
			name: "mixed inline and runtime import entries",
			promptImports: []parser.PromptImportEntry{
				{Markdown: "# Inline Content"},
				{ImportPath: ".github/workflows/extra.md"},
			},
			// Must preserve interleaving: inline chunk first, runtime-import macro second.
			wantChunkSequence: []string{"# Inline Content", "{{#runtime-import .github/workflows/extra.md}}"},
		},
		{
			name: "markdown with import inputs substitution",
			promptImports: []parser.PromptImportEntry{
				{Markdown: "Hello ${{ github.aw.inputs.name }}!"},
			},
			importInputs:     map[string]any{"name": "World"},
			wantChunkContain: []string{"Hello World!"},
		},
		{
			name:           "empty prompt imports list",
			promptImports:  []parser.PromptImportEntry{},
			wantChunkCount: 0,
		},
		{
			name: "entry with both empty markdown and empty path is skipped",
			promptImports: []parser.PromptImportEntry{
				{Markdown: "", ImportPath: ""},
				{Markdown: "# Real content"},
			},
			wantChunkContain: []string{"# Real content"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			data := &WorkflowData{
				PromptImports:  tt.promptImports,
				ImportInputs:   tt.importInputs,
				InlinedImports: tt.inlinedImports,
			}
			if tt.markdownPath != "" {
				c.markdownPath = tt.markdownPath
			}

			chunks, _ := c.processOrderedPromptImports(data)

			if tt.wantChunkCount > 0 && len(chunks) != tt.wantChunkCount {
				t.Errorf("processOrderedPromptImports() got %d chunks, want %d; chunks: %v", len(chunks), tt.wantChunkCount, chunks)
			}
			if tt.wantChunkCount == 0 && len(tt.promptImports) == 0 && len(chunks) != 0 {
				t.Errorf("processOrderedPromptImports() expected 0 chunks for empty imports, got %d", len(chunks))
			}
			for _, sub := range tt.wantChunkContain {
				found := false
				for _, ch := range chunks {
					if strings.Contains(ch, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("processOrderedPromptImports() expected chunks to contain %q; got: %v", sub, chunks)
				}
			}
			if len(tt.wantChunkSequence) > 0 {
				if len(chunks) != len(tt.wantChunkSequence) {
					t.Fatalf("processOrderedPromptImports() got %d chunks, want %d for sequence check; chunks: %v", len(chunks), len(tt.wantChunkSequence), chunks)
				}
				for i, want := range tt.wantChunkSequence {
					if !strings.Contains(chunks[i], want) {
						t.Errorf("processOrderedPromptImports() chunks[%d] = %q, want it to contain %q", i, chunks[i], want)
					}
				}
			}
		})
	}
}

// TestProcessOrderedPromptImportsFileReadFallback verifies that a missing import file falls back
// to a runtime-import macro when InlinedImports is true.
func TestProcessOrderedPromptImportsFileReadFallback(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, ".github", "workflows", "test.md")
	if err := os.MkdirAll(filepath.Dir(mdPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mdPath, []byte("---\n---\n# Main"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewCompiler()
	c.markdownPath = mdPath

	data := &WorkflowData{
		PromptImports: []parser.PromptImportEntry{
			{ImportPath: ".github/workflows/nonexistent.md"},
		},
		InlinedImports: true,
	}

	chunks, _ := c.processOrderedPromptImports(data)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "{{#runtime-import") {
		t.Errorf("expected runtime-import fallback, got: %s", chunks[0])
	}
}

// TestProcessLegacyPromptImports tests the processLegacyPromptImports function.
func TestProcessLegacyPromptImports(t *testing.T) {
	tests := []struct {
		name              string
		importedMarkdown  string
		importPaths       []string
		importInputs      map[string]any
		inlinedImports    bool
		wantChunkContain  []string
		wantChunkSequence []string // if set, chunks[i] must contain wantChunkSequence[i] (exact order)
	}{
		{
			name:             "no imported markdown, no paths",
			importedMarkdown: "",
			importPaths:      nil,
			wantChunkContain: nil,
		},
		{
			name:             "inline markdown with no paths",
			importedMarkdown: "# Imported heading\nSome body",
			wantChunkContain: []string{"# Imported heading"},
		},
		{
			name:             "inline markdown with import inputs substitution",
			importedMarkdown: "Role: ${{ github.aw.inputs.role }}",
			importInputs:     map[string]any{"role": "engineer"},
			wantChunkContain: []string{"Role: engineer"},
		},
		{
			name:        "import paths generate runtime-import macros",
			importPaths: []string{".github/workflows/shared.md", ".github/workflows/extra.md"},
			// Must preserve import order: shared.md first, extra.md second.
			wantChunkSequence: []string{"{{#runtime-import .github/workflows/shared.md}}", "{{#runtime-import .github/workflows/extra.md}}"},
		},
		{
			name:             "multiple import paths without inlined imports use runtime macros",
			importPaths:      []string{".github/workflows/a.md", ".github/workflows/b.md"},
			wantChunkContain: []string{"{{#runtime-import .github/workflows/a.md}}", "{{#runtime-import .github/workflows/b.md}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			data := &WorkflowData{
				ImportedMarkdown: tt.importedMarkdown,
				ImportPaths:      tt.importPaths,
				ImportInputs:     tt.importInputs,
				InlinedImports:   tt.inlinedImports,
			}

			chunks, _ := c.processLegacyPromptImports(data)

			for _, sub := range tt.wantChunkContain {
				found := false
				for _, ch := range chunks {
					if strings.Contains(ch, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("processLegacyPromptImports() expected chunks to contain %q; got: %v", sub, chunks)
				}
			}
			if len(tt.wantChunkSequence) > 0 {
				if len(chunks) != len(tt.wantChunkSequence) {
					t.Fatalf("processLegacyPromptImports() got %d chunks, want %d for sequence check; chunks: %v", len(chunks), len(tt.wantChunkSequence), chunks)
				}
				for i, want := range tt.wantChunkSequence {
					if !strings.Contains(chunks[i], want) {
						t.Errorf("processLegacyPromptImports() chunks[%d] = %q, want it to contain %q", i, chunks[i], want)
					}
				}
			}
		})
	}
}

// TestProcessLegacyPromptImportsInlinedFromDisk tests that inlined-imports mode reads files from disk.
func TestProcessLegacyPromptImportsInlinedFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a fake .github/workflows directory structure
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create the import file
	importContent := "---\n---\n# Shared Instructions\nBe helpful."
	importPath := filepath.Join(workflowsDir, "shared.md")
	if err := os.WriteFile(importPath, []byte(importContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mainMdPath := filepath.Join(workflowsDir, "main.md")

	c := NewCompiler()
	c.markdownPath = mainMdPath

	data := &WorkflowData{
		ImportPaths:    []string{".github/workflows/shared.md"},
		InlinedImports: true,
	}

	chunks, _ := c.processLegacyPromptImports(data)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk after inlining from disk")
	}
	// The inlined content should contain the body of the import
	combined := strings.Join(chunks, "\n")
	if !strings.Contains(combined, "# Shared Instructions") {
		t.Errorf("expected inlined content to contain '# Shared Instructions'; got: %s", combined)
	}
}

// TestProcessLegacyPromptImportsInlinedFallback tests that a missing inlined file falls back to runtime-import.
func TestProcessLegacyPromptImportsInlinedFallback(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	c := NewCompiler()
	c.markdownPath = filepath.Join(workflowsDir, "main.md")

	data := &WorkflowData{
		ImportPaths:    []string{".github/workflows/nonexistent.md"},
		InlinedImports: true,
	}

	chunks, _ := c.processLegacyPromptImports(data)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (runtime-import fallback), got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "{{#runtime-import") {
		t.Errorf("expected runtime-import fallback, got: %s", chunks[0])
	}
}

// TestEnrichExpressionMappings tests the enrichExpressionMappings function.
func TestEnrichExpressionMappings(t *testing.T) {
	tests := []struct {
		name                 string
		inlinePrompt         bool
		inlinedImports       bool
		mainWorkflowMarkdown string
		experiments          map[string][]string
		initialMappings      []*ExpressionMapping
		beforeActivationJobs []string
		wantMappingCount     int
		wantContainsEnvVar   []string
	}{
		{
			name:                 "empty state returns empty mappings",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "",
			experiments:          nil,
			initialMappings:      nil,
			wantMappingCount:     0,
		},
		{
			name:                 "experiment mappings are appended",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "",
			experiments:          map[string][]string{"ab-test": {"control", "variant"}},
			initialMappings:      nil,
			wantMappingCount:     1,
		},
		{
			name:                 "existing mappings are preserved",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "",
			experiments:          nil,
			initialMappings: []*ExpressionMapping{
				{EnvVar: "GH_AW_EXISTING", Content: "github.actor", Original: "${{ github.actor }}"},
			},
			wantMappingCount:   1,
			wantContainsEnvVar: []string{"GH_AW_EXISTING"},
		},
		{
			name:                 "inline mode skips main markdown expression extraction",
			inlinePrompt:         true,
			inlinedImports:       false,
			mainWorkflowMarkdown: "Uses ${{ github.event.issue.number }}",
			experiments:          nil,
			initialMappings:      nil,
			// In inline mode, expressions from main markdown are NOT extracted here
			// (they're handled in buildMainWorkflowPromptChunks instead)
			wantMappingCount: 0,
		},
		{
			name:                 "non-inline mode extracts expressions from main markdown",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "Issue: ${{ github.event.issue.number }}",
			experiments:          nil,
			initialMappings:      nil,
			wantMappingCount:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			c.inlinePrompt = tt.inlinePrompt

			data := &WorkflowData{
				InlinedImports:       tt.inlinedImports,
				MainWorkflowMarkdown: tt.mainWorkflowMarkdown,
				Experiments:          tt.experiments,
			}

			result := c.enrichExpressionMappings(data, tt.initialMappings, tt.beforeActivationJobs)

			if len(result) != tt.wantMappingCount {
				t.Errorf("enrichExpressionMappings() got %d mappings, want %d", len(result), tt.wantMappingCount)
			}
			for _, envVar := range tt.wantContainsEnvVar {
				found := false
				for _, m := range result {
					if m.EnvVar == envVar {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("enrichExpressionMappings() expected mapping with EnvVar %q", envVar)
				}
			}
		})
	}
}

// TestBuildMainWorkflowPromptChunks tests the buildMainWorkflowPromptChunks function.
func TestBuildMainWorkflowPromptChunks(t *testing.T) {
	tests := []struct {
		name                 string
		inlinePrompt         bool
		inlinedImports       bool
		mainWorkflowMarkdown string
		markdownPath         string
		wantChunkContain     []string
		wantChunkNotContain  []string
		wantChunkCount       int
	}{
		{
			name:                 "non-inline mode emits runtime-import macro",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "# Some content",
			markdownPath:         ".github/workflows/my-workflow.md",
			wantChunkContain:     []string{"{{#runtime-import .github/workflows/my-workflow.md}}"},
			wantChunkNotContain:  []string{"# Some content"},
		},
		{
			name:                 "inline mode embeds markdown content directly",
			inlinePrompt:         true,
			inlinedImports:       false,
			mainWorkflowMarkdown: "# Inline Heading\nSome body text",
			markdownPath:         ".github/workflows/my-workflow.md",
			wantChunkContain:     []string{"# Inline Heading"},
			wantChunkNotContain:  []string{"{{#runtime-import"},
		},
		{
			name:                 "inlined-imports mode embeds markdown content directly",
			inlinePrompt:         false,
			inlinedImports:       true,
			mainWorkflowMarkdown: "# InlinedImports Heading\nContent here",
			markdownPath:         ".github/workflows/my-workflow.md",
			wantChunkContain:     []string{"# InlinedImports Heading"},
			wantChunkNotContain:  []string{"{{#runtime-import"},
		},
		{
			name:                 "inline mode with empty markdown returns unchanged chunks",
			inlinePrompt:         true,
			inlinedImports:       false,
			mainWorkflowMarkdown: "",
			markdownPath:         ".github/workflows/my-workflow.md",
			wantChunkCount:       0,
		},
		{
			name:                 "non-inline uses filename only when path has no .github dir",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "# Some content",
			markdownPath:         "/tmp/some-other-path/my-workflow.md",
			wantChunkContain:     []string{"{{#runtime-import my-workflow.md}}"},
		},
		{
			name:                 "non-inline uses relative path from .github dir",
			inlinePrompt:         false,
			inlinedImports:       false,
			mainWorkflowMarkdown: "# Content",
			markdownPath:         "/repo/.github/workflows/deep.md",
			wantChunkContain:     []string{"{{#runtime-import .github/workflows/deep.md}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			c.inlinePrompt = tt.inlinePrompt
			c.markdownPath = tt.markdownPath

			data := &WorkflowData{
				InlinedImports:       tt.inlinedImports,
				MainWorkflowMarkdown: tt.mainWorkflowMarkdown,
			}

			chunks, _ := c.buildMainWorkflowPromptChunks(data, nil, nil)

			if tt.wantChunkCount > 0 && len(chunks) != tt.wantChunkCount {
				t.Errorf("buildMainWorkflowPromptChunks() got %d chunks, want %d", len(chunks), tt.wantChunkCount)
			}
			if tt.wantChunkCount == 0 && tt.mainWorkflowMarkdown == "" && len(chunks) != 0 {
				t.Errorf("expected 0 chunks for empty markdown, got %d", len(chunks))
			}

			for _, sub := range tt.wantChunkContain {
				found := false
				for _, ch := range chunks {
					if strings.Contains(ch, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("buildMainWorkflowPromptChunks() expected chunk to contain %q; chunks: %v", sub, chunks)
				}
			}
			combined := strings.Join(chunks, "\n")
			for _, sub := range tt.wantChunkNotContain {
				if strings.Contains(combined, sub) {
					t.Errorf("buildMainWorkflowPromptChunks() expected chunks NOT to contain %q", sub)
				}
			}
		})
	}
}

// TestBuildMainWorkflowPromptChunksExpressionExtraction verifies that inline mode extracts
// expressions from the main workflow markdown and returns them as mappings.
func TestBuildMainWorkflowPromptChunksExpressionExtraction(t *testing.T) {
	c := NewCompiler()
	c.inlinePrompt = true
	c.markdownPath = ".github/workflows/test.md"

	data := &WorkflowData{
		InlinedImports:       false,
		MainWorkflowMarkdown: "Issue number: ${{ github.event.issue.number }}",
	}

	_, mappings := c.buildMainWorkflowPromptChunks(data, nil, nil)
	if len(mappings) == 0 {
		t.Error("buildMainWorkflowPromptChunks() expected expression mappings to be extracted in inline mode")
	}
}

// TestProcessPromptImportEntriesDispatch tests that processPromptImportEntries delegates to
// the ordered path when PromptImports is set, and to legacy path when it is not.
func TestProcessPromptImportEntriesDispatch(t *testing.T) {
	t.Run("uses ordered path when PromptImports is set", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			PromptImports: []parser.PromptImportEntry{
				{ImportPath: ".github/workflows/shared.md"},
			},
		}
		chunks, _ := c.processPromptImportEntries(data)
		if len(chunks) == 0 {
			t.Error("processPromptImportEntries() expected chunks from ordered path")
		}
		if !strings.Contains(chunks[0], "{{#runtime-import") {
			t.Errorf("processPromptImportEntries() expected runtime-import macro, got: %s", chunks[0])
		}
	})

	t.Run("uses legacy path when PromptImports is empty", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			ImportPaths: []string{".github/workflows/shared.md"},
		}
		chunks, _ := c.processPromptImportEntries(data)
		if len(chunks) == 0 {
			t.Error("processPromptImportEntries() expected chunks from legacy path")
		}
		if !strings.Contains(chunks[0], "{{#runtime-import") {
			t.Errorf("processPromptImportEntries() expected runtime-import macro, got: %s", chunks[0])
		}
	})
}

// TestMergeKnownNeedsExpressions tests the mergeKnownNeedsExpressions function.
func TestMergeKnownNeedsExpressions(t *testing.T) {
	tests := []struct {
		name        string
		all         []*ExpressionMapping
		knownNeeds  []*ExpressionMapping
		wantEnvVars []string
		wantLen     int
	}{
		{
			name:        "empty both returns empty",
			all:         nil,
			knownNeeds:  nil,
			wantLen:     0,
			wantEnvVars: nil,
		},
		{
			name: "all-entries take precedence over knownNeeds for same env var",
			all: []*ExpressionMapping{
				{EnvVar: "GH_AW_X", Content: "all-value", Original: "${{ all-value }}"},
			},
			knownNeeds: []*ExpressionMapping{
				{EnvVar: "GH_AW_X", Content: "needs-value", Original: "${{ needs-value }}"},
			},
			wantLen:     1,
			wantEnvVars: []string{"GH_AW_X"},
		},
		{
			name: "non-conflicting entries are merged",
			all: []*ExpressionMapping{
				{EnvVar: "GH_AW_A", Content: "a", Original: "${{ a }}"},
			},
			knownNeeds: []*ExpressionMapping{
				{EnvVar: "GH_AW_B", Content: "b", Original: "${{ b }}"},
			},
			wantLen:     2,
			wantEnvVars: []string{"GH_AW_A", "GH_AW_B"},
		},
		{
			name: "all-value wins over knownNeeds for same key",
			all: []*ExpressionMapping{
				{EnvVar: "GH_AW_Z", Content: "all-wins", Original: "${{ all-wins }}"},
			},
			knownNeeds: []*ExpressionMapping{
				{EnvVar: "GH_AW_Z", Content: "needs-loses", Original: "${{ needs-loses }}"},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeKnownNeedsExpressions(tt.all, tt.knownNeeds)
			if len(result) != tt.wantLen {
				t.Errorf("mergeKnownNeedsExpressions() got %d entries, want %d", len(result), tt.wantLen)
			}
			for _, envVar := range tt.wantEnvVars {
				found := false
				for _, m := range result {
					if m.EnvVar == envVar {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("mergeKnownNeedsExpressions() expected entry with EnvVar %q", envVar)
				}
			}
			// Verify all-entries take precedence (content check)
			for _, a := range tt.all {
				for _, r := range result {
					if r.EnvVar == a.EnvVar && r.Content != a.Content {
						t.Errorf("mergeKnownNeedsExpressions() for EnvVar %q: got content %q, want %q (all-entry should win)", r.EnvVar, r.Content, a.Content)
					}
				}
			}
		})
	}
}

// TestResolveWorkspaceRoot tests the resolveWorkspaceRoot helper function.
func TestResolveWorkspaceRoot(t *testing.T) {
	tests := []struct {
		name         string
		markdownPath string
		wantRoot     string
	}{
		{
			name:         "path with .github in middle",
			markdownPath: "/repo/.github/workflows/test.md",
			wantRoot:     "/repo",
		},
		{
			name:         "path starting with .github/",
			markdownPath: ".github/workflows/test.md",
			wantRoot:     ".",
		},
		{
			name:         "path without .github directory",
			markdownPath: "/tmp/workflows/test.md",
			wantRoot:     "/tmp/workflows",
		},
		{
			name:         "deeply nested .github path",
			markdownPath: "/home/user/projects/myrepo/.github/workflows/daily.md",
			wantRoot:     "/home/user/projects/myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveWorkspaceRoot(tt.markdownPath)
			if got != tt.wantRoot {
				t.Errorf("resolveWorkspaceRoot(%q) = %q, want %q", tt.markdownPath, got, tt.wantRoot)
			}
		})
	}
}

func TestCollectRuntimeImportMarkdownForCompilerAnalysisTopologies(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	promptsDir := filepath.Join(tmpDir, ".github", "prompts")
	require.NoError(t, os.MkdirAll(filepath.Join(workflowsDir, "shared"), 0755))
	require.NoError(t, os.MkdirAll(promptsDir, 0755))

	write := func(path, content string) {
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	}
	write(filepath.Join(promptsDir, "direct.md"), "Direct ${{ needs.direct.outputs.value }}")
	write(filepath.Join(promptsDir, "optional.md"), "Optional ${{ needs.optional.outputs.value }}")
	write(filepath.Join(promptsDir, "legacy.md"), "Legacy ${{ needs.legacy.outputs.value }}")
	write(filepath.Join(promptsDir, "nested.md"), "Nested ${{ needs.nested.outputs.value }}")
	write(filepath.Join(promptsDir, "outer.md"), "Outer {{#runtime-import prompts/nested.md}}")
	write(filepath.Join(promptsDir, "range.md"), strings.Join([]string{
		"Outside ${{ needs.outside.outputs.value }}",
		"Inside ${{ needs.inside.outputs.value }}",
	}, "\n"))
	write(filepath.Join(workflowsDir, "shared", "frontmatter-import.md"), "Frontmatter import ${{ needs.frontmatter.outputs.value }}")

	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "topologies.md")
	data := &WorkflowData{
		MarkdownContent: `{{#runtime-import prompts/direct.md}}
{{#runtime-import? prompts/optional.md}}
{{#import: prompts/legacy.md}}
{{#runtime-import prompts/outer.md}}
{{#runtime-import prompts/range.md:2-2}}`,
		ImportPaths: []string{filepath.Join(".github", "workflows", "shared", "frontmatter-import.md")},
	}

	runtimeImportMarkdown := compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data)

	for _, want := range []string{
		"needs.direct.outputs.value",
		"needs.optional.outputs.value",
		"needs.legacy.outputs.value",
		"needs.nested.outputs.value",
		"needs.inside.outputs.value",
		"needs.frontmatter.outputs.value",
	} {
		assert.Contains(t, runtimeImportMarkdown, want)
	}
	assert.NotContains(t, runtimeImportMarkdown, "needs.outside.outputs.value")
}

func TestCollectRuntimeImportMarkdownForCompilerAnalysisLegacyImportOnly(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	promptsDir := filepath.Join(tmpDir, ".github", "prompts")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.MkdirAll(promptsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "legacy.md"), []byte("Legacy ${{ needs.legacy.outputs.value }}"), 0600))

	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "legacy-only.md")
	data := &WorkflowData{MarkdownContent: "{{#import: prompts/legacy.md}}"}

	runtimeImportMarkdown := compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data)

	assert.Contains(t, runtimeImportMarkdown, "needs.legacy.outputs.value")
}

func TestCollectRuntimeImportMarkdownForCompilerAnalysisRejectsTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "outside.md"), []byte("Outside ${{ secrets.OUTSIDE_TOKEN }}"), 0600))

	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "traversal.md")
	data := &WorkflowData{MarkdownContent: "{{#runtime-import ../outside.md}}"}

	runtimeImportMarkdown := compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data)

	assert.Empty(t, runtimeImportMarkdown)
}

func TestCollectRuntimeImportMarkdownForCompilerAnalysisSkipsUnsafeExpressions(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "unsafe.md"), []byte("Secret ${{ secrets.PRIVATE_TOKEN }}"), 0600))

	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "unsafe.md")
	data := &WorkflowData{MarkdownContent: "{{#runtime-import shared/unsafe.md}}"}

	runtimeImportMarkdown := compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data)

	assert.Empty(t, runtimeImportMarkdown)
}

func TestCollectRuntimeImportMarkdownForCompilerAnalysisStopsOnUnsafeCandidate(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	promptsDir := filepath.Join(tmpDir, ".github", "prompts")
	workflowPromptsDir := filepath.Join(workflowsDir, "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0755))
	require.NoError(t, os.MkdirAll(workflowPromptsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "ambiguous.md"), []byte("Secret ${{ secrets.PRIVATE_TOKEN }}"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(workflowPromptsDir, "ambiguous.md"), []byte("Safe ${{ needs.safe.outputs.value }}"), 0600))

	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "ambiguous.md")
	data := &WorkflowData{MarkdownContent: "{{#runtime-import prompts/ambiguous.md}}"}

	runtimeImportMarkdown := compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data)

	assert.Empty(t, runtimeImportMarkdown)
}

func TestCollectRuntimeImportMarkdownForCompilerAnalysisRejectsSymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "outside.md"), []byte("Outside ${{ needs.outside.outputs.value }}"), 0600))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "outside.md"), filepath.Join(workflowsDir, "outside-link.md")))

	compiler := NewCompiler()
	compiler.markdownPath = filepath.Join(workflowsDir, "symlink.md")
	data := &WorkflowData{MarkdownContent: "{{#runtime-import outside-link.md}}"}

	runtimeImportMarkdown := compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data)

	assert.Empty(t, runtimeImportMarkdown)
}

// TestExtractPromptChunksFromMarkdown tests the extractPromptChunksFromMarkdown function.
func TestExtractPromptChunksFromMarkdown(t *testing.T) {
	tests := []struct {
		name                string
		body                string
		wantChunks          int
		wantMappingCount    int
		wantChunkContain    []string
		wantChunkNotContain []string // substrings that must NOT appear in any chunk
	}{
		{
			name:       "empty body",
			body:       "",
			wantChunks: 1,
		},
		{
			name:             "plain text without expressions",
			body:             "# Simple Heading\nSome plain text",
			wantChunks:       1,
			wantChunkContain: []string{"# Simple Heading"},
		},
		{
			name:                "body with XML comments stripped",
			body:                "<!-- hidden -->\n# Visible",
			wantChunks:          1,
			wantChunkContain:    []string{"# Visible"},
			wantChunkNotContain: []string{"hidden"},
		},
		{
			name:             "body with GitHub expression",
			body:             "Issue: ${{ github.event.issue.number }}",
			wantChunks:       1,
			wantMappingCount: 1,
			wantChunkContain: []string{"Issue:"},
			// The raw expression must be replaced by an env-var placeholder in the chunk.
			wantChunkNotContain: []string{"${{ github.event.issue.number }}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, mappings := extractPromptChunksFromMarkdown(tt.body)
			if len(chunks) != tt.wantChunks {
				t.Errorf("extractPromptChunksFromMarkdown() got %d chunks, want %d", len(chunks), tt.wantChunks)
			}
			if len(mappings) != tt.wantMappingCount {
				t.Errorf("extractPromptChunksFromMarkdown() got %d mappings, want %d", len(mappings), tt.wantMappingCount)
			}
			for _, sub := range tt.wantChunkContain {
				found := false
				for _, ch := range chunks {
					if strings.Contains(ch, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("extractPromptChunksFromMarkdown() expected chunk to contain %q", sub)
				}
			}
			combined := strings.Join(chunks, "\n")
			for _, sub := range tt.wantChunkNotContain {
				if strings.Contains(combined, sub) {
					t.Errorf("extractPromptChunksFromMarkdown() expected chunks NOT to contain %q", sub)
				}
			}
		})
	}
}
