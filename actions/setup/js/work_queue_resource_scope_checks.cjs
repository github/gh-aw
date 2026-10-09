"use strict";

const assert = require("node:assert/strict");
const { canonical, parseStrictJSON } = require("./work_queue_codec.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { compactTransactions, replayTransactions, serializeProjection, validateClaimAuthority } = require("./work_queue_replay.cjs");
const { frozenResourceScope, validateEffectResource } = require("./work_queue_resource_scope.cjs");
const { bind, context, finish, genesis, grant, submission, workerActor } = require("./work_queue_test_helpers.cjs");
const fixture = require("../../../specs/work-queue/fixtures/resource-scope.json");
const { queueFixture, REF, REPOSITORY, WORKFLOW } = require("./work_queue_lifecycle.test_helpers.cjs");
const { authorizeWorkerClaim } = require("./finish_work_queue_claim.cjs");
const { withClaimExecution, assertClaimAuthorized, withClaimResourceEffects, withClaimResourceVerification, closeClaimEffectChannel, currentClaimResourceEffects, isProtectedClaimResourceTarget } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { newChildWork } = require("./work_queue_graph.cjs");

/** @param {(request: {path: string, body: object}) => void} onMutation */
function nativeEffectTransport(onMutation) {
  const { getOctokit } = require("./node_modules/@actions/github");
  return getOctokit("queue-native-effect-fixture", {
    request: {
      /** @param {string | URL | Request} input @param {RequestInit} [options] */
      async fetch(input, options) {
        let url;
        try {
          url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
        } catch (cause) {
          throw new Error("Invalid native fixture URL", { cause });
        }
        const reply = value => new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
        if (options?.method === "GET" && url.pathname === "/repos/owner/repo") return reply({ id: 7, full_name: REPOSITORY, default_branch: "main" });
        if (options?.method === "GET" && url.pathname === "/repos/owner/repo/issues/42") return reply({ id: 100, number: 42 });
        if (options?.method === "GET" && url.pathname === "/repos/owner/repo/issues/43") return reply({ id: 101, number: 43 });
        /** @type {unknown} */
        const body = parseStrictJSON(String(options?.body || "{}"));
        assert.ok(body && typeof body === "object" && !Array.isArray(body));
        if (url.pathname === "/graphql" && "query" in body && typeof body.query === "string" && body.query.startsWith("query WorkQueueEffectTargets")) {
          const query = body.query;
          const variables = "variables" in body ? body.variables : undefined;
          assert.ok(variables && typeof variables === "object" && "ids" in variables && Array.isArray(variables.ids));
          const nodes = variables.ids.map(id =>
            id === "repository-node"
              ? { id, __typename: "Repository", databaseId: 7, nameWithOwner: REPOSITORY }
              : id === "ref-node"
                ? {
                    id,
                    __typename: "Ref",
                    ...(query.includes("... on Ref") ? { name: "main", prefix: "refs/heads/", repository: { nameWithOwner: REPOSITORY, databaseId: 7 } } : {}),
                  }
                : { id, __typename: "Issue", databaseId: 100, number: 42, repository: { nameWithOwner: REPOSITORY, databaseId: 7 } }
          );
          return reply({ data: { nodes } });
        }
        onMutation({ path: url.pathname, body });
        return reply(url.pathname === "/graphql" ? { data: {} } : {});
      },
    },
  });
}

/** @typedef {{kind: string, host: string, repository: string, repository_id: string, resource_id: string, number: string}} ResourceFixtureSubject */
/** @typedef {{scope?: {version: number, resources: Array<Record<string, string>>}, subject?: ResourceFixtureSubject}} ResourceFixtureBinding */

/**
 * @param {Record<string, unknown>} payload
 * @param {ResourceFixtureSubject} [subject]
 */
function productionFixture(payload, subject) {
  const queue = queueFixture({
    bound: true,
    count: 1,
    configureWork(work) {
      work.payload = { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 0, max: 1 }] }, ...payload };
      if (subject) work.subject = subject;
    },
  });
  const assignment = queue.assignment;
  assert.ok(assignment);
  const member = assignment.claims[0];
  const actor = { ...queue.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: member.handle };
  queue.append("finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, actor);
  const authorize = request =>
    authorizeWorkerClaim({
      ...request,
      context: queue.workerContext,
      githubClient: queue.githubClient,
      workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
      readWorkQueueLog: queue.readWorkQueueLog,
    });
  return { queue, assignment, member, authorize };
}

/** @param {{kind: string, host: string, repository: string, repository_id: string, resource_id: string, number: string}} [subject] */
function completedScope(payloads, subject = undefined) {
  const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
  policy.pools.default.profiles.default.max_claims = payloads.length;
  const log = [genesis(policy)];
  log.push(
    submission(
      log,
      payloads.map((_, index) => `work-${index}`),
      {
        transform(node, index) {
          node.payload = payloads[index];
          if (subject) node.subject = subject;
          return node;
        },
      }
    )
  );
  const granted = grant(log, { max_claims: payloads.length });
  log.push(granted.commit);
  const dispatchId = granted.assignments[0].dispatch_id;
  const bound = bind(log, dispatchId, { runId: "202" });
  for (const member of granted.assignments[0].claims) bound.push(finish(bound, dispatchId, member.handle, "completed"));
  return { state: replayTransactions(bound), dispatchId };
}

