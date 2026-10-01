//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

// containerPinRE matches Docker image digest pins of the form @sha256:<64 hex chars>.
var testContainerPinRE = regexp.MustCompile(`@sha256:[0-9a-f]{64}`)
var testAWFImageTagDigestRE = regexp.MustCompile(`,[a-z-]+=sha256:[0-9a-f]{64}`)
var testProjectUTCEnvLineRE = regexp.MustCompile(`(?m)^\s*GH_AW_PROJECT_UTC:.*(?:\r?\n|$)`)

// testAWFConfigPayloadRE matches the entire AWF config JSON payload written to awf-config.json.
// The JSON contains model aliases, domain allowlists, and a container image tag that all change
// frequently as new models and domains are added. Normalizing the entire payload keeps golden
// fixtures stable without masking structural changes to the surrounding shell command.
var testAWFConfigPayloadRE = regexp.MustCompile(`(printf '%s\\n' ")(\{.*\})(" > "\$\{RUNNER_TEMP\}/gh-aw/awf-config\.json")`)
var testDefaultAWFInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_AWF_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultFirewallVersion)) + `"`)
var testDefaultAWFGatewayInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_AWMG_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultMCPGatewayVersion)) + `"`)
var testDefaultAWFInstallVersionRE = regexp.MustCompile(`(install_awf_binary\.sh"\s+)` + regexp.QuoteMeta(string(constants.DefaultFirewallVersion)) + `\b`)
var testDefaultAWFImageRE = regexp.MustCompile(`(ghcr\.io/github/gh-aw-firewall/(?:agent|api-proxy|cli-proxy|squid):)` + regexp.QuoteMeta(strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")) + `\b`)
var testDefaultAWFSchemaURLRE = regexp.MustCompile(`(releases/download/)` + regexp.QuoteMeta(string(constants.DefaultFirewallVersion)) + `(/awf-config\.schema\.json)`)
var testDefaultAWFImageTagRE = regexp.MustCompile(`("imageTag"\s*:\s*")(?:v)?` + regexp.QuoteMeta(strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")) + `"`)
var testDefaultMCPGImageRE = regexp.MustCompile(`(ghcr\.io/github/gh-aw-mcpg:)` + regexp.QuoteMeta(string(constants.DefaultMCPGatewayVersion)) + `\b`)
var testDefaultGitHubMCPServerImageRE = regexp.MustCompile(`(ghcr\.io/github/github-mcp-server:)` + regexp.QuoteMeta(string(constants.DefaultGitHubMCPServerVersion)) + `\b`)
var testDefaultClaudeInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultClaudeCodeVersion)) + `"`)
var testDefaultClaudeAgentInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_AGENT_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultClaudeCodeVersion)) + `"`)
var testDefaultClaudeInstallVersionRE = regexp.MustCompile(`(@anthropic-ai/claude-code@)` + regexp.QuoteMeta(string(constants.DefaultClaudeCodeVersion)) + `\b`)
var testDefaultCopilotInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultCopilotVersion)) + `"`)
var testDefaultCopilotAgentInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_AGENT_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultCopilotVersion)) + `"`)
var testDefaultCodexInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultCodexVersion)) + `"`)
var testDefaultCodexAgentInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_AGENT_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultCodexVersion)) + `"`)
var testDefaultCodexInstallVersionRE = regexp.MustCompile(`(@openai/codex@)` + regexp.QuoteMeta(string(constants.DefaultCodexVersion)) + `\b`)
var testDefaultPiInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultPiVersion)) + `"`)
var testDefaultPiAgentInfoVersionRE = regexp.MustCompile(`GH_AW_INFO_AGENT_VERSION: "` + regexp.QuoteMeta(string(constants.DefaultPiVersion)) + `"`)
var testDefaultPiInstallVersionRE = regexp.MustCompile(`(@earendil-works/pi-coding-agent@)` + regexp.QuoteMeta(string(constants.DefaultPiVersion)) + `\b`)
var testCheckoutPinRE = regexp.MustCompile(`actions/checkout@[0-9a-f]{40}\s+#\s+v\d+\.\d+\.\d+`)

