//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalSorted_Primitives(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string", "test", `"test"`},
		{"number", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := marshalSorted(tt.input)
			assert.Equal(t, tt.expected, result, "Should marshal primitive correctly")
		})
	}
}

func TestMarshalSorted_EmptyContainers(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"empty object", map[string]any{}, "{}"},
		{"empty array", []any{}, "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := marshalSorted(tt.input)
			assert.Equal(t, tt.expected, result, "Should marshal empty container correctly")
		})
	}
}

func TestMarshalSorted_SortedKeys(t *testing.T) {
	input := map[string]any{
		"zebra":   1,
		"apple":   2,
		"banana":  3,
		"charlie": 4,
	}

	result := marshalSorted(input)
	expected := `{"apple":2,"banana":3,"charlie":4,"zebra":1}`
	assert.Equal(t, expected, result, "Keys should be sorted alphabetically")
}

func TestMarshalSorted_NestedSorting(t *testing.T) {
	input := map[string]any{
		"outer": map[string]any{
			"z": 1,
			"a": 2,
		},
		"another": map[string]any{
			"nested": map[string]any{
				"y": 3,
				"b": 4,
			},
		},
	}

	result := marshalSorted(input)
	// Keys at all levels should be sorted
	assert.Contains(t, result, `"another":`, "Should contain outer key")
	assert.Contains(t, result, `"outer":`, "Should contain outer key")
	assert.Contains(t, result, `"a":2`, "Should contain sorted nested keys")
	assert.Contains(t, result, `"z":1`, "Should contain sorted nested keys")
}

func TestComputeFrontmatterHashFromFile_NonExistent(t *testing.T) {
	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHashFromFile("/nonexistent/file.md", cache)
	require.Error(t, err, "Should error for nonexistent file")
	assert.Empty(t, hash, "Hash should be empty on error")
	assert.Contains(t, err.Error(), strconv.Quote("/nonexistent/file.md"), "Error should include the quoted file path")
}

func TestComputeFrontmatterHashFromFileWithParsedFrontmatter_ReadErrorIncludesPath(t *testing.T) {
	filePath := "/test/missing-workflow.md"
	customReader := func(filePath string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	hash, err := ComputeFrontmatterHashFromFileWithParsedFrontmatter(filePath, map[string]any{}, nil, customReader)

	require.Error(t, err, "Should error when custom reader cannot read file")
	assert.Empty(t, hash, "Hash should be empty on error")
	assert.Contains(t, err.Error(), "could not read file")
	assert.Contains(t, err.Error(), strconv.Quote(filePath), "Error should include the quoted file path")
}

func TestComputeFrontmatterHashFromFileWithReader_MalformedFrontmatterIncludesPath(t *testing.T) {
	filePath := "/test/malformed-workflow.md"
	customReader := func(filePath string) ([]byte, error) {
		return []byte("---\nengine: copilot\n"), nil
	}

	hash, err := ComputeFrontmatterHashFromFileWithReader(filePath, nil, customReader)

	require.Error(t, err, "Should error when frontmatter is not closed")
	assert.Empty(t, hash, "Hash should be empty on error")
	assert.Contains(t, err.Error(), "could not extract frontmatter")
	assert.Contains(t, err.Error(), strconv.Quote(filePath), "Error should include the quoted file path")
}

func TestComputeFrontmatterHashFromFile_ValidFile(t *testing.T) {
	// Create a temporary workflow file
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "test-workflow.md")

	content := `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow

This is a test workflow.
`

	err := os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Should write test file")

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Should compute hash from file")
	assert.Len(t, hash, 64, "Hash should be 64 characters")

	// Compute again to verify determinism
	hash2, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Should compute hash again")
	assert.Equal(t, hash, hash2, "Hash should be deterministic")
}

