"use strict";

const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { buildAWPolicy } = require("./work_queue_settings.cjs");
const { MAX_WORKFLOW_BYTES, verifyWorkerRoute, verifyWorkerRoutes } = require("./work_queue_provisioning.cjs");

const validYAML = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n";
const profile = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: "a".repeat(40) }).pools.default.profiles.default;

/** @param {string} content @param {{contentError?: Error, registrationError?: Error}} failures */
function nativeRoute(content = validYAML, failures = {}) {
  const calls = [];
  const bytes = Buffer.from(content, "utf8");
  const state = {
    file: { type: "file", path: profile.workflow, encoding: "base64", content: bytes.toString("base64"), size: bytes.length },
    registration: { path: profile.workflow, state: "active" },
    contentError: failures.contentError ?? null,
    registrationError: failures.registrationError ?? null,
  };
  const githubClient = {
    rest: {
      repos: {
        getContent: async options => {
          calls.push(["contents", options]);
          if (state.contentError) throw state.contentError;
          return { data: state.file };
        },
      },
      actions: {
        getWorkflow: async options => {
          calls.push(["registration", options]);
          if (state.registrationError) throw state.registrationError;
          return { data: state.registration };
        },
      },
    },
  };
  return { githubClient, state, calls };
}

function options(native, worker = profile) {
  return { githubClient: native.githubClient, owner: "owner", repo: "repo", profile: worker };
}

async function rejected(native, worker = profile) {
  await assert.rejects(verifyWorkerRoute(options(native, worker)), error => error instanceof Error && "code" in error && error.code === "policy_missing");
}

