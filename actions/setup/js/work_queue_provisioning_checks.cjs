"use strict";

const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const { Readable } = require("node:stream");
const { randomUUID } = require("node:crypto");
const path = require("node:path");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { buildAWPolicy } = require("./work_queue_settings.cjs");
const { MAX_WORKFLOW_BYTES, verifyWorkerRoute, verifyWorkerRoutes, readLockFileHeaders, workerContract } = require("./work_queue_provisioning.cjs");

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
      for (const [, call] of native.calls) {
        assert.equal(call.request.retries, 0);
        assert.equal(call.request.retryCount, 0);
        assert.ok(call.request.signal instanceof AbortSignal);
        assert.equal(call.request.signal.aborted, false);
      }
      assert.deepEqual(native.calls, [
        ["contents", { owner: "owner", repo: "repo", path: profile.workflow, ref: profile.ref, request: native.calls[0][1].request }],
        ["registration", { owner: "owner", repo: "repo", workflow_id: "worker.lock.yml", request: native.calls[1][1].request }],
      ]);
    });
    it("verifies route metadata without parsing workflow YAML", async () => {
      for (const contents of [validYAML, "this is deliberately not workflow YAML", "on: [\n"]) await verifyWorkerRoute(options(nativeRoute(contents)));
    });
    it("reads lock metadata headers through a bounded reader", async () => {
      const metadata = { schema_version: "v1" };
      const manifest = { work_queue: { worker: true, logical_contract: "a".repeat(64) } };
      const reader = Readable.from([`# gh-aw-metadata: ${JSON.stringify(metadata)}\n`, `# gh-aw-manifest: ${JSON.stringify(manifest)}\n`, "on: [\n", "# gh-aw-manifest: invalid\n"]);
      const headers = await readLockFileHeaders(reader);
      assert.equal(headers.metadata?.schema_version, metadata.schema_version);
      assert.equal(workerContract(headers.manifest), manifest.work_queue.logical_contract);
      assert.equal(reader.destroyed, true);
      const lateManifest = Readable.from(["comment\n", "on: [\n", "jobs:\n", `# gh-aw-manifest: ${JSON.stringify(manifest)}\n`]);
      const boundedHeaders = await readLockFileHeaders(lateManifest);
      assert.equal(workerContract(boundedHeaders.manifest), undefined);
      assert.equal(lateManifest.destroyed, true);
    });
    it("reads only valid worker metadata from the compiler manifest", () => {
      const contract = "a".repeat(64);
      assert.equal(workerContract({ work_queue: { worker: true, logical_contract: contract } }), contract);
      assert.equal(workerContract({ work_queue: { worker: false, logical_contract: contract } }), undefined);
      assert.equal(workerContract({ work_queue: { worker: true } }), undefined);
      assert.equal(workerContract(undefined), undefined);
      assert.equal(workerContract({ work_queue: { worker: true, logical_contract: "invalid" } }), undefined);
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
    it("does not accept invalid UTF-8 in the manifest header as worker metadata", async () => {
      const bytes = Buffer.concat([Buffer.from("# gh-aw-manifest: "), Buffer.from([0xc0, 0xaf])]);
      const headers = await readLockFileHeaders(Readable.from([bytes]));
      assert.equal(workerContract(headers.manifest), undefined);
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
          else await assert.rejects(verifyWorkerRoute({ ...options(native), sleepFn: async () => {} }), error => error === native.state[endpoint]);
          assert.equal(native.calls.length, (endpoint === "contentError" ? 0 : 1) + (status === 500 ? 3 : 1));
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
    it("verifies worker routes without loading a YAML parser", () => {
      const script = `
        const assert = require("node:assert/strict");
        const Module = require("node:module");
        const load = Module._load;
        Module._load = function(name, ...args) {
          if (name === "./work_queue_yaml.cjs") throw new Error("work queue provisioning must not load a YAML parser");
          return load.call(this, name, ...args);
        };
        const { verifyWorkerRoute } = require(process.argv[1]);
        const profile = JSON.parse(process.argv[2]);
        const bytes = Buffer.from(process.argv[3], "utf8");
        let registrations = 0;
        const githubClient = {rest: {
          repos: {getContent: async () => ({data: {type: "file", path: profile.workflow,
            encoding: "base64", content: bytes.toString("base64"), size: bytes.length}})},
          actions: {getWorkflow: async () => { registrations++; return {data: {path: profile.workflow, state: "active"}}; }}
        }};
        verifyWorkerRoute({githubClient, owner: "owner", repo: "repo", profile}).then(
          () => assert.equal(registrations, 1),
          error => { console.error(error); process.exitCode = 1; }
        ).catch(error => { console.error(error); process.exitCode = 1; });
      `;
      const result = spawnSync(process.execPath, ["-e", script, require.resolve("./work_queue_provisioning.cjs"), JSON.stringify(profile), validYAML], { encoding: "utf8" });
      assert.equal(result.status, 0, result.stderr);
    });
    it("provisions from copied runtime scripts with all npm package imports denied", () => {
      const directory = path.resolve(`.queue-route-runtime-${randomUUID()}`);
      fs.mkdirSync(directory);
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
        assert.ok(!copied.has("work_queue_yaml.cjs"));
        assert.ok(copied.has("work_queue_host.cjs"));
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
          const { verifyWorkerRoute, verifiedDefaultReference, configuredWorkerDeployment } = require(process.argv[1]);
          const profile = {...JSON.parse(process.argv[2]), logical_contract: "a".repeat(64)};
          const bytes = Buffer.from('# gh-aw-manifest: ' + JSON.stringify({work_queue: {worker: true, logical_contract: profile.logical_contract}}) + '\\n', "utf8");
          let reads = 0;
          const githubClient = {rest: {
            repos: {
              get: async () => ({data: {full_name: "owner/repo", default_branch: "main"}}),
              getContent: async ({path, ref}) => {
              assert.equal(path, profile.workflow);
              assert.equal(ref, profile.ref);
              reads++;
              const content = bytes;
              return {data: {type: "file", path, encoding: "base64",
                content: content.toString("base64"), size: content.length}};
            }},
            git: {getRef: async () => ({data: {ref: "refs/heads/main", object: {sha: profile.ref}}})},
            actions: {getWorkflow: async () => {
              reads++;
              return {data: {path: profile.workflow, state: "active"}};
            }}
          }};
          const options = {githubClient, owner: "owner", repo: "repo", profile};
          verifyWorkerRoute(options)
            .then(async () => {
              const revision = await verifiedDefaultReference(options);
              assert.equal(revision, profile.ref);
              const route = await configuredWorkerDeployment(options, revision);
              assert.deepEqual(route, {profile, available: true});
              assert.equal(reads, 4);
            })
            .catch(error => { console.error(error); process.exitCode = 1; });
        `;
        const result = spawnSync(process.execPath, ["-e", script, path.join(directory, "work_queue_provisioning.cjs"), JSON.stringify(profile)], { encoding: "utf8", env: { ...process.env, NODE_PATH: "" } });
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