func TestComputeFrontmatterHash_WithImports(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()

	// Create a shared workflow
	sharedDir := filepath.Join(tempDir, "shared")
	err := os.MkdirAll(sharedDir, 0755)
	require.NoError(t, err, "Should create shared directory")

	sharedFile := filepath.Join(sharedDir, "common.md")
	sharedContent := `---
tools:
  playwright:
    version: v1.41.0
labels:
  - shared
  - common
---

# Shared Content

This is shared.
`
	err = os.WriteFile(sharedFile, []byte(sharedContent), 0644)
	require.NoError(t, err, "Should write shared file")

	// Create a main workflow that imports the shared workflow
	mainFile := filepath.Join(tempDir, "main.md")
	mainContent := `---
engine: copilot
description: Main workflow
imports:
  - shared/common.md
labels:
  - main
---

# Main Workflow

This is the main workflow.
`
	err = os.WriteFile(mainFile, []byte(mainContent), 0644)
	require.NoError(t, err, "Should write main file")

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHashFromFile(mainFile, cache)
	require.NoError(t, err, "Should compute hash with imports")
	assert.Len(t, hash, 64, "Hash should be 64 characters")

	// The hash should include contributions from the imported file
	// We can't easily verify the exact hash, but we can verify it's deterministic
	hash2, err := ComputeFrontmatterHashFromFile(mainFile, cache)
	require.NoError(t, err, "Should compute hash again with imports")
	assert.Equal(t, hash, hash2, "Hash with imports should be deterministic")
}

