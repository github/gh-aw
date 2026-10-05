//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadKiroWorkflowDefinition(t *testing.T) EngineDefinition {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/shared/kiro.md")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) != 3 {
		t.Fatal("shared Kiro workflow has invalid frontmatter")
	}
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
		t.Fatal(err)
	}
	if frontmatter.Engine.Behaviors == nil || frontmatter.Engine.Behaviors.MCP == nil {
		t.Fatal("shared Kiro workflow has no MCP behaviors")
	}
	return frontmatter.Engine
}

func TestKiroWorkflowConfiguresContainerRuntime(t *testing.T) {
	for _, path := range []string{
		"../../.github/workflows/shared/kiro.md",
		"../../.github/workflows/smoke-kiro.lock.yml",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read Kiro workflow file %s: %v", path, err)
		}
		config := string(content)

		if strings.Contains(config, `MCP_GATEWAY_HOST_DOMAIN || "localhost"`) {
			t.Errorf("expected %s to avoid the host-only MCP gateway domain", path)
		}
		if !strings.Contains(config, `MCP_GATEWAY_DOMAIN || "host.docker.internal"`) {
			t.Errorf("expected %s to use the container MCP gateway domain", path)
		}
		if !strings.Contains(config, "PATH: `${binDir}:${process.env.PATH || \"\"}`") {
			t.Errorf("expected %s to expose Kiro's sibling binaries on PATH", path)
		}
	}
}

func TestKiroSmokeExercisesNativeMCPAndGoBuild(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/smoke-kiro.md")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, expected := range []string{
		"    - go\n",
		"runtimes:\n  go:\n",
		"Do not substitute `gh`, curl, or another CLI",
		"`kiro-cli --version` matches `GH_AW_ENGINE_VERSION`",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("smoke-kiro must contain %q", expected)
		}
	}
	if strings.Contains(source, "shared/gh.md") {
		t.Error("smoke-kiro must not replace native GitHub MCP with gh-proxy")
	}

	lock, err := os.ReadFile("../../.github/workflows/smoke-kiro.lock.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"GH_AW_ENGINE_VERSION: " + loadKiroWorkflowDefinition(t).Version,
		"proxy.golang.org",
		"sum.golang.org",
		"storage.googleapis.com",
		`"container": "ghcr.io/github/github-mcp-server:`,
		`"GITHUB_READ_ONLY": "1"`,
	} {
		if !strings.Contains(string(lock), expected) {
			t.Errorf("compiled smoke-kiro must contain %q", expected)
		}
	}
	if strings.Contains(string(lock), "--enable-gh-proxy") {
		t.Error("compiled smoke-kiro must not replace native GitHub MCP with gh-proxy")
	}
}

func TestKiroMCPAdapter(t *testing.T) {
	adapter := loadKiroWorkflowDefinition(t).Behaviors.MCP.ConfigAdapter
	t.Run("native config and secret-safe logging", func(t *testing.T) {
		workspace := t.TempDir()
		gateway := `{"mcpServers":{
				"github":{"url":"http://localhost:8080/mcp/github","headers":{"Authorization":"Bearer fixture-mcp-secret"},"tools":["search"]},
				"safeoutputs":{"url":"http://localhost:8080/mcp/safeoutputs"},
				"local":{"command":"node","args":["server.cjs"]}
			}}`
		output, err := runKiroAdapter(t, adapter, workspace, gateway, `["safeoutputs"]`)
		if err != nil {
			t.Fatalf("adapter failed: %v\n%s", err, output)
		}
		assertKiroMCPConfig(t, workspace)
		if !strings.Contains(output, "2 native MCP server(s), 1 HTTP server(s), skipped 1 CLI-mounted server(s); permissions=0600") {
			t.Fatalf("missing adapter diagnostics:\n%s", output)
		}
		if strings.Contains(output, "fixture-mcp-secret") || strings.Contains(output, "Authorization") {
			t.Fatalf("adapter logs exposed credentials:\n%s", output)
		}
	})
	for _, test := range []struct {
		name, gateway, cliServers, want string
	}{
		{"malformed gateway", `{"secret":"fixture-mcp-secret"`, `[]`, "must contain valid JSON"},
		{"missing servers", `{}`, `[]`, "must contain an mcpServers object"},
		{"array servers", `{"mcpServers":[]}`, `[]`, "must contain an mcpServers object"},
		{"invalid entry", `{"mcpServers":{"github":null}}`, `[]`, "invalid server entry"},
		{"malformed CLI list", `{"mcpServers":{}}`, `fixture-mcp-secret`, "must be a JSON array"},
		{"object CLI list", `{"mcpServers":{}}`, `{}`, "must be a JSON array"},
		{"non-string CLI name", `{"mcpServers":{}}`, `[1]`, "must be a JSON array"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := runKiroAdapter(t, adapter, t.TempDir(), test.gateway, test.cliServers)
			if err == nil || !strings.Contains(output, test.want) || strings.Contains(output, "fixture-mcp-secret") {
				t.Fatalf("expected secret-safe failure %q, got %v:\n%s", test.want, err, output)
			}
		})
	}
}