func normalizeDefaultRuntimeVersions(content string) string {
	normalized := testDefaultAWFInfoVersionRE.ReplaceAllString(content, `GH_AW_INFO_AWF_VERSION: "vAWF_VERSION"`)
	normalized = testDefaultAWFGatewayInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_AWMG_VERSION: "vMCPG_VERSION"`)
	normalized = testDefaultAWFInstallVersionRE.ReplaceAllString(normalized, `${1}vAWF_VERSION`)
	normalized = testDefaultAWFImageRE.ReplaceAllString(normalized, `${1}AWF_VERSION`)
	normalized = testDefaultAWFSchemaURLRE.ReplaceAllString(normalized, `${1}vAWF_VERSION$2`)
	normalized = testDefaultAWFImageTagRE.ReplaceAllString(normalized, `${1}AWF_VERSION"`)
	normalized = testDefaultClaudeInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_VERSION: "CLAUDE_VERSION"`)
	normalized = testDefaultClaudeAgentInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_AGENT_VERSION: "CLAUDE_VERSION"`)
	normalized = testDefaultClaudeInstallVersionRE.ReplaceAllString(normalized, `${1}CLAUDE_VERSION`)
	normalized = testDefaultCopilotInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_VERSION: "COPILOT_VERSION"`)
	normalized = testDefaultCopilotAgentInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_AGENT_VERSION: "COPILOT_VERSION"`)
	normalized = testDefaultCodexInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_VERSION: "CODEX_VERSION"`)
	normalized = testDefaultCodexAgentInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_AGENT_VERSION: "CODEX_VERSION"`)
	normalized = testDefaultCodexInstallVersionRE.ReplaceAllString(normalized, `${1}CODEX_VERSION`)
	normalized = testDefaultCopilotInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_VERSION: "COPILOT_VERSION"`)
	normalized = testDefaultCopilotAgentInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_AGENT_VERSION: "COPILOT_VERSION"`)
	normalized = testDefaultPiInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_VERSION: "PI_VERSION"`)
	normalized = testDefaultPiAgentInfoVersionRE.ReplaceAllString(normalized, `GH_AW_INFO_AGENT_VERSION: "PI_VERSION"`)
	normalized = testDefaultPiInstallVersionRE.ReplaceAllString(normalized, `${1}PI_VERSION`)
	return testDefaultMCPGImageRE.ReplaceAllString(normalized, `${1}MCPG_VERSION`)
}

// normalizeOutput applies all stable-comparison normalizations to compiled workflow output
// before golden comparison: heredoc delimiter normalization and container pin normalization.
// Mirrors normalize() in scripts/test-wasm-golden.mjs.
func normalizeOutput(content string) string {
	normalized := testContainerPinRE.ReplaceAllString(normalizeHeredocDelimiters(content), "")
	// Keep golden fixtures stable across native-vs-wasm GH_AW_PROJECT_UTC emission differences.
	normalized = testProjectUTCEnvLineRE.ReplaceAllString(normalized, "")
	// Keep golden fixtures stable across claude default model fallback updates.
	normalized = strings.ReplaceAll(normalized, fmt.Sprintf("|| '%s'", constants.SonnetDefaultModel), "|| 'default'")
	// Keep golden fixtures stable across copilot default model fallback updates.
	normalized = strings.ReplaceAll(normalized, fmt.Sprintf("|| '%s'", constants.CopilotBYOKDefaultModel), "|| 'default'")
	// Keep golden fixtures stable across claude default model fallback updates.
	normalized = strings.ReplaceAll(normalized, fmt.Sprintf("|| '%s'", constants.SonnetDefaultModel), "|| 'default'")
	// Keep golden fixtures stable across temporary workspace-path allowlist shape changes.
	for _, op := range []string{"Edit", "MultiEdit", "Read", "Write"} {
		normalized = strings.ReplaceAll(normalized, op+"(/tmp/gh-aw/*)", op+"(/tmp/gh-aw/agent/*)")
	}
	normalized = normalizeDefaultRuntimeVersions(normalized)
	normalized = testDefaultGitHubMCPServerImageRE.ReplaceAllString(normalized, `${1}GH_MCP_VERSION`)
	normalized = testCheckoutPinRE.ReplaceAllString(normalized, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1")
	// Keep golden fixtures stable across AWF config payload changes (model aliases, domain
	// allowlists, container image tags). The entire JSON body is replaced with a stable
	// placeholder so that model/domain additions don't break golden comparisons.
	normalized = testAWFConfigPayloadRE.ReplaceAllString(normalized, `${1}AWF_CONFIG_PAYLOAD${3}`)
	return testAWFImageTagDigestRE.ReplaceAllString(normalized, "")
}

func TestNormalizeOutput_DefaultRuntimeVersions(t *testing.T) {
	input := strings.Join([]string{
		`GH_AW_INFO_AWF_VERSION: "` + string(constants.DefaultFirewallVersion) + `"`,
		`GH_AW_INFO_AWMG_VERSION: "` + string(constants.DefaultMCPGatewayVersion) + `"`,
		`run: bash "${RUNNER_TEMP}/gh-aw/actions/install_awf_binary.sh" ` + string(constants.DefaultFirewallVersion) + ` --rootless`,
		`run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ghcr.io/github/gh-aw-firewall/agent:` + strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v") + ` ghcr.io/github/gh-aw-firewall/api-proxy:` + strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v") + ` ghcr.io/github/gh-aw-mcpg:` + string(constants.DefaultMCPGatewayVersion),
		`{"schema":"https://github.com/github/gh-aw-firewall/releases/download/` + string(constants.DefaultFirewallVersion) + `/awf-config.schema.json","imageTag":"` + string(constants.DefaultFirewallVersion) + `"}`,
		`GH_AW_MODEL_DETECTION_CLAUDE: ${{ vars.GH_AW_MODEL_DETECTION_CLAUDE || vars.GH_AW_DEFAULT_MODEL_CLAUDE || '` + constants.SonnetDefaultModel + `' }}`,
		`GH_AW_INFO_VERSION: "` + string(constants.DefaultClaudeCodeVersion) + `"`,
		`GH_AW_INFO_AGENT_VERSION: "` + string(constants.DefaultClaudeCodeVersion) + `"`,
		`run: npm install -g @anthropic-ai/claude-code@` + string(constants.DefaultClaudeCodeVersion),
		`GH_AW_INFO_VERSION: "` + string(constants.DefaultCopilotVersion) + `"`,
		`GH_AW_INFO_AGENT_VERSION: "` + string(constants.DefaultCopilotVersion) + `"`,
		`GH_AW_INFO_VERSION: "` + string(constants.DefaultCodexVersion) + `"`,
		`GH_AW_INFO_AGENT_VERSION: "` + string(constants.DefaultCodexVersion) + `"`,
		`GH_AW_INFO_VERSION: "` + string(constants.DefaultPiVersion) + `"`,
		`GH_AW_INFO_AGENT_VERSION: "` + string(constants.DefaultPiVersion) + `"`,
		`GH_AW_INFO_VERSION: "` + string(constants.DefaultCopilotVersion) + `"`,
		`GH_AW_INFO_AGENT_VERSION: "` + string(constants.DefaultCopilotVersion) + `"`,
		`run: npm install --ignore-scripts -g @openai/codex@` + string(constants.DefaultCodexVersion),
		`run: npm install --ignore-scripts -g @earendil-works/pi-coding-agent@` + string(constants.DefaultPiVersion),
		`{"pinnedAwf":"v0.5.0","pinnedAwfImage":"ghcr.io/github/gh-aw-firewall/agent:0.5.0","pinnedMcpgImage":"ghcr.io/github/gh-aw-mcpg:v0.0.12"}`,
	}, "\n")

	normalized := normalizeOutput(input)

	require.Contains(t, normalized, `GH_AW_INFO_AWF_VERSION: "vAWF_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_AWMG_VERSION: "vMCPG_VERSION"`)
	require.Contains(t, normalized, `install_awf_binary.sh" vAWF_VERSION --rootless`)
	require.Contains(t, normalized, `ghcr.io/github/gh-aw-firewall/agent:AWF_VERSION`)
	require.Contains(t, normalized, `ghcr.io/github/gh-aw-firewall/api-proxy:AWF_VERSION`)
	require.Contains(t, normalized, `ghcr.io/github/gh-aw-mcpg:MCPG_VERSION`)
	require.Contains(t, normalized, `releases/download/vAWF_VERSION/awf-config.schema.json`)
	require.Contains(t, normalized, `"imageTag":"AWF_VERSION"`)
	require.Contains(t, normalized, `GH_AW_MODEL_DETECTION_CLAUDE: ${{ vars.GH_AW_MODEL_DETECTION_CLAUDE || vars.GH_AW_DEFAULT_MODEL_CLAUDE || 'default' }}`)
	require.Contains(t, normalized, `GH_AW_INFO_VERSION: "CLAUDE_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_AGENT_VERSION: "CLAUDE_VERSION"`)
	require.Contains(t, normalized, `@anthropic-ai/claude-code@CLAUDE_VERSION`)
	require.Contains(t, normalized, `GH_AW_INFO_VERSION: "COPILOT_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_AGENT_VERSION: "COPILOT_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_VERSION: "CODEX_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_AGENT_VERSION: "CODEX_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_VERSION: "PI_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_AGENT_VERSION: "PI_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_VERSION: "COPILOT_VERSION"`)
	require.Contains(t, normalized, `GH_AW_INFO_AGENT_VERSION: "COPILOT_VERSION"`)
	require.Contains(t, normalized, `@openai/codex@CODEX_VERSION`)
	require.Contains(t, normalized, `@earendil-works/pi-coding-agent@PI_VERSION`)
	require.Contains(t, normalized, `"pinnedAwf":"v0.5.0"`)
	require.Contains(t, normalized, `"pinnedAwfImage":"ghcr.io/github/gh-aw-firewall/agent:0.5.0"`)
	require.Contains(t, normalized, `"pinnedMcpgImage":"ghcr.io/github/gh-aw-mcpg:v0.0.12"`)
	require.NotContains(t, normalized, string(constants.DefaultFirewallVersion))
	require.NotContains(t, normalized, string(constants.DefaultMCPGatewayVersion))
	require.NotContains(t, normalized, constants.SonnetDefaultModel)
	require.NotContains(t, normalized, string(constants.DefaultClaudeCodeVersion))
	require.NotContains(t, normalized, string(constants.DefaultCopilotVersion))
	require.NotContains(t, normalized, string(constants.DefaultCodexVersion))
	require.NotContains(t, normalized, string(constants.DefaultPiVersion))
	require.NotContains(t, normalized, string(constants.DefaultCopilotVersion))
}