func TestComputeFrontmatterHashFromFileWithReader_CustomReader(t *testing.T) {
	// Create in-memory file system mock
	mockFS := map[string]string{
		"/test/workflow.md": `---
engine: copilot
description: Test workflow
---

# Workflow Body`,
		"/test/shared/imported.md": `---
tools:
  bash: true
---

# Imported Content`,
	}

	// Create custom file reader
	customReader := func(filePath string) ([]byte, error) {
		content, exists := mockFS[filePath]
		if !exists {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}

	// Test basic hash computation
	hash, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.NoError(t, err, "Should compute hash with custom reader")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
	assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")

	// Verify determinism
	hash2, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.NoError(t, err, "Should compute hash again")
	assert.Equal(t, hash, hash2, "Hash should be deterministic")
}

func TestComputeFrontmatterHashFromFileWithReader_WithImports(t *testing.T) {
	// Create in-memory file system mock with imports
	mockFS := map[string]string{
		"/test/workflow.md": `---
engine: copilot
imports:
  - shared/imported.md
---

# Main Workflow`,
		"/test/shared/imported.md": `---
tools:
  bash: true
---

# Imported Content`,
	}

	// Create custom file reader
	customReader := func(filePath string) ([]byte, error) {
		content, exists := mockFS[filePath]
		if !exists {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}

	// Test hash computation with imports
	hash, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.NoError(t, err, "Should compute hash with imports using custom reader")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
	assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")
}

func TestFrontmatterHashInputSizeLimit(t *testing.T) {
	// S-6 compliance: inputs exceeding 1 MiB MUST fail with a deterministic error.
	oversizedValue := strings.Repeat("a", maxFrontmatterHashInputBytes+1)
	customReader := func(filePath string) ([]byte, error) {
		switch filePath {
		case "/test/workflow.md":
			return []byte("---\ndescription: " + oversizedValue + "\n---\n\n# Workflow\n"), nil
		default:
			return nil, os.ErrNotExist
		}
	}

	_, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.Error(t, err, "Should reject oversized normalized frontmatter input")
	require.EqualError(t, err, "frontmatter hash input exceeds 1048576 bytes after normalization")

	_, err = ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.Error(t, err, "Should return the same deterministic error on repeated calls")
	require.EqualError(t, err, "frontmatter hash input exceeds 1048576 bytes after normalization")
}

func TestExtractImportsFromText_ObjectFormUsesImport(t *testing.T) {
	// Object-form "uses:" imports must have their path extracted (not silently dropped).
	frontmatterText := `imports:
  - uses: ./serena.md
    with:
      languages: ["go"]
  - shared/common.md`

	result := extractImportsFromText(frontmatterText)
	assert.Equal(t, []string{"./serena.md", "shared/common.md"}, result,
		"Object-form uses: import path must be extracted alongside plain string imports")
}

func TestExtractImportsFromText_ObjectFormPathImport(t *testing.T) {
	// Object-form "path:" imports must have their path extracted (not silently dropped).
	frontmatterText := `imports:
  - path: shared/tool.md
    inputs:
      key: value`

	result := extractImportsFromText(frontmatterText)
	assert.Equal(t, []string{"shared/tool.md"}, result,
		"Object-form path: import path must be extracted")
}

func TestCollectRuntimeImportTemplateExpressionsTopologies(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	promptsDir := filepath.Join(tempDir, ".github", "prompts")
	require.NoError(t, os.MkdirAll(filepath.Join(workflowDir, "shared"), 0755))
	require.NoError(t, os.MkdirAll(promptsDir, 0755))

	write := func(path, content string) {
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	}
	write(filepath.Join(promptsDir, "direct.md"), "Direct ${{ needs.select.outputs.issue_numbers }}")
	write(filepath.Join(promptsDir, "optional.md"), "Optional ${{ inputs.item_number }}")
	write(filepath.Join(promptsDir, "legacy.md"), "Legacy ${{ vars.LEGACY_PROMPT }}")
	write(filepath.Join(promptsDir, "outer.md"), "Outer {{#runtime-import prompts/nested.md}}")
	write(filepath.Join(promptsDir, "nested.md"), "Nested ${{ needs.nested.outputs.value }}")
	write(filepath.Join(promptsDir, "ranged.md"), strings.Join([]string{
		"Ignored ${{ needs.outside.outputs.value }}",
		"Selected ${{ needs.inside.outputs.value }}",
		"Also selected ${{ env.RANGED }}",
	}, "\n"))
	write(filepath.Join(workflowDir, "shared", "imported.md"), `---
description: Imported prompt
---
Imported body ${{ needs.imported.outputs.value }}
{{#runtime-import prompts/direct.md}}`)

	frontmatterText := `engine: copilot
imports:
  - shared/imported.md`
	markdown := `{{#runtime-import prompts/direct.md}}
{{#runtime-import? prompts/optional.md}}
{{#import: prompts/legacy.md}}
{{#runtime-import prompts/outer.md}}
{{#runtime-import prompts/ranged.md:2-3}}
{{#runtime-import https://example.com/ignored.md}}
{{#runtime-import? prompts/missing.md}}`

	expressions := collectRuntimeImportTemplateExpressions(frontmatterText, markdown, workflowDir, DefaultFileReader)

	assert.Equal(t, []string{
		"${{ env.RANGED }}",
		"${{ inputs.item_number }}",
		"${{ needs.imported.outputs.value }}",
		"${{ needs.inside.outputs.value }}",
		"${{ needs.nested.outputs.value }}",
		"${{ needs.select.outputs.issue_numbers }}",
		"${{ vars.LEGACY_PROMPT }}",
	}, expressions)
}

func TestCollectRuntimeImportTemplateExpressionsSkipsSymlinkEscape(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "outside.md"), []byte("Outside ${{ needs.outside.outputs.value }}"), 0644))
	require.NoError(t, os.Symlink(filepath.Join(tempDir, "outside.md"), filepath.Join(workflowDir, "outside-link.md")))

	expressions := collectRuntimeImportTemplateExpressions("", "{{#runtime-import outside-link.md}}", workflowDir, DefaultFileReader)

	assert.Empty(t, expressions)
}

