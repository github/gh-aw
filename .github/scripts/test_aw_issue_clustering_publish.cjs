const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { buildCorpus, refreshClosed, validatePlan, island, reconcile, publish } = require("./aw_issue_clustering_publish.cjs");

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

test("github-script entry point revalidates live evidence and previews short deferral without writes", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-clustering-"));
  const filename = path.join(dir, "agent-output.json");
  const value = { clusters: [cluster([1])], deferred: [{ number: 2, reason: "Duplicate" }], shortfall_reason: "Only one actionable assignment is supported." };
  fs.writeFileSync(filename, JSON.stringify({ items: [{ type: "publish_essential_issues", plan: JSON.stringify(value) }] }));
  const previous = process.env.GH_AW_AGENT_OUTPUT;
  const staged = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  process.env.GH_AW_AGENT_OUTPUT = filename;
  process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
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
  const github = {
    paginate: async (fn, params) => (params.state === "open" ? [issue(1), issue(2)] : params.state === "closed" ? [] : [{ name: "aw-essential" }]),
    graphql: async () => ({
      repository: {
        discussions: {
          nodes: [],
          pageInfo: { hasNextPage: false },
        },
      },
    }),
    rest: { actions: { getWorkflowRun: async () => ({ data: { created_at: cutoff } }) }, issues: { listForRepo() {}, listLabelsForRepo() {} } },
  };
  try {
    await publish({ github, context: { repo: { owner: "github", repo: "gh-aw" }, runId: 123 }, core });
  } finally {
    if (previous === undefined) delete process.env.GH_AW_AGENT_OUTPUT;
    else process.env.GH_AW_AGENT_OUTPUT = previous;
    if (staged === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    else process.env.GH_AW_SAFE_OUTPUTS_STAGED = staged;
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
