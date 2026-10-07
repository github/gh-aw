"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { temporaryDirectory } = require("./work_queue_effect_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("ordinary missing-output preservation", () => {
    it("preserves legacy patch discovery when ordinary safe output is missing or empty", async () => {
      const root = temporaryDirectory("queue-collector");
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
      const readdir = fs.readdirSync;
      try {
        fs.mkdirSync(root, { recursive: true });
        constantsModule.exports = { ...constants, TMP_GH_AW_PATH: root };
        delete require.cache[collectorPath];
        process.env.RUNNER_TEMP = root;
        process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = path.join(root, "no-config.json");
        process.env.GH_AW_VALIDATION_CONFIG_PATH = path.join(root, "no-validation.json");
        delete process.env.GH_AW_WORK_QUEUE_ENABLED;
        global.context = {
          ...previousContext,
          repo: { owner: "owner", repo: "repo" },
          payload: { repository: { full_name: "owner/repo", name: "repo", owner: { login: "owner" } } },
          /** @returns {{owner: string, repo: string, number: number}} */
          get issue() {
            throw new Error("Missing-output fixture has no trigger issue");
          },
        };
        fs.existsSync = filename => {
          if (filename === "/tmp/gh-aw") return true;
          return exists(filename);
        };
        fs.readdirSync = new Proxy(readdir, {
          apply: (target, receiver, args) => (args[0] === "/tmp/gh-aw" ? ["aw-existing.patch"] : Reflect.apply(target, receiver, args)),
        });
        const { main } = require(collectorPath);
        for (const present of [false, true]) {
          const outputs = new Map();
          global.core = new Proxy({ ...previousCore, setOutput: (key, value) => outputs.set(key, value) }, { get: (target, name) => target[name] || (() => {}) });
          const filename = path.join(root, "source.jsonl");
          process.env.GH_AW_SAFE_OUTPUTS = filename;
          if (present) fs.writeFileSync(filename, "");
          await main();
          assert.equal(outputs.get("has_patch"), "true");
          assert.equal(outputs.get("raw_output"), "");
          assert.equal(JSON.parse(outputs.get("output")).items[0].type, "report_incomplete");
        }
      } finally {
        fs.existsSync = exists;
        fs.readdirSync = readdir;
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