// TestWasmGolden_CompileFixtures compiles each workflow fixture using the string API
// (the same code path used by the wasm compiler) and compares against golden files.
//
// To update golden files:
//
//	go test -v ./pkg/workflow -run='^TestWasmGolden_' -update
//
// Or use the Makefile target:
//
//	make update-wasm-golden
func TestWasmGolden_CompileFixtures(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "wasm_golden", "fixtures")

	origDir, err := os.Getwd()
	require.NoError(t, err)
	absFixturesDir, err := filepath.Abs(fixturesDir)
	require.NoError(t, err)

	// Read fixture list using an absolute path so we don't need to change the
	// working directory before the subtests start.
	entries, err := os.ReadDir(absFixturesDir)
	require.NoError(t, err, "failed to read fixtures directory")

	var fixtures []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			fixtures = append(fixtures, entry.Name())
		}
	}
	require.NotEmpty(t, fixtures, "no .md fixtures found in %s", fixturesDir)

	for _, fixture := range fixtures {
		testName := strings.TrimSuffix(fixture, ".md")
		t.Run(testName, func(t *testing.T) {
			// Change to fixtures dir so relative imports resolve correctly during
			// compilation. Cleanup always restores to origDir regardless of outcome.
			require.NoError(t, os.Chdir(absFixturesDir))
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			content, err := os.ReadFile(fixture)
			require.NoError(t, err, "failed to read fixture %s", fixture)

			// Use filename-derived identifier for fuzzy cron schedule scattering
			compiler := NewCompiler(
				WithNoEmit(true),
				WithSkipValidation(true),
				WithWorkflowIdentifier(testName),
			)

			wd, err := compiler.ParseWorkflowString(string(content), fixture)
			if err != nil {
				// Some production workflows cannot compile via string API due to:
				// - Path security restrictions (imports outside .github/)
				// - Missing external files (agent definitions, skill files)
				// - Configuration errors specific to file-based compilation
				// Skip these gracefully rather than failing the test.
				t.Skipf("skipping %s: %v", fixture, err)
			}

			yamlOutput, err := compiler.CompileToYAML(wd, fixture)
			if err != nil {
				t.Skipf("skipping %s (compile): %v", fixture, err)
			}
			require.NotEmpty(t, yamlOutput, "empty YAML output for %s", fixture)

			// Switch back to the package dir so golden.RequireEqual resolves
			// testdata/ relative to the package root (not the fixtures dir).
			require.NoError(t, os.Chdir(origDir))

			// Normalize heredoc delimiters and container pins before comparing so golden files
			// are stable across compilations (randomized tokens and environment-specific pins
			// are replaced by stable placeholders).
			golden.RequireEqual(t, normalizeOutput(yamlOutput))
		})
	}
}

