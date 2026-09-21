const fs = require("fs");
const path = require("path");
const assert = require("assert");
const os = require("os");
const { spawnSync } = require("child_process");
const { copySafeOutputsBundle } = require("./fixtures/safeoutputs_bundle.fixture.cjs");

describe("start_safe_outputs_server.sh", () => {
  it("checks safe_outputs_mcp_arguments.cjs before starting the server", () => {
    const scriptPath = path.join(__dirname, "../sh/start_safe_outputs_server.sh");
    const content = fs.readFileSync(scriptPath, "utf8");
    const requiredDepsBlock = content.match(/REQUIRED_DEPS=\(([\s\S]*?)\)/);

    assert.ok(requiredDepsBlock, "REQUIRED_DEPS block should exist");
    assert.ok(requiredDepsBlock[1].includes('"safe_outputs_mcp_arguments.cjs"'), "REQUIRED_DEPS should include safe_outputs_mcp_arguments.cjs");
    assert.ok(requiredDepsBlock[1].includes('"branch_pattern_helpers.cjs"'), "REQUIRED_DEPS should include the shared branch-policy helper");
  });

  it("loads both safe-output servers from the actual isolated setup bundle", () => {
    const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-safeoutputs-bundle-"));
    try {
      const bundle = path.join(scratch, "bundle");
      const files = copySafeOutputsBundle(bundle);
      assert.ok(files.includes("branch_pattern_helpers.cjs"));
      const args = ["-e", 'require(process.argv[1]); require(process.argv[2]); process.stdout.write("bundle loaded");', path.join(bundle, "safe_outputs_mcp_server.cjs"), path.join(bundle, "safe_outputs_mcp_server_http.cjs")];
      const options = { cwd: scratch, env: { ...process.env, NODE_PATH: "" }, encoding: "utf8", timeout: 10_000, maxBuffer: 256 * 1024, windowsHide: true };
      const loaded = spawnSync(process.execPath, args, options);
      assert.equal(loaded.status, 0, loaded.stderr);
      assert.ok(loaded.stdout.includes("bundle loaded"));
      fs.unlinkSync(path.join(bundle, "branch_pattern_helpers.cjs"));
      const missing = spawnSync(process.execPath, args, options);
      assert.notEqual(missing.status, 0, "missing packaged modules must not resolve from the full source tree");
      assert.ok(missing.stderr.includes("Cannot find module './branch_pattern_helpers.cjs'"));
    } finally {
      fs.rmSync(scratch, { recursive: true, force: true });
    }
  });
});