function registerTests({ describe, it }) {
  describe("authenticated immutable worker route provisioning", () => {
    it("reads exact approved immutable Contents and exact active native registration", async () => {
      const native = nativeRoute();
      await verifyWorkerRoute(options(native));
      assert.deepEqual(native.calls, [
        ["contents", { owner: "owner", repo: "repo", path: profile.workflow, ref: profile.ref }],
        ["registration", { owner: "owner", repo: "repo", workflow_id: "worker.lock.yml" }],
      ]);
    });
    it("accepts optional string inputs and genuine quoted, flow, alias and merge YAML", async () => {
      for (const contents of [
        validYAML,
        validYAML + "        required: false\n",
        validYAML + "        required: true\n",
        '"on": {workflow_dispatch: {inputs: {work_queue_assignment: {type: "string"}}}}\n',
        "assignment: &assignment {type: string}\non: {workflow_dispatch: {inputs: {work_queue_assignment: *assignment}}}\n",
        "assignment: &assignment {type: string}\non: {workflow_dispatch: {inputs: {work_queue_assignment: {<<: *assignment, required: false}}}}\n",
        validYAML + "name: 'worker 🦄'\njobs:\n  worker:\n    steps:\n      - run: |\n          echo '${{ inputs.work_queue_assignment }}'\n",
      ])
        await verifyWorkerRoute(options(nativeRoute(contents)));
    });
    it("does not mistake comments, block strings or foreign inputs for a dispatch assignment", async () => {
      for (const contents of [
        "# " + validYAML.replaceAll("\n", "\n# "),
        "on: push\nexample: |\n  " + validYAML.replaceAll("\n", "\n  "),
        "on: {workflow_dispatch: {inputs: {foreign: {type: string}}}}\n",
        "on: {workflow_dispatch: {inputs: {work_queue_assignment: {type: boolean}}}}\n",
        "on: {workflow_dispatch: {inputs: {work_queue_assignment: {required: true}}}}\n",
      ])
        await rejected(nativeRoute(contents));
    });
    it("rejects malformed YAML, duplicate keys, extra documents and recursive aliases", async () => {
      for (const contents of [validYAML + "broken: [\n", validYAML + "on: {workflow_dispatch: {inputs: {work_queue_assignment: {type: string}}}}\n", validYAML + "---\n" + validYAML, "on: &cycle {workflow_dispatch: *cycle}\n"])
        await rejected(nativeRoute(contents));
    });
    it("rejects constructor revisions without a native read", async () => {
      for (const ref of ["0".repeat(40), "0".repeat(64)]) {
        const native = nativeRoute();
        await rejected(native, { ...profile, ref });
        assert.deepEqual(native.calls, []);
      }
    });
    it("retains the approved 64-digit revision without branch or producer fallback", async () => {
      const native = nativeRoute();
      const worker = { ...profile, ref: "b".repeat(64) };
      await verifyWorkerRoute(options(native, worker));
      assert.equal(native.calls[0][1].ref, worker.ref);
    });
    it("denies malformed, foreign and non-file Contents responses", async () => {
      for (const change of [file => (file.type = "dir"), file => (file.path = ".github/workflows/foreign.lock.yml"), file => (file.encoding = "utf-8"), file => (file.content = "%%%"), file => (file.content = "AAAA A===")]) {
        const native = nativeRoute();
        change(native.state.file);
        await rejected(native);
        assert.equal(native.calls.length, 1);
      }
    });
    it("rejects invalid UTF-8 rather than decoding replacement characters", async () => {
      const native = nativeRoute();
      const bytes = Buffer.from([0xc0, 0xaf]);
      native.state.file.content = bytes.toString("base64");
      native.state.file.size = bytes.length;
      await rejected(native);
      assert.equal(native.calls.length, 1);
    });
    it("accepts CRLF-wrapped GitHub base64 content", async () => {
      const native = nativeRoute();
      native.state.file.content = native.state.file.content.replace(/(.{16})/g, "$1\r\n");
      await verifyWorkerRoute(options(native));
    });
    it("checks the exact 1 MiB decoded boundary and refuses one additional byte", async () => {
      const contents = validYAML + "#" + "x".repeat(MAX_WORKFLOW_BYTES - Buffer.byteLength(validYAML) - 1);
      await verifyWorkerRoute(options(nativeRoute(contents)));
      const oversized = nativeRoute(contents + "x");
      await rejected(oversized);
      assert.equal(oversized.calls.length, 1);
    });
    it("bounds actual decoded bytes independently of API size metadata, matching native Go", async () => {
      const native = nativeRoute();
      native.state.file.size = MAX_WORKFLOW_BYTES * 100;
      await verifyWorkerRoute(options(native));
      const oversized = nativeRoute(validYAML + "#".repeat(MAX_WORKFLOW_BYTES));
      oversized.state.file.size = 1;
      await rejected(oversized);
    });
    it("denies missing, inactive and foreign native registrations", async () => {
      for (const change of [registration => (registration.path = ".github/workflows/foreign.lock.yml"), registration => (registration.state = "disabled_manually"), registration => (registration.state = "")]) {
        const native = nativeRoute();
        change(native.state.registration);
        await rejected(native);
      }
    });
    it("fails closed on authenticated native read errors", async () => {
      for (const endpoint of ["contentError", "registrationError"])
        for (const status of [404, 401, 403, 429, 500]) {
          const native = nativeRoute();
          native.state[endpoint] = Object.assign(new Error("native read failed"), { status });
          if (status === 404) await rejected(native);
          else await assert.rejects(verifyWorkerRoute(options(native)), error => error === native.state[endpoint]);
          assert.equal(native.calls.length, endpoint === "contentError" ? 1 : 2);
        }
    });
    it("deduplicates identical routes within one fresh verification, not across calls", async () => {
      const native = nativeRoute();
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: profile.ref });
      policy.pools.default.profiles.second = { ...profile, principal: "1002" };
      policy.pools.second = structuredClone(policy.pools.default);
      const input = { githubClient: native.githubClient, owner: "owner", repo: "repo", policy };
      await verifyWorkerRoutes(input);
      assert.equal(native.calls.length, 2);
      await verifyWorkerRoutes(input);
      assert.equal(native.calls.length, 4);
    });
    it("validates shared AW registry without reading unrelated native routes", async () => {
      const native = nativeRoute();
      native.state.contentError = Object.assign(new Error("unused worker unavailable"), { status: 404 });
      const policy = buildAWPolicy({
        repository: "owner/repo",
        ref: profile.ref,
        workflows: ["worker", ...Array.from({ length: 30 }, (_, index) => `report-${index}`)],
        settings: { pools: { reports: { concurrency: 3 } } },
      });
      await verifyWorkerRoutes({ githubClient: native.githubClient, owner: "owner", repo: "repo", policy });
      assert.equal(native.calls.length, 0);
      await rejected(native, policy.pools.default.profiles.worker);
      assert.equal(native.calls.length, 1);
    });
    it("fails explicitly before registration when the production YAML parser is missing", () => {
      const script = `
        const assert = require("node:assert/strict");
        const Module = require("node:module");
        const load = Module._load;
        Module._load = function(name, ...args) {
          if (name === "./work_queue_yaml.cjs") throw Object.assign(new Error("parser absent"), {code: "MODULE_NOT_FOUND"});
          return load.call(this, name, ...args);
        };
        const { verifyWorkerRoute } = require(process.argv[1]);
        const profile = JSON.parse(process.argv[2]);
        const bytes = Buffer.from(process.argv[3], "utf8");
        let registrations = 0;
        const githubClient = {rest: {
          repos: {getContent: async () => ({data: {type: "file", path: profile.workflow,
            encoding: "base64", content: bytes.toString("base64"), size: bytes.length}})},
          actions: {getWorkflow: async () => { registrations++; throw new Error("unexpected registration"); }}
        }};
        verifyWorkerRoute({githubClient, owner: "owner", repo: "repo", profile}).then(
          () => { throw new Error("unexpected provisioning success"); },
          error => {
            assert.equal(error.code, "policy_missing");
            assert.match(error.message, /deployed YAML parser/);
            assert.equal(registrations, 0);
          }
        ).catch(error => { console.error(error); process.exitCode = 1; });
      `;
      const result = spawnSync(process.execPath, ["-e", script, require.resolve("./work_queue_provisioning.cjs"), JSON.stringify(profile), validYAML], { encoding: "utf8" });
      assert.equal(result.status, 0, result.stderr);
    });
    it("provisions from copied runtime scripts with all npm package imports denied", () => {
      const directory = fs.mkdtempSync(path.join(os.tmpdir(), "queue-route-runtime-"));
      try {
        const copied = new Set();
        function copy(name) {
          if (copied.has(name)) return;
          copied.add(name);
          assert.match(name, /^(?:work_queue_[a-z_]+|mcp_logger|error_helpers)\.cjs$/);
          const source = fs.readFileSync(path.join(__dirname, name), "utf8");
          fs.writeFileSync(path.join(directory, name), source);
          for (const match of source.matchAll(/require\(["']\.\/([a-z_]+\.cjs)["']\)/g)) copy(match[1]);
        }
        copy("work_queue_provisioning.cjs");
        assert.ok(copied.has("work_queue_yaml.cjs"));
        assert.ok(copied.has("work_queue_logging.cjs"));
        assert.ok(copied.has("mcp_logger.cjs"));
        const script = `
          const assert = require("node:assert/strict");
          const Module = require("node:module");
          const load = Module._load;
          Module._load = function(name, ...args) {
            if (!name.startsWith("node:") && !name.startsWith(".") && !name.startsWith("/"))
              throw new Error("runtime npm package forbidden: " + name);
            return load.call(this, name, ...args);
          };
          const { verifyWorkerRoute } = require(process.argv[1]);
          const profile = JSON.parse(process.argv[2]);
          const bytes = Buffer.from(process.argv[3], "utf8");
          let reads = 0;
          const githubClient = {rest: {
            repos: {getContent: async ({path, ref}) => {
              assert.equal(path, profile.workflow);
              assert.equal(ref, profile.ref);
              reads++;
              return {data: {type: "file", path, encoding: "base64",
                content: bytes.toString("base64"), size: bytes.length}};
            }},
            actions: {getWorkflow: async () => {
              reads++;
              return {data: {path: profile.workflow, state: "active"}};
            }}
          }};
          verifyWorkerRoute({githubClient, owner: "owner", repo: "repo", profile})
            .then(() => { assert.equal(reads, 2); })
            .catch(error => { console.error(error); process.exitCode = 1; });
        `;
        const result = spawnSync(process.execPath, ["-e", script, path.join(directory, "work_queue_provisioning.cjs"), JSON.stringify(profile), validYAML], { encoding: "utf8", env: { ...process.env, NODE_PATH: "" } });
        assert.equal(result.status, 0, result.stderr);
        assert.equal(fs.existsSync(path.join(directory, "node_modules")), false);
      } finally {
        fs.rmSync(directory, { recursive: true, force: true });
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
