"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { withClaimExecution, claimArtifactPath } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient, withClaimEffectClients } = require("./work_queue_effect_client.cjs");
const { assertGitPushAuthorized, gitPushRepository } = require("./work_queue_git_effects.cjs");
const { verifyClaimDelivery } = require("./work_queue_delivery.cjs");

const assignment = {
  version: 3,
  dispatch_id: "d",
  request_id: "r",
  commit_id: "commit",
  policy_epoch: "epoch",
  pool: "default",
  worker_profile: "default",
  claims: ["h1", "h2"].map(handle => ({ handle, claim_id: `c:${handle}`, work_id: `w:${handle}`, work: {}, result_refs: [] })),
};

function registerTests({ describe, it }) {
  describe("fresh per-request Claim effect boundaries", () => {
    it("rejects a client reused under another dispatch with the same local handle before any API call", async () => {
      let writes = 0;
      const source = {
        rest: {
          issues: {
            update: async () => {
              writes++;
            },
          },
        },
      };
      const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
      const first = structuredClone(assignment);
      const second = structuredClone(assignment);
      second.dispatch_id = "another-dispatch";
      second.claims[0].claim_id = "another-claim";
      let client;
      await withClaimExecution({ assignment: first, claim_handle: "h1", authorize }, () => {
        client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
      });
      await withClaimExecution({ assignment: second, claim_handle: "h1", authorize }, async () => {
        await assert.rejects(async () => client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 1 }), /original immutable Claim identity/);
      });
      assert.equal(writes, 0);
    });

    it("gates every REST and raw POST mutation, including later cancellation and actual foreign routes", async () => {
      let writes = 0;
      let checks = 0;
      let cancelled = false;
      const authorize = async request => {
        checks++;
        assert.equal(request.requireCompletion, true);
        return { claim_handle: request.claim_handle, authorized: !cancelled && request.message.repo === "owner/repo", ...(cancelled ? { state: "cancelled", suppressed: true } : {}) };
      };
      const request = async () => {
        writes++;
      };
      request.endpoint = { DEFAULTS: { method: "GET" } };
      const source = {
        rest: {
          issues: {
            create: async () => {
              writes++;
            },
            update: async () => {
              writes++;
            },
          },
        },
        request,
      };
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        await client.rest.issues.create({ owner: "owner", repo: "repo" });
        await client.request("POST /repos/{owner}/{repo}/issues", { owner: "owner", repo: "repo" });
        await assert.rejects(client.request("POST /repos/foreign/repo/issues", { owner: "owner", repo: "repo" }), /same-Claim/);
        await assert.rejects(client.request("POST /repos/owner/repo/issues", { url: "/repos/foreign/repo/issues" }), /same-Claim/);
        assert.equal(writes, 2);
        cancelled = true;
        await assert.rejects(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 1 }), error => error instanceof Error && "suppressed" in error && error.suppressed === true);
        assert.equal(writes, 2);
        assert.equal(checks, 5);
      });
    });

    it("cannot bypass targets with call/apply/bind and supports immutable SDK namespaces", async () => {
      let writes = 0;
      const create = async () => {
        writes++;
      };
      const source = Object.freeze({ rest: Object.freeze({ issues: Object.freeze({ create }) }) });
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === "owner/repo" });
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        assert.deepEqual(Object.keys(client.rest.issues), ["create"]);
        const misleadingReceiver = { owner: "owner", repo: "repo" };
        await assert.rejects(client.rest.issues.create.call(misleadingReceiver, { owner: "foreign", repo: "repo" }), /same-Claim/);
        await assert.rejects(client.rest.issues.create.apply(misleadingReceiver, [{ owner: "foreign", repo: "repo" }]), /same-Claim/);
        await assert.rejects(client.rest.issues.create.bind(misleadingReceiver, { owner: "foreign", repo: "repo" })(), /same-Claim/);
        await client.rest.issues.create.call(null, { owner: "owner", repo: "repo" });
        const descriptor = Object.getOwnPropertyDescriptor(client.rest.issues, "create");
        assert.ok(descriptor);
        await assert.rejects(descriptor.value({ owner: "foreign", repo: "repo" }), /same-Claim/);
        assert.equal(writes, 1);
      });
    });

    it("independently resolves all GraphQL node targets before mutation", async () => {
      let mutations = 0;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === "owner/repo" });
      const source = {
        graphql: async (query, variables) => {
          if (query.startsWith("mutation")) {
            mutations++;
            return {};
          }
          return { nodes: variables.ids.map(id => ({ id, __typename: "Issue", number: 1, repository: { nameWithOwner: id === "foreign" ? "other/repo" : "owner/repo" } })) };
        },
      };
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        await client.graphql("mutation($id: ID!) { closeIssue(input: {issueId: $id}) { clientMutationId } }", { id: "own" });
        await assert.rejects(client.graphql("mutation($input: LinkInput!) { link(input: $input) { clientMutationId } }", { input: { parentId: "own", childId: "foreign" } }), /same-Claim/);
        await assert.rejects(client.graphql('mutation { closeIssue(input: {issueId: "own"}) { clientMutationId } }'), /variable-bound/);
        assert.throws(
          () => client.graphql("mutation($id: ID!) { closeIssue(input: {issueId: $id}) { clientMutationId } }", { id: "own", query: 'mutation { closeIssue(input: {issueId: "foreign"}) { clientMutationId } }' }),
          /override the authorized operation/
        );
        await assert.rejects(
          client.graphql("mutation($target: String!) { commit(input: {branch: {repositoryNameWithOwner: $target}}) { clientMutationId } }", { target: "other/repo", unused: { repositoryNameWithOwner: "owner/repo" } }),
          /same-Claim/
        );
        await assert.rejects(
          client.graphql('mutation($input: Input!) { first(input: $input) { clientMutationId } second(input: {body: "unknown target"}) { clientMutationId } }', { input: { repositoryNameWithOwner: "owner/repo" } }),
          /no resolved node targets/
        );
        await client.graphql("mutation($input: Input!) { commit(input: $input) { clientMutationId } }", { input: { branch: { repositoryNameWithOwner: "owner/repo" }, expectedHeadOid: "a".repeat(40) } });
        assert.equal(mutations, 2);
      });
    });

    it("freezes effect arguments across authorization and rejects origin and pagination escapes", async () => {
      let writes = 0;
      const parameters = { owner: "owner", repo: "repo" };
      const authorize = async request => {
        parameters.owner = "foreign";
        return { claim_handle: request.claim_handle, authorized: true };
      };
      const source = {
        request: async () => {
          writes++;
        },
        paginate: async () => {
          writes++;
        },
        rest: {
          issues: {
            create: async args => {
              assert.equal(args.owner, "owner");
              writes++;
            },
          },
        },
      };
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        await client.rest.issues.create(parameters);
        assert.throws(() => client.request("POST /repos/owner/repo/issues", { baseUrl: "https://other.example" }), /API base URL/);
        assert.throws(() => client.request("GET https://other.example/repos/owner/repo/issues"), /API origin/);
        assert.throws(() => client.paginate("POST /repos/owner/repo/issues"), /read endpoints/);
        assert.throws(() => client.paginate("GET /repos/owner/repo/issues", { method: "POST" }), /read endpoints/);
        assert.equal(writes, 1);
      });
    });

    it("authorizes concrete route targets and independently resolves opaque comment parents", async () => {
      let writes = 0;
      const targets = [];
      const authorize = async request => {
        targets.push(request.message.item_number);
        return { claim_handle: request.claim_handle, authorized: request.message.item_number === 42 };
      };
      const source = {
        request: async () => {
          writes++;
        },
        rest: {
          issues: {
            getComment: async ({ comment_id }) => ({ data: { id: comment_id, issue_url: `https://api.github.com/repos/owner/repo/issues/${comment_id === 1 ? 42 : 43}` } }),
            updateComment: async () => {
              writes++;
            },
          },
        },
      };
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        await assert.rejects(client.request("PATCH /repos/owner/repo/issues/43", { issue_number: 42 }), /same-Claim/);
        await assert.rejects(client.rest.issues.updateComment({ owner: "owner", repo: "repo", issue_number: 42, comment_id: 2, body: "wrong parent" }), /same-Claim/);
        await client.rest.issues.updateComment({ owner: "owner", repo: "repo", comment_id: 1, body: "own parent" });
        assert.equal(writes, 1);
        assert.deepEqual(targets, [43, 43, 42]);
      });
    });

    it("does not turn hidden auxiliary or ambiguous API effects into successful Results", async () => {
      const scope = JSON.parse(JSON.stringify(assignment));
      scope.claims[0].work.effect_contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] };
      const effects = [];
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
      const source = {
        rest: {
          issues: {
            create: async () => ({ data: { id: 42, number: 10 } }),
            update: async () => ({ data: { id: 43, number: 11 } }),
          },
        },
      };
      await withClaimExecution({ assignment: scope, claim_handle: "h1", authorize, effects }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        await client.rest.issues.create({ owner: "owner", repo: "repo" });
        const options = {
          assignment: scope,
          claim_handle: "h1",
          authorize,
          effects,
          messages: [{ type: "create_issue", claim_handle: "h1", repo: "owner/repo" }],
          results: [{ messageIndex: 0, success: true, claim_handle: "h1", result: { number: 10 } }],
          verifyOutput: async () => ({ verified: true, claim_handle: "h1", resource: { kind: "issue", repository: "owner/repo", id: "42", number: 10 }, evidence: { source: "independent_read" } }),
        };
        assert.equal((await verifyClaimDelivery(options)).verification, "verified");
        await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 11 });
        assert.equal((await verifyClaimDelivery(options)).verification, "unknown");
        scope.claims[0].work.effect_contract = { kind: "none" };
        await assert.rejects(verifyClaimDelivery({ assignment: scope, claim_handle: "h1", authorize, effects }), /immutable assignment/);
      });
      assert.equal((await verifyClaimDelivery({ assignment: scope, claim_handle: "h1", authorize, effects })).verification, "unknown");
    });

    it("authorizes all actual git push destinations after native URL rewriting", async () => {
      const proofs = [];
      const authorize = async request => {
        proofs.push(request.message.repo);
        return { claim_handle: request.claim_handle, authorized: request.message.repo === "owner/repo" };
      };
      assert.equal(gitPushRepository("git@github.com:owner/repo.git"), "owner/repo");
      assert.throws(() => gitPushRepository("https://other.example/owner/repo.git"), /unapproved server/);
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        await assertGitPushAuthorized({
          remote: "origin",
          branch: "work",
          execGitSync: args => {
            assert.deepEqual(args, ["remote", "get-url", "--push", "--all", "--", "origin"]);
            return "https://github.com/owner/repo.git\n";
          },
        });
        await assert.rejects(assertGitPushAuthorized({ remote: "origin", branch: "work", execGitSync: () => "https://github.com/owner/repo.git\nhttps://github.com/foreign/repo.git\n" }), /same-Claim/);
        await assert.rejects(
          assertGitPushAuthorized({
            remote: "https://github.com/owner/repo.git",
            branch: "work",
            execGitSync: args => {
              assert.equal(args[0], "-c");
              return "https://github.com/foreign/repo.git\n";
            },
          }),
          /same-Claim/
        );
      });
      assert.deepEqual(proofs, ["owner/repo", "owner/repo", "foreign/repo", "foreign/repo"]);
    });

    it("partitions actual artifact uploads, resolver files and outputs and gates their real repository", async () => {
      const directory = path.resolve(".queue-validation-cache", `claim-artifacts-${randomUUID()}`);
      const keys = ["RUNNER_TEMP", "GH_AW_ARTIFACT_RESOLVER_FILE", "GITHUB_REPOSITORY", "GITHUB_RUN_ID"];
      const oldEnvironment = keys.map(key => process.env[key]);
      const oldCore = global.core;
      const oldFactory = global.__createArtifactClient;
      const uploads = [];
      const outputs = [];
      fs.mkdirSync(directory, { recursive: true });
      process.env.RUNNER_TEMP = directory;
      process.env.GH_AW_ARTIFACT_RESOLVER_FILE = path.join(directory, "artifact-resolver.json");
      process.env.GITHUB_REPOSITORY = "owner/repo";
      process.env.GITHUB_RUN_ID = "123";
      global.core = { info() {}, warning() {}, setOutput: (key, value) => outputs.push([key, value]) };
      global.__createArtifactClient = () => ({
        uploadArtifact: async (name, files, root) => {
          uploads.push({ name, files, root, content: fs.readFileSync(files[0], "utf8") });
          return { id: uploads.length, size: 1, digest: "f".repeat(64) };
        },
      });
      const modulePath = require.resolve("./upload_artifact.cjs");
      delete require.cache[modulePath];
      try {
        const { main, verifyArtifactDelivery } = require(modulePath);
        const effects = [];
        const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
        let firstHandler;
        for (const handle of ["h1", "h2"]) {
          const root = claimArtifactPath(path.join(directory, "gh-aw", "safeoutputs", "upload-artifacts"), handle, assignment);
          fs.mkdirSync(root, { recursive: true });
          fs.writeFileSync(path.join(root, "same.txt"), handle);
          await withClaimExecution({ assignment, claim_handle: handle, authorize, effects }, async () => {
            const handler = await main();
            if (handle === "h1") firstHandler = handler;
            const result = await handler({ type: "upload_artifact", claim_handle: handle, path: "same.txt", temporary_id: "aw_same" });
            assert.equal(result.success, true);
            const client = {
              rest: {
                actions: {
                  getArtifact: async () => ({
                    data: {
                      id: result.artifactId,
                      name: result.artifactName,
                      digest: `sha256:${"f".repeat(64)}`,
                      size_in_bytes: 1,
                      expired: false,
                      workflow_run: { id: 123 },
                    },
                  }),
                },
              },
            };
            const member = assignment.claims.find(claim => claim.handle === handle);
            assert.equal((await verifyArtifactDelivery({ claim: member, result, github: client })).verified, true);
            assert.equal((await verifyArtifactDelivery({ claim: member, result: structuredClone(result), github: client })).verified, false);
            assert.equal((await verifyArtifactDelivery({ claim: assignment.claims.find(claim => claim.handle !== handle), result, github: client })).verified, false);
            const getArtifact = client.rest.actions.getArtifact;
            client.rest.actions.getArtifact = async () => {
              const metadata = await getArtifact();
              return { ...metadata, data: { ...metadata.data, digest: `sha256:${"0".repeat(64)}` } };
            };
            assert.equal((await verifyArtifactDelivery({ claim: member, result, github: client })).verified, false);
          });
        }
        assert.deepEqual(
          uploads.map(upload => upload.content),
          ["h1", "h2"]
        );
        assert.notEqual(uploads[0].name, uploads[1].name);
        assert.notEqual(uploads[0].root, uploads[1].root);
        assert.equal(new Set(outputs.map(([key]) => key)).size, outputs.length);
        for (const handle of ["h1", "h2"]) {
          const resolver = JSON.parse(fs.readFileSync(path.join(claimArtifactPath(directory, handle, assignment), "artifact-resolver.json"), "utf8"));
          assert.equal(resolver.aw_same, uploads[handle === "h1" ? 0 : 1].name);
        }
        assert.deepEqual(
          effects.map(effect => effect.claim_handle),
          ["h1", "h2"]
        );
        await withClaimExecution({ assignment, claim_handle: "h2", authorize }, async () => {
          await assert.rejects(firstHandler({ type: "upload_artifact", claim_handle: "h2", path: "same.txt" }), /original Claim/);
        });
        let checks = 0;
        const cancelling = async request => {
          const authorized = ++checks === 1;
          return { claim_handle: request.claim_handle, authorized, state: authorized ? "completed" : "cancelled", ...(authorized ? {} : { suppressed: true }) };
        };
        await withClaimExecution({ assignment, claim_handle: "h1", authorize: cancelling }, async () => {
          const handler = await main();
          const result = await handler({ type: "upload_artifact", claim_handle: "h1", path: "same.txt" });
          assert.equal(result.success, false);
          assert.match(result.error, /cancelled/);
        });
        assert.equal(uploads.length, 2);
      } finally {
        delete require.cache[modulePath];
        global.core = oldCore;
        global.__createArtifactClient = oldFactory;
        keys.forEach((key, index) => (oldEnvironment[index] === undefined ? delete process.env[key] : (process.env[key] = oldEnvironment[index])));
        fs.rmSync(directory, { recursive: true, force: true });
      }
    });

    it("rejects cross-Claim client reuse and restores ordinary client factories", async () => {
      const source = { rest: { issues: { create: async () => ({ ok: true }) } } };
      const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
      const oldGithub = global.github;
      const oldFactory = global.getOctokit;
      Reflect.set(global, "github", source);
      const factory = () => source;
      Reflect.set(global, "getOctokit", factory);
      let captured;
      try {
        await withClaimExecution({ assignment, claim_handle: "h1", authorize }, () =>
          withClaimEffectClients({ claim_handle: "h1", authorize }, async () => {
            captured = global.getOctokit("fixture-token");
            await captured.rest.issues.create({ owner: "owner", repo: "repo" });
          })
        );
        assert.equal(global.github, source);
        assert.equal(global.getOctokit, factory);
        await withClaimExecution({ assignment, claim_handle: "h2", authorize }, async () => {
          assert.throws(() => captured.rest.issues.create({ owner: "owner", repo: "repo" }), /escape/);
        });
      } finally {
        global.github = oldGithub;
        global.getOctokit = oldFactory;
      }
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