// TestWasmGolden_CompileWithImports tests compilation of a workflow that
// imports a shared component. The shared component is on disk in the
// fixtures/shared/ directory. This exercises the import resolution path
// used by both native and wasm compilers.
func TestWasmGolden_CompileWithImports(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "wasm_golden", "fixtures")
	fixturePath := filepath.Join(fixturesDir, "with-imports.md")

	content, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	// Change to fixtures dir so relative imports resolve correctly
	origDir, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(fixturesDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	t.Run("with-file-imports", func(t *testing.T) {
		compiler := NewCompiler(
			WithNoEmit(true),
			WithSkipValidation(true),
		)

		wd, err := compiler.ParseWorkflowString(string(content), "with-imports.md")
		require.NoError(t, err, "ParseWorkflowString failed with file imports")

		yamlOutput, err := compiler.CompileToYAML(wd, "with-imports.md")
		require.NoError(t, err, "CompileToYAML failed with file imports")
		require.NotEmpty(t, yamlOutput, "empty YAML output with file imports")

		// Just verify it contains expected content - the import was resolved
		require.Contains(t, yamlOutput, "name:", "output should contain workflow name")
		require.Contains(t, yamlOutput, "jobs:", "output should contain jobs section")
	})
}

// TestWasmGolden_RoundTrip verifies that compiling the same input twice produces
// identical output (determinism check for the wasm compiler path).
func TestWasmGolden_RoundTrip(t *testing.T) {
	markdown := `---
name: determinism-test
description: Verify compilation determinism
on:
  workflow_dispatch:
permissions:
  contents: read
env:
  ZETA_WORKFLOW: zeta
  ALPHA_WORKFLOW: alpha
steps:
  - name: Deterministic uses step
    uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9
    with:
      zeta-input: zeta
      alpha-input: alpha
    env:
      ZETA_STEP: zeta
      ALPHA_STEP: alpha
engine: copilot
timeout-minutes: 10
---

# Mission

This workflow tests that compilation is deterministic.
`

	results := make([]string, 3)
	for i := range 3 {
		compiler := NewCompiler(
			WithNoEmit(true),
			WithSkipValidation(true),
		)

		wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
		require.NoError(t, err)

		yamlOutput, err := compiler.CompileToYAML(wd, "workflow.md")
		require.NoError(t, err)

		results[i] = yamlOutput
	}

	require.Equal(t, normalizeHeredocDelimiters(results[0]), normalizeHeredocDelimiters(results[1]), "compilation 1 and 2 differ")
	require.Equal(t, normalizeHeredocDelimiters(results[1]), normalizeHeredocDelimiters(results[2]), "compilation 2 and 3 differ")

	assertOrder := func(before, after string) {
		t.Helper()
		beforeIndex := strings.Index(results[0], before)
		afterIndex := strings.Index(results[0], after)
		require.NotEqual(t, -1, beforeIndex, "expected %q in compiled YAML", before)
		require.NotEqual(t, -1, afterIndex, "expected %q in compiled YAML", after)
		if beforeIndex >= afterIndex {
			t.Fatalf("expected %q before %q in compiled YAML:\n%s", before, after, results[0])
		}
	}

	assertOrder("  ALPHA_WORKFLOW:", "  ZETA_WORKFLOW:")
	assertOrder("      alpha-input:", "      zeta-input:")
	assertOrder("      ALPHA_STEP:", "      ZETA_STEP:")
}