/** @param {ResourceFixtureBinding & {name: string, ancestors: ResourceFixtureBinding[]}} test */
function completedAncestorScope(test) {
  const root = test.ancestors[0];
  const f = productionFixture("scope" in root ? { resource_scope: root.scope } : {}, "subject" in root ? root.subject : undefined);
  let dispatchId = f.assignment.dispatch_id;
  let handle = f.member.handle;
  let actor = { ...f.queue.workerActor, dispatch_id: dispatchId, claim_handle: handle };
  for (const [index, binding] of [...test.ancestors.slice(1), test].entries()) {
    const payload = { task: `${test.name}-${index}`, ...("scope" in binding ? { resource_scope: binding.scope } : {}) };
    const childWork = { ...newChildWork(f.queue.state, actor, payload, `resource-fixture-child-${index}`, f.queue.at), ...("subject" in binding && binding.subject ? { subject: binding.subject } : {}) };
    f.queue.append("submit", { nodes: [childWork] }, actor);
    const previousDispatches = new Set(f.queue.state.dispatches.keys());
    f.queue.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: f.queue.policy.limits.assignment_bytes }, f.queue.dispatcher);
    const child = [...f.queue.state.dispatches.values()].find(dispatch => !previousDispatches.has(dispatch.dispatch_id));
    assert.ok(child);
    assert.equal(child.claims.length, 1);
    assert.equal(child.claims[0].work_id, childWork.work_id);
    const runId = String(43 + index);
    const run = { ...f.queue.binding, run_id: runId };
    f.queue.append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: child.dispatch_id, state: "started", sender: f.queue.dispatcher }] }, f.queue.dispatcher);
    f.queue.append(
      "dispatch",
      {
        operations: [
          {
            kind: "Dispatch",
            dispatch_id: child.dispatch_id,
            state: "bound",
            run,
            evidence: { kind: "reconciliation", source: "github_api", repository: REPOSITORY, workflow: WORKFLOW, ref: REF, principal: run.principal, checked_at: f.queue.at, run_id: runId, run_attempt: 1 },
          },
        ],
      },
      f.queue.dispatcher
    );
    dispatchId = child.dispatch_id;
    handle = child.claims[0].handle;
    actor = { ...f.queue.workerActor, run_id: runId, dispatch_id: dispatchId, claim_handle: handle };
    f.queue.append("finish", { dispatch_id: dispatchId, claim_handle: handle, outcome: "completed" }, actor);
  }
  return { state: f.queue.state, dispatchId, handle };
}

function authorize(state, dispatchId, resource, handle = "h1") {
  const actor = workerActor(state, dispatchId, handle);
  const dispatch = state.dispatches.get(dispatchId);
  const member = dispatch.claims.find(candidate => candidate.handle === handle);
  return validateClaimAuthority(state, member.claim_id, context(actor, { ref: dispatch.run.ref, event: dispatch.run.event }), { requireCompletion: true, resource });
}

function scopeError(error) {
  return error instanceof Error && "code" in error && error.code === "claim_scope_invalid";
}

