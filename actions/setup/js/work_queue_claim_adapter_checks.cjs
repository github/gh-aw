"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { createClaimAdapterHandler, preparedAdapterPath, verifyClaimAdapterOutput } = require("./work_queue_claim_adapters.cjs");
const { prepareAdapterContext } = require("./work_queue_prepare_claim_adapter.cjs");
const { verifyClaimDelivery } = require("./work_queue_delivery.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { claimArtifactPath } = require("./work_queue_claim_scope.cjs");
const { createHash } = require("node:crypto");
const { main: createAssetHandler, verifyAssetDelivery } = require("./work_queue_upload_assets.cjs");
const { validateAdapter } = require("./work_queue_claim_adapters.cjs");

function assignment() {
  return {
    version: 3,
    dispatch_id: "dispatch",
    request_id: "request",
    commit_id: "commit",
    policy_epoch: "epoch",
    pool: "default",
    worker_profile: "default",
    claims: ["h1", "h2"].map(handle => ({
      handle,
      claim_id: `claim:${handle}`,
      work_id: `work:${handle}`,
      work: { effect_contract: { version: 1, outputs: [{ type: "custom", min: 1, max: 1 }] } },
      result_refs: [],
    })),
  };
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
        request: async () => {
          throw new Error("Preview cannot invoke a native effect");
        },
      };
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
          const rest = createRestEffectHandler(
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
          await assert.rejects(rest({ type: "custom", claim_handle: "h2" }), /cannot escape/);
          const client = wrapClaimEffectClient(github, { claim_handle: "h1", authorize });
          await assert.rejects(client.request("POST /repos/owner/repo/check-runs", { name: "forbidden-preview" }), /read-only Claim preview/);
        });
      } finally {
        previous === undefined ? delete process.env.GH_AW_SAFE_OUTPUTS_STAGED : (process.env.GH_AW_SAFE_OUTPUTS_STAGED = previous);
      }
    });

    it("supports declared custom REST effects with complete independent native field verification, not job success", async () => {
      const root = path.resolve(".queue-validation-cache", `claim-rest-${randomUUID()}`);
      const configured = {
        mode: "prepared",
        "effect-type": "github_rest",
        "target-repo": "owner/repo",
        "field-map": { name: "check_name", head_sha: "revision" },
        expected: { status: "completed", conclusion: "success" },
        request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
        verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", fields: { name: "name", head_sha: "head_sha", status: "status", conclusion: "conclusion" } },
      };
      const scoped = assignment();
      const effects = [];
      const message = { type: "custom", claim_handle: "h1", check_name: "claimed-check", revision: "a".repeat(40) };
      let observed;
      let writes = 0;
      let cancelled = false;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: !cancelled && (request.message.repo === undefined || request.message.repo === "owner/repo") });
      const source = {
        request: async (route, fields) => {
          if (route.startsWith("POST ")) {
            writes++;
            assert.equal(route, "POST /repos/owner/repo/check-runs");
            assert.deepEqual(fields, { name: "claimed-check", head_sha: "a".repeat(40), status: "completed", conclusion: "success" });
            observed = { id: 91, name: fields.name, head_sha: fields.head_sha, status: fields.status, conclusion: fields.conclusion };
            return { data: structuredClone(observed) };
          }
          assert.equal(route, "GET /repos/owner/repo/check-runs/91");
          assert.equal(fields, undefined);
          return { data: observed };
        },
      };
      try {
        await withClaimExecution({ assignment: scoped, claim_handle: "h1", authorize, effects }, async () => {
          const filename = preparedAdapterPath(root, "h1", "custom");
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type: "custom", messages: [{ input: message, payload: { check_name: message.check_name, revision: message.revision } }] }));
          const handler = await createClaimAdapterHandler({ adapter: configured, filename, github: source });
          const originalRoute = configured.request.route;
          configured.request.route = "/repos/{owner}/{repo}/issues";
          const result = await handler(message);
          configured.request.route = originalRoute;
          const verification = {
            assignment: scoped,
            claim_handle: "h1",
            authorize,
            effects,
            messages: [message],
            results: [{ messageIndex: 0, success: true, result }],
            github: source,
            verifyOutput: input => verifyClaimAdapterOutput({ ...input, adapter: configured }),
          };
          assert.equal((await verifyClaimDelivery(verification)).verification, "verified");
          assert.equal((await verifyClaimAdapterOutput({ claim: scoped.claims[0], result: structuredClone(result), adapter: configured, github: source })).verified, false);
          observed.conclusion = "failure";
          assert.equal((await verifyClaimDelivery(verification)).verification, "unknown");
          assert.equal(writes, 1);
          cancelled = true;
          await assert.rejects(handler(message), /same-Claim/);
          assert.equal(writes, 1);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
      /** @type {Record<string, any>} */
      const incomplete = structuredClone(configured);
      delete incomplete.verifier.fields.head_sha;
      assert.throws(() => validateAdapter(incomplete), /Every declared REST effect field/);
      const override = structuredClone(configured);
      override["field-map"].headers = "credentials";
      assert.throws(() => validateAdapter(override), /reserved effect fields/);
      for (const field of ["data", "auth", "mediaType", "__proto__", "constructor", "prototype"]) {
        const malformed = structuredClone(configured);
        Object.defineProperty(malformed["field-map"], field, { value: "value", enumerable: true });
        assert.throws(() => validateAdapter(malformed), /reserved effect fields/);
      }
      const foreign = structuredClone(configured);
      foreign.verifier.route = "https://foreign.example/91";
      assert.throws(() => validateAdapter(foreign), /repository-relative/);
    });

    it("prepares only original scoped inputs without Completion or shrinking immutable assignment", async () => {
      const root = path.resolve(".queue-validation-cache", `claim-adapter-${randomUUID()}`);
      try {
        const messages = [
          { type: "custom", claim_handle: "h1", number: 42, content: "one" },
          { type: "custom", claim_handle: "h2", number: 43, content: "two" },
          { type: "custom", number: 99, content: "ambiguous" },
        ];
        const requests = [];
        const authorize = async request => {
          requests.push(request);
          assert.equal(request.requireCompletion, false);
          return { claim_handle: request.claim_handle, authorized: true };
        };
        const first = await prepareAdapterContext({ assignment: assignment(), adapter, index: 0, type: "custom", messages, authorize, artifactRoot: root });
        const second = await prepareAdapterContext({ assignment: assignment(), adapter, index: 1, type: "custom", messages, authorize, artifactRoot: root });
        assert.deepEqual(first.messages, [messages[0]]);
        assert.deepEqual(second.messages, [messages[1]]);
        assert.notEqual(first.directory, second.directory);
        assert.equal(JSON.parse(fs.readFileSync(first.assignmentFile, "utf8")).claims.length, 2);
        assert.equal((await prepareAdapterContext({ assignment: assignment(), adapter, index: 2, type: "custom", messages, authorize, artifactRoot: root })).active, false);
        assert.equal(requests.length, 2);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("applies prepared data only through fresh guarded effects and never accepts job success or uploaded receipts", async () => {
      const root = path.resolve(".queue-validation-cache", `claim-adapter-${randomUUID()}`);
      const scoped = assignment();
      const effects = [];
      const message = { type: "custom", claim_handle: "h1", number: 42, content: "original" };
      let writes = 0;
      let observed = "prepared";
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === undefined || request.message.repo === "owner/repo" });
      const source = {
        rest: {
          issues: {
            update: async args => {
              writes++;
              return { data: { id: 7, number: args.issue_number } };
            },
            get: async () => ({ data: { id: 7, number: 42, body: observed, html_url: "https://github.com/owner/repo/issues/42" } }),
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
          const result = await handler(message);
          assert.equal(writes, 1);
          const verification = {
            assignment: scoped,
            claim_handle: "h1",
            authorize,
            effects,
            messages: [message],
            results: [{ messageIndex: 0, success: true, claim_handle: "h1", result }],
            verifyOutput: input => verifyClaimAdapterOutput({ ...input, adapter, effects }),
            github: source,
          };
          assert.equal((await verifyClaimDelivery(verification)).verification, "verified");
          const forged = structuredClone(result);
          assert.equal((await verifyClaimAdapterOutput({ claim: scoped.claims[0], message, result: forged, adapter, github: source, effects })).verified, false);
          observed = "prepared with incorrect extra content";
          assert.equal((await verifyClaimDelivery(verification)).verification, "unknown");
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("rejects foreign/malformed prepared artifacts before effects while valid sibling adapters can settle", async () => {
      const root = path.resolve(".queue-validation-cache", `claim-adapter-${randomUUID()}`);
      const scoped = assignment();
      let writes = 0;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
      try {
        for (const handle of ["h1", "h2"]) {
          const input = { type: "custom", claim_handle: handle, number: 42, content: "body" };
          const filename = preparedAdapterPath(root, handle, "custom", scoped);
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: handle === "h1" ? "h2" : handle, type: "custom", messages: [{ input, payload: { number: 42, content: "body" } }] }));
          await withClaimExecution({ assignment: scoped, claim_handle: handle, authorize }, async () => {
            const handler = await createClaimAdapterHandler({
              adapter,
              filename,
              loadEffectHandler: async () => async () => {
                writes++;
                return { success: true };
              },
            });

            if (handle === "h1") await assert.rejects(handler(input), /foreign immutable attribution/);
            else assert.equal((await handler(input)).success, true);
          });
        }
        assert.equal(writes, 1);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
    it("publishes isolated assets with exact immutable git evidence and never trusts copied receipts", async () => {
      const root = path.resolve(".queue-validation-cache", `claim-assets-${randomUUID()}`);
      const scoped = assignment();
      const sourcePath = "image.png";
      const content = Buffer.from("claim-only-asset");
      const sha = createHash("sha256").update(content).digest("hex");
      const effects = [];
      const calls = [];
      let ref, file;
      let incorrect = false;
      const source = {
        rest: {
          git: {
            getRef: async () => {
              if (!ref) throw Object.assign(new Error("missing"), { status: 404 });
              return { data: { object: { sha: ref } } };
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
              assert.match(args.ref, /^refs\/heads\/assets\/trusted\/claims\//);
              ref = args.sha;
              return { data: { object: { sha: ref } } };
            },
            getCommit: async () => ({ data: { sha: "commit-sha", tree: { sha: "tree-sha" }, parents: [] } }),
            getTree: async () => ({ data: { sha: "tree-sha", truncated: false, tree: [file] } }),
            getBlob: async () => ({ data: { sha: "blob-sha", encoding: "base64", content: (incorrect ? Buffer.from("incorrect") : content).toString("base64") } }),
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
        await withClaimExecution({ assignment: scoped, claim_handle: "h1", authorize, effects }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
          const handler = await createAssetHandler({ "target-repo": "owner/repo", branch: "assets/trusted", "assets-dir": root }, client);
          const message = { type: "upload_asset", claim_handle: "h1", path: sourcePath, sha };
          const result = await handler(message);
          assert.equal(result.sha, sha);
          assert.deepEqual(calls, ["blob", "tree", "commit", "ref"]);
          assert.equal((await verifyAssetDelivery({ claim: scoped.claims[0], result: structuredClone(result), github: source })).verified, false);
          const options = { assignment: scoped, claim_handle: "h1", messages: [message], results: [{ messageIndex: 0, success: true, result }], effects, github: source, authorize, verifyOutput: verifyAssetDelivery };
          assert.equal((await verifyClaimDelivery(options)).verification, "verified");
          incorrect = true;
          assert.equal((await verifyClaimDelivery(options)).verification, "unknown");
          await assert.rejects(handler({ ...message, claim_handle: "h2" }), /cannot escape/);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
