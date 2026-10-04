const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { buildCorpus, refreshClosed, validatePlan, validateFile, island, reconcile, cleanupCompleted, publish, publishDashboard, dashboardBody, collectDiscussions } = require("./aw_issue_clustering_publish.cjs");

const repo = "github/gh-aw";
const cutoff = "2026-10-03T00:00:00Z";
const cluster = (members = [1, 2]) => ({
  key: "ledger-persistence",
  title: "Repair shared ledger persistence",
  summary: "Fix missing projections across production and smoke workflows.",
  fix: "Make append persistence update the projection and add coverage.",
  rationale: "Independent workflows demonstrate the same missing projection.",
  acceptance: ["A regression test checks persistence after a restart."],
  members,
  reports: [],
  impact: 5,
  confidence: 4,
  effort: 2,
});
const issue = (number, overrides = {}) => ({
  number,
  title: "Repair shared ledger persistence",
  body: "<!-- gh-aw-agentic-workflow: Smoke Ledgers -->",
  user: { type: "Bot" },
  state: "open",
  assignees: [],
  labels: [],
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
  html_url: `https://github.com/${repo}/issues/${number}`,
  ...overrides,
});
const owned = (number, value, overrides = {}) =>
  issue(number, {
    title: `[AW Top 10] 01 ${value.title}`,
    body: `Operator notes\n${island(value, 1, repo)}\nMore notes\n<!-- gh-aw-workflow-id: aw-issue-clustering -->`,
    ...overrides,
  });
const corpus = () => buildCorpus(repo, [issue(1), issue(2)], [], cutoff);
const plan = (value = cluster()) => ({ clusters: [value], deferred: [] });

test("short but meaningful deferral reasons pass; blank and metadata injections fail", () => {
  const data = corpus();
  const value = { clusters: [cluster([1])], deferred: [{ number: 2, reason: "Duplicate" }], shortfall_reason: "Only one actionable assignment is supported." };
  assert.equal(validatePlan(value, data).length, 1);
  for (const reason of ["  ", "<!-- injected -->"]) {
    assert.throws(() => validatePlan({ ...value, deferred: [{ number: 2, reason }] }, data), /deferral reason/);
  }
});

test("unverified or uncovered members and changed assigned scopes are rejected", () => {
  const data = corpus();
  assert.throws(() => validatePlan(plan(cluster([1, 99])), data), /unverified/);
  assert.throws(() => validatePlan(plan(cluster([1])), data), /Every eligible/);
  data.managed = [owned(20, cluster(), { assignees: [{ login: "operator" }] })];
  const reordered = { ...cluster(), effort: 2 };
  assert.equal(validatePlan({ ...plan(reordered), shortfall_reason: "Only one coherent assignment is supported." }, data).length, 1);
  assert.throws(() => validatePlan(plan({ ...cluster(), effort: 3 }), data), /unchanged/);
});

test("clipped issue titles and prose are rejected, but assigned scopes remain frozen", () => {
  const data = corpus();
  for (const [field, value] of [
    ["title", "Make AI credits accounting resilient to unknown model pricin"],
    ["summary", "Several workflows fail in post-run ledger/repo-memory push j"],
    ["fix", "Make push_repo_memory validation non-destructive (never remove the working directory), surface the f"],
    ["rationale", "Three failed-jobs reports share the push job "],
    ["acceptance", ["Calling push_repo_memory mid-session leaves files "]],
  ]) {
    const clipped = { ...cluster(), [field]: value };
    assert.throws(() => validatePlan(plan(clipped), data), /complete phrase|sentence punctuation/);
    data.managed = [owned(20, clipped, { assignees: [{ login: "operator" }] })];
    assert.equal(validatePlan({ ...plan(clipped), shortfall_reason: "Only one actionable assignment is supported." }, data).length, 1);
    data.managed = [];
  }
});

test("only verified mid-run closures are removed before validating remaining members", () => {
  const data = buildCorpus(
    repo,
    [
      issue(1),
      issue(2, {
        state: "closed",
        closed_at: "2026-10-03T01:00:00Z",
      }),
    ],
    [],
    cutoff
  );
  assert.deepEqual(refreshClosed(plan(), data).clusters[0].members, [1]);
  assert.deepEqual(validatePlan(refreshClosed(plan(), data), data)[0].members, [1]);
  assert.throws(() => validatePlan(plan(cluster([1, 3])), data), /unverified/);
});