function registerTests({ describe, it }) {
  describe("immutable per-Claim Work resource authority", () => {
    assert.equal(fixture.version, 1);
    assert.equal(fixture.cases.length, 47);
    assert.equal(fixture.ancestor_cases.length, 10);
    assert.equal(new Set(fixture.cases.map(test => test.name)).size, fixture.cases.length);
    assert.equal(new Set(fixture.ancestor_cases.map(test => test.name)).size, fixture.ancestor_cases.length);
    for (const test of fixture.cases) {
      it(`matches independent resource scope: ${test.name}`, () => {
        const payload = { task: test.name, ...(Object.hasOwn(test, "scope") ? { resource_scope: test.scope } : {}) };
        const { state, dispatchId } = completedScope([payload], test.subject);
        const before = canonical(serializeProjection(state));
        if (test.valid) assert.doesNotThrow(() => authorize(state, dispatchId, test.target));
        else assert.throws(() => authorize(state, dispatchId, test.target), scopeError);
        assert.equal(canonical(serializeProjection(state)), before);
      });
    }
    for (const test of fixture.ancestor_cases) {
      it(`matches independent ancestor scope: ${test.name}`, () => {
        const { state, dispatchId, handle } = completedAncestorScope(test);
        const before = canonical(serializeProjection(state));
        if (test.valid) assert.doesNotThrow(() => authorize(state, dispatchId, test.target, handle));
        else assert.throws(() => authorize(state, dispatchId, test.target, handle), scopeError);
        assert.equal(canonical(serializeProjection(state)), before);
      });
    }
    it("keeps sibling scopes separate and rejects replacement of frozen assignment payloads", () => {
      const binding = { host: "github.com", repository: "owner/repo", repository_id: "7", kind: "issue", resource_id: "70" };
      const { state, dispatchId } = completedScope([{ resource_scope: { version: 1, resources: [{ ...binding, number: "7" }] } }, { resource_scope: { version: 1, resources: [{ ...binding, resource_id: "80", number: "8" }] } }]);
      const target = { ...binding, number: "7" };
      assert.doesNotThrow(() => authorize(state, dispatchId, target));
      assert.throws(() => authorize(state, dispatchId, target, "h2"), scopeError);
      const members = state.dispatches.get(dispatchId).claims;
      state.works.get(members[1].work_id).payload = structuredClone(members[0].work);
      assert.throws(() => authorize(state, dispatchId, target, "h2"), scopeError);
    });
    it("intersects installed and frozen profile scopes instead of taking their union", () => {
      for (const changeFrozen of [false, true]) {
        const { state, dispatchId } = completedScope([{ task: "profile intersection" }]);
        state.policy.pools.default.profiles.default.effect_scope = "other/repo";
        if (changeFrozen) state.dispatches.get(dispatchId).profile.effect_scope = "other/repo";
        const target = { repository: changeFrozen ? "owner/repo" : "other/repo" };
        assert.throws(() => authorize(state, dispatchId, target), scopeError);
      }
    });
    it("checks the exact 128-selector bound and UTF-8 identity byte limits", () => {
      for (const count of [128, 129]) {
        const resources = Array.from({ length: count }, (_, index) => ({ host: "github.com", repository: "owner/repo", repository_id: "7", ref: `refs/heads/${index}` }));
        const { state, dispatchId } = completedScope([{ resource_scope: { version: 1, resources } }]);
        if (count === 128) assert.doesNotThrow(() => authorize(state, dispatchId, resources[0]));
        else assert.throws(() => authorize(state, dispatchId, resources[0]), scopeError);
      }
      for (const ref of ["\\".repeat(256), "\u00e9".repeat(128)]) assert.doesNotThrow(() => validateEffectResource({ repository: "owner/repo", ref }));
      assert.throws(() => validateEffectResource({ repository: "owner/repo", ref: "\u00e9".repeat(129) }), scopeError);
    });
    it("rejects malformed direct targets and canonicalizes only selector object key ordering", () => {
      for (const target of [
        null,
        [],
        { repository: "owner/repo", number: 7 },
        { repository: "owner/repo", ref: "\uD800" },
        { repository: "owner/repo", path: "/src/a" },
        { repository: "owner/repo", path: "src//a" },
        { repository: "owner/repo", path: "src/./a" },
        { repository: "owner/repo", path: "src/../a" },
        { repository: "owner/repo", path: "src\\a" },
      ]) {
        assert.throws(() => validateEffectResource(target), scopeError);
      }
      assert.throws(
        () =>
          frozenResourceScope({
            resource_scope: {
              version: 1,
              resources: [
                { repository: "owner/repo", number: "7" },
                { number: "7", repository: "owner/repo" },
              ],
            },
          }),
        scopeError
      );
      const { state, dispatchId } = completedScope([{ task: "invalid explicit target" }]);
      assert.throws(() => authorize(state, dispatchId, null), scopeError);
      assert.throws(() => authorize(state, dispatchId, { repository: "owner/repo", path: "src/a" }), scopeError);
    });
    it("detaches validated target and selector data and requires own closed namespace fields", () => {
      const target = { repository: "owner/repo", number: "7" };
      const validated = validateEffectResource(target);
      target.number = "8";
      assert.equal(validated.number, "7");
      const payload = { resource_scope: { version: 1, resources: [target] } };
      const scope = frozenResourceScope(payload);
      assert.ok(scope);
      payload.resource_scope.resources[0].number = "9";
      assert.equal(scope.resources[0].number, "8");
      for (const inherited of [Object.create({ version: 1, resources: [target] }), Object.assign(Object.create({ version: 1 }), { resources: [target] })]) {
        assert.throws(() => frozenResourceScope({ resource_scope: inherited }), scopeError);
      }
    });
    it("requires positive numeric Work binding before actual builtin and raw REST writes", async () => {
      const target = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      for (const resources of [undefined, [{ repository: REPOSITORY }], [target]]) {
        const bound = resources?.some(resource => "repository_id" in resource && !!resource.repository_id) ?? false;
        const f = productionFixture(resources === undefined ? {} : { resource_scope: { version: 1, resources } });
        let writes = 0;
        const mutate = async (_routeOrParameters, _parameters = {}) => {
          writes++;
          return { data: { id: 100, number: 42 } };
        };
        const source = { rest: { ...f.queue.githubClient.rest, issues: { create: mutate } }, request: mutate };
        await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
          for (const effect of [() => client.rest.issues.create({ owner: "owner", repo: "repo", title: "Work-bound issue" }), () => client.request("POST /repos/owner/repo/issues", { title: "Work-bound issue" })]) {
            if (bound) await effect();
            else await assert.rejects(effect(), /positive immutable Work target binding/);
          }
        });
        assert.equal(writes, bound ? 2 : 0);
      }
    });
    it("does not let generic run, ref or path effects bypass a numeric Subject or borrow a sibling resource", async () => {
      const subject = { repository: REPOSITORY, host: "github.com", repository_id: "7", kind: "issue", resource_id: "100", number: "42" };
      const f = productionFixture({}, subject);
      let writes = 0;
      const source = {
        rest: {
          ...f.queue.githubClient.rest,
          issues: {
            get: async ({ issue_number }) => ({ data: { id: issue_number === 42 ? 100 : 101, number: issue_number } }),
            update: async _parameters => {
              writes++;
            },
          },
          git: {
            createRef: async _parameters => {
              writes++;
            },
            createTree: async _parameters => {
              writes++;
            },
          },
          actions: {
            ...f.queue.githubClient.rest.actions,
            cancelWorkflowRun: async _parameters => {
              writes++;
            },
          },
        },
      };
      await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
        await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 42 });
        await assert.rejects(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 43 }), /immutable Work subject/);
        for (const effect of [
          () => client.rest.git.createRef({ owner: "owner", repo: "repo", ref: "refs/heads/bypass", sha: REF }),
          () => client.rest.git.createTree({ owner: "owner", repo: "repo", tree: [{ path: "src/bypass", mode: "100644", type: "blob", sha: REF }] }),
          () => client.rest.actions.cancelWorkflowRun({ owner: "owner", repo: "repo", run_id: 42 }),
          () => assertClaimAuthorized({ type: "work_queue_git_effect", claim_handle: f.member.handle, repo: REPOSITORY }, { effect: true, resource: { repository: REPOSITORY, host: "github.com", repository_id: "7", path: "src/bypass" } }),
        ])
          await assert.rejects(effect(), /immutable Work subject/);
      });
      assert.equal(writes, 1);
    });
    it("requires exact compiled generic selectors, not merely numeric repository scope", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const f = productionFixture({ resource_scope: { version: 1, resources: [repository] } });
      let writes = 0;
      const source = {
        rest: f.queue.githubClient.rest,
        request: async (_route, _parameters = {}) => {
          writes++;
        },
      };
      await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
        for (const resource of [
          { ...repository, path: "src/a" },
          { ...repository, ref: "refs/heads/a" },
          { ...repository, run_id: "42" },
        ]) {
          await assert.rejects(assertClaimAuthorized({ type: "work_queue_resource_effect", claim_handle: f.member.handle, repo: REPOSITORY }, { effect: true, resource }), /positive immutable Work target binding/);
        }
        const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
        for (const effect of [
          () => client.request("PUT /repos/owner/repo/contents/src/a", { content: "YQ==", branch: "main" }),
          () => client.request("PATCH /repos/owner/repo/git/refs/heads/a", { sha: REF }),
          () => client.request("POST /repos/owner/repo/git/trees", { tree: [{ path: "src/a", mode: "100644", type: "blob", sha: REF }] }),
        ])
          await assert.rejects(effect(), /positive immutable Work target binding/);
      });
      assert.equal(writes, 0);
      for (const [selector, route, parameters] of [
        [{ ...repository, ref: "refs/heads/main", path: "src/a" }, "PUT /repos/owner/repo/contents/src/a", { content: "YQ==", branch: "main" }],
        [{ ...repository, ref: "refs/heads/a" }, "PATCH /repos/owner/repo/git/refs/heads/a", { sha: REF }],
        [{ ...repository, path: "src/a" }, "POST /repos/owner/repo/git/trees", { tree: [{ path: "src/a", mode: "100644", type: "blob", sha: REF }] }],
      ]) {
        const bound = productionFixture({ resource_scope: { version: 1, resources: [selector] } });
        await withClaimExecution({ assignment: bound.assignment, claim_handle: bound.member.handle, authorize: bound.authorize }, () => wrapClaimEffectClient(source, { claim_handle: bound.member.handle }).request(route, parameters));
      }
      assert.equal(writes, 3);
    });
    it("checks actual SDK data, defaults and concrete query-bearing URLs before any write", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const target = { ...repository, ref: "refs/heads/main", path: "src/a" };
      const f = productionFixture({ resource_scope: { version: 1, resources: [target] } });
      let writes = 0;
      const source = nativeEffectTransport(() => writes++);
      await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
        const route = "PUT /repos/owner/repo/contents/src/a";
        await assert.rejects(client.request(route, { branch: "main", data: { branch: "foreign", content: "YQ==" } }));
        await assert.rejects(client.request(route, { branch: "main", data: '{"branch":"foreign","content":"YQ=="}' }));
        await assert.rejects(client.request.defaults({ data: { branch: "foreign", content: "YQ==" } })(route, { branch: "main" }));
        await assert.rejects(withClaimResourceEffects([target], [`request:${route}`], () => client.request.defaults({ branch: "foreign" })(route, { content: "YQ==" })));
        assert.equal(writes, 0);
        await client.request(route, { data: { branch: "main", content: "YQ==" } });
        assert.equal(writes, 1);
      });
      const subject = { ...repository, kind: "issue", resource_id: "100", number: "42" };
      const bound = productionFixture({}, subject);
      writes = 0;
      await withClaimExecution({ assignment: bound.assignment, claim_handle: bound.member.handle, authorize: bound.authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: bound.member.handle });
        for (const route of ["PATCH /repos/owner/repo/issues/43?selector=ignored", "PATCH /repos/owner/repo/issues/%34%33"]) {
          await assert.rejects(client.request(route, { issue_number: 42, state: "closed" }));
        }
      });
      assert.equal(writes, 0);
    });
    it("cannot substitute GraphQL or another HTTP endpoint for the authorized native transport", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const f = productionFixture({ resource_scope: { version: 1, resources: [repository] } });
      const query = "mutation($id: ID!) { closeIssue(input: {issueId: $id}) { clientMutationId } }";
      let writes = 0;
      const source = nativeEffectTransport(() => writes++);
      await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
        await assert.rejects(async () => client.request("POST /graphql", { owner: "owner", repo: "repo", query, variables: { id: "issue-node" } }));
        await assert.rejects(async () => client.graphql.defaults({ url: "/repos/foreign/repo/actions/runs/77/cancel" })(query, { id: "issue-node" }));
        await assert.rejects(async () => client.graphql.defaults({ data: { query: "mutation { deleteRepository(input: {}) { clientMutationId } }" } })(query, { id: "issue-node" }));
        await assert.rejects(async () => client.graphql.defaults({ data: { query, variables: { id: "foreign-node" } } })(query, { id: "issue-node" }));
        await assert.rejects(async () => client.graphql.defaults({ data: { query, variables: { id: "issue-node" } } })("query { viewer { login } }"));
        await client.graphql(query, { id: "issue-node" });
      });
      assert.equal(writes, 1);
    });
    it("cannot use body fields ignored by the native endpoint as immutable Work selectors", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const subject = { ...repository, kind: "issue", resource_id: "100", number: "42" };
      const cases = [
        { target: { ...repository, ref: "refs/heads/main", path: "src/a" }, selectors: { ref: "refs/heads/main", path: "src/a" } },
        { target: { ...repository, run_id: "42" }, selectors: { run_id: "42" } },
        { target: subject, selectors: { issue_number: 42 } },
      ];
      for (const { target, selectors } of cases) {
        const f = productionFixture(target === subject ? {} : { resource_scope: { version: 1, resources: [target] } }, target === subject ? subject : undefined);
        let writes = 0;
        const source = nativeEffectTransport(() => writes++);
        await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
          await assert.rejects(async () => client.request("POST /repos/owner/repo/issues", { title: "Foreign effect kind", ...selectors }));
          await assert.rejects(withClaimResourceEffects([target], ["request:POST /repos/owner/repo/issues"], () => client.request("POST /repos/owner/repo/issues", { title: "Borrowed semantic binding", ...selectors })));
        });
        assert.equal(writes, 0);
      }
    });
    it("preserves one decoding of native paths and pins wire defaults across asynchronous authorization", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const f = productionFixture({ resource_scope: { version: 1, resources: [{ ...repository, ref: "refs/heads/main", path: "src/a" }] } });
      const writes = [];
      const source = nativeEffectTransport(write => writes.push(write));
      const native = source.request.defaults({ data: { branch: "main", content: "YQ==" } });
      await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
        await assert.rejects(async () => client.request("PUT /repos/owner/repo/contents/src%252Fa", { branch: "main", content: "YQ==" }));
        await assert.rejects(async () => client.request("PUT /repos/owner/repo/contents/src%3Fforeign", { branch: "main", content: "YQ==" }));
        const pinned = wrapClaimEffectClient(
          { ...source, request: native },
          {
            claim_handle: f.member.handle,
            async authorize(request) {
              const result = await f.authorize(request);
              native.endpoint.DEFAULTS.data.branch = "foreign";
              return result;
            },
          }
        );
        await pinned.request("PUT /repos/owner/repo/contents/src/a");
      });
      assert.equal(writes.length, 1);
      assert.equal(writes[0].body.branch, "main");
    });
    it("keeps compiler semantic git-tree targets on the actual SDK route and rejects every foreign path", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const target = { ...repository, ref: "refs/heads/main", path: "src/a" };
      const f = productionFixture({ resource_scope: { version: 1, resources: [target] } });
      let writes = 0;
      const source = nativeEffectTransport(() => writes++);
      const route = "POST /repos/owner/repo/git/trees";
      await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
        const entry = { path: "src/a", mode: "100644", type: "blob", sha: REF };
        await withClaimResourceEffects([target], [`request:${route}`], async () => {
          await client.request(route, { tree: [entry] });
          await assert.rejects(client.request(route, { tree: [entry, { ...entry, path: "src/foreign" }] }));
        });
      });
      assert.equal(writes, 1);
    });
    it("preserves repository-bound PR creation without inventing authority from its referenced heads", async () => {
      const ordinaryRepository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const ordinary = productionFixture({ resource_scope: { version: 1, resources: [ordinaryRepository] } });
      let ordinaryWrites = 0;
      const ordinarySource = nativeEffectTransport(() => ordinaryWrites++);
      await withClaimExecution({ assignment: ordinary.assignment, claim_handle: ordinary.member.handle, authorize: ordinary.authorize }, async () => {
        const client = wrapClaimEffectClient(ordinarySource, { claim_handle: ordinary.member.handle });
        await client.request("POST /repos/owner/repo/pulls", { title: "Bound PR", head: "main", base: "main" });
        await client.request("POST /repos/owner/repo/pulls", { title: "Bound PR", head: "fork:main", head_repo: "fork-repository", base: "main" });
      });
      assert.equal(ordinaryWrites, 2);
    });
    it("binds actual named GraphQL branch/file and native Ref effects to compiled selectors", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const cases = [
        {
          query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
          input: { branch: { repositoryNameWithOwner: REPOSITORY, branchName: "main" }, expectedHeadOid: REF, message: { headline: "Claim change" }, fileChanges: { additions: [{ path: "src/a", contents: "YQ==" }] } },
          target: { ...repository, ref: "refs/heads/main", path: "src/a" },
        },
        { query: "mutation($input: CreateRefInput!) { createRef(input: $input) { ref { id } } }", input: { repositoryId: "repository-node", name: "refs/heads/main", oid: REF }, target: { ...repository, ref: "refs/heads/main" } },
        { query: "mutation($input: UpdateRefInput!) { updateRef(input: $input) { ref { id } } }", input: { refId: "ref-node", oid: REF }, target: { ...repository, ref: "refs/heads/main" } },
        { query: "mutation($input: DeleteRefInput!) { deleteRef(input: $input) { clientMutationId } }", input: { refId: "ref-node" }, target: { ...repository, ref: "refs/heads/main" } },
        {
          query: "mutation($input: UpdateRefsInput!) { updateRefs(input: $input) { clientMutationId } }",
          input: { repositoryId: "repository-node", refUpdates: [{ name: "refs/heads/main", beforeOid: REF, afterOid: REF }] },
          target: { ...repository, ref: "refs/heads/main" },
        },
        {
          query: "mutation($input: CreateCommitOnBranchInput!) { change: createCommitOnBranch(input: $input) { commit { oid } } }",
          input: { branch: { id: "ref-node" }, expectedHeadOid: REF, message: { headline: "Claim change" }, fileChanges: { deletions: [{ path: "src/a" }] } },
          target: { ...repository, ref: "refs/heads/main", path: "src/a" },
        },
      ];
      for (const test of cases) {
        for (const exact of [false, true]) {
          const f = productionFixture({ resource_scope: { version: 1, resources: [exact ? test.target : repository] } });
          let writes = 0;
          const source = nativeEffectTransport(() => writes++);
          await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
            const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
            const call = () => client.graphql(test.query, { input: test.input });
            if (exact) {
              await call();
              if ("fileChanges" in test.input) {
                const foreign = structuredClone(test.input);
                foreign.fileChanges = {
                  additions: [
                    { path: "src/a", contents: "YQ==" },
                    { path: "src/foreign", contents: "YQ==" },
                  ],
                };
                await assert.rejects(client.graphql(test.query, { input: foreign }));
              }
              if (Array.isArray(test.input.refUpdates)) await assert.rejects(client.graphql(test.query, { input: { ...test.input, refUpdates: [...test.input.refUpdates, { name: "refs/heads/foreign", beforeOid: REF, afterOid: REF }] } }));
            } else await assert.rejects(call());
          });
          assert.equal(writes, exact ? 1 : 0);
        }
      }
    });
    it("intersects actual worker-created child targets with immutable ancestor scope", () => {
      const parentTarget = { repository: REPOSITORY, host: "github.com", repository_id: "7", kind: "issue", resource_id: "100", number: "42" };
      for (const widened of [false, true]) {
        const f = productionFixture({ resource_scope: { version: 1, resources: [parentTarget] } });
        const target = widened ? { ...parentTarget, resource_id: "101", number: "43" } : parentTarget;
        const parentActor = { ...f.queue.workerActor, dispatch_id: f.assignment.dispatch_id, claim_handle: f.member.handle };
        const childWork = newChildWork(f.queue.state, parentActor, { resource_scope: { version: 1, resources: [target] }, effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 1, max: 1 }] } }, "bound-child", f.queue.at);
        f.queue.append("submit", { nodes: [childWork] }, parentActor);
        f.queue.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: f.queue.policy.limits.assignment_bytes }, f.queue.dispatcher);
        const child = [...f.queue.state.dispatches.values()].find(dispatch => dispatch.dispatch_id !== f.assignment.dispatch_id);
        assert.ok(child);
        const run = { ...f.queue.binding, run_id: "43" };
        f.queue.append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: child.dispatch_id, state: "started", sender: f.queue.dispatcher }] }, f.queue.dispatcher);
        f.queue.append(
          "dispatch",
          {
            operations: [
              {
                kind: "Dispatch",
                dispatch_id: child.dispatch_id,
                state: "bound",
                run,
                evidence: { kind: "reconciliation", source: "github_api", repository: REPOSITORY, workflow: WORKFLOW, ref: REF, principal: run.principal, checked_at: f.queue.at, run_id: "43", run_attempt: 1 },
              },
            ],
          },
          f.queue.dispatcher
        );
        const member = child.claims[0];
        const actor = { ...f.queue.workerActor, run_id: "43", dispatch_id: child.dispatch_id, claim_handle: member.handle };
        f.queue.append("finish", { dispatch_id: child.dispatch_id, claim_handle: member.handle, outcome: "completed" }, actor);
        const authorizeChild = () => validateClaimAuthority(f.queue.state, member.claim_id, context(actor, { ref: REF, event: "workflow_dispatch" }), { requireCompletion: true, resource: target });
        const authorizeCompactedChild = state => validateClaimAuthority(state, member.claim_id, context(actor, { ref: REF, event: "workflow_dispatch" }), { requireCompletion: true, resource: target });
        if (widened) {
          assert.throws(authorizeChild, /immutable Work resource_scope|positive immutable Work target binding/);
        } else assert.doesNotThrow(authorizeChild);
        const checkpoint = compactTransactions(f.queue.transactions, "c".repeat(40), f.queue.administrator, f.queue.at + 1);
        const restored = replayTransactions(checkpoint);
        if (widened) assert.throws(() => authorizeCompactedChild(restored), /immutable Work resource_scope|positive immutable Work target binding/);
        else assert.doesNotThrow(() => authorizeCompactedChild(restored));
      }
    });
    it("resolves GraphQL numeric targets independently and rejects missing Work bindings before mutations", async () => {
      const subject = { repository: REPOSITORY, host: "github.com", repository_id: "7", kind: "issue", resource_id: "100", number: "42" };
      for (const kind of ["Issue", "Repository"])
        for (const bound of [false, true]) {
          const f = productionFixture(
            kind === "Repository" && bound ? { resource_scope: { version: 1, resources: [{ repository: REPOSITORY, host: "github.com", repository_id: "7" }] } } : {},
            kind === "Issue" && bound ? subject : undefined
          );
          let writes = 0;
          const source = {
            rest: f.queue.githubClient.rest,
            graphql: async (query, variables) => {
              if (query.startsWith("mutation")) {
                writes++;
                return { updateIssue: { issue: { id: "I42" } } };
              }
              assert.deepEqual(variables.ids, [kind === "Issue" ? "I42" : "R7"]);
              return {
                nodes:
                  kind === "Issue"
                    ? [{ __typename: "Issue", id: "I42", databaseId: 100, number: 42, repository: { nameWithOwner: REPOSITORY, databaseId: 7 } }]
                    : [{ __typename: "Repository", id: "R7", databaseId: 7, nameWithOwner: REPOSITORY }],
              };
            },
          };
          await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
            const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
            const effect = client.graphql(kind === "Issue" ? "mutation($input:UpdateIssueInput!){updateIssue(input:$input){issue{id}}}" : "mutation($input:CreateIssueInput!){createIssue(input:$input){issue{id}}}", {
              input: kind === "Issue" ? { id: "I42", title: "Bound Work" } : { repositoryId: "R7", title: "Bound Work" },
            });
            if (bound) await effect;
            else await assert.rejects(effect, /positive immutable Work target binding/);
          });
          assert.equal(writes, bound ? 1 : 0);
        }
    });
    it("binds compiler adapter contexts in process without letting generic calls or another native target borrow them", async () => {
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const target = { ...repository, ref: "refs/heads/compiled", path: "src/a" };
      for (const bound of [false, true]) {
        const f = productionFixture(bound ? { resource_scope: { version: 1, resources: [repository] } } : {});
        let writes = 0;
        const source = {
          rest: {
            ...f.queue.githubClient.rest,
            git: {
              createRef: async _parameters => {
                writes++;
              },
            },
          },
        };
        await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize }, async () => {
          const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
          const call = () => client.rest.git.createRef({ owner: "owner", repo: "repo", ref: target.ref, sha: REF });
          assert.equal(currentClaimResourceEffects("rest.git.createRef"), null);
          for (const fabricated of [true, false, JSON.stringify(target), { ...target }]) assert.equal(isProtectedClaimResourceTarget(fabricated), false);
          const effect = withClaimResourceEffects([target], ["rest.git.createRef"], async () => {
            assert.equal(isProtectedClaimResourceTarget(target), true);
            assert.ok(Object.isFrozen(currentClaimResourceEffects("rest.git.createRef")));
            await call();
            await assert.rejects(client.rest.git.createRef({ owner: "owner", repo: "repo", ref: "refs/heads/foreign", sha: REF }), /conflicts with.*Claim target/);
          });
          if (bound) await effect;
          else await assert.rejects(effect, /positive immutable Work target binding/);
          await assert.rejects(call(), /positive immutable Work target binding/);
          assert.equal(currentClaimResourceEffects("rest.git.createRef"), null);
        });
        assert.equal(writes, bound ? 1 : 0);
      }
    });
    it("enforces frozen Work targets and no-write contracts on actual persistent SARIF uploads", async () => {
      const { main: scanning, verifyCodeScanningDelivery } = require("./work_queue_code_scanning.cjs");
      const repository = { repository: REPOSITORY, host: "github.com", repository_id: "7" };
      const previous = { repository: process.env.GITHUB_REPOSITORY, revision: process.env.GITHUB_SHA };
      try {
        process.env.GITHUB_REPOSITORY = REPOSITORY;
        process.env.GITHUB_SHA = REF;
        for (const mode of ["missing", "bound", "no-writes"]) {
          const f = productionFixture({
            effect_contract: mode === "no-writes" ? { kind: "none" } : { version: 1, outputs: [{ type: "create_code_scanning_alert", min: 1, max: 1 }] },
            ...(mode === "missing" ? {} : { resource_scope: { version: 1, resources: [repository] } }),
          });
          let writes = 0;
          let report;
          const effects = [];
          const source = {
            rest: {
              ...f.queue.githubClient.rest,
              repos: { ...f.queue.githubClient.rest.repos, getCommit: async () => ({ data: { sha: REF } }) },
              codeScanning: {
                uploadSarif: async parameters => {
                  writes++;
                  report = parseStrictJSON(require("node:zlib").gunzipSync(Buffer.from(parameters.sarif, "base64")).toString());
                  return { data: { id: "sarif-1" } };
                },
                getSarif: async () => ({ data: { processing_status: "complete", analyses_url: `https://api.github.com/repos/${REPOSITORY}/code-scanning/analyses?sarif_id=sarif-1` } }),
                getAnalysis: async () => ({ data: report }),
              },
            },
            request: async () => ({
              data: [{ id: 11, sarif_id: "sarif-1", commit_sha: REF, ref: "refs/heads/main", category: report.runs[0].automationDetails.id, tool: { name: report.runs[0].tool.driver.name }, results_count: 1 }],
            }),
          };
          await withClaimExecution({ assignment: f.assignment, claim_handle: f.member.handle, authorize: f.authorize, effects }, async () => {
            const client = wrapClaimEffectClient(source, { claim_handle: f.member.handle });
            const handler = await scanning({ "target-ref": "refs/heads/main" }, client);
            const message = { type: "create_code_scanning_alert", file: "src/a", line: 1, severity: "warning", message: "Bound native finding" };
            const effect = handler(message);
            if (mode === "bound") {
              const result = await effect;
              assert.equal(result.success, true);
              const liveProof = await verifyCodeScanningDelivery({ claim: f.member, result, github: client });
              await assert.rejects(
                withClaimResourceVerification(liveProof, () => client.rest.codeScanning.uploadSarif({ owner: "owner", repo: "repo", ref: "refs/heads/main" })),
                /verification is read-only/
              );
              closeClaimEffectChannel();
              const proof = await verifyCodeScanningDelivery({ claim: f.member, result, github: client });
              assert.equal(proof.verified, true);
              assert.ok("authority_resource" in proof);
              assert.ok(Object.isFrozen(proof));
              assert.ok(Object.isFrozen(proof.authority_resource));
              assert.equal(isProtectedClaimResourceTarget(proof.authority_resource), false);
              await assert.rejects(assertClaimAuthorized({ type: "work_queue_resource_verification", claim_handle: f.member.handle }, { resource: proof.authority_resource }), /positive immutable Work target binding/);
              await withClaimResourceVerification(proof, () => assertClaimAuthorized({ type: "work_queue_resource_verification", claim_handle: f.member.handle }, { resource: { ...proof.authority_resource } }));
              const delivery = await withClaimResourceVerification(proof, () =>
                require("./work_queue_delivery.cjs").inspectClaimDelivery({
                  assignment: f.assignment,
                  claim_handle: f.member.handle,
                  authorize: f.authorize,
                  github: client,
                  messages: [message],
                  results: [{ messageIndex: 0, success: true, result }],
                  effects,
                  verifyOutput: () => proof,
                })
              );
              assert.equal(delivery.verification, "verified");
              for (const fabricated of [true, false, JSON.stringify(proof), { ...proof }, parseStrictJSON(JSON.stringify(proof))]) {
                await assert.rejects(
                  withClaimResourceVerification(fabricated, () => {
                    throw new Error("untrusted receipt executed");
                  }),
                  /private in-process authority receipt/
                );
              }
              await assert.rejects(
                withClaimResourceVerification(proof, () => client.rest.codeScanning.uploadSarif({ owner: "owner", repo: "repo", ref: "refs/heads/main" })),
                /closed|read-only/
              );
            } else await assert.rejects(effect, mode === "missing" ? /positive immutable Work target binding/ : /prohibits ordinary resource writes/);
          });
          assert.equal(writes, mode === "bound" ? 1 : 0);
          assert.equal(effects.length, writes, "a reused scoped client records each actual upload exactly once");
        }
      } finally {
        if (previous.repository === undefined) delete process.env.GITHUB_REPOSITORY;
        else process.env.GITHUB_REPOSITORY = previous.repository;
        if (previous.revision === undefined) delete process.env.GITHUB_SHA;
        else process.env.GITHUB_SHA = previous.revision;
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
