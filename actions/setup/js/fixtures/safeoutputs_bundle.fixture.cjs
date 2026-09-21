"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

function copySafeOutputsBundle(destination) {
  const source = path.resolve(__dirname, "..");
  const setup = fs.readFileSync(path.join(source, "..", "setup.sh"), "utf8");
  const block = setup.match(/^SAFE_OUTPUTS_FILES=\(([\s\S]*?)^\)/m);
  assert.ok(block, "setup.sh must declare the safe-outputs bundle");
  const files = [...block[1].matchAll(/"([^"]+)"/g)].map(match => match[1]);
  assert.ok(files.length, "safe-outputs bundle must not be empty");
  fs.mkdirSync(destination);
  for (const filename of files) {
    assert.equal(path.basename(filename), filename, "bundle entries must be literal basenames");
    assert.ok(filename.endsWith(".cjs"), "bundle entries must be CommonJS modules");
    fs.copyFileSync(path.join(source, filename), path.join(destination, filename));
  }
  for (const [filename, target] of [
    ["safe-outputs-mcp-server.cjs", "mcp-server.cjs"],
    ["safe_outputs_tools.json", "safe_outputs_tools.json"],
    ["safe_outputs_tools.json", "tools.json"],
  ]) {
    fs.copyFileSync(path.join(source, filename), path.join(destination, target));
  }
  return files;
}

module.exports = { copySafeOutputsBundle };
