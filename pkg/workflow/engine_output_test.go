//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEngineArtifactPaths_ClaudeDebugLog(t *testing.T) {
	paths := getEngineArtifactPaths(NewClaudeEngine())

	assert.Contains(t, paths, claudeDebugLogFile)
	assert.Contains(t, paths, RedactedURLsLogPath)
}

// TestGetEngineArtifactPaths_WithOutputFiles verifies that engines with declared output files
// return those paths plus the redacted URLs log appended.
func TestGetEngineArtifactPaths_WithOutputFiles(t *testing.T) {
	// Codex engine declares output files
	codexEngine := NewCodexEngine()
	declaredFiles := codexEngine.GetDeclaredOutputFiles()
	require.NotEmpty(t, declaredFiles, "Codex engine must declare at least one output file for this test")

	paths := getEngineArtifactPaths(codexEngine)

	assert.NotNil(t, paths, "getEngineArtifactPaths should return paths when engine has output files")
	assert.Len(t, paths, len(declaredFiles)+1, "result should contain declared files plus the redacted URLs log")

	// The redacted URLs log should be the last appended path
	assert.Equal(t, RedactedURLsLogPath, paths[len(paths)-1],
		"RedactedURLsLogPath should be the last entry in the returned paths")

	// All original declared files should be present
	for _, f := range declaredFiles {
		assert.Contains(t, paths, f, "declared file %q should appear in the result", f)
	}
}

// TestGetEngineArtifactPaths_GeminiEngine verifies Gemini wildcard paths are preserved.
func TestGetEngineArtifactPaths_GeminiEngine(t *testing.T) {
	geminiEngine := NewGeminiEngine()
	paths := getEngineArtifactPaths(geminiEngine)

	require.NotNil(t, paths, "Gemini engine should have artifact paths")

	// Gemini declares the client-error wildcard log
	wildcardFound := false
	for _, p := range paths {
		if strings.Contains(p, "gemini-client-error") {
			wildcardFound = true
			break
		}
	}
	assert.True(t, wildcardFound, "Gemini artifact paths should include gemini-client-error wildcard")

	// Redacted URLs log should be present
	assert.Contains(t, paths, RedactedURLsLogPath, "RedactedURLsLogPath should be present in Gemini artifact paths")
}

// TestGenerateEngineOutputCleanup_NoOutputFiles verifies that engines with no declared
// output files produce no cleanup YAML.
func TestGenerateEngineOutputCleanup_NoOutputFiles(t *testing.T) {
	compiler := NewCompiler()
	var yaml strings.Builder

	// Claude has no declared output files
	claudeEngine := NewClaudeEngine()
	compiler.generateEngineOutputCleanup(&yaml, claudeEngine)

	assert.Empty(t, yaml.String(), "generateEngineOutputCleanup should produce no output for engines with no declared files")
}

// TestGenerateEngineOutputCleanup_WithWorkspaceFiles verifies that workspace files
// (outside /tmp/gh-aw/) get a cleanup step while /tmp/gh-aw/ files do not.
func TestGenerateEngineOutputCleanup_WithWorkspaceFiles(t *testing.T) {
	compiler := NewCompiler()
	var yaml strings.Builder

	// Codex declares /tmp/gh-aw/ files; those shouldn't get a rm -fr cleanup
	codexEngine := NewCodexEngine()
	declaredFiles := codexEngine.GetDeclaredOutputFiles()
	require.NotEmpty(t, declaredFiles, "Codex must declare files for this test")

	compiler.generateEngineOutputCleanup(&yaml, codexEngine)
	result := yaml.String()

	// If all Codex output files are under /tmp/gh-aw/, no cleanup step should be emitted
	allUnderTmpGhAw := true
	for _, f := range declaredFiles {
		if !strings.HasPrefix(f, "/tmp/gh-aw/") {
			allUnderTmpGhAw = false
			break
		}
	}

	if allUnderTmpGhAw {
		assert.Empty(t, result, "no cleanup step should be emitted when all files are under /tmp/gh-aw/")
	} else {
		assert.Contains(t, result, "Clean up engine output files", "cleanup step should be present for workspace files")
	}
}