func runKiroAdapter(t *testing.T, adapter, workspace, gateway, cliServers string) (string, error) {
	t.Helper()
	scriptPath := filepath.Join(workspace, "adapter.cjs")
	gatewayPath := filepath.Join(workspace, "gateway.json")
	for path, content := range map[string]string{scriptPath: adapter, gatewayPath: gateway} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("node", scriptPath)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GITHUB_WORKSPACE=" + workspace,
		"MCP_GATEWAY_OUTPUT=" + gatewayPath,
		"MCP_GATEWAY_PORT=8080",
		"GH_AW_MCP_CLI_SERVERS=" + cliServers,
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func assertKiroMCPConfig(t *testing.T, workspace string) {
	t.Helper()
	configPath := filepath.Join(workspace, ".kiro", "settings", "mcp.json")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Type    string            `json:"type"`
			Headers map[string]string `json:"headers"`
			Tools   []string          `json:"tools"`
			Command string            `json:"command"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	github := config.MCPServers["github"]
	if len(config.MCPServers) != 2 || github.URL != "http://host.docker.internal:8080/mcp/github" ||
		github.Type != "http" || github.Headers["Authorization"] != "Bearer fixture-mcp-secret" || len(github.Tools) != 0 {
		t.Error("adapter did not preserve native HTTP MCP configuration")
	}
	if config.MCPServers["local"].Command != "node" {
		t.Error("adapter did not preserve the stdio server")
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("MCP config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestKiroHarness(t *testing.T) {
	definition := loadKiroWorkflowDefinition(t)
	if definition.Version != "2.27.1" {
		t.Fatalf("expected verified Kiro release 2.27.1, got %q", definition.Version)
	}
	scriptPath := filepath.Join(t.TempDir(), "kiro_harness.cjs")
	if err := os.WriteFile(scriptPath, []byte(definition.Behaviors.HarnessScript), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{
		"success", "arm64", "secret-fallback", "no-mcp", "missing-key",
		"missing-prompt", "empty-prompt", "invalid-model", "empty-model",
		"version-mismatch", "unsupported-arch", "invalid-mcp", "missing-mcp-servers",
		"checksum", "download", "missing-binary", "verification", "exit3", "exit7", "signal", "spawn",
	} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.Command("node", "-e", kiroHarnessTestDriver, scriptPath, scenario)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Kiro harness scenario failed: %v\n%s", err, output)
			}
		})
	}
}

const kiroHarnessTestDriver = `
	const assert = require("node:assert/strict");
	const fs = require("node:fs");
	const vm = require("node:vm");
	const [scriptPath, scenario] = process.argv.slice(1);
	const calls = [];
	const logs = [];
	let cleaned = false;
	const prompt = "--fixture-private-prompt";
	const apiKey = "fixture-api-key";
	const mcpSecret = "fixture-mcp-secret";
	const arch = scenario === "arm64" ? "arm64" : scenario === "unsupported-arch" ? "riscv64" : "x64";
	const checksum = arch === "arm64"
	  ? "33ad5462c3111ba4ef527f1d58bda08a9d1997e0ae73841a7c2ca734ebcba2d0"
	  : "3c0d7268a4bfb73f8e827822049978fa578e020b271afc1021c7532602d45d99";
	const env = {
	  KIRO_API_KEY: apiKey, KIRO_MODEL: "kiro/auto", GH_AW_ENGINE_VERSION: "2.27.1",
	  GH_AW_PROMPT: "/prompt.md", GITHUB_WORKSPACE: "/workspace",
	  GH_AW_MCP_CONFIG: "/workspace/.kiro/settings/mcp.json", PATH: "/usr/bin",
	};
	if (scenario === "secret-fallback") {
	  delete env.KIRO_API_KEY;
	  env.SECRET_KIRO_API_KEY = apiKey;
	}
	if (scenario === "missing-key") delete env.KIRO_API_KEY;
	if (scenario === "missing-prompt") delete env.GH_AW_PROMPT;
	if (scenario === "invalid-model") env.KIRO_MODEL = "openai/auto";
	if (scenario === "empty-model") env.KIRO_MODEL = "kiro/ ";
	if (scenario === "version-mismatch") env.GH_AW_ENGINE_VERSION = "2.16.1";
	if (scenario === "no-mcp") delete env.GH_AW_MCP_CONFIG;
	const mockFS = {
	  mkdtempSync: () => "/tmp/kiro-fixture",
	  existsSync: () => scenario !== "missing-binary",
	  readFileSync: path => {
	    if (path === "/prompt.md") return scenario === "empty-prompt" ? " \n" : prompt;
	    if (path.endsWith("mcp.json")) {
	      if (scenario === "invalid-mcp") return '{"secret":"' + mcpSecret;
	      if (scenario === "missing-mcp-servers") return "{}";
	      return JSON.stringify({mcpServers: {github: {
	        url: "http://host.docker.internal:8080/mcp/github",
	        headers: {Authorization: "Bearer " + mcpSecret},
	      }}});
	    }
	    assert.equal(path, "/tmp/kiro-fixture/kiro-cli.tar.gz");
	    return Buffer.from("fixture-archive");
	  },
	  rmSync: (path, options) => {
	    assert.equal(path, "/tmp/kiro-fixture");
	    assert.equal(options.recursive, true);
	    cleaned = true;
	  },
	};
	const spawnSync = (command, args, options) => {
	  calls.push({command, args, options});
	  assert.equal(options.shell, undefined);
	  assert.equal(options.stdio, "inherit");
	  if (scenario === "download" && command === "curl") return {status: 22};
	  if (scenario === "verification" && args[0] === "--version") return {status: 9};
	  if (args[0] === "chat") {
	    if (scenario === "exit3") return {status: 3};
	    if (scenario === "exit7") return {status: 7};
	    if (scenario === "signal") return {status: null, signal: "SIGTERM"};
	    if (scenario === "spawn") return {error: new Error("spawn ENOENT"), status: null};
	  }
	  return {status: 0};
	};
	const mockProcess = {
	  argv: ["node", scriptPath, "kiro-cli", "chat", "--no-interactive", "--trust-all-tools", "--require-mcp-startup"],
	  env, arch, stderr: {write: text => logs.push(text)},
	};
	vm.runInNewContext(fs.readFileSync(scriptPath, "utf8"), {
	  require: name => {
	    if (name === "fs") return mockFS;
	    if (name === "child_process") return {spawnSync};
	    if (name === "crypto") return {createHash: algorithm => {
	      assert.equal(algorithm, "sha256");
	      return {update: data => {
	        assert.equal(data.toString(), "fixture-archive");
	        return {digest: encoding => {
	          assert.equal(encoding, "hex");
	          return scenario === "checksum" ? "bad-checksum" : checksum;
	        }};
	      }};
	    }};
	    return require(name);
	  },
	  process: mockProcess, Buffer,
	});
	assert.equal(cleaned, true);
	const output = logs.join("");
	for (const secret of [prompt, apiKey, mcpSecret, "Authorization"]) {
	  assert.equal(output.includes(secret), false, "logs exposed " + secret);
	}
	assert.ok(output.includes("[kiro-harness] Starting Kiro CLI 2.27.1"));
	assert.ok(output.includes("Cleaned up Kiro CLI installation; total duration="));
	const failures = {
	  "missing-key": [1, "KIRO_API_KEY is required"],
	  "missing-prompt": [1, "GH_AW_PROMPT is required"],
	  "empty-prompt": [1, "non-empty instruction"],
	  "invalid-model": [1, "kiro/model format"],
	  "empty-model": [1, "must include a model name"],
	  "version-mismatch": [1, "supports only engine.version 2.27.1"],
	  "unsupported-arch": [1, "Unsupported Kiro CLI architecture"],
	  "invalid-mcp": [1, "GH_AW_MCP_CONFIG must contain valid JSON"],
	  "missing-mcp-servers": [1, "must contain an mcpServers object"],
	  "checksum": [1, "checksum did not match"],
	  "download": [22, "download failed with exit code 22"],
	  "missing-binary": [1, "executable was not found"],
	  "verification": [9, "verification failed with exit code 9"],
	  "exit3": [3, "required MCP server startup failed"],
	  "exit7": [7, "execution failed with exit code 7"],
	  "signal": [1, "execution failed with signal SIGTERM"],
	  "spawn": [1, "spawn ENOENT"],
	};
	if (failures[scenario]) {
	  const [code, detail] = failures[scenario];
	  assert.equal(mockProcess.exitCode, code);
	  assert.ok(output.includes(detail), output);
	  assert.equal(output.includes("Kiro CLI execution completed"), false);
	  if (["missing-key", "missing-prompt", "empty-prompt", "invalid-model", "empty-model",
	       "version-mismatch", "unsupported-arch", "invalid-mcp", "missing-mcp-servers"].includes(scenario)) {
	    assert.equal(calls.length, 0, "invalid input must fail before downloading");
	  }
	} else {
	  assert.equal(mockProcess.exitCode || 0, 0);
	  assert.equal(calls.length, 4);
	  assert.equal(calls[0].command, "curl");
	  assert.ok(calls[0].args.at(-1).endsWith(
	    "/2.27.1/kirocli-" + (arch === "arm64" ? "aarch64" : "x86_64") + "-linux.tar.gz"
	  ));
	  assert.deepEqual(Array.from(calls[1].args), ["-xzf", "/tmp/kiro-fixture/kiro-cli.tar.gz", "-C", "/tmp/kiro-fixture"]);
	  assert.deepEqual(Array.from(calls[2].args), ["--version"]);
	  assert.deepEqual(Array.from(calls[3].args), [
	    "chat", "--no-interactive", "--trust-all-tools", "--require-mcp-startup",
	    "--model", "auto", "--", prompt,
	  ]);
	  assert.equal(calls[3].options.cwd, "/workspace");
	  assert.equal(calls[3].options.env.KIRO_API_KEY, apiKey);
	  assert.equal(calls[3].options.env.PATH, "/tmp/kiro-fixture/kirocli/bin:/usr/bin");
	  assert.ok(output.includes("Kiro CLI SHA-256 checksum verified"));
	  assert.ok(output.includes("Kiro CLI execution completed in "));
	  assert.ok(output.includes("Prompt loaded ("));
	  assert.ok(output.includes(scenario === "no-mcp"
	    ? "No GH_AW_MCP_CONFIG path supplied" : "Native MCP configuration loaded (1 server(s))"));
	}
	`
