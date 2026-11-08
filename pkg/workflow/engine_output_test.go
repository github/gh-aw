//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBehaviorDefinedEngineOutputFiles(t *testing.T) {
	var definition EngineDefinition
	require.NoError(t, yaml.Unmarshal([]byte(`
id: diagnostics
behaviors:
  output-files:
    - /tmp/gh-aw/agent/native.traj.json
`), &definition))
	engine, err := NewBehaviorDefinedEngine(&definition)
	require.NoError(t, err)
	compiler := NewCompiler()
	data := &WorkflowData{AI: definition.ID, SafeOutputs: &SafeOutputsConfig{}}
	paths := compiler.collectArtifactPaths(data, engine, constants.AgentStdioLogPath, nil)
	require.Contains(t, paths, "/tmp/gh-aw/agent/native.traj.json")
	require.True(t, isPathScannedBySecretRedaction("/tmp/gh-aw/agent/native.traj.json"))

	var generated strings.Builder
	compiler.generateUnifiedArtifactUpload(&generated, paths, "", data, false)
	var steps []struct {
		If   string `yaml:"if"`
		With struct {
			Path string `yaml:"path"`
		} `yaml:"with"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(generated.String()), &steps))
	require.Len(t, steps, 1)
	require.Contains(t, steps[0].With.Path, "/tmp/gh-aw/agent/native.traj.json")
	require.Equal(t, "always() && steps.redact_secrets.outcome == 'success' && steps.validate_recovery_envelopes.outcome == 'success' && steps.validate_final_recovery_envelopes.outcome == 'success'", steps[0].If)
	require.NotContains(t, steps[0].If, "agentic_execution", "failed executions must retain redacted diagnostics")
	require.Empty(t, engine.GetLogParserScriptId(), "diagnostic retention must not enable structured parsing")

	generated.Reset()
	compiler.generateAgentOutputFallbackUpload(&generated, data, "", false)
	require.NoError(t, yaml.Unmarshal([]byte(generated.String()), &steps))
	require.Len(t, steps, 1)
	require.Equal(t, "always() && steps.redact_secrets.outcome == 'success' && steps.validate_recovery_envelopes.outcome == 'success' && steps.validate_final_recovery_envelopes.outcome == 'success'", steps[0].If)
	require.NotContains(t, steps[0].With.Path, "native.traj.json", "large diagnostics do not belong in the small safe-output fallback")

	declared := engine.GetDeclaredOutputFiles()
	declared[0] = "/tmp/gh-aw/agent/changed.json"
	require.Equal(t, []string{"/tmp/gh-aw/agent/native.traj.json"}, engine.GetDeclaredOutputFiles())
}

func TestBehaviorDefinedEngineRejectsUnsafeOutputFiles(t *testing.T) {
	for _, file := range []string{
		"/etc/passwd", "/tmp/gh-aw/agent/../secret.json", "/tmp/gh-aw/agent/a/../../secret.json",
		"/tmp/gh-aw/agent/*.json", "/tmp/gh-aw/agent/native.json\n/etc/passwd",
		"${{ runner.temp }}/gh-aw-protected-capture-1/stderr.log",
		"/tmp/gh-aw/agent/native.db", "/tmp/gh-aw/agent/", "/tmp/gh-aw/agent/a//native.json",
		"/tmp/gh-aw/agent/native.json;echo", "/tmp/gh-aw/agent/native\\file.json",
	} {
		t.Run(file, func(t *testing.T) {
			_, err := NewBehaviorDefinedEngine(&EngineDefinition{ID: "diagnostics", Behaviors: &EngineBehaviorDefinition{
				OutputFiles: []string{file},
			}})
			require.ErrorContains(t, err, "behaviors.output-files")
		})
	}
}

func TestMiniSweAgentTrajectoryArtifactBinding(t *testing.T) {
	var source struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/shared/mini-swe-agent.md", &source)
	engine, err := NewBehaviorDefinedEngine(&source.Engine)
	require.NoError(t, err)
	paths := NewCompiler().collectArtifactPaths(&WorkflowData{AI: source.Engine.ID}, engine, constants.AgentStdioLogPath, nil)
	require.Contains(t, paths, "/tmp/gh-aw/agent/mini-swe-agent.traj.json")
	require.Empty(t, engine.GetLogParserScriptId())
}

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
