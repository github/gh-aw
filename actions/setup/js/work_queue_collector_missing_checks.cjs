"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");

function registerTests({ describe, it }) {
  describe("ordinary missing-output preservation", () => {
    it("does not discover stale patches when ordinary safe output is missing or empty", async () => {
      const root = path.resolve(".queue-validation-cache", `queue-collector-${randomUUID()}`);
      const keys = ["RUNNER_TEMP", "GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_VALIDATION_CONFIG_PATH", "GH_AW_WORK_QUEUE_ENABLED"];
      const previous = keys.map(key => process.env[key]);
      const constantsPath = require.resolve("./constants.cjs");
      const constants = require(constantsPath);
      const constantsModule = require.cache[constantsPath];
      assert.ok(constantsModule);
      const collectorPath = require.resolve("./collect_ndjson_output.cjs");
      const cachedCollector = require.cache[collectorPath];
      const previousCore = global.core;
      const previousContext = global.context;
      const exists = fs.existsSync;
      try {
        fs.mkdirSync(root, { recursive: true });
        constantsModule.exports = { ...constants, TMP_GH_AW_PATH: root };
        delete require.cache[collectorPath];
        process.env.RUNNER_TEMP = root;
        process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = path.join(root, "no-config.json");
        process.env.GH_AW_VALIDATION_CONFIG_PATH = path.join(root, "no-validation.json");
        delete process.env.GH_AW_WORK_QUEUE_ENABLED;
        global.context = { repo: { owner: "owner", repo: "repo" }, payload: { repository: { full_name: "owner/repo" } } };
        fs.existsSync = filename => {
          if (filename === "/tmp/gh-aw") throw new Error("Missing ordinary outputs must not examine the legacy patch directory");
          return exists(filename);
        };
        const { main } = require(collectorPath);
        for (const present of [false, true]) {
          const outputs = new Map();
          global.core = new Proxy({ setOutput: (key, value) => outputs.set(key, value) }, { get: (target, name) => target[name] || (() => {}) });
          const filename = path.join(root, "source.jsonl");
          process.env.GH_AW_SAFE_OUTPUTS = filename;
          if (present) fs.writeFileSync(filename, "");
          await main();
          assert.equal(outputs.get("has_patch"), "false");
          assert.equal(outputs.get("raw_output"), "");
          assert.equal(JSON.parse(outputs.get("output")).items[0].type, "report_incomplete");
        }
      } finally {
        fs.existsSync = exists;
        constantsModule.exports = constants;
        if (cachedCollector) require.cache[collectorPath] = cachedCollector;
        else delete require.cache[collectorPath];
        global.core = previousCore;
        global.context = previousContext;
        keys.forEach((key, index) => (previous[index] === undefined ? delete process.env[key] : (process.env[key] = previous[index])));
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