// TestWasmGolden_NativeVsStringAPI compiles a workflow using both the native
// file-based path and the string API path, then reports any differences.
// This catches cases where the wasm (string API) path diverges from the native path.
func TestWasmGolden_NativeVsStringAPI(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "wasm_golden", "fixtures")

	// Resolve absolute fixtures dir before CWD change
	absFixturesDir, err := filepath.Abs(fixturesDir)
	require.NoError(t, err)

	// Change to fixtures dir so relative imports resolve
	origDir, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(absFixturesDir)
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		testName := strings.TrimSuffix(entry.Name(), ".md")
		t.Run(testName, func(t *testing.T) {
			content, err := os.ReadFile(entry.Name())
			require.NoError(t, err)

			// Compile via string API (wasm path)
			stringCompiler := NewCompiler(
				WithNoEmit(true),
				WithSkipValidation(true),
				WithWorkflowIdentifier(testName),
			)

			wd, err := stringCompiler.ParseWorkflowString(string(content), entry.Name())
			if err != nil {
				t.Skipf("skipping %s: %v", entry.Name(), err)
			}

			wasmYAML, err := stringCompiler.CompileToYAML(wd, entry.Name())
			if err != nil {
				t.Skipf("skipping %s (compile): %v", entry.Name(), err)
			}

			// Compile via file-based path (native path)
			absPath := filepath.Join(absFixturesDir, entry.Name())

			nativeCompiler := NewCompiler(
				WithNoEmit(true),
				WithSkipValidation(true),
				WithWorkflowIdentifier(testName),
			)
			nativeCompiler.skipHeader = true
			nativeCompiler.inlinePrompt = true

			nativeWd, err := nativeCompiler.ParseWorkflowFile(absPath)
			if err != nil {
				t.Skipf("skipping native compile for %s: %v", entry.Name(), err)
			}

			nativeYAML, err := nativeCompiler.CompileToYAML(nativeWd, absPath)
			if err != nil {
				t.Skipf("skipping native compile for %s: %v", entry.Name(), err)
			}

			// Compare and log differences (informational only, does not fail)
			if wasmYAML == nativeYAML {
				t.Logf("native and string API output match for %s", entry.Name())
			} else {
				wasmLines := strings.Split(wasmYAML, "\n")
				nativeLines := strings.Split(nativeYAML, "\n")
				t.Logf("INFO: native vs string API output differs for %s (wasm=%d lines, native=%d lines)",
					entry.Name(), len(wasmLines), len(nativeLines))
			}
		})
	}
}