func TestFrontmatterHashIncludesRuntimeImportExpressionSet(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	promptsDir := filepath.Join(tempDir, ".github", "prompts")
	require.NoError(t, os.MkdirAll(workflowDir, 0755))
	require.NoError(t, os.MkdirAll(promptsDir, 0755))

	workflowFile := filepath.Join(workflowDir, "runtime-import-hash.md")
	workflowContent := `---
engine: copilot
on:
  workflow_dispatch:
---
{{#runtime-import prompts/runtime.md}}
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))

	promptFile := filepath.Join(promptsDir, "runtime.md")
	require.NoError(t, os.WriteFile(promptFile, []byte("Issue: ${{ needs.select.outputs.issue_numbers }}\n"), 0644))
	hashA, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(promptFile, []byte("Unrelated text change\nIssue: ${{ needs.select.outputs.issue_numbers }}\n"), 0644))
	hashSameExpression, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)
	assert.Equal(t, hashA, hashSameExpression, "runtime-import body text changes should not affect the frontmatter hash when the detected expression set is unchanged")

	require.NoError(t, os.WriteFile(promptFile, []byte("Issue: ${{ needs.select.outputs.marker }}\n"), 0644))
	hashB, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)
	assert.NotEqual(t, hashA, hashB, "runtime-import expression set changes must affect the frontmatter hash")

	jsHash, err := computeHashViaNode(workflowFile)
	if err != nil {
		t.Logf("JavaScript not available for runtime-import hash parity: %v", err)
		return
	}
	assert.Equal(t, hashB, jsHash, "Go and JS frontmatter hashes must include the same runtime-import expression set")
}

func TestFrontmatterHashRuntimeImportLineRangeUsesSelectedLines(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	promptsDir := filepath.Join(tempDir, ".github", "prompts")
	require.NoError(t, os.MkdirAll(workflowDir, 0755))
	require.NoError(t, os.MkdirAll(promptsDir, 0755))

	workflowFile := filepath.Join(workflowDir, "runtime-import-range-hash.md")
	workflowContent := `---
engine: copilot
on:
  workflow_dispatch:
---
{{#runtime-import prompts/ranged.md:2-2}}
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))

	promptFile := filepath.Join(promptsDir, "ranged.md")
	require.NoError(t, os.WriteFile(promptFile, []byte("Ignored ${{ needs.outside.outputs.value }}\nSelected ${{ needs.inside.outputs.value }}\n"), 0644))
	hashA, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(promptFile, []byte("Ignored ${{ needs.outside.outputs.changed }}\nSelected ${{ needs.inside.outputs.value }}\n"), 0644))
	hashOutsideChanged, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)
	assert.Equal(t, hashA, hashOutsideChanged, "expressions outside the selected runtime-import line range should not affect the frontmatter hash")

	require.NoError(t, os.WriteFile(promptFile, []byte("Ignored ${{ needs.outside.outputs.changed }}\nSelected ${{ needs.inside.outputs.changed }}\n"), 0644))
	hashInsideChanged, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)
	assert.NotEqual(t, hashA, hashInsideChanged, "expressions inside the selected runtime-import line range must affect the frontmatter hash")
}

func TestFrontmatterHashRuntimeImportCyclesTerminate(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	workflowFile := filepath.Join(workflowDir, "runtime-import-cycle-hash.md")
	workflowContent := `---
engine: copilot
imports:
  - shared/a.md
on:
  workflow_dispatch:
---
Cycle hash coverage.
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "a.md"), []byte("A ${{ needs.a.outputs.value }}\n{{#runtime-import shared/b.md}}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "b.md"), []byte("B ${{ needs.b.outputs.value }}\n{{#runtime-import shared/a.md}}\n"), 0644))

	hash, err := ComputeFrontmatterHashFromFile(workflowFile, NewImportCache(workflowDir))
	require.NoError(t, err)
	assert.Len(t, hash, 64)

	expressions := collectRuntimeImportTemplateExpressions("imports:\n  - shared/a.md", "Cycle hash coverage.", workflowDir, DefaultFileReader)
	assert.Equal(t, []string{
		"${{ needs.a.outputs.value }}",
		"${{ needs.b.outputs.value }}",
	}, expressions)
}
