"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { withClaimExecution, withClaimOutputType, claimArtifactPath } = require("./work_queue_claim_scope.cjs");
const { appendClaimEffects, wrapClaimEffectClient, withClaimEffectClients } = require("./work_queue_effect_client.cjs");
const { assertGitPushAuthorized, gitPushRepository } = require("./work_queue_git_effects.cjs");
const { readJsonlFile } = require("./jsonl_helpers.cjs");

function singletonAssignment(dispatch_id, claim_id, work_id) {
  return { dispatch_id, claims: [{ handle: "h1", claim_id, work_id, work: {}, result_refs: [] }] };
}

// One Claim per Dispatch: two concurrent dispatches may both use the local handle `h1`.
const assignment = singletonAssignment("d", "c:h1", "w:h1");
const otherAssignment = singletonAssignment("another-dispatch", "c:other", "w:other");

function registerTests({ describe, it }) {
  describe("fresh per-request Claim effect boundaries", () => {
    it("rejects a two-member assignment before any client, authorization or API call", async () => {
      let writes = 0;
      let checks = 0;
      let ran = false;
      const source = {
        rest: {
          issues: {
            update: async (/** @type {unknown} */ _request) => {
              writes++;
            },
          },
        },
      };
      const authorize = async request => {
        checks++;
        return { authorized: true, claim_handle: request.claim_handle };
      };
      const twoMember = { dispatch_id: "d", claims: [...assignment.claims, { handle: "h2", claim_id: "c:h2", work_id: "w:h2", work: {}, result_refs: [] }] };
      assert.throws(
        () =>
          withClaimExecution({ assignment: twoMember, claim_handle: "h1", authorize }, async () => {
            ran = true;
            await wrapClaimEffectClient(source, { claim_handle: "h1", authorize }).rest.issues.update({ owner: "owner", repo: "repo", issue_number: 1 });
          }),
        /exactly one open Claim/
      );
      assert.throws(() => claimArtifactPath("artifacts", "h1", twoMember), /exactly one open Claim/);
      assert.equal(ran, false);
      assert.equal(checks, 0);
      assert.equal(writes, 0);
    });

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
      const request = async (_route, _parameters = {}) => {
        writes++;
      };
      request.endpoint = { DEFAULTS: { method: "GET" } };
      const source = {
        rest: {
          issues: {
            create: async _parameters => {
              writes++;
            },
            update: async _parameters => {
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
      const create = async _parameters => {
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

    it("resolves repository identities before create effects that have no existing parent resource", async () => {
      let writes = 0;
      let repositoryId = 7;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.resource.repository_id === "7" });
      const source = {
        rest: {
          repos: { get: async () => ({ data: { full_name: "owner/repo", id: repositoryId } }) },
          issues: {
            create: async _parameters => {
              writes++;
              return { data: { id: 100, number: 42 } };
            },
          },
        },
      };
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        repositoryId = 8;
        await assert.rejects(client.rest.issues.create({ owner: "owner", repo: "repo", title: "wrong native repository" }), /same-Claim/);
        assert.equal(writes, 0);
        repositoryId = 7;
        await client.rest.issues.create({ owner: "owner", repo: "repo", title: "approved native repository" });
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

    it("does not authorize GraphQL identities from an API on another host", async () => {
      const previousServerUrl = process.env.GITHUB_SERVER_URL;
      const previousApiUrl = process.env.GITHUB_API_URL;
      process.env.GITHUB_SERVER_URL = "https://contoso.example";
      process.env.GITHUB_API_URL = "https://api.evil.example";
      let mutations = 0;
      const authorize = async request => ({
        claim_handle: request.claim_handle,
        authorized: request.resource.host === "contoso.example",
      });
      const source = {
        graphql: async (query, variables) => {
          if (query.startsWith("mutation")) {
            mutations++;
            return {};
          }
          return {
            nodes: variables.ids.map(id => ({
              id,
              __typename: "Issue",
              databaseId: 2,
              number: 1,
              repository: { nameWithOwner: "owner/repo", databaseId: 7 },
            })),
          };
        },
      };
      try {
        await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
          await assert.rejects(client.graphql("mutation($id: ID!) { closeIssue(input: {issueId: $id}) { clientMutationId } }", { id: "issue" }), /same-Claim/);
        });
        assert.equal(mutations, 0);
      } finally {
        previousServerUrl === undefined ? delete process.env.GITHUB_SERVER_URL : (process.env.GITHUB_SERVER_URL = previousServerUrl);
        previousApiUrl === undefined ? delete process.env.GITHUB_API_URL : (process.env.GITHUB_API_URL = previousApiUrl);
      }
    });

    it("freezes effect arguments across authorization and rejects origin and pagination escapes", async () => {
      let writes = 0;
      const parameters = { owner: "owner", repo: "repo" };
      const authorize = async request => {
        parameters.owner = "foreign";
        return { claim_handle: request.claim_handle, authorized: true };
      };
      const source = {
        request: async (_route, _parameters = {}) => {
          writes++;
        },
        paginate: async (_route, _parameters = {}) => {
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
        request: async (_route, _parameters = {}) => {
          writes++;
        },
        rest: {
          issues: {
            getComment: async ({ comment_id }) => ({ data: { id: comment_id, issue_url: `https://api.github.com/repos/owner/repo/issues/${comment_id === 1 ? 42 : 43}` } }),
            updateComment: async _parameters => {
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

    it("appends succeeded visible effects with their safe-output type to the effects file", async () => {
      const directory = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-effects");
      const filename = path.join(directory, "effects.jsonl");
      const effects = [];
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
      const source = {
        rest: {
          repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
          issues: {
            create: async _parameters => ({ data: { id: 42, number: 10 } }),
            update: async _parameters => {
              throw new Error("rejected");
            },
          },
          git: { createBlob: async _parameters => ({ data: { sha: "b".repeat(40) } }) },
        },
      };
      try {
        await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
          await withClaimOutputType("create_issue", () => client.rest.issues.create({ owner: "owner", repo: "repo", title: "t" }));
          await assert.rejects(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 10, title: "u" }), /rejected/);
          await client.rest.git.createBlob({ owner: "owner", repo: "repo", content: "x" });
        });
        assert.equal(appendClaimEffects(effects, filename), 1);
        assert.deepEqual([...readJsonlFile(filename, { strict: true })], [{ claim_handle: "h1", kind: "create_issue", resource: { repository: "owner/repo", number: 10, id: "42" } }]);
      } finally {
        fs.rmSync(directory, { recursive: true, force: true });
      }
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
              assert.deepEqual(args.slice(2), ["remote", "-v"]);
              const alias = args[1].match(/^remote\.(.+)\.url=/)?.[1];
              assert.ok(alias);
              return `${alias}\thttps://github.com/foreign/repo.git (push)\n`;
            },
          }),
          /same-Claim/
        );
      });
      assert.deepEqual(proofs, ["owner/repo", "owner/repo", "owner/repo", "owner/repo", "foreign/repo", "foreign/repo"]);
    });

    it("partitions actual artifact uploads, resolver files and outputs and gates their real repository", async () => {
      const directory = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-artifacts");
      const keys = ["RUNNER_TEMP", "GH_AW_ARTIFACT_RESOLVER_FILE", "GITHUB_REPOSITORY", "GITHUB_RUN_ID"];
      const oldEnvironment = keys.map(key => process.env[key]);
      const oldCore = global.core;
      const oldGithub = global.github;
      const oldFactory = global.__createArtifactClient;
      const uploads = [];
      const outputs = [];
      fs.mkdirSync(directory, { recursive: true });
      process.env.RUNNER_TEMP = directory;
      process.env.GH_AW_ARTIFACT_RESOLVER_FILE = path.join(directory, "artifact-resolver.json");
      process.env.GITHUB_REPOSITORY = "owner/repo";
      process.env.GITHUB_RUN_ID = "123";
      global.core = { ...oldCore, info() {}, warning() {}, setOutput: (key, value) => outputs.push([key, value]) };
      Reflect.set(global, "github", {
        rest: {
          repos: {
            get: async args => {
              assert.deepEqual(args, { owner: "owner", repo: "repo" });
              return { data: { full_name: "owner/repo", id: 7 } };
            },
          },
        },
      });
      global.__createArtifactClient = () => ({
        uploadArtifact: async (name, files, root) => {
          uploads.push({ name, files, root, content: fs.readFileSync(files[0], "utf8") });
          return { id: uploads.length, size: 1, digest: "f".repeat(64) };
        },
      });
      const modulePath = require.resolve("./upload_artifact.cjs");
      delete require.cache[modulePath];
      try {
        const { main } = require(modulePath);
        const effectsByDispatch = [];
        const authorize = async request => {
          assert.equal(request.requireCompletion, true);
          if (request.resource !== undefined) assert.deepEqual(request.resource, { repository: "owner/repo", host: "github.com", repository_id: "7", run_id: "123" });
          return { authorized: true, claim_handle: request.claim_handle };
        };
        let firstHandler;
        for (const current of [assignment, otherAssignment]) {
          const root = claimArtifactPath(path.join(directory, "gh-aw", "safeoutputs", "upload-artifacts"), "h1", current);
          fs.mkdirSync(root, { recursive: true });
          fs.writeFileSync(path.join(root, "same.txt"), current.dispatch_id);
          const effects = [];
          effectsByDispatch.push(effects);
          await withClaimExecution({ assignment: current, claim_handle: "h1", authorize, effects }, async () => {
            const handler = await main();
            if (current === assignment) firstHandler = handler;
            const result = await handler({ type: "upload_artifact", claim_handle: "h1", path: "same.txt", temporary_id: "aw_same" });
            assert.equal(result.success, true, result.error);
          });
        }
        assert.deepEqual(
          uploads.map(upload => upload.content),
          [assignment.dispatch_id, otherAssignment.dispatch_id]
        );
        assert.notEqual(uploads[0].name, uploads[1].name);
        assert.notEqual(uploads[0].root, uploads[1].root);
        assert.equal(new Set(outputs.map(([key]) => key)).size, outputs.length);
        [assignment, otherAssignment].forEach((current, index) => {
          const resolver = JSON.parse(fs.readFileSync(path.join(claimArtifactPath(directory, "h1", current), "artifact-resolver.json"), "utf8"));
          assert.equal(resolver.aw_same, uploads[index].name);
        });
        assert.deepEqual(
          effectsByDispatch.map(effects => effects.map(effect => effect.claim_handle)),
          [["h1"], ["h1"]]
        );
        await withClaimExecution({ assignment: otherAssignment, claim_handle: "h1", authorize }, async () => {
          await assert.rejects(firstHandler({ type: "upload_artifact", claim_handle: "h1", path: "same.txt" }), /original immutable Claim identity/);
        });
        let checks = 0;
        const cancelling = async request => {
          const authorized = ++checks === 1;
          return { claim_handle: request.claim_handle, authorized, state: authorized ? "open" : "cancelled", ...(authorized ? {} : { suppressed: true }) };
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
        global.github = oldGithub;
        global.__createArtifactClient = oldFactory;
        keys.forEach((key, index) => (oldEnvironment[index] === undefined ? delete process.env[key] : (process.env[key] = oldEnvironment[index])));
        fs.rmSync(directory, { recursive: true, force: true });
      }
    });

    it("rejects client reuse under another dispatch's Claim and restores ordinary client factories", async () => {
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
        await withClaimExecution({ assignment: otherAssignment, claim_handle: "h1", authorize }, async () => {
          assert.throws(() => captured.rest.issues.create({ owner: "owner", repo: "repo" }), /original immutable Claim identity/);
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
