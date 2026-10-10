package workflow

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

// TestAvengerUsesBashCapableEngine guards against a regression where Avenger
// (.github/workflows/avenger.md) silently loses all tool access, not just
// shell access.
//
// Root cause (see github/gh-aw#66817): the Codex engine unconditionally
// disables features.shell_tool (and code_mode/code_mode_only) whenever
// engine.model-provider resolves to "github" (see buildNativeConfig in
// codex_config.go), regardless of whether tools.bash is enabled. Codex has
// no MCP-routed fallback for bash (see codexNativeServerDefaults), so a
// workflow combining engine: codex + model-provider: github + tools.bash
// ends up with bash declared but entirely non-functional at runtime. A real
// Avenger run (37994092775) confirmed the agent observed zero available
// tools and exited cleanly after a single turn — a silent capability gap
// that was previously misclassified as prompt/token exhaustion.
//
// Avenger was switched to engine: copilot, which enforces tools.bash
// natively via --allow-tool shell and has no such GitHub-provider
// restriction. This test fails loudly if Avenger's engine ever reverts to
// codex (or any other engine incapable of enforcing bash) while
// model-provider resolves to github and tools.bash remains enabled.
func TestAvengerUsesBashCapableEngine(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	avengerPath := filepath.Join(repoRoot, ".github", "workflows", "avenger.md")

	content, err := os.ReadFile(avengerPath)
	require.NoError(t, err, "failed to read avenger.md")

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err, "failed to parse avenger.md frontmatter")

	engineRaw, ok := result.Frontmatter["engine"]
	require.True(t, ok, "avenger.md must declare an engine")
	engineMap, ok := engineRaw.(map[string]any)
	require.True(t, ok, "avenger.md engine must be a mapping")

	engineID, _ := engineMap["id"].(string)
	modelProvider, _ := engineMap["model-provider"].(string)

	toolsRaw, ok := result.Frontmatter["tools"]
	require.True(t, ok, "avenger.md must declare tools")
	toolsMap, ok := toolsRaw.(map[string]any)
	require.True(t, ok, "avenger.md tools must be a mapping")

	bashEnabled := HasBashExplicitRestriction(toolsMap) == false
	_, bashDeclared := toolsMap["bash"]
	require.True(t, bashDeclared, "avenger.md must declare tools.bash")

	if modelProvider == "github" && bashEnabled {
		engine, err := GetGlobalEngineRegistry().GetEngine(engineID)
		require.NoError(t, err, "unknown engine id %q", engineID)
		require.True(t, engine.GetCapabilities().BashCommandAllowlist,
			"avenger.md uses engine %q with model-provider: github and tools.bash enabled, but this engine "+
				"does not enforce a bash allowlist natively; for engine 'codex' this silently disables "+
				"features.shell_tool for GitHub-provider inference (see codex_config.go buildNativeConfig), "+
				"stripping all tool access despite tools.bash being declared (github/gh-aw#66817)", engineID)
	}
}