test("mid-run assignments restore the trusted scope before validation without changing the proposal", () => {
  const frozen = cluster();
  const data = corpus();
  data.managed = [owned(20, frozen, { assignees: [{ login: "operator" }], updated_at: "2026-10-03T01:00:00Z" })];
  const proposal = plan({ ...frozen, title: "Revise the failure diagnostics assignment", fix: "Replace the proposed implementation and acceptance criteria.", acceptance: ["New criteria were drafted before the operator assigned it."] });
  const messages = [];
  const refreshed = refreshClosed(proposal, data, { info: message => messages.push(message) });
  assert.deepEqual(refreshed.clusters, [frozen]);
  assert.notDeepEqual(proposal.clusters, [frozen]);
  assert.deepEqual(validatePlan({ ...refreshed, shortfall_reason: "Only one actionable assignment is supported." }, data), [frozen]);
  assert.match(messages[0], /#20.*preserved.*frozen scope/);
});

test("frozen scopes with closed sources are retained and existing assignments still reject changes", () => {
  const frozen = cluster();
  const data = buildCorpus(repo, [issue(1), owned(20, frozen, { assignees: [{ login: "operator" }] })], [], cutoff);
  data.recently_closed = [2];
  const value = { ...plan(frozen), shortfall_reason: "Only one actionable assignment is supported." };
  assert.deepEqual(refreshClosed(value, data).clusters, [frozen]);
  assert.deepEqual(validatePlan(value, data), [frozen]);
  assert.throws(() => validatePlan(refreshClosed(plan({ ...frozen, effort: 3 }), data), data), /unchanged/);
  assert.throws(() => validatePlan({ clusters: [], deferred: [{ number: 1, reason: "Deferred." }] }, data), /Assigned clusters/);
});

test("restoring a mid-run assignment does not hide coverage gaps or duplicate members", () => {
  const data = buildCorpus(repo, [issue(1), issue(2), issue(3)], [], cutoff);
  data.managed = [owned(20, cluster([1]), { assignees: [{ login: "operator" }], updated_at: cutoff })];
  assert.throws(() => validatePlan(refreshClosed(plan(cluster([1, 2, 3])), data), data), /Every eligible/);
  const proposal = { clusters: [cluster([1]), { ...cluster([1, 2, 3]), key: "other-fix" }], deferred: [] };
  assert.throws(() => validatePlan(refreshClosed(proposal, data), data), /multiple clusters/);
});

test("empty candidates retire after verified closures, but humans and pre-run closures cannot disappear", () => {
  const data = buildCorpus(repo, [issue(1, { state: "closed", closed_at: cutoff }), issue(2), issue(3, { state: "closed", closed_at: "2026-10-02T00:00:00Z" })], [], cutoff);
  const value = { clusters: [cluster([1])], deferred: [{ number: 2, reason: "Needs more evidence." }] };
  const refreshed = refreshClosed(value, data);
  assert.deepEqual(refreshed.clusters, []);
  assert.match(refreshed.shortfall_reason, /lost all eligible sources/);
  assert.deepEqual(validatePlan(refreshed, data), []);
  assert.throws(() => validatePlan(refreshClosed(plan(cluster([2, 3])), data), data), /unverified/);
});

test("deterministic ranking, ten-item cap, stable identity and report provenance remain enforced", () => {
  const data = buildCorpus(
    repo,
    Array.from({ length: 11 }, (_, index) => issue(index + 1)),
    [],
    cutoff
  );
  const value = { clusters: Array.from({ length: 10 }, (_, index) => ({ ...cluster([index + 1]), key: `fix-${index + 1}` })), deferred: [{ number: 11, reason: "Needs more evidence." }] };
  assert.equal(validatePlan(value, data).length, 10);
  assert.throws(() => validatePlan({ clusters: [...value.clusters, { ...cluster([11]), key: "fix-eleven" }], deferred: [] }, data), /At most ten/);
  const smaller = corpus();
  assert.throws(() => validatePlan(plan({ ...cluster(), reports: [99] }), smaller), /Report lacks AW/);
  assert.throws(() => validatePlan(plan(cluster([1, 1])), smaller), /unique/);
  smaller.managed = [owned(20, cluster([3]))];
  assert.throws(() => validatePlan(plan(), smaller), /repurpose/);
  smaller.managed = [];
  const low = { ...cluster([2]), key: "local-followup", impact: 1 };
  const high = cluster([1]);
  assert.deepEqual(
    validatePlan({ clusters: [low, high], deferred: [] }, smaller).map(item => item.key),
    [high.key, low.key]
  );
});

test("linked discussion URLs match the exact repository name", async () => {
  let queries = 0;
  const github = {
    graphql: async () => {
      queries++;
      return { repository: { discussions: { nodes: [], pageInfo: { hasNextPage: false } } } };
    },
  };
  const sources = [issue(1, { body: "https://github.com/github/ghXaw/discussions/12" })];
  assert.deepEqual(await collectDiscussions(github, "github", "gh.aw", sources, { warning() {} }), []);
  assert.equal(queries, 1);
});

test("live dashboard discovery uses a valid Octokit GraphQL variable", async () => {
  const calls = [];
  const github = {
    graphql: async (document, variables) => {
      assert.ok(!Object.hasOwn(variables, "query"));
      calls.push([document, variables]);
      if (document.includes("search(")) {
        assert.match(document, /search\(query:\$searchTerm/);
        assert.equal(variables.searchTerm, 'repo:github/gh-aw in:title "AW Essential 10"');
        return { search: { pageInfo: { hasNextPage: false }, nodes: [] } };
      }
      if (document.includes("discussionCategories(")) {
        return { repository: { id: "repo-id", discussionCategories: { nodes: [{ id: "category-id", name: "Audits" }], pageInfo: { hasNextPage: false } } } };
      }
      return { createDiscussion: { discussion: { id: "discussion-id" } } };
    },
  };
  await publishDashboard(github, "github", "gh-aw", "A valid dashboard body", false, { info() {} });
  assert.equal(calls.length, 3);
  assert.match(calls[2][0], /createDiscussion/);
});

test("staged reconciliation never writes, and preserves operator text and safe prose", async () => {
  const value = cluster();
  value.summary += " @maintainer /close https://example.com <script>";
  const item = owned(20, value);
  const data = corpus();
  data.managed = [item];
  const github = {
    rest: {
      issues: {
        get: () => {
          throw new Error("unexpected mutation or live read");
        },
        update: () => {
          throw new Error("unexpected mutation");
        },
      },
    },
  };
  const result = await reconcile(github, "github", "gh-aw", [value], data, true, { info() {} });
  assert.equal(result[0][2], item.html_url);
  assert.match(island(value, 1, repo), /@​maintainer \/​close \[URL omitted; use evidence links below\] &lt;script&gt;/);
  assert.match(item.body, /Operator notes/);
});

test("live queue refuses an eleventh issue", async () => {
  const data = corpus();
  const summaries = Array.from({ length: 10 }, (_, i) => owned(i + 20, cluster([1])));
  const github = {
    paginate: async () => summaries,
    rest: {
      issues: {
        listForRepo() {},
        create: () => {
          throw new Error("unexpected create");
        },
      },
    },
  };
  await assert.rejects(reconcile(github, "github", "gh-aw", [cluster()], data, false, { info() {} }), /eleventh/);
});

test("new summaries use attributed issue creation and preserve managed metadata", async () => {
  const { setupGlobals, createIssue } = require("../../actions/setup/js/create-issue.cjs");
  const data = corpus();
  const calls = [];
  const github = {
    hook: { before() {} },
    paginate: async () => [],
    rest: {
      issues: {
        listForRepo() {},
        async create(parameters) {
          calls.push(parameters);
          return { data: issue(20, parameters) };
        },
      },
    },
  };
  const env = {
    GH_AW_WORKFLOW_NAME: "AW Essential Issue Clustering",
    GH_AW_WORKFLOW_ID: "aw-issue-clustering",
    GH_AW_CALLER_WORKFLOW_ID: "github/gh-aw/aw-issue-clustering",
    GH_AW_PROMPTS_DIR: path.resolve(__dirname, "../../actions/setup/md"),
    GH_AW_SAFE_OUTPUTS_STAGED: "false",
  };
  const previousEnv = Object.fromEntries(Object.keys(env).map(key => [key, process.env[key]]));
  const globalKeys = ["core", "github", "context", "exec", "io", "getOctokit", "runtimeFeatures", "hasRuntimeFeature", "getRuntimeFeatureValue"];
  const previousGlobals = Object.fromEntries(globalKeys.map(key => [key, global[key]]));
  const core = { info() {}, debug() {}, warning() {}, error() {} };
  Object.assign(process.env, env);
  try {
    setupGlobals(core, github, { repo: { owner: "github", repo: "gh-aw" }, runId: 123, payload: {} }, {}, {}, () => github);
    const result = await reconcile(github, "github", "gh-aw", [cluster()], data, false, core, createIssue);
    assert.equal(calls.length, 1);
    assert.ok(calls[0].body.includes(island(cluster(), 1, repo)));
    assert.match(calls[0].body, /> Generated by \[AW Essential Issue Clustering\]/);
    assert.match(calls[0].body, /gh-aw-agentic-workflow: AW Essential Issue Clustering/);
    assert.ok(calls[0].body.includes("https://github.com/github/gh-aw/actions/runs/123"));
    assert.equal(calls[0].title, "[AW Top 10] 01 Repair shared ledger persistence");
    assert.deepEqual(calls[0].labels, ["aw-essential", "automation", "agentic-workflows", "cookie"]);
    assert.equal(result[0][2], `https://github.com/${repo}/issues/20`);
    assert.equal(buildCorpus(repo, [issue(20, calls[0])], [], cutoff).managed.length, 1);
  } finally {
    for (const [key, value] of Object.entries(previousEnv)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    for (const [key, value] of Object.entries(previousGlobals)) {
      if (value === undefined) delete global[key];
      else global[key] = value;
    }
  }
});

test("live reconciliation refuses a concurrent assignment without writing", async () => {
  const value = cluster();
  const item = owned(20, value, { labels: [{ name: "aw-essential" }] });
  const data = corpus();
  data.managed = [item];
  const github = {
    rest: {
      issues: {
        get: async () => ({ data: { ...item, assignees: [{ login: "operator" }], updated_at: "2026-10-04T00:00:00Z" } }),
        update: () => {
          throw new Error("unexpected mutation");
        },
      },
    },
  };
  await assert.rejects(reconcile(github, "github", "gh-aw", [value], data, false, { info() {} }), /Summary edited or assigned/);
});

test("unchanged managed issue is not rewritten", async () => {
  const value = cluster();
  const item = owned(20, value, { labels: [{ name: "aw-essential" }] });
  const data = corpus();
  data.managed = [item];
  let reads = 0;
  const github = {
    rest: {
      issues: {
        get: async () => {
          reads++;
          return { data: item };
        },
        update: () => {
          throw new Error("unexpected mutation");
        },
        addLabels: () => {
          throw new Error("unexpected relabel");
        },
      },
    },
  };
  const result = await reconcile(github, "github", "gh-aw", [value], data, false, { info() {} });
  assert.equal(reads, 1);
  assert.equal(result[0][2], item.html_url);
});

test("assigned summary receives its label without changing frozen scope or operator labels", async () => {
  const value = cluster();
  const item = owned(20, value, { assignees: [{ login: "operator" }], labels: [{ name: "cookie" }] });
  const data = corpus();
  data.managed = [item];
  let labelsAdded = false;
  const github = {
    rest: {
      issues: {
        get: async () => ({ data: labelsAdded ? { ...item, labels: [...item.labels, { name: "aw-essential" }] } : item }),
        addLabels: async params => {
          assert.deepEqual(params.labels, ["aw-essential"]);
          labelsAdded = true;
        },
        update: () => {
          throw new Error("unexpected mutation of assigned scope");
        },
      },
    },
  };
  const result = await reconcile(github, "github", "gh-aw", [value], data, false, { info() {} });
  assert.equal(labelsAdded, true);
  assert.equal(result[0][2], item.html_url);
});

test("reconciliation retires before creation and labels replacements", async () => {
  const retired = owned(20, { ...cluster(), key: "old-summary" });
  const data = corpus();
  data.managed = [retired];
  const calls = [];
  const github = {
    paginate: async () => [],
    rest: {
      issues: {
        listForRepo() {},
        get: async () => ({ data: retired }),
        update: async params => {
          assert.equal(params.state_reason, "not_planned");
          calls.push("retire");
        },
        create: async params => {
          assert.ok(params.labels.includes("aw-essential"));
          calls.push("create");
          return { data: { html_url: "https://github.com/github/gh-aw/issues/21" } };
        },
      },
    },
  };
  const createIssue = async parameters => {
    const { data: created } = await github.rest.issues.create(parameters);
    return { staged: false, issue: created };
  };
  await reconcile(github, "github", "gh-aw", [cluster()], data, false, { info() {} }, createIssue);
  assert.deepEqual(calls, ["retire", "create"]);
});

test("dashboard updates only owned bot discussion and preserves operator text", async () => {
  const body = `<!-- aw-essential-start -->\nNew table\n<!-- aw-essential-end -->\n<!-- gh-aw-workflow-id: aw-issue-clustering -->`;
  const previous = body.replace("New table", "Old table");
  const calls = [];
  const github = {
    graphql: async (document, variables) => {
      calls.push([document, variables]);
      if (document.includes("search("))
        return {
          search: {
            pageInfo: { hasNextPage: false },
            nodes: [
              { id: "human", title: "AW Essential 10", body: previous, closed: false, author: { __typename: "User" } },
              { id: "owned", title: "AW Essential 10", body: `Operator note.\n${previous}`, closed: false, author: { __typename: "Bot" } },
            ],
          },
        };
      assert.equal(variables.id, "owned");
      assert.ok(variables.body.startsWith("Operator note."));
      return {};
    },
  };
  await publishDashboard(github, "github", "gh-aw", body, false, { info() {} });
  assert.equal(calls.length, 2);
  assert.match(calls[1][0], /updateDiscussion/);
});

test("denied dashboard edits fall back explicitly; unrelated errors and comment failures propagate", async () => {
  const body = `<!-- aw-essential-start -->\nNew table\n<!-- aw-essential-end -->\n<!-- gh-aw-workflow-id: aw-issue-clustering -->`;
  const current = { id: "owned", title: "AW Essential 10", body: body.replace("New table", "Old table"), closed: false, author: { __typename: "Bot" } };
  for (const failure of [undefined, new Error("rate limit exceeded"), new Error("comment failed")]) {
    const calls = [];
    const github = {
      graphql: async (document, variables) => {
        calls.push([document, variables]);
        if (document.includes("search(")) return { search: { pageInfo: { hasNextPage: false }, nodes: [current] } };
        if (document.includes("updateDiscussion")) throw failure?.message === "rate limit exceeded" ? failure : new Error("Resource not accessible by integration");
        if (failure) throw failure;
        return {};
      },
    };
    if (failure) await assert.rejects(publishDashboard(github, "github", "gh-aw", body, false, { info() {} }), failure);
    else await publishDashboard(github, "github", "gh-aw", body, false, { info() {} });
    assert.equal(calls.length, failure?.message === "rate limit exceeded" ? 2 : 3);
    if (calls.length === 3) assert.match(calls[2][0], /addDiscussionComment/);
  }
});

async function withPlanArtifact(value, callback) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-clustering-"));
  const filename = path.join(dir, "agent-output.json");
  const planFile = path.join(dir, "agent/aw-issue-clustering/plan.json");
  fs.mkdirSync(path.dirname(planFile), { recursive: true });
  fs.writeFileSync(planFile, JSON.stringify(value));
  fs.writeFileSync(filename, JSON.stringify({ items: [{ type: "publish_essential_issues", plan_path: "agent/aw-issue-clustering/plan.json" }] }));
  const previous = process.env.GH_AW_AGENT_OUTPUT;
  const staged = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  process.env.GH_AW_AGENT_OUTPUT = filename;
  process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
  try {
    await callback({ dir, filename, planFile });
  } finally {
    if (previous === undefined) delete process.env.GH_AW_AGENT_OUTPUT;
    else process.env.GH_AW_AGENT_OUTPUT = previous;
    if (staged === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    else process.env.GH_AW_SAFE_OUTPUTS_STAGED = staged;
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

function previewGithub(issues) {
  return {
    paginate: async (fn, params) => (params.state === "open" ? issues : params.state === "closed" ? [] : [{ name: "aw-essential" }]),
    graphql: async () => ({ repository: { discussions: { nodes: [], pageInfo: { hasNextPage: false } } } }),
    rest: { actions: { getWorkflowRun: async () => ({ data: { created_at: cutoff } }) }, issues: { listForRepo() {}, listLabelsForRepo() {} } },
  };
}

test("github-script entry point revalidates live evidence and previews short deferrals without writes", async () => {
  const value = { clusters: [cluster([1])], deferred: [{ number: 2, reason: "Duplicate" }], shortfall_reason: "Only one actionable assignment is supported." };
  const core = {
    info() {},
    warning() {},
    summary: {
      addRaw(body) {
        assert.match(body, /Duplicate/);
        return { write: async () => {} };
      },
    },
  };
  await withPlanArtifact(value, () => publish({ github: previewGithub([issue(1), issue(2)]), context: { repo: { owner: "github", repo: "gh-aw" }, runId: 123 }, core }));
});

test("450-source plans larger than 10 KB publish from artifacts with full deferral reasons", async () => {
  const sources = Array.from({ length: 450 }, (_, index) => issue(index + 1));
  const value = {
    clusters: Array.from({ length: 10 }, (_, index) => ({ ...cluster([index + 1]), key: `coherent-fix-${index + 1}` })),
    deferred: sources.slice(10).map(item => ({ number: item.number, reason: "This finding needs current reproducible evidence before assignment." })),
  };
  assert.ok(Buffer.byteLength(JSON.stringify(value)) > 10240);
  let summary;
  const core = {
    info() {},
    summary: {
      addRaw(body) {
        summary = body;
        return { write: async () => {} };
      },
    },
  };
  await withPlanArtifact(value, async ({ dir, planFile }) => {
    const corpusFile = path.join(dir, "corpus.json");
    fs.writeFileSync(corpusFile, JSON.stringify(buildCorpus(repo, sources, [], cutoff)));
    assert.equal(validateFile(["--repo", repo, "--corpus", corpusFile, "--plan", planFile]), "Validated 10 clusters; complete coverage of 450 AW issues");
    await publish({ github: previewGithub(sources), context: { repo: { owner: "github", repo: "gh-aw" }, runId: 123 }, core });
  });
  assert.match(summary, /Coverage: 450 AW issues/);
  assert.equal((summary.match(/This finding needs current reproducible evidence/g) || []).length, 440);
});

test("publisher rejects arbitrary paths, duplicate calls, symlinks, oversized and missing artifacts", async () => {
  await withPlanArtifact(plan(), async ({ filename, planFile }) => {
    for (const plan_path of ["../../credentials.json", "/tmp/plan.json", "other.json"]) {
      fs.writeFileSync(filename, JSON.stringify({ items: [{ type: "publish_essential_issues", plan_path }] }));
      await assert.rejects(publish({ context: { repo: { owner: "github", repo: "gh-aw" } } }), /artifact path/);
    }
    const item = { type: "publish_essential_issues", plan_path: "agent/aw-issue-clustering/plan.json" };
    fs.writeFileSync(filename, JSON.stringify({ items: [item, item] }));
    await assert.rejects(publish({ context: { repo: { owner: "github", repo: "gh-aw" } } }), /Exactly one/);
    fs.writeFileSync(filename, JSON.stringify({ items: [item] }));
    fs.writeFileSync(planFile, "x".repeat(1048577));
    await assert.rejects(publish({ context: { repo: { owner: "github", repo: "gh-aw" } } }), /1 MiB/);
    fs.unlinkSync(planFile);
    fs.symlinkSync(filename, planFile);
    await assert.rejects(publish({ context: { repo: { owner: "github", repo: "gh-aw" } } }), /regular JSON/);
    fs.unlinkSync(planFile);
    await assert.rejects(publish({ context: { repo: { owner: "github", repo: "gh-aw" } } }), /ENOENT/);
  });
});

test("github-script publisher preserves a scope assigned after collection while refreshing the dashboard", async () => {
  const frozen = cluster();
  const assigned = owned(20, frozen, { assignees: [{ login: "operator" }], updated_at: "2026-10-03T01:00:00Z" });
  const value = { ...plan({ ...frozen, fix: "An agent proposed this change before the scope was assigned." }), shortfall_reason: "Only one actionable assignment is supported." };
  const core = {
    info() {},
    summary: {
      addRaw(body) {
        assert.match(body, /Essential AW fixes/);
        assert.ok(!body.includes("An agent proposed"));
        return { write: async () => {} };
      },
    },
  };
  await withPlanArtifact(value, () => publish({ github: previewGithub([issue(1), issue(2), assigned]), context: { repo: { owner: "github", repo: "gh-aw" }, runId: 123 }, core }));
  assert.ok(dashboardBody([[1, frozen, assigned.html_url]], value, corpus(), 123).includes(frozen.title));
});

test("cleanup tracks completed summaries and closes only unchanged verified AW sources", async () => {
  const completed = owned(20, cluster([1, 2, 3, 4, 5, 6]), { state: "closed", state_reason: "completed", closed_at: cutoff });
  const retired = owned(21, { ...cluster([7]), key: "retired-fix" }, { state: "closed", state_reason: "not_planned", closed_at: cutoff });
  const sources = [issue(1), issue(2, { user: { type: "User" } }), issue(3, { updated_at: "2026-10-03T01:00:00Z" }), issue(4, { title: "[WIP] Smoke" }), issue(5, { body: "<!-- gh-aw-group: Deep Report -->" }), issue(6), issue(7)];
  const active = owned(22, { ...cluster([6]), key: "active-fix" }, { assignees: [{ login: "operator" }] });
  const data = buildCorpus(repo, [...sources, completed, retired, active], [], cutoff);
  assert.deepEqual(
    data.cleanup.map(item => item.number),
    [1]
  );
  assert.deepEqual(
    data.issues.map(item => item.number),
    [3, 7]
  );
  const writes = [];
  const github = {
    rest: {
      issues: {
        get: async params => ({ data: params.issue_number === 20 ? completed : sources.find(item => item.number === params.issue_number) }),
        update: async params => writes.push(params),
      },
    },
  };
  const cleaned = await cleanupCompleted(github, "github", "gh-aw", data, false, { info() {} });
  assert.deepEqual(writes, [{ owner: "github", repo: "gh-aw", issue_number: 1, state: "closed", state_reason: "completed" }]);
  assert.deepEqual(
    cleaned.map(item => item.number),
    [1]
  );
  assert.match(dashboardBody([], { deferred: [] }, data, 123, cleaned), /Cleanup: closed 1/);
});

test("cleanup is staged, idempotent, and cancels closures when summaries or sources change", async () => {
  const completed = owned(20, cluster([1]), { state: "closed", state_reason: "completed", closed_at: cutoff });
  const source = issue(1);
  const data = buildCorpus(repo, [source, completed], [], cutoff);
  for (const [summary, liveSource, expected] of [
    [completed, source, 1],
    [{ ...completed, state: "open" }, source, 0],
    [{ ...completed, state_reason: "not_planned" }, source, 0],
    [{ ...completed, body: completed.body.replace("ledger-persistence", "different-scope") }, source, 0],
    [completed, { ...source, state: "closed" }, 0],
    [completed, { ...source, updated_at: cutoff }, 0],
    [completed, { ...source, user: { type: "User" } }, 0],
    [completed, { ...source, pull_request: {} }, 0],
  ]) {
    const github = {
      rest: {
        issues: {
          get: async params => ({ data: params.issue_number === 20 ? summary : liveSource }),
          update: () => {
            throw new Error("staged cleanup must not write");
          },
        },
      },
    };
    assert.equal((await cleanupCompleted(github, "github", "gh-aw", data, true, { info() {} })).length, expected);
  }
  const github = {
    rest: {
      issues: {
        get: async params => ({ data: params.issue_number === 20 ? completed : source }),
        update: async () => {
          throw new Error("permission denied");
        },
      },
    },
  };
  await assert.rejects(cleanupCompleted(github, "github", "gh-aw", data, false, { info() {} }), /permission denied/);
});

test("sources resolved by summaries completed mid-run are removed from stale proposals", () => {
  const completed = owned(20, cluster(), { state: "closed", state_reason: "completed", closed_at: "2026-10-03T01:00:00Z" });
  const data = buildCorpus(repo, [issue(1), issue(2), completed], [], cutoff);
  const refreshed = refreshClosed(plan(), data);
  assert.deepEqual(refreshed.clusters, []);
  assert.deepEqual(validatePlan(refreshed, data), []);
  assert.deepEqual(
    data.cleanup.map(item => item.number),
    [1, 2]
  );
});

test("cleanup-only github-script publication previews closures with an empty validated plan", async () => {
  const completed = owned(20, cluster([1]), { state: "closed", state_reason: "completed", closed_at: cutoff });
  const source = issue(1);
  const github = previewGithub([source]);
  github.paginate = async (fn, params) => (params.state === "open" ? [source] : params.state === "closed" ? [completed] : [{ name: "aw-essential" }]);
  github.rest.issues.get = async params => ({ data: params.issue_number === 20 ? completed : source });
  const core = {
    info() {},
    summary: {
      addRaw(body) {
        assert.match(body, /Cleanup: would close 1/);
        return { write: async () => {} };
      },
    },
  };
  await withPlanArtifact({ clusters: [], deferred: [] }, () => publish({ github, context: { repo: { owner: "github", repo: "gh-aw" }, runId: 123 }, core }));
});
