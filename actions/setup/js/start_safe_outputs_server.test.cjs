const fs = require("fs");
const os = require("os");
const path = require("path");
const assert = require("assert");
const { spawnSync } = require("child_process");

describe("start_safe_outputs_server.sh", () => {
  it("checks safe_outputs_mcp_arguments.cjs before starting the server", () => {
    const scriptPath = path.join(__dirname, "../sh/start_safe_outputs_server.sh");
    const content = fs.readFileSync(scriptPath, "utf8");
    const requiredDepsBlock = content.match(/REQUIRED_DEPS=\(([\s\S]*?)\)/);

    assert.ok(requiredDepsBlock, "REQUIRED_DEPS block should exist");
    assert.ok(requiredDepsBlock[1].includes('"safe_outputs_mcp_arguments.cjs"'), "REQUIRED_DEPS should include safe_outputs_mcp_arguments.cjs");
  });

  it.each(["start_safe_outputs_server.sh", "start_mcp_scripts_server.sh"])("%s inherits DEBUG instead of forcing verbose logging", script => {
    const content = fs.readFileSync(path.join(__dirname, "../sh", script), "utf8");
    const launchCommand = script === "start_mcp_scripts_server.sh" ? /^"\$GH_AW_NODE" mcp-server\.cjs >> /m : /^node mcp-server\.cjs >> /m;
    assert.match(content, launchCommand);
    assert.doesNotMatch(content, /DEBUG=["']\*["']/);
  });

  it.each([
    ["start_safe_outputs_server.sh", "GH_AW_SAFE_OUTPUTS"],
    ["start_mcp_scripts_server.sh", "GH_AW_MCP_SCRIPTS"],
  ])("%s forwards the exact API key without shell quoting", (script, prefix) => {
    const content = fs.readFileSync(path.join(__dirname, "../sh", script), "utf8");
    const outputBlock = content.match(/\{\n  echo "port=[\s\S]*?\} >> "\$GITHUB_OUTPUT"/);
    assert.ok(outputBlock, "Client configuration output block should exist");
    const apiKey = "test-key-with-'quotes'-and-$literal";
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-startup-output-"));
    const outputPath = path.join(outputDir, "github-output");
    try {
      const result = spawnSync("bash", ["-c", outputBlock[0]], {
        env: { ...process.env, [`${prefix}_PORT`]: "3000", [`${prefix}_API_KEY`]: apiKey, GITHUB_OUTPUT: outputPath },
        encoding: "utf8",
      });
      assert.strictEqual(result.status, 0, result.stderr);
      assert.strictEqual(fs.readFileSync(outputPath, "utf8"), `port=3000\napi_key=${apiKey}\n`);
    } finally {
      fs.rmSync(outputDir, { recursive: true, force: true });
    }
  });

  it.each(["start_safe_outputs_server.sh", "start_mcp_scripts_server.sh"])("%s does not log the API key", script => {
    const content = fs.readFileSync(path.join(__dirname, "../sh", script), "utf8");
    assert.doesNotMatch(content, /API Key:/);
    assert.doesNotMatch(content, /API_KEY:\d/);
  });

  it.each([
    ["false", "", 1, "127.0.0.1"],
    ["true", "fd00::1\n172.17.0.1", 0, "172.17.0.1"],
    ["true", "fd00::1\n0.0.0.0", 0, null],
    ["true", "", 1, null],
  ])("selects a scoped MCP Scripts bind address (bridged=%s, Docker result=%s/%s)", (bridged, gateways, dockerStatus, expected) => {
    const content = fs.readFileSync(path.join(__dirname, "../sh/start_mcp_scripts_server.sh"), "utf8");
    const binding = content.match(/^GH_AW_MCP_SCRIPTS_BIND_HOST=127\.0\.0\.1[\s\S]*?^export GH_AW_MCP_SCRIPTS_BIND_HOST$/m);
    assert.ok(binding, "MCP Scripts bind configuration should exist");
    const result = spawnSync("bash", ["-ec", `docker() { printf '%s\\n' "$TEST_GATEWAYS"; return "$TEST_DOCKER_STATUS"; }\n${binding[0]}\nprintenv GH_AW_MCP_SCRIPTS_BIND_HOST`], {
      env: { ...process.env, GH_AW_MCP_SCRIPTS_BIND_HOST: "0.0.0.0", GH_AW_MCP_SCRIPTS_BRIDGED_GATEWAY: bridged, TEST_GATEWAYS: gateways, TEST_DOCKER_STATUS: String(dockerStatus) },
      encoding: "utf8",
    });
    if (expected === null) {
      assert.strictEqual(result.status, 1);
      assert.match(result.stdout, /^::error::Cannot /);
    } else {
      assert.strictEqual(result.status, 0, result.stderr);
      assert.strictEqual(result.stdout.trim(), expected);
    }
  });
});
