"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { createClaimAdapterHandler, preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");
const { prepareAdapterContext } = require("./work_queue_prepare_claim_adapter.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { claimArtifactPath } = require("./work_queue_claim_scope.cjs");
const { createHash } = require("node:crypto");
const { main: createAssetHandler } = require("./work_queue_upload_assets.cjs");
const { validateAdapter } = require("./work_queue_claim_adapters.cjs");

function assignment(dispatch = "dispatch") {
  return {
    dispatch_id: dispatch,
    claims: [
      {
        handle: "h1",
        claim_id: `claim:${dispatch}`,
        work_id: `work:${dispatch}`,
        work: { effect_contract: { version: 1, outputs: [{ type: "custom", min: 1, max: 1 }] } },
        result_refs: [],
      },
    ],
  };
}

function twoMemberAssignment() {
  const scoped = assignment();
  scoped.claims.push({ ...structuredClone(scoped.claims[0]), handle: "h2", claim_id: "claim:h2", work_id: "work:h2" });
  return scoped;
}

const adapter = { mode: "prepared", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": { body: "content", item_number: "number" } };

function registerTests({ describe, it }) {
  describe("trusted per-Claim custom preparation and independent delivery adapters", () => {
    it("keeps custom and direct standalone adapters read-only in preview without Completion or handler loading", async () => {
      const previous = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      const authorize = async request => {
        assert.equal(request.requireCompletion, false);
        return { claim_handle: request.claim_handle, authorized: true };
      };
      const github = {
        request: async (_route, _parameters = {}) => {
          throw new Error("Preview cannot invoke a native effect");
        },
      };
      let rest;
      try {
        await withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
          const handler = await createClaimAdapterHandler({
            adapter,
            filename: "/nonexistent-preparation.json",
            loadEffectHandler: async () => {
              throw new Error("Preview cannot load an effect handler");
            },
          });
          assert.equal((await handler({ type: "custom", claim_handle: "h1" })).staged, true);
          const { createRestEffectHandler } = require("./work_queue_rest_adapter.cjs");
          rest = createRestEffectHandler(
            {
              mode: "prepared",
              "effect-type": "github_rest",
              "target-repo": "owner/repo",
              "field-map": { name: "name" },
              request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
              verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", fields: { name: "name" } },
            },
            github
          );
          assert.equal((await rest({ type: "custom", claim_handle: "h1" })).staged, true);
          const { createGitTreeEffectHandler } = require("./work_queue_git_tree_adapter.cjs");
          const code = createGitTreeEffectHandler(
            {
              mode: "prepared",
              "effect-type": "git_tree",
              "target-repo": "owner/repo",
              "field-map": { files: "files" },
              "git-tree": { "base-revision": "a".repeat(40), "branch-prefix": "automation/code" },
            },
            github
          );
          const preview = await code({ type: "custom", claim_handle: "h1" });
          assert.ok("staged" in preview);
          assert.equal(preview.staged, true);
          await assert.rejects(rest({ type: "custom", claim_handle: "h2" }), /foreign/);
          const client = wrapClaimEffectClient(github, { claim_handle: "h1", authorize });
          await assert.rejects(client.request("POST /repos/owner/repo/check-runs", { name: "forbidden-preview" }), /read-only Claim preview/);
        });
        await withClaimExecution({ assignment: assignment("next-dispatch"), claim_handle: "h1", authorize }, async () => {
          await assert.rejects(rest({ type: "custom", claim_handle: "h1" }), /cannot escape its original immutable Claim identity/);
        });
      } finally {
        previous === undefined ? delete process.env.GH_AW_SAFE_OUTPUTS_STAGED : (process.env.GH_AW_SAFE_OUTPUTS_STAGED = previous);
      }
    });

    it("executes custom REST effects once through the guarded client without readback", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-rest");
      const configured = {
        mode: "prepared",
        "effect-type": "github_rest",
        "target-repo": "owner/repo",
        "field-map": { name: "check_name", head_sha: "revision" },
        expected: { status: "completed", conclusion: "success" },
        request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
      };
      const scoped = assignment();
      const message = { type: "custom", claim_handle: "h1", check_name: "claimed-check", revision: "a".repeat(40) };
      let writes = 0;
      let reads = 0;
      let cancelled = false;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: !cancelled && (request.message.repo === undefined || request.message.repo === "owner/repo") });
      const source = {
        rest: { repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) } },
        request: async (route, fields) => {
          if (!route.startsWith("POST ")) {
            reads++;
            return { data: {} };
          }
          writes++;
          assert.equal(route, "POST /repos/owner/repo/check-runs");
          assert.deepEqual(fields, { name: "claimed-check", head_sha: "a".repeat(40), status: "completed", conclusion: "success" });
          return { data: { id: 91 } };
        },
      };
      try {
        await withClaimExecution({ assignment: scoped, claim_handle: "h1", authorize, effects: [] }, async () => {
          const filename = preparedAdapterPath(root, "h1", "custom");
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type: "custom", messages: [{ input: message, payload: { check_name: message.check_name, revision: message.revision } }] }));
          const handler = await createClaimAdapterHandler({ adapter: configured, filename, github: source });
          const originalRoute = configured.request.route;
          configured.request.route = "/repos/{owner}/{repo}/issues";
          const result = await handler(message);
          configured.request.route = originalRoute;
          assert.deepEqual(result, { success: true, repo: "owner/repo", id: "91" });
          assert.equal(writes, 1);
          assert.equal(reads, 0);
          cancelled = true;
          await assert.rejects(handler(message), /not open/);
          assert.equal(writes, 1);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
      const override = structuredClone(configured);
      override["field-map"].headers = "credentials";
      assert.throws(() => validateAdapter(override), /reserved effect fields/);
      for (const field of ["data", "auth", "mediaType", "__proto__", "constructor", "prototype"]) {
        const malformed = structuredClone(configured);
        Object.defineProperty(malformed["field-map"], field, { value: "value", enumerable: true });
        assert.throws(() => validateAdapter(malformed), /reserved effect fields/);
      }
      const foreign = structuredClone(configured);
      foreign.request.route = "https://foreign.example/91";
      assert.throws(() => validateAdapter(foreign), /repository-relative/);
      const unbound = structuredClone(configured);
      unbound.request.route = "/repos/{owner}/{repo}/check-runs/{check_id}";
      assert.throws(() => validateAdapter(unbound), /unbound selector/);
      const legacy = structuredClone(configured);
      legacy.verifier = { anything: true };
      legacy["verifier-id"] = "ignored";
      assert.equal(validateAdapter(legacy), legacy);
      delete legacy.verifier;
      assert.equal(validateAdapter(legacy), legacy);
    });

    it("prepares only original scoped inputs without Completion or shrinking immutable assignment", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-adapter");
      try {
        const scoped = assignment();
        const messages = [
          { type: "custom", claim_handle: "h1", number: 42, content: "one" },
          { type: "custom", claim_handle: "h2", number: 43, content: "foreign" },
          { type: "custom", number: 44, content: "omitted" },
        ];
        const requests = [];
        const authorize = async request => {
          requests.push(request);
          assert.equal(request.requireCompletion, false);
          return { claim_handle: request.claim_handle, authorized: true };
        };
        const rejected = [];
        const rejectedAuthorize = async request => {
          rejected.push(request);
          return { claim_handle: request.claim_handle, authorized: true };
        };
        await assert.rejects(prepareAdapterContext({ assignment: twoMemberAssignment(), adapter, index: 0, type: "custom", messages, authorize: rejectedAuthorize, artifactRoot: root }), /exactly one open Claim/);
        assert.deepEqual(rejected, []);
        assert.deepEqual(fs.readdirSync(root), []);
        const prepared = await prepareAdapterContext({ assignment: scoped, adapter, index: 0, type: "custom", messages, authorize, artifactRoot: root });
        assert.deepEqual(prepared.messages, [messages[0], { ...messages[2], claim_handle: "h1" }]);
        assert.deepEqual(JSON.parse(fs.readFileSync(prepared.assignmentFile, "utf8")), scoped);
        assert.deepEqual(await prepareAdapterContext({ assignment: scoped, adapter, index: 1, type: "custom", messages, authorize, artifactRoot: root }), { active: false });
        const message = { type: "work_queue_adapter_prepare", claim_handle: "h1", repo: "owner/repo" };
        assert.deepEqual(
          requests.map(({ claim_handle, message, resource, requireCompletion }) => ({ claim_handle, message, resource, requireCompletion })),
          [
            { claim_handle: "h1", message, resource: undefined, requireCompletion: false },
            { claim_handle: "h1", message, resource: { repository: "owner/repo", host: require("./work_queue_host.cjs").expectedGitHubHost() }, requireCompletion: false },
          ]
        );
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("applies prepared data only through fresh guarded effects", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-adapter");
      const scoped = assignment();
      const effects = [];
      const message = { type: "custom", claim_handle: "h1", number: 42, content: "original" };
      let writes = 0;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === undefined || request.message.repo === "owner/repo" });
      const source = {
        rest: {
          repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
          issues: {
            update: async args => {
              writes++;
              return { data: { id: 7, number: args.issue_number } };
            },
          },
        },
      };
      try {
        await withClaimExecution({ assignment: scoped, claim_handle: "h1", authorize, effects }, async () => {
          const filename = preparedAdapterPath(root, "h1", "custom");
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type: "custom", messages: [{ input: message, payload: { number: 42, content: "prepared" } }] }));
          const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
          const handler = await createClaimAdapterHandler({
            adapter,
            filename,
            loadEffectHandler: async () => async projected => {
              assert.equal(projected.claim_handle, "h1");
              const response = await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: projected.item_number, body: projected.body });
              return { success: true, repo: "owner/repo", number: response.data.number };
            },
          });
          await handler(message);
          assert.equal(writes, 1);
          assert.ok(effects.length >= 1);
          assert.ok(effects.every(effect => effect.claim_handle === "h1"));
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("rejects foreign-dispatch and malformed prepared artifacts before effects while an independent Claim's adapter settles", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-adapter");
      // Two concurrent Dispatches reuse handle h1; each owns one valid singleton assignment.
      const own = assignment("dispatch");
      const other = assignment("other-dispatch");
      const input = { type: "custom", claim_handle: "h1", number: 42, content: "body" };
      const prepared = claimHandle => JSON.stringify({ version: 3, claim_handle: claimHandle, type: "custom", messages: [{ input, payload: { number: 42, content: "body" } }] });
      let writes = 0;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
      const loadEffectHandler = async () => async () => {
        writes++;
        return { success: true };
      };
      try {
        const ownFile = preparedAdapterPath(root, "h1", "custom", own);
        const otherFile = preparedAdapterPath(root, "h1", "custom", other);
        assert.notEqual(path.dirname(ownFile), path.dirname(otherFile));
        fs.mkdirSync(path.dirname(ownFile), { recursive: true });
        fs.mkdirSync(path.dirname(otherFile), { recursive: true });
        fs.writeFileSync(ownFile, prepared("h2"));
        fs.writeFileSync(otherFile, prepared("h1"));
        let ownHandler;
        await withClaimExecution({ assignment: own, claim_handle: "h1", authorize }, async () => {
          ownHandler = await createClaimAdapterHandler({ adapter, filename: ownFile, loadEffectHandler });
          await assert.rejects(ownHandler(input), /foreign immutable attribution/);
          const foreignArtifact = await createClaimAdapterHandler({ adapter, filename: otherFile, loadEffectHandler });
          await assert.rejects(foreignArtifact(input), /outside its original Claim namespace/);
        });
        await withClaimExecution({ assignment: other, claim_handle: "h1", authorize }, async () => {
          await assert.rejects(ownHandler(input), /cannot escape its original immutable Claim identity/);
          const handler = await createClaimAdapterHandler({ adapter, filename: otherFile, loadEffectHandler });
          assert.equal((await handler(input)).success, true);
        });
        assert.equal(writes, 1);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
    it("publishes isolated assets through Claim-namespaced git effects", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-assets");
      const scoped = assignment();
      const namespace = path.basename(claimArtifactPath("", "h1", scoped));
      const sourcePath = "image.png";
      const content = Buffer.from("claim-only-asset");
      const sha = createHash("sha256").update(content).digest("hex");
      const calls = [];
      let file, createdRef;
      const source = {
        rest: {
          repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
          git: {
            getRef: async (...parameters) => {
              const requested = parameters[0]?.ref;
              if (requested === "heads/assets/trusted") return { data: { object: { sha: "legacy-sha" } } };
              if (createdRef && requested === createdRef.replace(/^refs\//, "")) return { data: { object: { sha: "commit-sha" } } };
              throw Object.assign(new Error("missing"), { status: 404 });
            },
            createBlob: async args => {
              calls.push("blob");
              assert.equal(args.content, content.toString("base64"));
              return { data: { sha: "blob-sha" } };
            },
            createTree: async args => {
              calls.push("tree");
              file = args.tree[0];
              return { data: { sha: "tree-sha" } };
            },
            createCommit: async args => {
              calls.push("commit");
              assert.deepEqual(args.parents, []);
              return { data: { sha: "commit-sha" } };
            },
            createRef: async args => {
              calls.push("ref");
              assert.equal(args.ref, `refs/heads/assets/trusted-claims/${namespace}`);
              createdRef = args.ref;
              return { data: { object: { sha: args.sha } } };
            },
          },
        },
      };
      const authorize = async request => {
        if (request.resource?.path) assert.match(request.resource.path, /^claims\//);
        return { claim_handle: request.claim_handle, authorized: request.message.repo === undefined || request.message.repo === "owner/repo" };
      };
      try {
        const directory = claimArtifactPath(root, "h1", scoped);
        fs.mkdirSync(directory, { recursive: true });
        fs.writeFileSync(path.join(directory, createHash("sha256").update(sourcePath).digest("hex") + ".png"), content);
        scoped.claims[0].work.effect_contract.outputs[0].type = "upload_asset";
        let handler;
        await withClaimExecution({ assignment: scoped, claim_handle: "h1", authorize, effects: [] }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
          handler = await createAssetHandler({ "target-repo": "owner/repo", branch: "assets/trusted", "assets-dir": root }, client);
          assert.equal((await source.rest.git.getRef({ ref: "heads/assets/trusted" })).data.object.sha, "legacy-sha");
          const message = { type: "upload_asset", claim_handle: "h1", path: sourcePath, sha };
          const result = await handler(message);
          assert.equal(result.sha, sha);
          assert.deepEqual(calls, ["blob", "tree", "commit", "ref"]);
          assert.equal(createdRef, `refs/heads/assets/trusted-claims/${namespace}`);
          assert.equal(file.path, `claims/${namespace}/${sha}.png`);
          await assert.rejects(handler({ ...message, claim_handle: "h2" }), /foreign/);
        });
        const next = assignment("next-dispatch");
        next.claims[0].work.effect_contract.outputs[0].type = "upload_asset";
        await withClaimExecution({ assignment: next, claim_handle: "h1", authorize, effects: [] }, async () => {
          await assert.rejects(handler({ type: "upload_asset", claim_handle: "h1", path: sourcePath, sha }), /cannot escape its original immutable Claim identity/);
          assert.deepEqual(calls, ["blob", "tree", "commit", "ref"]);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