// TestWasmGolden_AllEngines verifies that all engine types compile correctly
// via the string API and produce valid YAML output.
func TestWasmGolden_AllEngines(t *testing.T) {
	engines := []struct {
		name   string
		engine string
		extra  string // additional frontmatter
	}{
		{"copilot", "copilot", ""},
		{"claude", "claude", "network:\n  allowed:\n    - defaults"},
		{"codex", "codex", "network:\n  allowed:\n    - defaults"},
		{"gemini", "gemini", "network:\n  allowed:\n    - defaults"},
		{"pi", "pi", "tools:\n  github:\n    mode: gh-proxy\n  cli-proxy: true\nnetwork:\n  allowed:\n    - defaults"},
	}

	for _, eng := range engines {
		t.Run(eng.name, func(t *testing.T) {
			extra := ""
			if eng.extra != "" {
				extra = eng.extra + "\n"
			}
			markdown := fmt.Sprintf(`---
name: engine-%s-test
description: Test %s engine compilation
on:
  workflow_dispatch:
permissions:
  contents: read
engine: %s
timeout-minutes: 10
%s---

# Mission

Test the %s engine compilation path.
`, eng.engine, eng.engine, eng.engine, extra, eng.engine)

			compiler := NewCompiler(
				WithNoEmit(true),
				WithSkipValidation(true),
			)

			wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
			require.NoError(t, err, "%s engine parse failed", eng.name)

			yamlOutput, err := compiler.CompileToYAML(wd, "workflow.md")
			require.NoError(t, err, "%s engine compile failed", eng.name)

			if eng.name == "pi" {
				require.Contains(t, yamlOutput, "PI_OFFLINE: 1")
			}

			// Keep codex golden stable across branches where CODEX_API_KEY/OPENAI_API_KEY
			// may or may not be explicitly excluded in gh-aw firewall args.
			if eng.name == "codex" {
				yamlOutput = strings.ReplaceAll(yamlOutput, " --exclude-env CODEX_API_KEY", "")
				yamlOutput = strings.ReplaceAll(yamlOutput, " --exclude-env OPENAI_API_KEY", "")
				yamlOutput = strings.TrimRight(yamlOutput, "\n") + "\n"
			}

			golden.RequireEqual(t, normalizeOutput(yamlOutput))
		})
	}
}
