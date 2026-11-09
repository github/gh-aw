import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs, { readFileSync } from "node:fs";
import { test } from "node:test";
import { sanitizeContent } from "../actions/setup/js/sanitize_content.cjs";
import {
  activeAgent,
  approvableRuns,
  buildNudgeReport,
  buildQueue,
  checkRateLimit,
  classify,
  fetchActiveRuns,
  fetchCandidate,
  fetchQueue,
  formatAssignmentRequest as formatAssignmentBody,
  formatCIFollowUp as formatCIFollowUpBody,
  formatNudge as formatNudgeBody,
  formatSousChefData,
  formatSummary,
  gh,
  readSousChefData,
  sousChefDataSchema,
} from "./pr-sous-chef.mjs";
import { validateItem, resetValidationConfigCache } from "../actions/setup/js/safe_output_type_validator.cjs";

function publishedBody(body, data, collapseData = true) {
  const previous = process.env.GH_AW_VALIDATION_CONFIG;
  process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify({
    add_comment: {
      fields: { body: { type: "string", required: true, sanitize: true } },
      dataEnabled: true,
      dataSchema: sousChefDataSchema,
    },
  });
  resetValidationConfigCache();
  try {
    const result = validateItem({ type: "add_comment", body, data }, "add_comment", 1, { collapseData, allowedAliases: ["copilot"] });
    assert.ok(result.isValid, result.error);
    assert.deepEqual(result.normalizedItem.data, data);
    return result.normalizedItem.body;
  } finally {
    if (previous === undefined) delete process.env.GH_AW_VALIDATION_CONFIG;
    else process.env.GH_AW_VALIDATION_CONFIG = previous;
    resetValidationConfigCache();
  }
}

function formatNudge(pr) {
  const body = formatNudgeBody(pr);
  return body ? publishedBody(body, formatSousChefData(pr, "running")) : null;
}

function formatAssignmentRequest(pr) {
  const body = formatAssignmentBody(pr);
  return body ? publishedBody(body, formatSousChefData(pr, "requested")) : null;
}

function formatCIFollowUp(pr) {
  const body = formatCIFollowUpBody(pr);
  return body ? publishedBody(body, formatSousChefData(pr, "ci-follow-up")) : null;
}

const now = Date.parse("2026-10-03T18:00:00Z");
const recent = "2026-10-03T16:00:00Z";
const pr = {
  number: 42,
  title: "Fix a bug",
  state: "OPEN",
  author: { login: "Copilot" },
  headRefOid: "abc",
  headCommittedAt: recent,
  headRefName: "copilot/fix",
  baseRefName: "main",
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: recent,
  changedFiles: 2,
  mergeStateStatus: "CONFLICTING",
  statusCheckRollup: [],
};
const failingPr = {
  ...pr,
  mergeStateStatus: "BLOCKED",
  statusCheckRollup: [{ name: "CGO", status: "COMPLETED", conclusion: "FAILURE" }],
};
const waitingRun = {
  id: 7,
  workflow_id: 12,
  name: "CGO",
  path: ".github/workflows/cgo.yml",
  event: "pull_request",
  status: "waiting",
  head_branch: pr.headRefName,
  head_sha: pr.headRefOid,
  pull_requests: [{ number: pr.number }],
};

function detail(threads = []) {
  return {
    reviewThreads: { nodes: threads, pageInfo: { hasNextPage: false } },
    reviews: { nodes: [], pageInfo: { hasPreviousPage: false } },
  };
}

function nudge(state, at = "2026-10-03T16:30:00Z") {
  return {
    id: 123,
    user: { login: "gh-aw-bot" },
    created_at: at,
    body: `@copilot fix conflicts\nSous-chef state: ${state}`,
  };
}

function response() {
  return {
    id: 124,
    user: { login: "Copilot" },
    created_at: "2026-10-03T17:00:00Z",
    body: "Work completed; please recheck the blockers",
  };
}

function thread(overrides = {}) {
  return {
    id: "PRRT_example",
    isResolved: false,
    comments: {
      pageInfo: { hasNextPage: false },
      nodes: [
        {
          author: { login: "reviewer" },
          authorAssociation: "MEMBER",
          body: "Fix the regression",
          createdAt: recent,
          updatedAt: recent,
          url: "https://github.com/github/gh-aw/pull/42#discussion_r1",
        },
      ],
    },
    ...overrides,
  };
}

test("unchanged implementation work never gets a repeat poke, even after the idle interval", () => {
  const first = classify(failingPr, detail(), [], [], now);
  assert.ok(first.state_fingerprint);
  const comments = [nudge(first.state_fingerprint)];
  assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
  assert.equal(classify(failingPr, detail(), comments, [], now + 24 * 60 * 60 * 1000).skip_reason, "unchanged");
});

test("conflicting or behind branches require an intervening comment before another idle nudge", () => {
  for (const mergeStateStatus of ["CONFLICTING", "BEHIND"]) {
    const candidate = { ...pr, mergeStateStatus };
    const first = classify(candidate, detail(), [], [], now);
    assert.equal(classify(candidate, detail(), [nudge(first.state_fingerprint)], [], now).skip_reason, "last_comment_sous_chef");
    assert.ok(classify(candidate, detail(), [nudge(first.state_fingerprint), response()], [], now).state_fingerprint);
    assert.equal(classify(candidate, detail(), [nudge(first.state_fingerprint, "2026-10-03T17:55:00Z")], [], now).skip_reason, "not_idle");
  }
  for (const mergeStateStatus of ["BLOCKED", "DIRTY", "UNKNOWN", "CLEAN"]) {
    const candidate = { ...failingPr, mergeStateStatus };
    const first = classify(candidate, detail(), [], [], now);
    assert.equal(classify(candidate, detail(), [nudge(first.state_fingerprint)], [], now).skip_reason, "unchanged");
  }
});

function stripHtmlComments(text) {
  let previous;
  do {
    previous = text;
    text = text.replace(/<!--[\s\S]*?--!?>|<!--/g, "");
  } while (text !== previous);
  return text;
}

test("HTML comment fixture removal stabilizes complete and unterminated markers together", () => {
  for (const [input, expected] of [
    ["", ""],
    ["visible text", "visible text"],
    ["before<!-- hidden -->after", "beforeafter"],
    ["before<!-- hidden --!>after", "beforeafter"],
    ["before<!-- hidden\non another line -->after", "beforeafter"],
    ["before<!-- one --><!-- two -->after", "beforeafter"],
    ["before<!-- outer <!-- inner --> tail -->after", "before tail -->after"],
    ["before<!-- unterminated", "before unterminated"],
    ["before<!--<!-- nested unterminated", "before nested unterminated"],
    ["<!-<!-- hidden -->-visible-->", ""],
    ["<!-<!-- hidden --!>-visible--!>", ""],
    ["<!<!----", ""],
    ["<!<!----visible-->", "<!"],
    ["<!<!----visible--!>", "<!"],
    ["<!<!<!------", ""],
    ["<<!--!--", ""],
    ["<!<!-- one -->-<!-- two -->-visible-->", ""],
    ["before<!-<!-- hidden -->-visible-->after", "beforeafter"],
  ]) {
    const stripped = stripHtmlComments(input);
    assert.equal(stripped, expected, input);
    assert.ok(!stripped.includes("<!--"), input);
    assert.equal(stripHtmlComments(stripped), stripped, input);
  }
});

test("visible fingerprint survives HTML marker removal and arbitrary comment order", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const reply = {
    id: 124,
    user: { login: "Copilot" },
    created_at: "2026-10-03T17:00:00Z",
    body: "Already addressed; awaiting maintainer CI",
  };
  for (const comments of [
    [reply, nudge(first.state_fingerprint)],
    [nudge(first.state_fingerprint), reply],
  ]) {
    assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
  }
});

test("assignment state comments deduplicate without mentioning Copilot", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const body = stripHtmlComments(formatNudge(first));
  assert.doesNotMatch(body, /@copilot|assign.to.agent/i);
  assert.equal(classify(failingPr, detail(), [{ ...nudge(first.state_fingerprint), body }], [], now).skip_reason, "unchanged");
  assert.equal(classify(failingPr, detail(), [{ ...nudge(first.state_fingerprint), body, user: { login: "outsider" } }], [], now).skip_reason, undefined);
});

test("started coding agents can record assignment markers without member association", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const body = stripHtmlComments(formatNudge(first));
  for (const login of ["Copilot", "github-copilot[bot]", "copilot-swe-agent[bot]"]) {
    const comment = { ...nudge(first.state_fingerprint), body, user: { login }, author_association: "CONTRIBUTOR" };
    assert.equal(classify(failingPr, detail(), [comment], [], now).skip_reason, "unchanged");
  }
  const outsider = { ...nudge(first.state_fingerprint), body, user: { login: "copilot-impostor" } };
  assert.ok(classify(failingPr, detail(), [outsider], [], now).nudge_needed);
});

test("pending assignment comments suppress consecutive requests without recording active work", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const request = formatAssignmentRequest(first);
  assert.ok(request.startsWith("@copilot\n\n"));
  assert.doesNotMatch(request, /\[@copilot\]\(/);
  assert.ok(sanitizeContent(request, { allowedAliases: ["copilot"] }).startsWith("@copilot\n\n"));
  const body = stripHtmlComments(request);
  assert.deepEqual(readSousChefData(body), formatSousChefData(first, "requested"));
  assert.doesNotMatch(body, /Sous-chef (head|work|state):/);
  const pending = { ...nudge(first.state_fingerprint), body };
  assert.equal(classify(failingPr, detail(), [pending], [], now).skip_reason, "last_comment_sous_chef");
  assert.equal(classify(failingPr, detail(), [{ ...pending, body: request }], [], now).skip_reason, "last_comment_sous_chef");
  assert.ok(classify(failingPr, detail(), [pending, response()], [], now).nudge_needed);
  const acknowledged = {
    ...pending,
    user: { login: "copilot-swe-agent[bot]" },
    body: body.replace("@copilot\n\n", "").replace('"kind": "requested"', '"kind": "running"'),
  };
  assert.doesNotMatch(acknowledged.body, /@copilot/);
  assert.equal(classify(failingPr, detail(), [pending, acknowledged], [], now).skip_reason, "unchanged");
  assert.equal(formatAssignmentRequest({ ...first, nudge_needed: false }), null);
});

test("failed or fallback finisher publications permit a later dispatch after another comment", () => {
  const first = classify(failingPr, detail(), [], [], now);
  for (const note of [
    "> [!WARNING]\n> The `push_to_pull_request_branch` operation failed: denied. The code changes were not applied.",
    "> [!NOTE]\n> The pull request was not created — a fallback review issue was created instead due to protected file changes: #1",
    "> [!NOTE]\n> Direct push to the original pull request branch was not possible (diverged/non-fast-forward). A fallback pull request was created instead: #1",
  ]) {
    const body = `${note}\n\n${formatNudge(first)}`;
    const comment = { ...nudge(first.state_fingerprint), body };
    assert.equal(classify(failingPr, detail(), [comment], [], now).skip_reason, "last_comment_sous_chef");
    assert.ok(classify(failingPr, detail(), [comment, response()], [], now).nudge_needed);
  }
});

test("new HEAD and changed blockers can advance again", () => {
  const first = classify(pr, detail(), [], [], now);
  const comments = [nudge(first.state_fingerprint), response()];
  assert.ok(classify({ ...pr, headRefOid: "def" }, detail(), comments, [], now).state_fingerprint);
  assert.ok(classify(pr, detail([thread()]), comments, [], now).state_fingerprint);
});

test("latest sous-chef comments block new work regardless of provenance format or array order", () => {
  const first = classify(pr, detail(), [], [], now);
  for (const body of [
    formatAssignmentRequest(first),
    stripHtmlComments(formatAssignmentRequest(first)),
    formatNudge(first),
    "Acknowledged\n<!-- gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef -->",
    "> Generated by [PR Sous Chef](https://github.com/github/gh-aw/actions/runs/1)",
  ]) {
    const last = { ...nudge(first.state_fingerprint), body };
    const earlier = { ...response(), created_at: recent, updated_at: "2026-10-03T17:00:00Z" };
    for (const comments of [
      [earlier, last],
      [last, earlier],
    ]) {
      for (const candidate of [pr, { ...failingPr, headRefOid: "new-head" }]) {
        assert.equal(classify(candidate, detail([thread()]), comments, [], now).skip_reason, "last_comment_sous_chef");
      }
    }
    assert.ok(classify(pr, detail(), [response(), last], [], now).nudge_needed);
  }
});

test("latest comment selection handles GraphQL timestamps and creation-time ties", () => {
  const last = { ...nudge("a".repeat(64)), id: 126, created_at: undefined, createdAt: "2026-10-03T17:00:00Z" };
  assert.equal(classify(pr, detail(), [last, response()], [], now).skip_reason, "last_comment_sous_chef");
  assert.ok(classify(pr, detail(), [last, { ...response(), id: 127 }], [], now).nudge_needed);
});

test("shared bot authors and untrusted provenance do not block new work", () => {
  for (const comment of [
    { ...nudge("a".repeat(64)), body: "Unrelated workflow report" },
    { ...nudge("a".repeat(64)), user: { login: "outsider" } },
  ]) {
    assert.ok(classify(pr, detail(), [comment], [], now).nudge_needed);
  }
});

test("latest sous-chef comments do not block independent CI approvals or review cleanup", () => {
  const comments = [nudge("a".repeat(64))];
  const data = detail();
  data.reviews.nodes = [{ author: { login: "github-actions" }, state: "CHANGES_REQUESTED", databaseId: 8 }];
  const result = classify(pr, data, comments, [waitingRun], now);
  assert.equal(result.nudge_needed, false);
  assert.equal(formatAssignmentRequest(result), null);
  assert.deepEqual(result.dismiss_reviews, [8]);
  assert.deepEqual(
    result.approve_runs.map(run => run.run_id),
    [7]
  );
});

test("bot replies and old updatedAt churn do not change implementation state", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const comments = [nudge(first.state_fingerprint), { user: { login: "github-actions[bot]" }, created_at: recent, body: "Report updated" }];
  assert.equal(classify({ ...failingPr, updatedAt: "2026-10-03T17:49:00Z" }, detail(), comments, [], now).skip_reason, "unchanged");
});

test("legacy provenance without a hidden marker suppresses unchanged PRs", () => {
  const comment = nudge("unused");
  comment.body = "@copilot run pr-finisher\n> Generated by [PR Sous Chef](https://github.com/github/gh-aw/actions/runs/1)";
  assert.equal(classify({ ...pr, mergeStateStatus: "BLOCKED" }, detail(), [comment], [], now).skip_reason, "unchanged_legacy");
});

test("latest legacy nudge is selected by timestamp, not REST array order", () => {
  const older = { ...nudge("unused", "2026-10-03T15:00:00Z"), body: "<!-- gh-aw-pr-sous-chef-nudge --> @copilot fix" };
  const newer = { ...older, created_at: "2026-10-03T16:30:00Z" };
  assert.equal(classify({ ...pr, mergeStateStatus: "BLOCKED" }, detail(), [older, newer], [], now).skip_reason, "unchanged_legacy");
});

test("legacy nudges do not suppress newly actionable blockers", () => {
  const legacy = { ...nudge("unused"), body: "<!-- gh-aw-pr-sous-chef-nudge --> @copilot fix" };
  assert.ok(classify({ ...pr, mergeStateStatus: "CONFLICTING" }, detail(), [legacy, response()], [], now).state_fingerprint);
  assert.ok(classify({ ...pr, mergeStateStatus: "BEHIND" }, detail(), [legacy, response()], [], now).state_fingerprint);
  assert.equal(classify(failingPr, detail(), [legacy], [], now).skip_reason, "unchanged_legacy");
});

test("dependency bots are ignored using all known login formats", () => {
  for (const author of ["dependabot[bot]", "app/dependabot", "dependabot", "renovate[bot]", "app/renovate"]) {
    assert.equal(classify({ ...pr, author: { login: author } }, detail(), [], [], now).skip_reason, "dependency_bot");
  }
});

test("bot comments cannot revive a completely stale PR", () => {
  const stale = detail();
  const comments = [{ user: { login: "Copilot" }, body: "status", created_at: recent }];
  assert.equal(classify({ ...pr, createdAt: "2026-09-01T00:00:00Z", headCommittedAt: "2026-09-01T00:00:00Z" }, stale, comments, [], now).skip_reason, "stale");
});

test("fresh human feedback revives an older PR", () => {
  const stale = detail([thread()]);
  assert.ok(classify({ ...pr, headCommittedAt: "2026-09-01T00:00:00Z" }, stale, [], [], now).state_fingerprint);
});

test("an active coding agent blocks even an old run on a previous SHA", () => {
  for (const status of ["queued", "in_progress", "pending", "requested", "waiting"]) {
    const run = {
      name: "Copilot cloud agent",
      status,
      head_branch: pr.headRefName,
      head_sha: "old",
      created_at: "2026-09-01T00:00:00Z",
    };
    assert.equal(classify(pr, detail(), [], [run], now).skip_reason, "agent_active");
  }
  assert.equal(activeAgent(pr, [{ name: "Copilot cloud agent", status: "completed", head_branch: pr.headRefName }]), false);
});

test("active reviewer workflows block duplicate sous-chef work", () => {
  for (const name of ["PR Code Quality Reviewer", "Ponytail Reviewer"]) {
    assert.equal(activeAgent(pr, [{ name, status: "queued", head_branch: pr.headRefName }]), true);
  }
});

test("default-branch PR Finisher runs block only their selected PR", () => {
  for (const status of ["queued", "in_progress", "pending", "requested", "waiting"]) {
    const run = { name: "PR Finisher", path: ".github/workflows/pr-finisher.lock.yml", display_title: "PR Finisher #42", head_branch: "main", status };
    assert.equal(activeAgent(pr, [run]), true);
    assert.equal(activeAgent({ ...pr, number: 420 }, [run]), false);
    assert.equal(activeAgent(pr, [{ ...run, status: "completed" }]), false);
    assert.equal(activeAgent(pr, [{ ...run, path: ".github/workflows/unrelated.yml" }]), false);
  }
});

test("active agents match PR number even without a branch match", () => {
  assert.equal(activeAgent(pr, [{ name: "Q", status: "in_progress", pull_requests: [{ number: 42 }] }]), true);
});

test("long-running agent checks never age out", () => {
  const checks = [{ name: "Copilot coding agent", status: "IN_PROGRESS", startedAt: "2026-09-01T00:00:00Z" }];
  assert.equal(classify({ ...pr, statusCheckRollup: checks }, detail(), [], [], now).skip_reason, "agent_active");
});

test("short pending CI gates, WAITING CI remains directly approvable", () => {
  const checks = [{ name: "CGO", status: "QUEUED", createdAt: "2026-10-03T17:50:00Z" }];
  assert.equal(classify({ ...pr, statusCheckRollup: checks }, detail(), [], [], now).skip_reason, "checks_pending");
  const waiting = classify({ ...pr, statusCheckRollup: [{ name: "CGO", status: "WAITING" }] }, detail(), [], [], now);
  assert.equal(waiting.failed_checks[0].conclusion, "ACTION_REQUIRED");
});

test("maintainer Copilot requests also restart the idle interval", () => {
  const comments = [{ user: { login: "maintainer" }, author_association: "MEMBER", body: "@copilot run pr-finisher", created_at: "2026-10-03T17:55:00Z" }];
  assert.equal(classify(pr, detail(), comments, [], now).skip_reason, "not_idle");
});

test("DIRTY or BLOCKED alone is not an implementation blocker", () => {
  for (const mergeStateStatus of ["DIRTY", "BLOCKED", "CLEAN", "UNKNOWN"]) {
    assert.equal(classify({ ...pr, mergeStateStatus }, detail(), [], [], now).skip_reason, "nothing_actionable");
  }
  assert.ok(classify({ ...pr, mergeStateStatus: "BEHIND" }, detail(), [], [], now).state_fingerprint);
});

test("check reruns with identical failures do not change the fingerprint", () => {
  const check = { name: "CGO", conclusion: "FAILURE", status: "COMPLETED", detailsUrl: "https://github.com/run/1" };
  const first = classify({ ...failingPr, statusCheckRollup: [check] }, detail(), [], [], now);
  const second = classify({ ...failingPr, statusCheckRollup: [{ ...check, detailsUrl: "https://github.com/run/2" }] }, detail(), [nudge(first.state_fingerprint)], [], now);
  assert.equal(second.skip_reason, "unchanged");
});

test("author replies remove unresolved feedback instead of re-poking it", () => {
  const feedback = thread();
  const candidate = { ...pr, mergeStateStatus: "BLOCKED" };
  const first = classify(candidate, detail([feedback]), [], [], now);
  feedback.comments.nodes.push({ author: { login: "Copilot" }, body: "Fixed it", createdAt: "2026-10-03T17:00:00Z" });
  assert.equal(classify(candidate, detail([feedback]), [nudge(first.state_fingerprint)], [], now).skip_reason, "nothing_actionable");
});

test("truncated review data fails closed", () => {
  const truncated = detail();
  truncated.reviewThreads.pageInfo.hasNextPage = true;
  assert.equal(classify(pr, truncated, [], [], now).skip_reason, "incomplete_reviews");
  const feedback = thread();
  feedback.comments.pageInfo.hasNextPage = true;
  assert.equal(classify(pr, detail([feedback]), [], [], now).skip_reason, "incomplete_reviews");
});

test("resolved feedback permits direct dismissal of stale automation reviews", () => {
  const resolved = detail([thread({ isResolved: true })]);
  resolved.reviews.nodes = [{ author: { login: "github-actions[bot]" }, state: "CHANGES_REQUESTED", databaseId: 8 }];
  const result = classify({ ...pr, mergeStateStatus: "BLOCKED" }, resolved, [], [], now);
  assert.deepEqual(result.dismiss_reviews, [8]);
  assert.deepEqual(result.unresolved_reviews, []);
});

test("GraphQL bot logins without REST suffixes permit review cleanup", () => {
  const resolved = detail();
  resolved.reviews.nodes = [{ author: { login: "github-actions" }, state: "CHANGES_REQUESTED", databaseId: 9 }];
  assert.deepEqual(classify({ ...pr, mergeStateStatus: "BLOCKED" }, resolved, [], [], now).dismiss_reviews, [9]);
});

test("deleted PR authors do not crash classification", () => {
  const authorless = { ...pr, author: null };
  assert.equal(classify(authorless, detail(), [], [], now).author, "unknown");
  assert.equal(classify(authorless, detail([thread()]), [], [], now).author, "unknown");
});

test("external discussion does not change the trusted-state fingerprint", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const comments = [nudge(first.state_fingerprint), { user: { login: "external" }, author_association: "NONE", body: "Please investigate again", created_at: "2026-10-03T17:00:00Z" }];
  assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
});

test("informational sous-chef acknowledgements made with a maintainer token are not progress", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const comments = [
    nudge(first.state_fingerprint),
    { user: { login: "maintainer" }, author_association: "MEMBER", body: "Invocation acknowledged\n<!-- gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef -->", created_at: "2026-10-03T17:00:00Z" },
  ];
  assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
});

test("external replies cannot change a trusted review fingerprint", () => {
  const feedback = thread();
  const first = classify(failingPr, detail([feedback]), [], [], now);
  feedback.comments.nodes.push({ author: { login: "external" }, authorAssociation: "NONE", body: "Try again", createdAt: "2026-10-03T17:00:00Z" });
  assert.equal(classify(failingPr, detail([feedback]), [nudge(first.state_fingerprint)], [], now).skip_reason, "unchanged");
});

test("out-of-scope unresolved feedback does not wake an implementation agent or dismiss reviews", () => {
  const external = thread();
  external.comments.nodes[0].authorAssociation = "NONE";
  const data = detail([external]);
  data.reviews.nodes = [{ author: { login: "github-actions" }, state: "CHANGES_REQUESTED", databaseId: 9 }];
  assert.equal(classify({ ...pr, mergeStateStatus: "BLOCKED" }, data, [], [], now).skip_reason, "nothing_actionable");
});

test("empty reviewer comments do not constitute implementation work", () => {
  const empty = thread();
  empty.comments.nodes[0].body = "  ";
  assert.equal(classify({ ...pr, mergeStateStatus: "CLEAN" }, detail([empty]), [], [], now).skip_reason, "nothing_actionable");
});

test("active-run lookup paginates every nonterminal status", async () => {
  const calls = [];
  const runs = await fetchActiveRuns("github/gh-aw", args => {
    calls.push(args);
    return [{ workflow_runs: [{ id: 1 }] }, { workflow_runs: [{ id: 2 }] }];
  });
  assert.equal(calls.length, 5);
  assert.equal(runs.length, 10);
  assert.ok(calls.every(args => args.includes("--paginate") && args.includes("--slurp")));
});

test("targeted active-run lookup includes finishers dispatched on the default branch", async () => {
  const calls = [];
  await fetchActiveRuns(
    "github/gh-aw",
    args => {
      calls.push(args);
      return [];
    },
    "copilot/fix a bug"
  );
  assert.equal(calls.length, 10);
  assert.equal(calls.filter(args => args[1].includes("&branch=copilot%2Ffix%20a%20bug")).length, 5);
  const finishers = calls.filter(args => args[1].includes("/actions/workflows/pr-finisher.lock.yml/runs?"));
  assert.equal(finishers.length, 5);
  assert.ok(finishers.every(args => !args[1].includes("&branch=") && args.includes("--paginate") && args.includes("--slurp")));
});

const graphPr = {
  ...pr,
  mergeStateStatus: "DIRTY",
  isDraft: false,
  url: "https://github.com/github/gh-aw/pull/42",
  labels: { nodes: [], pageInfo: { hasNextPage: false } },
};
const headCommit = { sha: pr.headRefOid, commit: { committer: { date: recent } } };

function metadataResponse(candidate = graphPr) {
  return { data: { repository: { pullRequest: candidate } } };
}

test("targeted refresh uses explicit GraphQL metadata and paginated REST checks", async () => {
  const calls = [];
  const candidate = await fetchCandidate("github/gh-aw", "42", args => {
    calls.push(args);
    if (args[1] === "graphql") return metadataResponse();
    if (args[1] === "repos/github/gh-aw/commits/abc") return headCommit;
    if (args[1].includes("/check-runs?")) return [{ check_runs: [{ name: "CGO", status: "completed", conclusion: "failure", html_url: "https://github.com/run/1" }] }];
    return [{ statuses: [{ context: "external-ci", state: "pending", target_url: "https://github.com/run/2" }] }];
  });
  assert.equal(candidate.number, 42);
  assert.equal(calls[0][1], "graphql");
  assert.match(calls[0][3], /repository\(owner:\$owner,name:\$name\)\{pullRequest\(number:\$number\)/);
  assert.doesNotMatch(calls[0][3], /statusCheckRollup|commits\(/);
  assert.doesNotMatch(calls[0][3], /headRef\{|committedDate|\.\.\. on Commit/);
  assert.deepEqual(calls[0].slice(4), ["-f", "owner=github", "-f", "name=gh-aw", "-F", "number=42"]);
  assert.deepEqual(calls.slice(1), [
    ["api", "repos/github/gh-aw/commits/abc"],
    ["api", "repos/github/gh-aw/commits/abc/check-runs?per_page=100&filter=latest", "--paginate", "--slurp"],
    ["api", "repos/github/gh-aw/commits/abc/status?per_page=100", "--paginate", "--slurp"],
  ]);
  assert.equal(candidate.mergeStateStatus, "CONFLICTING");
  assert.equal(candidate.baseRefName, "main");
  assert.equal(candidate.headCommittedAt, recent);
  assert.equal(candidate.statusCheckRollup[0].conclusion, "FAILURE");
  assert.equal(candidate.statusCheckRollup[1].state, "PENDING");
});

test("targeted refresh fails closed when metadata is incomplete or the PR is absent", async () => {
  for (const result of [
    null,
    { ...graphPr, number: 43 },
    { ...graphPr, updatedAt: null },
    { ...graphPr, headRefOid: null },
    { ...graphPr, headRefName: null },
    { ...graphPr, baseRefName: null },
    { ...graphPr, state: null },
    { ...graphPr, isDraft: null },
    { ...graphPr, labels: null },
    { ...graphPr, labels: { nodes: [null], pageInfo: { hasNextPage: false } } },
    { ...graphPr, labels: { nodes: [{}], pageInfo: { hasNextPage: false } } },
    { ...graphPr, labels: { nodes: [], pageInfo: { hasNextPage: true } } },
  ]) {
    await assert.rejects(() => fetchCandidate("github/gh-aw", "42", () => metadataResponse(result)), /could not refresh PR metadata/);
  }
});

test("head commit activity must be valid and match the refreshed PR head", async () => {
  for (const head of [null, [], {}, { ...headCommit, sha: "old-head" }, { sha: pr.headRefOid }, { sha: pr.headRefOid, commit: null }, { sha: pr.headRefOid, commit: { committer: { date: "invalid date" } } }]) {
    let calls = 0;
    await assert.rejects(
      () =>
        fetchCandidate("github/gh-aw", "42", args => {
          calls++;
          return args[1] === "graphql" ? metadataResponse() : head;
        }),
      /PR #42: could not refresh head commit activity/
    );
    assert.equal(calls, 2);
  }
});

test("proxy-filtered and GraphQL error responses never become successful refreshes", async () => {
  for (const response of [[], {}, null, { data: null }, { data: { repository: null } }, { ...metadataResponse(), errors: [{ message: "access denied" }] }]) {
    let calls = 0;
    await assert.rejects(
      () =>
        fetchCandidate("github/gh-aw", "42", () => {
          calls++;
          return response;
        }),
      /could not refresh PR metadata|access denied/
    );
    assert.equal(calls, 1);
  }
});

test("GraphQL metadata normalizes conflicts and preserves branch states and opted-out labels", async () => {
  for (const mergeStateStatus of ["DIRTY", "BEHIND", "BLOCKED", "UNKNOWN"]) {
    const { api } = queueAPI({ candidate: { ...graphPr, mergeStateStatus } });
    assert.equal((await fetchCandidate("github/gh-aw", "42", api, now)).mergeStateStatus, mergeStateStatus === "DIRTY" ? "CONFLICTING" : mergeStateStatus);
  }
  const { api, calls } = queueAPI({ candidate: { ...graphPr, labels: { nodes: [{ name: "broccoli" }], pageInfo: { hasNextPage: false } } } });
  assert.equal((await fetchQueue("github/gh-aw", "42", api, now)).skipped[0].skip_reason, "opted_out");
  assert.equal(calls.length, 2);
});

test("deduplication finds a nudge beyond the first hundred comments", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const comments = Array.from({ length: 130 }, (_, id) => ({
    id,
    user: { login: "github-actions[bot]" },
    body: "Status",
    created_at: recent,
  }));
  comments.push(nudge(first.state_fingerprint));
  assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
});

test("queue selects exactly four PRs with deterministic blocker priority", async () => {
  const candidates = Array.from({ length: 6 }, (_, index) => ({ ...pr, number: 48 - index }));
  const queue = await buildQueue(
    candidates,
    item => ({
      ...item,
      priority: item.number === 48 ? 0 : 2,
    }),
    now
  );
  assert.deepEqual(
    queue.prs.map(item => item.number),
    [48, 43, 44, 45]
  );
  assert.equal(queue.fetched, 6);
  assert.equal(queue.deferred, 2);
});

test("dependency bots and clearly stale queue entries require no detail reads", async () => {
  const queue = await buildQueue(
    [
      { ...pr, author: { login: "dependabot[bot]" } },
      { ...pr, number: 43, updatedAt: "2026-09-01T00:00:00Z" },
    ],
    () => assert.fail("No detail API call should be made"),
    now
  );
  assert.equal(queue.prs.length, 0);
  assert.deepEqual(
    queue.skipped.map(item => item.skip_reason),
    ["dependency_bot", "stale"]
  );
});

test("invalid metadata and failed reads are errors, not an empty success queue", async () => {
  await assert.rejects(() => buildQueue([{ ...pr, updatedAt: "bad timestamp" }], () => ({}), now), /Invalid activity timestamp/);
  await assert.rejects(
    () =>
      buildQueue(
        [pr],
        () => {
          throw new Error("HTTP 403");
        },
        now
      ),
    /HTTP 403/
  );
});

test("eligible context excludes raw successful checks and comment bodies", () => {
  const checks = Array.from({ length: 100 }, (_, index) => ({
    name: `check-${index}`,
    status: "COMPLETED",
    conclusion: "SUCCESS",
  }));
  const result = classify({ ...pr, statusCheckRollup: checks }, detail(), [], [], now);
  assert.ok(result.state_fingerprint);
  assert.equal(result.statusCheckRollup, undefined);
  assert.deepEqual(result.failed_checks, []);
  assert.equal(result.comments, undefined);
});

test("step summary reports skip counts from the queue output", () => {
  const summary = formatSummary({
    fetched: 3,
    prs: [{}],
    deferred: 0,
    skipped: [{ skip_reason: "stale" }, { skip_reason: "stale" }, { skip_reason: "agent_active" }],
  });
  assert.match(summary, /\| stale \| 2 \|/);
  assert.match(summary, /\| agent_active \| 1 \|/);
});

test("idle threshold is exactly ten minutes, including updated metadata", () => {
  for (const elapsed of [0, 9 * 60 * 1000, 10 * 60 * 1000 - 1, 10 * 60 * 1000]) {
    const candidate = { ...pr, updatedAt: new Date(now - elapsed).toISOString() };
    const result = classify(candidate, detail(), [], [], now);
    if (elapsed < 10 * 60 * 1000) assert.equal(result.skip_reason, "not_idle");
    else {
      assert.ok(result.state_fingerprint);
      assert.equal(result.last_activity_at, candidate.updatedAt);
    }
  }
});

test("fresh commits, reviews, comments, and thread replies restart the idle interval", () => {
  const fresh = "2026-10-03T17:51:00Z";
  assert.equal(classify({ ...pr, headCommittedAt: fresh }, detail(), [], [], now).skip_reason, "not_idle");
  const reviewed = detail();
  reviewed.reviews.nodes.push({ author: { login: "reviewer" }, authorAssociation: "MEMBER", submittedAt: fresh });
  assert.equal(classify(pr, reviewed, [], [], now).skip_reason, "not_idle");
  for (const author of ["Copilot", "github-actions[bot]", "maintainer", "external"]) {
    assert.equal(classify(pr, detail(), [{ user: { login: author }, body: "Update", created_at: fresh }], [], now).skip_reason, "not_idle");
    const replied = thread();
    replied.comments.nodes.push({ author: { login: author }, body: "Reply", createdAt: fresh });
    assert.equal(classify(pr, detail([replied]), [], [], now).skip_reason, "not_idle");
  }
});

test("all coding-agent login forms count as replies, but Copilot reviewer feedback does not", () => {
  for (const author of ["Copilot", "app/copilot-swe-agent", "copilot-swe-agent[bot]", "github-copilot", "human-author"]) {
    const replied = thread();
    replied.comments.nodes.push({ author: { login: author }, body: "Addressed in abc", createdAt: "2026-10-03T17:00:00Z" });
    const result = classify({ ...pr, author: { login: "human-author" } }, detail([replied, thread({ id: "unanswered" })]), [], [], now);
    assert.deepEqual(
      result.unresolved_reviews.map(review => review.id),
      ["unanswered"]
    );
  }
  const reviewed = thread();
  reviewed.comments.nodes[0].author.login = "copilot-pull-request-reviewer";
  assert.equal(classify(pr, detail([reviewed]), [], [], now).unresolved_reviews.length, 1);
});

test("new feedback after an author reply reopens only that feedback", () => {
  const replied = thread({ path: "scripts/example.mjs", line: 12 });
  replied.comments.nodes.push(
    { author: { login: "Copilot" }, body: "Fixed it", createdAt: "2026-10-03T16:30:00Z" },
    { author: { login: "reviewer" }, authorAssociation: "MEMBER", body: "Also handle null values", url: "https://github.com/github/gh-aw/pull/42#discussion_r2", createdAt: "2026-10-03T17:00:00Z" }
  );
  const result = classify(pr, detail([replied]), [], [], now);
  assert.deepEqual(
    result.unresolved_reviews[0].comments.map(comment => comment.body),
    ["Also handle null values"]
  );
  replied.comments.nodes[2].updatedAt = "2026-10-03T17:10:00Z";
  assert.equal(classify(pr, detail([replied]), [], [], now).state_fingerprint, result.state_fingerprint);
  replied.comments.nodes[2].body = "Also handle empty arrays";
  assert.notEqual(classify(pr, detail([replied]), [], [], now).state_fingerprint, result.state_fingerprint);
});

test("empty replies and outsider replies do not hide trusted feedback", () => {
  const replied = thread();
  replied.comments.nodes.push({ author: { login: "Copilot" }, body: "  ", createdAt: "2026-10-03T17:00:00Z" }, { author: { login: "outsider" }, body: "Fixed", createdAt: "2026-10-03T17:10:00Z" });
  assert.equal(classify(pr, detail([replied]), [], [], now).unresolved_reviews.length, 1);
});

test("human chatter does not bypass deduplication", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const comments = [nudge(first.state_fingerprint), { id: 99, user: { login: "maintainer" }, author_association: "MEMBER", body: "Thanks!", created_at: "2026-10-03T17:00:00Z" }];
  assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
});

test("a precise nudge lists only unanswered feedback and always requests a branch update", () => {
  const answered = thread({ id: "answered" });
  answered.comments.nodes.push({ author: { login: "Copilot" }, body: "Fixed", createdAt: "2026-10-03T17:00:00Z" });
  const unanswered = thread({ id: "unanswered", path: "scripts/example.mjs", line: 12 });
  unanswered.comments.nodes[0].body = "Handle null input\n\n@copilot please address.";
  const result = classify(pr, detail([answered, unanswered, thread({ isResolved: true, id: "resolved" })]), [], [], now);
  const body = formatNudge(result);
  assert.match(body, /🍳 Apron on\\?! Copilot is working through this prep list:/);
  assert.match(body, /1\. Update this branch.*`main`.*`make merge-main`/);
  assert.match(body, /2\. Review \(scripts\/example.mjs:12\): Handle null input/);
  assert.match(body, /#discussion_r1/);
  assert.doesNotMatch(body, /Fix the regression/);
  assert.doesNotMatch(body, /@copilot/);
  assert.doesNotMatch(body, /pr-finisher/);
  assert.match(body, /No merging or CI triggering/);
  assert.equal(readSousChefData(body).state, result.state_fingerprint);
});

test("assignment and running comments publish full fingerprints only through collapsed structured data", () => {
  const candidate = { ...failingPr, headRefOid: "a".repeat(40) };
  const result = classify(candidate, detail([thread()]), [], [], now);
  for (const requested of [false, true]) {
    const report = buildNudgeReport(result, requested);
    assert.ok(!report.children.some(node => node.type === "details" || node.type === "code_block" || node.type === "structured_data"));
    assert.doesNotMatch(requested ? formatAssignmentBody(result) : formatNudgeBody(result), /[a-f0-9]{40,64}|Sous-chef (?:requested-)?(?:head|work|state):/);
    const body = requested ? formatAssignmentRequest(result) : formatNudge(result);
    const sanitized = sanitizeContent(body, { allowedAliases: ["copilot"] });
    for (const published of [body, sanitized, stripHtmlComments(sanitized)]) {
      const visible = published.replace(/<details>[\s\S]*?<\/details>/g, "");
      assert.doesNotMatch(visible, /Sous-chef (?:requested-)?(?:head|work|state):|[a-f0-9]{40,64}/);
      assert.match(visible, /1\. Update this branch/);
      assert.match(visible, /\[Review comment\]\(https:\/\/github\.com\//);
      assert.match(visible, /\[Check run\]|see the PR checks/);
      assert.doesNotMatch(published, /<details open/);
      assert.deepEqual(readSousChefData(published), formatSousChefData(result, requested ? "requested" : "running"));
      const comment = { ...nudge(result.state_fingerprint), body: published };
      assert.equal(classify(candidate, detail([thread()]), [comment], [], now).skip_reason, requested ? "last_comment_sous_chef" : "unchanged");
    }
  }
});

test("structured state is recovered from flat and collapsed data but not quoted or ambiguous examples", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const data = formatSousChefData(first, "running");
  for (const collapsed of [true, false]) {
    const body = publishedBody(formatNudgeBody(first), data, collapsed);
    assert.deepEqual(readSousChefData(body), data);
    assert.equal(classify(failingPr, detail(), [{ ...nudge(first.state_fingerprint), body }], [], now).skip_reason, "unchanged");
    const quoted = body
      .split("\n")
      .map(line => `> ${line}`)
      .join("\n");
    assert.equal(readSousChefData(quoted), null);
    assert.equal(readSousChefData(`${body}\n\n${body}`), null);
  }
});

test("structured bookkeeping validates its version, kind, hashes, work tokens, and task identifiers", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const data = formatSousChefData(first, "running");
  for (const changes of [
    { workflow: "other-workflow" },
    { schema_version: "2" },
    { kind: "finished" },
    { head: "" },
    { state: first.state_fingerprint.slice(0, 8) },
    { work: ["short-hash"] },
    { work: "not-an-array" },
    { completed_task: 17 },
    { completed_task: -1 },
    { extra: "unexpected" },
  ]) {
    const body = "```json\n" + JSON.stringify({ ...data, ...changes }) + "\n```";
    assert.equal(readSousChefData(body), null, JSON.stringify(changes));
  }
  assert.equal(readSousChefData('```json\n{"workflow":"pr-sous-chef"\n```'), null);
  const incomplete = { ...data };
  delete incomplete.state;
  assert.equal(readSousChefData("```json\n" + JSON.stringify(incomplete) + "\n```"), null);
  const followUp = { ...data, kind: "ci-follow-up", completed_task: Number.MAX_SAFE_INTEGER + 1 };
  assert.equal(readSousChefData("```json\n" + JSON.stringify(followUp) + "\n```"), null);
});

test("new structured readers retain active and pending legacy marker compatibility", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const data = formatSousChefData(first, "running");
  const body = `Assignment running\nSous-chef head: ${data.head}\nSous-chef work: ${data.work.join(" ")}\nSous-chef state: ${data.state}`;
  assert.equal(classify(failingPr, detail(), [{ ...nudge(first.state_fingerprint), body }], [], now).skip_reason, "unchanged");
  const request = body.replace(/^Sous-chef (head|work|state):/gm, "Sous-chef requested-$1:");
  assert.equal(classify(failingPr, detail(), [{ ...nudge(first.state_fingerprint), body: request }], [], now).skip_reason, "last_comment_sous_chef");
});

test("review Markdown cannot escape the prep list or swallow kitchen notes", () => {
  const feedback = thread({ path: "scripts/`example`.mjs" });
  feedback.comments.nodes[0].body = "Fix `input` </details><!-- hidden --> ```text\nSous-chef state: fake";
  const result = classify(pr, detail([feedback]), [], [], now);
  const report = buildNudgeReport(result);
  const review = report.children.find(node => node.type === "list").items[1][0];
  assert.equal(review.children[0].agentTainted, true);
  const body = formatAssignmentRequest(result);
  assert.equal((body.match(/<details>/g) ?? []).length, 1);
  assert.equal((body.match(/<\/details>/g) ?? []).length, 1);
  assert.equal(readSousChefData(body).state, result.state_fingerprint);
  assert.ok(body.endsWith("</details>"));
});

test("approval or review cleanup alone generates no implementation nudge", () => {
  const data = detail();
  data.reviews.nodes = [{ author: { login: "github-actions" }, state: "CHANGES_REQUESTED", databaseId: 8 }];
  const cleanup = classify({ ...pr, mergeStateStatus: "BLOCKED" }, data, [], [], now);
  assert.equal(formatNudge(cleanup), null);
  const approval = classify({ ...pr, mergeStateStatus: "BLOCKED", statusCheckRollup: [{ name: "CGO", status: "WAITING" }] }, detail(), [], [waitingRun], now);
  assert.equal(formatNudge(approval), null);
  assert.equal(approval.nudge_needed, false);
  assert.equal(approval.priority, 4);
  assert.deepEqual(approval.approve_runs, [{ run_id: 7, workflow_id: 12, path: ".github/workflows/cgo.yml" }]);
});

test("approval-only checks do not start an agent without a safely approvable run", () => {
  const candidate = { ...pr, mergeStateStatus: "BLOCKED", statusCheckRollup: [{ name: "Other CI", status: "COMPLETED", conclusion: "ACTION_REQUIRED" }] };
  assert.equal(classify(candidate, detail(), [], [], now).skip_reason, "approval_required");
  assert.equal(classify(candidate, detail(), [], [{ ...waitingRun, path: ".github/workflows/untrusted.yml" }], now).skip_reason, "approval_required");
  assert.equal(classify({ ...candidate, statusCheckRollup: [{ name: "Other CI", status: "WAITING" }] }, detail(), [], [], now).skip_reason, "approval_required");
});

test("approval candidates require exact workflow, PR, current head, and approval state", () => {
  for (const changes of [
    { id: 0 },
    { workflow_id: undefined },
    { event: "pull_request_target" },
    { status: "in_progress" },
    { status: "completed", conclusion: "success" },
    { head_branch: "other" },
    { head_sha: "old" },
    { path: ".github/workflows/cgo.yml.lock.yml" },
    { path: ".github/workflows/nested/cgo.yml" },
    { pull_requests: [] },
    { pull_requests: [{ number: 43 }] },
    { pull_requests: [{ number: 42 }, { number: 43 }] },
  ]) {
    assert.deepEqual(approvableRuns(pr, [{ ...waitingRun, ...changes }]), [], JSON.stringify(changes));
  }
  for (const path of [".github/workflows/cjs.yml", ".github/workflows/cgo.yml", ".github/workflows/CWI.yml", ".github/workflows/cgo.yml@refs/heads/main"]) {
    assert.equal(approvableRuns(pr, [{ ...waitingRun, path }]).length, 1);
  }
  assert.equal(approvableRuns(pr, [{ ...waitingRun, status: "action_required" }]).length, 1);
  assert.equal(approvableRuns(pr, [{ ...waitingRun, status: "completed", conclusion: "action_required" }]).length, 1);
  assert.equal(approvableRuns(pr, [waitingRun, waitingRun]).length, 1);
});

test("new approvals and review cleanup can proceed without repeating covered implementation work", () => {
  const original = classify(failingPr, detail(), [], [], now);
  const comments = [{ ...nudge(original.state_fingerprint), body: formatNudge(original) }];
  const approved = classify(failingPr, detail(), comments, [waitingRun], now);
  assert.deepEqual(
    approved.approve_runs.map(run => run.run_id),
    [7]
  );
  assert.equal(approved.nudge_needed, false);
  assert.equal(approved.priority, 4);
  assert.equal(formatNudge(approved), null);
  const resolved = detail();
  resolved.reviews.nodes = [{ author: { login: "github-actions" }, state: "CHANGES_REQUESTED", databaseId: 8 }];
  const cleanup = classify(failingPr, resolved, comments, [], now);
  assert.deepEqual(cleanup.dismiss_reviews, [8]);
  assert.equal(cleanup.nudge_needed, false);
  assert.equal(formatNudge(cleanup), null);
  const legacy = { ...comments[0], body: "<!-- gh-aw-pr-sous-chef-nudge --> @copilot fix" };
  assert.equal(formatNudge(classify(failingPr, resolved, [legacy], [], now)), null);
});

test("human-only approval blockers do not suppress genuine new implementation work", () => {
  const candidate = { ...failingPr, statusCheckRollup: [...failingPr.statusCheckRollup, { name: "Other CI", status: "COMPLETED", conclusion: "ACTION_REQUIRED" }] };
  const result = classify(candidate, detail([thread()]), [], [], now);
  assert.equal(result.nudge_needed, true);
  assert.deepEqual(result.approve_runs, []);
  assert.match(formatNudge(result), /Fix the regression/);
  assert.doesNotMatch(formatNudge(result), /Other CI/);
});

test("deployed fingerprints survive the migration and later human chatter", () => {
  const data = detail([thread()]);
  const result = classify(failingPr, data, [], [], now);
  const previous = createHash("sha256")
    .update(
      JSON.stringify({
        head: failingPr.headRefOid,
        branch: null,
        checks: ["CGO:FAILURE"],
        feedback: result.unresolved_reviews.map(({ id, reviewer, url, feedback_at, feedback_hash }) => ({ id, reviewer, url, feedback_at, feedback_hash })),
        human_activity: [],
        zero_diff: false,
      })
    )
    .digest("hex");
  const comments = [nudge(previous), { id: 99, user: { login: "maintainer" }, author_association: "MEMBER", body: "Thanks!", created_at: "2026-10-03T17:00:00Z" }];
  assert.equal(classify(failingPr, data, comments, [], now).skip_reason, "unchanged");
  assert.ok(classify({ ...failingPr, headRefOid: "new-head" }, data, comments, [], now).state_fingerprint);
});

test("answering some feedback does not re-nudge the remaining already-requested work", () => {
  const candidate = { ...pr, mergeStateStatus: "BLOCKED" };
  const first = thread({ id: "first" });
  const second = thread({ id: "second" });
  const data = detail([first, second]);
  const original = classify(candidate, data, [], [], now);
  const comments = [{ ...nudge(original.state_fingerprint), body: formatNudge(original) }];
  first.comments.nodes.push({ author: { login: "Copilot" }, body: "Fixed", createdAt: "2026-10-03T17:00:00Z" });
  assert.equal(classify(candidate, data, comments, [], now).skip_reason, "unchanged");
  first.isResolved = true;
  assert.equal(classify(candidate, data, comments, [], now).skip_reason, "unchanged");
  second.comments.nodes[0].body = "Newly discovered regression";
  assert.equal(classify(candidate, data, comments, [], now).skip_reason, "last_comment_sous_chef");
  assert.ok(classify(candidate, data, [...comments, response()], [], now).state_fingerprint);
});

test("passing checks do not re-nudge unchanged failures on the same head", () => {
  const candidate = { ...failingPr, statusCheckRollup: [...failingPr.statusCheckRollup, { name: "Lint", status: "COMPLETED", conclusion: "FAILURE" }] };
  const original = classify(candidate, detail(), [], [], now);
  const comments = [{ ...nudge(original.state_fingerprint), body: formatNudge(original) }];
  assert.equal(classify(failingPr, detail(), comments, [], now).skip_reason, "unchanged");
  assert.equal(classify({ ...failingPr, headRefOid: "new-head" }, detail(), comments, [], now).skip_reason, "last_comment_sous_chef");
  assert.ok(classify({ ...failingPr, headRefOid: "new-head" }, detail(), [...comments, response()], [], now).state_fingerprint);
});

const completedTask = {
  id: 17,
  name: "Copilot coding agent",
  path: "dynamic/copilot-swe-agent",
  event: "dynamic",
  status: "completed",
  conclusion: "success",
  head_branch: pr.headRefName,
  pull_requests: [{ number: pr.number }],
  updated_at: "2026-10-03T17:00:00Z",
  html_url: "https://github.com/github/gh-aw/actions/runs/17",
};

test("a completed sous-chef task gets one status-only comment for remaining CI failures", () => {
  const first = classify({ ...failingPr, statusCheckRollup: [...failingPr.statusCheckRollup, { name: "Lint", status: "COMPLETED", conclusion: "FAILURE" }] }, detail(), [], [], now);
  const running = { ...nudge(first.state_fingerprint), body: formatNudge(first) };
  const result = classify(failingPr, detail(), [running], [completedTask], now);
  assert.equal(result.nudge_needed, false);
  assert.equal(result.ci_follow_up.run_id, completedTask.id);
  assert.equal(formatAssignmentRequest(result), null);
  const body = formatCIFollowUp(result);
  assert.match(body, /The prep is done, but CI needs another taste test/);
  assert.match(body, /`CGO` \(FAILURE\)/);
  assert.match(body, /\[Completed task\]\(https:\/\/github\.com\/github\/gh-aw\/actions\/runs\/17\)/);
  assert.doesNotMatch(body, /`Lint`/);
  assert.doesNotMatch(body, /@copilot|make merge-main|Fix the regression/);
  assert.equal(readSousChefData(body).completed_task, 17);
  assert.equal(formatCIFollowUp(first), null);
  for (const published of [body, stripHtmlComments(body), sanitizeContent(body)]) {
    const followUp = { ...nudge(first.state_fingerprint, "2026-10-03T17:15:00Z"), body: published };
    assert.equal(classify(failingPr, detail(), [running, followUp], [completedTask], now).skip_reason, "unchanged");
  }
  const outsider = { ...nudge(first.state_fingerprint), user: { login: "outsider" }, body };
  assert.ok(classify(failingPr, detail(), [running, outsider], [completedTask], now).ci_follow_up);
});

test("completion follow-ups require a successful, idle, PR-scoped Copilot task after the assignment", () => {
  const first = classify(failingPr, detail(), [], [], now);
  const running = { ...nudge(first.state_fingerprint), body: formatNudge(first) };
  for (const changes of [
    { status: "in_progress" },
    { conclusion: "failure" },
    { head_branch: "other-branch" },
    { pull_requests: [{ number: 99 }] },
    { name: "Code scanning", path: "dynamic/agents/github-advanced-security" },
    { name: "Running Copilot Code Review", path: "dynamic/agents/copilot-pull-request-reviewer" },
    { path: ".github/workflows/copilot-review.yml" },
    { updated_at: recent },
    { updated_at: "2026-10-03T17:55:00Z" },
    { id: 0 },
  ]) {
    const result = classify(failingPr, detail(), [running], [{ ...completedTask, ...changes }], now);
    assert.ok(!result.ci_follow_up, JSON.stringify(changes));
  }
  assert.ok(!classify(failingPr, detail(), [], [completedTask], now).ci_follow_up);
  const approval = { ...failingPr, statusCheckRollup: [{ name: "CGO", status: "COMPLETED", conclusion: "ACTION_REQUIRED" }] };
  assert.ok(!classify(approval, detail(), [running], [completedTask], now).ci_follow_up);
  assert.equal(classify(failingPr, detail(), [running], [completedTask, { ...completedTask, id: 18, status: "in_progress" }], now).skip_reason, "agent_active");
});

const budgets = {
  resources: {
    core: { limit: 5000, remaining: 5000, reset: 1791057600 },
    graphql: { limit: 5000, remaining: 5000, reset: 1791057600 },
    search: { limit: 30, remaining: 30, reset: 1791057600 },
  },
};
const idleMsForTest = 10 * 60 * 1000;

function queueAPI({ listed = [pr], candidate = graphPr, head = headCommit, checks = [], statuses = [], data = detail(), comments = [], runs = [], approvals = [], completedRuns = [] } = {}) {
  const calls = [];
  return {
    calls,
    api(args) {
      calls.push(args);
      if (args[1] === "rate_limit") return budgets;
      if (args[0] === "pr") return listed;
      if (args[1] === "graphql" && args[3].includes("headRefOid")) return metadataResponse(candidate);
      if (args[1].includes("/check-runs?")) return [{ check_runs: checks }];
      if (args[1].includes("/status?")) return [{ statuses }];
      if (args[1] === `repos/github/gh-aw/commits/${candidate.headRefOid}`) return head;
      if (args[1].includes("/actions/runs?status=completed&event=dynamic")) return { workflow_runs: completedRuns };
      if (args[1].includes("/actions/runs?") || args[1].includes("/actions/workflows/pr-finisher.lock.yml/runs?")) return [{ workflow_runs: args[1].includes("status=action_required") ? approvals : runs }];
      if (args[1] === "graphql") {
        const field = args[3].includes("reviewThreads(") ? "reviewThreads" : "reviews";
        return { data: { repository: { pullRequest: { number: candidate.number, [field]: data[field] } } } };
      }
      if (args[1].includes("/comments?")) return [comments];
      assert.fail(`Unexpected API call: ${args.join(" ")}`);
    },
  };
}

test("scheduled and targeted queues refresh completed tasks only for previously assigned failing PRs", async () => {
  const first = classify(failingPr, detail(), [], [], now);
  const checks = [{ name: "CGO", status: "completed", conclusion: "failure", html_url: "https://github.com/github/gh-aw/actions/runs/2" }];
  for (const number of [undefined, "42"]) {
    const { api, calls } = queueAPI({
      candidate: { ...graphPr, mergeStateStatus: "BLOCKED" },
      checks,
      comments: [{ ...nudge(first.state_fingerprint), body: formatNudge(first) }],
      completedRuns: [completedTask],
    });
    const queue = await fetchQueue("github/gh-aw", number, api, now);
    assert.equal(queue.prs.length, 1);
    assert.equal(queue.prs[0].nudge_needed, false);
    assert.equal(queue.prs[0].ci_follow_up.run_id, 17);
    const lookup = calls.find(args => args[1].includes("status=completed&event=dynamic"));
    assert.match(lookup[1], /branch=copilot%2Ffix&per_page=100$/);
  }
  const { api, calls } = queueAPI({ candidate: { ...graphPr, mergeStateStatus: "BLOCKED" }, checks });
  await fetchQueue("github/gh-aw", "42", api, now);
  assert.ok(!calls.some(args => args[1].includes("status=completed&event=dynamic")));
});

test("completed-task lookup failures remain failed reads", async () => {
  const first = classify(failingPr, detail(), [], [], now);
  const { api } = queueAPI({
    candidate: { ...graphPr, mergeStateStatus: "BLOCKED" },
    checks: [{ name: "CGO", status: "completed", conclusion: "failure" }],
    comments: [{ ...nudge(first.state_fingerprint), body: formatNudge(first) }],
  });
  for (const response of [null, [], {}]) {
    await assert.rejects(
      fetchQueue("github/gh-aw", "42", args => (args[1].includes("status=completed&event=dynamic") ? response : api(args)), now),
      /could not refresh completed coding tasks/
    );
  }
});

test("scheduled and targeted queues scan every comment page before allowing another message", async () => {
  for (const number of [undefined, "42"]) {
    const { api, calls } = queueAPI();
    const queue = await fetchQueue(
      "github/gh-aw",
      number,
      args => {
        if (args[1].includes("/comments?")) {
          calls.push(args);
          assert.ok(args.includes("--paginate") && args.includes("--slurp"));
          return [Array.from({ length: 100 }, (_, id) => ({ ...response(), id, created_at: recent })), [{ ...nudge("unused"), body: "Acknowledged\n<!-- gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef -->" }]];
        }
        return api(args);
      },
      now
    );
    assert.deepEqual(queue.prs, []);
    assert.deepEqual(queue.skipped, [{ pr_number: 42, skip_reason: "last_comment_sous_chef" }]);
  }
});

test("refresh isolates filtered reviews from thread connections and head commit activity", async () => {
  const data = detail([thread()]);
  const approvedReview = { databaseId: 7, author: { login: "reviewer" }, authorAssociation: "MEMBER", state: "COMMENTED", submittedAt: recent };
  const deniedReview = { ...approvedReview, databaseId: 8, author: { login: "outsider" }, authorAssociation: "NONE" };
  data.reviews.nodes = [approvedReview, deniedReview];
  const { api, calls } = queueAPI({ data });
  const queue = await fetchQueue(
    "github/gh-aw",
    "42",
    args => {
      const response = api(args);
      if (args[1] !== "graphql" || !args[3].includes("reviews(last:")) return response;
      const filtered = structuredClone(response);
      // Reproduce the pinned proxy's first-connection replacement after filtering.
      function replaceNodes(value) {
        if (!value || typeof value !== "object" || Array.isArray(value)) return false;
        if ("nodes" in value) {
          value.nodes = [approvedReview];
          return true;
        }
        return Object.values(value).some(replaceNodes);
      }
      assert.equal(replaceNodes(filtered.data), true);
      return filtered;
    },
    now
  );
  assert.equal(queue.prs[0].number, 42);
  assert.deepEqual(
    queue.prs[0].unresolved_reviews.map(review => review.id),
    ["PRRT_example"]
  );
  const body = formatNudge(queue.prs[0]);
  assert.match(body, /Fix the regression/);
  assert.match(readSousChefData(body).state, /^[a-f0-9]{64}$/);
  const queries = calls.filter(args => args[1] === "graphql").map(args => args[3]);
  assert.equal(queries.length, 3);
  assert.ok(queries.every(query => !query.includes("commits(")));
  assert.ok(queries.every(query => !(query.includes("reviewThreads(") && query.includes("reviews("))));
  assert.match(queries[1], /number author\{login\} authorAssociation/);
});

test("malformed or proxy-corrupted review connections fail closed with actionable errors", async () => {
  const review = { databaseId: 7, state: "COMMENTED" };
  for (const [field, connection] of [
    ["reviewThreads", undefined],
    ["reviewThreads", { nodes: [], pageInfo: {} }],
    ["reviewThreads", { nodes: [review], pageInfo: { hasNextPage: false } }],
    ["reviewThreads", { nodes: [null], pageInfo: { hasNextPage: false } }],
    ["reviewThreads", { nodes: [thread({ comments: { nodes: [], pageInfo: {} } })], pageInfo: { hasNextPage: false } }],
    ["reviewThreads", { nodes: [thread({ comments: { nodes: [review], pageInfo: { hasNextPage: false } } })], pageInfo: { hasNextPage: false } }],
    ["reviews", undefined],
    ["reviews", { nodes: [], pageInfo: {} }],
    ["reviews", { nodes: [thread()], pageInfo: { hasPreviousPage: false } }],
    ["reviews", { nodes: [{ state: "CHANGES_REQUESTED", databaseId: "7" }], pageInfo: { hasPreviousPage: false } }],
  ]) {
    const data = { ...detail(), [field]: connection };
    const { api, calls } = queueAPI({ data });
    await assert.rejects(() => fetchQueue("github/gh-aw", "42", api, now), new RegExp(`PR #42: could not refresh ${field}; missing or malformed GitHub response`));
    assert.ok(!calls.some(args => args[1].includes("/comments?")), "Do not continue with invalid eligibility evidence");
  }
});

test("absent or denied detail responses never produce a successful eligibility queue", async () => {
  for (const field of ["reviewThreads", "reviews"]) {
    for (const response of [null, [], {}, { data: null }, { data: { repository: { pullRequest: null } } }, { data: { repository: { pullRequest: { number: 43, [field]: detail()[field] } } } }, { errors: [{ message: "access denied" }] }]) {
      const { api } = queueAPI();
      await assert.rejects(() => fetchQueue("github/gh-aw", "42", args => (args[1] === "graphql" && args[3].includes(`${field}(`) ? response : api(args)), now), /could not refresh reviewThreads|could not refresh reviews|access denied/);
    }
  }
});

test("isolated review reads preserve incomplete-page exclusions and cleanup eligibility", async () => {
  for (const field of ["reviewThreads", "reviews"]) {
    const data = detail();
    data[field].pageInfo[field === "reviews" ? "hasPreviousPage" : "hasNextPage"] = true;
    const { api } = queueAPI({ data });
    const queue = await fetchQueue("github/gh-aw", "42", api, now);
    assert.deepEqual(queue.prs, []);
    assert.deepEqual(queue.skipped, [{ pr_number: 42, skip_reason: "incomplete_reviews" }]);
  }
  const data = detail([thread({ isResolved: true })]);
  data.reviews.nodes = [{ databaseId: 7, author: { login: "github-actions" }, state: "CHANGES_REQUESTED", submittedAt: recent }];
  const { api } = queueAPI({ candidate: { ...graphPr, mergeStateStatus: "CLEAN" }, data });
  const queue = await fetchQueue("github/gh-aw", "42", api, now);
  assert.deepEqual(queue.prs[0].dismiss_reviews, [7]);
  assert.equal(formatNudge(queue.prs[0]), null);
});

test("refreshed head commit activity prevents nudging a recently pushed PR", async () => {
  const { api } = queueAPI({ head: { ...headCommit, commit: { committer: { date: "2026-10-03T17:55:00Z" } } } });
  const queue = await fetchQueue("github/gh-aw", "42", api, now);
  assert.deepEqual(queue.prs, []);
  assert.deepEqual(queue.skipped, [{ pr_number: 42, skip_reason: "not_idle" }]);
});

test("scheduled and proxied refreshes read head activity by SHA without a GraphQL headRef target", async () => {
  for (const number of [undefined, "42"]) {
    const { api, calls } = queueAPI();
    const queue = await fetchQueue("github/gh-aw", number, api, now);
    assert.equal(queue.prs[0].number, 42);
    assert.ok(queue.prs[0].state_fingerprint);
    assert.equal(calls.filter(args => args[1] === "repos/github/gh-aw/commits/abc").length, 1);
    const query = calls.find(args => args[1] === "graphql")[3];
    assert.doesNotMatch(query, /headRef\{|committedDate|\.\.\. on Commit/);
  }
});

test("failed head activity reads never become an eligible queue", async () => {
  for (const number of [undefined, "42"]) {
    const { api, calls } = queueAPI();
    await assert.rejects(
      () =>
        fetchQueue(
          "github/gh-aw",
          number,
          args => {
            if (args[1] === "repos/github/gh-aw/commits/abc") throw new Error("gh: HTTP 403");
            return api(args);
          },
          now
        ),
      /HTTP 403/
    );
    assert.ok(!calls.some(args => args[1].includes("/check-runs?")));
  }
});

function retryTestConfig(t) {
  const delays = [];
  const warnings = [];
  t.mock.method(fs, "appendFileSync", () => {});
  t.mock.method(fs, "mkdirSync", () => {});
  t.mock.method(global.core, "info", () => {});
  t.mock.method(global.core, "debug", () => {});
  t.mock.method(global.core, "warning", message => warnings.push(message));
  return {
    delays,
    warnings,
    config: { jitterMs: 0, sleep: async ms => delays.push(ms) },
  };
}

test("GitHub CLI retries transient HTTP reads with bounded exponential backoff", async t => {
  const { config, delays, warnings } = retryTestConfig(t);
  let attempts = 0;
  const result = await gh(
    ["api", "graphql", "-f", "query=query { viewer { login } }"],
    async (command, args, options) => {
      assert.equal(command, "gh");
      assert.equal(args[1], "graphql");
      assert.equal(options.timeout, 60_000);
      if (++attempts < 3) throw Object.assign(new Error("Command failed"), { code: 1, stderr: "gh: HTTP 504\n", stdout: "<!DOCTYPE html>untrusted error page" });
      return { stdout: '{"data":{"viewer":{"login":"test"}}}' };
    },
    config
  );
  assert.deepEqual(result, { data: { viewer: { login: "test" } } });
  assert.equal(attempts, 3);
  assert.deepEqual(delays, [2000, 4000]);
  assert.equal(warnings.length, 2);
  assert.ok(warnings.every(message => message.includes("HTTP 504") && !message.includes("untrusted error page")));
});

test("GitHub CLI does not retry permanent errors or malformed successful JSON", async t => {
  const { config, delays } = retryTestConfig(t);
  for (const stderr of ["gh: Bad credentials (HTTP 401)", "gh: Resource not accessible (HTTP 403)", "gh: rate limit exceeded (HTTP 403)", "gh: Not Found (HTTP 404)", "gh: Validation failed (HTTP 422)", "gh: Field does not exist"]) {
    let attempts = 0;
    await assert.rejects(
      () =>
        gh(
          ["api", "graphql"],
          async () => {
            attempts++;
            throw Object.assign(new Error("Command failed"), { code: 1, stderr });
          },
          config
        ),
      error => error.message.includes(stderr)
    );
    assert.equal(attempts, 1);
  }
  let attempts = 0;
  await assert.rejects(
    () =>
      gh(
        ["api", "rate_limit"],
        async () => {
          attempts++;
          return { stdout: "<!DOCTYPE html>not JSON" };
        },
        config
      ),
    SyntaxError
  );
  assert.equal(attempts, 1);
  assert.deepEqual(delays, []);
});

test("GitHub CLI retries server and network failures but not buffer overflows", async t => {
  const { config } = retryTestConfig(t);
  for (const stderr of ["gh: HTTP 500", "gh: HTTP 502", "gh: HTTP 503", "gh: read ECONNRESET", "gh: socket hang up"]) {
    let attempts = 0;
    assert.deepEqual(
      await gh(
        ["api", "rate_limit"],
        async () => {
          if (++attempts === 1) throw Object.assign(new Error("Command failed"), { stderr });
          return { stdout: JSON.stringify(budgets) };
        },
        config
      ),
      budgets
    );
    assert.equal(attempts, 2);
  }
  let attempts = 0;
  await assert.rejects(
    () =>
      gh(
        ["api", "rate_limit"],
        async () => {
          attempts++;
          throw Object.assign(new Error("stdout maxBuffer length exceeded"), { killed: true, signal: "SIGTERM", code: "ERR_CHILD_PROCESS_STDIO_MAXBUFFER" });
        },
        config
      ),
    /stdout maxBuffer length exceeded/
  );
  assert.equal(attempts, 1);
});

test("GitHub CLI retries network timeouts but stops after three attempts", async t => {
  const { config, delays } = retryTestConfig(t);
  let attempts = 0;
  await assert.rejects(
    () =>
      gh(
        ["api", "rate_limit"],
        async () => {
          attempts++;
          throw Object.assign(new Error("Command failed"), { killed: true, signal: "SIGTERM" });
        },
        config
      ),
    /All retry attempts exhausted/
  );
  assert.equal(attempts, 3);
  assert.deepEqual(delays, [2000, 4000]);
});

test("a transient metadata failure retries only that read and preserves the queue", async t => {
  const { config } = retryTestConfig(t);
  const { api } = queueAPI();
  let metadataAttempts = 0;
  let listAttempts = 0;
  const queue = await fetchQueue(
    "github/gh-aw",
    undefined,
    args =>
      gh(
        args,
        async (_command, commandArgs) => {
          if (commandArgs[0] === "pr") listAttempts++;
          if (commandArgs[1] === "graphql" && commandArgs[3].includes("headRefOid") && ++metadataAttempts === 1) {
            throw Object.assign(new Error("Command failed"), { code: 1, stderr: "gh: HTTP 504" });
          }
          return { stdout: JSON.stringify(api(commandArgs)) };
        },
        config
      ),
    now
  );
  assert.equal(metadataAttempts, 2);
  assert.equal(listAttempts, 1);
  assert.equal(queue.rate_limit.low, false);
  assert.equal(queue.prs[0].number, 42);
  assert.ok(queue.prs[0].state_fingerprint);
});

test("exhausted API retries reject the queue instead of returning a partial success", async t => {
  const { config } = retryTestConfig(t);
  const { api } = queueAPI({ listed: [pr, { ...pr, number: 43 }] });
  let metadataAttempts = 0;
  await assert.rejects(
    () =>
      fetchQueue(
        "github/gh-aw",
        undefined,
        args =>
          gh(
            args,
            async (_command, commandArgs) => {
              if (commandArgs[1] === "graphql" && commandArgs.includes("number=43")) {
                metadataAttempts++;
                throw Object.assign(new Error("Command failed"), { code: 1, stderr: "gh: HTTP 503" });
              }
              return { stdout: JSON.stringify(api(commandArgs)) };
            },
            config
          ),
        now
      ),
    /All retry attempts exhausted/
  );
  assert.equal(metadataAttempts, 3);
});

test("check-runs HTTP 500 exhaustion fails closed after the bounded retries", async t => {
  const { config, delays } = retryTestConfig(t);
  const { api, calls } = queueAPI();
  let attempts = 0;
  await assert.rejects(
    () =>
      fetchQueue(
        "github/gh-aw",
        undefined,
        args =>
          gh(
            args,
            async (_command, commandArgs) => {
              if (commandArgs[1].includes("/check-runs?")) {
                attempts++;
                throw Object.assign(new Error("Command failed"), { code: 1, stderr: "gh: HTTP 500" });
              }
              return { stdout: JSON.stringify(api(commandArgs)) };
            },
            config
          ),
        now
      ),
    /All retry attempts exhausted/
  );
  assert.equal(attempts, 3);
  assert.deepEqual(delays, [2000, 4000]);
  assert.ok(!calls.some(args => args[1].includes("/status?") || args[1].includes("/actions/")));
});

test("cheap metadata exclusions avoid all detail and active-run reads", async () => {
  for (const [changes, reason] of [
    [{ isDraft: true }, "closed_or_draft"],
    [{ state: "CLOSED" }, "closed_or_draft"],
    [{ labels: [{ name: "broccoli" }] }, "opted_out"],
    [{ updatedAt: "2026-10-03T17:55:00Z" }, "not_idle"],
    [{ updatedAt: "2026-09-01T00:00:00Z" }, "stale"],
    [{ author: { login: "dependabot[bot]" } }, "dependency_bot"],
  ]) {
    const { api, calls } = queueAPI({ listed: [{ ...pr, ...changes }] });
    const queue = await fetchQueue("github/gh-aw", undefined, api, now);
    assert.deepEqual(queue.skipped, [{ pr_number: 42, skip_reason: reason }]);
    assert.equal(calls.length, 2);
  }
  for (const elapsed of [idleMsForTest - 1, idleMsForTest]) {
    const { api, calls } = queueAPI({ listed: [{ ...pr, updatedAt: new Date(now - elapsed).toISOString() }] });
    const queue = await fetchQueue("github/gh-aw", undefined, api, now);
    assert.equal(calls.length === 2, elapsed < idleMsForTest);
    assert.equal(queue.prs.length, elapsed < idleMsForTest ? 0 : 1);
  }
});

test("targeted refresh preserves metadata gates before fetching checks", async () => {
  for (const [changes, reason] of [
    [{ isDraft: true }, "closed_or_draft"],
    [{ state: "CLOSED" }, "closed_or_draft"],
    [{ labels: { nodes: [{ name: "broccoli" }], pageInfo: { hasNextPage: false } } }, "opted_out"],
    [{ updatedAt: "2026-10-03T17:55:00Z" }, "not_idle"],
    [{ author: { login: "renovate[bot]" } }, "dependency_bot"],
  ]) {
    const { api, calls } = queueAPI({ candidate: { ...graphPr, ...changes } });
    const queue = await fetchQueue("github/gh-aw", "42", api, now);
    assert.deepEqual(queue.skipped, [{ pr_number: 42, skip_reason: reason }]);
    assert.equal(calls.length, 2);
  }
});

test("metadata is rechecked when the PR changed after list enumeration", async () => {
  const { api, calls } = queueAPI({ candidate: { ...graphPr, updatedAt: "2026-10-03T17:55:00Z" } });
  assert.equal((await fetchQueue("github/gh-aw", undefined, api, now)).skipped[0].skip_reason, "not_idle");
  assert.equal(calls.length, 4);
});

test("pending checks and active agent checks avoid review and run lookups", async () => {
  for (const [check, reason] of [
    [{ name: "build", status: "queued", created_at: "2026-10-03T17:55:00Z" }, "checks_pending"],
    [{ name: "Copilot coding agent", status: "in_progress", started_at: recent }, "agent_active"],
  ]) {
    const { api, calls } = queueAPI({ checks: [check] });
    assert.equal((await fetchQueue("github/gh-aw", undefined, api, now)).skipped[0].skip_reason, reason);
    assert.ok(!calls.some(args => (args[1] === "graphql" && args[3].includes("reviewThreads")) || args[1].includes("/actions/runs?")));
  }
});

test("approval-required runs are discovered by conclusion, scoped to the PR branch", async () => {
  for (const check of [
    { name: "CGO", status: "completed", conclusion: "action_required" },
    { name: "CGO", status: "waiting" },
  ]) {
    const { api, calls } = queueAPI({
      candidate: { ...graphPr, mergeStateStatus: "BLOCKED" },
      checks: [check],
      approvals: [{ ...waitingRun, status: "completed", conclusion: "action_required" }],
    });
    const queue = await fetchQueue("github/gh-aw", "42", api, now);
    assert.equal(queue.prs[0].nudge_needed, false);
    assert.equal(queue.prs[0].approve_runs[0].run_id, 7);
    const lookup = calls.find(args => args[1].includes("status=action_required"));
    assert.match(lookup[1], /branch=copilot%2Ffix/);
    assert.ok(lookup.includes("--paginate") && lookup.includes("--slurp"));
  }
});

test("healthy idle PRs and human-only approvals produce empty queues", async () => {
  for (const checks of [[], [{ name: "Other CI", status: "completed", conclusion: "action_required" }]]) {
    const { api } = queueAPI({ candidate: { ...graphPr, mergeStateStatus: "CLEAN" }, checks });
    const queue = await fetchQueue("github/gh-aw", undefined, api, now);
    assert.deepEqual(queue.prs, []);
    assert.equal(queue.skipped[0].skip_reason, checks.length ? "approval_required" : "nothing_actionable");
    assert.match(formatSummary(queue), /Agent skipped: no executable progress actions remain/);
  }
});

test("approval lookup errors remain failed reads, not a successful no-op", async () => {
  const { api } = queueAPI({ checks: [{ name: "CGO", status: "completed", conclusion: "action_required" }] });
  await assert.rejects(
    () =>
      fetchQueue(
        "github/gh-aw",
        "42",
        args => {
          if (args[1].includes("status=action_required")) throw new Error("HTTP 403");
          return api(args);
        },
        now
      ),
    /HTTP 403/
  );
});

test("workflow polls every five minutes but never starts an agent for low budgets or empty automatic queues", () => {
  const workflow = readFileSync(new URL("../.github/workflows/pr-sous-chef.md", import.meta.url), "utf8");
  assert.match(workflow, /^  schedule: every 5m$/m);
  assert.match(workflow, /^max-daily-ai-credits: -1$/m);
  assert.match(workflow, /^  skip-if-no-match: .* -label:broccoli"$/m);
  assert.match(workflow, /^if: needs\.prefilter\.outputs\.rate_limit_low == 'false' && \(needs\.prefilter\.outputs\.eligible_count > 0 \|\| needs\.activation\.outputs\.slash_command == 'souschef'\)$/m);
  assert.match(workflow, /sparse-checkout: \|\n\s+scripts\n\s+actions\/setup\/js\n\s+\.github\/scripts/);
  assert.match(workflow, /assign-to-agent:\n\s+name: copilot\n\s+allowed: \[copilot\]\n\s+target: "\*"\n\s+max: 4\n\s+github-token: \$\{\{ secrets\.AWI_MAINTENANCE_TOKEN \}\}/);
  assert.match(workflow, /post its `nudges\[0\]\.body` and `nudges\[0\]\.data` exactly via `add_comment` on\n\s+`nudges\[0\]\.pr_number`/);
  assert.match(workflow, /The comment must start with the literal `@copilot` mention/);
  assert.match(workflow, /If the latest is from PR Sous Chef, do not add an acknowledgement/);
  assert.match(workflow, /leading Copilot mention, replacing the opening with/);
  assert.match(workflow, /Do not quote the assignment request,\n\s+shorten its hashes, or display them outside the collapsed block/);
  assert.match(workflow, /as `pull_number` and `copilot` as `agent`/);
  assert.match(workflow, /custom-instructions: \|\n\s+Work on this PR's existing branch/);
  assert.match(workflow, /changing only kind from "requested" to "running"/);
  assert.match(workflow, /^  data:\n    type: object$/m);
  assert.match(workflow, /^    collapse-data: true$/m);
  assert.match(workflow, /min-integrity: approved/);
  assert.match(workflow, /^  mentions:\n    allowed: \["@copilot"\]$/m);
  assert.doesNotMatch(workflow, /^\s+dispatch-workflow:/m);
});

test("targeted refresh detects an active default-branch finisher", async () => {
  const { api } = queueAPI({
    runs: [{ name: "PR Finisher", path: ".github/workflows/pr-finisher.lock.yml", display_title: "PR Finisher #42", head_branch: "main", status: "in_progress" }],
  });
  assert.equal((await fetchQueue("github/gh-aw", "42", api, now)).skipped[0].skip_reason, "agent_active");
});

test("finisher pins checkout and all writes to its validated input, excluding workflow YAML", () => {
  const workflow = readFileSync(new URL("../.github/workflows/pr-finisher.md", import.meta.url), "utf8");
  assert.match(workflow, /run-name: "PR Finisher #\$\{\{ inputs.pr_number \}\}"/);
  assert.match(workflow, /ref: \$\{\{ needs.preflight.outputs.head_sha \}\}/);
  assert.match(workflow, /pull-request: false/);
  assert.match(workflow, /PR_HEAD_SHA: \$\{\{ needs.preflight.outputs.head_sha \}\}/);
  assert.match(workflow, /git switch -C "\$PR_HEAD_REF" "\$PR_HEAD_SHA"/);
  assert.match(workflow, /"GH_AW_PR_HEAD_BASE_SHA=\$PR_HEAD_SHA"/);
  assert.match(workflow, /min-integrity: approved/);
  assert.match(workflow, /push-to-pull-request-branch:\n\s+target: \$\{\{ needs.preflight.outputs.pr_number \}\}/);
  assert.equal([...workflow.matchAll(/target: \$\{\{ needs.preflight.outputs.pr_number \}\}/g)].length, 4);
  assert.match(workflow, /exclude-lock-yml: true/);
  assert.match(workflow, /excluded-files: \[".github\/workflows\/\*\.yml", ".github\/workflows\/\*\.yaml"\]/);
  assert.match(workflow, /fallback-as-pull-request: false/);
  assert.doesNotMatch(workflow, /^\s+(assign-to-agent|dispatch-workflow|create-pull-request):/m);
});

test("finisher preflight rejects invalid numbers and ineligible PRs before checkout", async () => {
  const workflow = readFileSync(new URL("../.github/workflows/pr-finisher.md", import.meta.url), "utf8");
  const script = workflow.match(/          script: \|\n([\s\S]*?)\nsteps:/)[1].replace(/^            /gm, "");
  const run = new (Object.getPrototypeOf(async function () {}).constructor)("github", "context", "core", "process", script);
  const candidate = { state: "open", draft: false, head: { repo: { full_name: "github/gh-aw" }, sha: "abc", ref: "copilot/fix" }, base: { ref: "main" }, labels: [], user: { login: "Copilot" } };
  const outputs = {};
  const context = { repo: { owner: "github", repo: "gh-aw" } };
  const core = { setOutput: (key, value) => (outputs[key] = value) };
  const process = { env: { PR_NUMBER: "42", GITHUB_REPOSITORY: "github/gh-aw" } };
  const github = {
    rest: {
      pulls: {
        get: async args => {
          assert.deepEqual(args, { owner: "github", repo: "gh-aw", pull_number: 42 });
          return { data: candidate };
        },
      },
    },
  };
  await run(github, context, core, process);
  assert.deepEqual(outputs, { pr_number: 42, head_sha: "abc", head_ref: "copilot/fix", base_ref: "main" });
  for (const number of ["", "0", "-1", "1.5", "NaN", "9007199254740992"]) {
    await assert.rejects(() => run(github, context, core, { env: { ...process.env, PR_NUMBER: number } }), /positive integer/);
  }
  for (const changes of [
    { state: "closed" },
    { draft: true },
    { head: { repo: null } },
    { head: { repo: { full_name: "outsider/fork" } } },
    { labels: [{ name: "broccoli" }] },
    { user: { login: "dependabot[bot]" } },
    { user: { login: "renovate[bot]" } },
  ]) {
    const ineligible = { rest: { pulls: { get: async () => ({ data: { ...candidate, ...changes } }) } } };
    await assert.rejects(() => run(ineligible, context, core, process), /requires an open/);
  }
});

test("finisher distinguishes staged publication from published fixes and review cleanup", () => {
  const workflow = readFileSync(new URL("../.github/workflows/pr-finisher.md", import.meta.url), "utf8");
  assert.match(workflow, /Describe new changes as submitted for publication, not pushed/);
  assert.match(workflow, /staging\s+`push_to_pull_request_branch` does not confirm a remote commit/);
  assert.match(workflow, /defer implementation-dependent review replies and\s+resolutions to a later pass/);
  assert.match(workflow, /Reply to each addressed review comment before resolving its thread/);
  assert.match(workflow, /Revisit\s+unresolved threads whose fixes are already published/);
  assert.match(workflow, /never bypass that protection or broaden credentials to force a push/);
});

test("each API budget no-ops at exactly ten percent remaining", async () => {
  assert.equal((await checkRateLimit(() => budgets)).low, false);
  for (const resource of ["core", "graphql", "search"]) {
    const limited = structuredClone(budgets);
    limited.resources[resource].remaining = Math.ceil(limited.resources[resource].limit * 0.1);
    assert.equal((await checkRateLimit(() => limited)).low, true);
    limited.resources[resource].remaining++;
    assert.equal((await checkRateLimit(() => limited)).low, false);
  }
});

test("low rate limits stop before any PR lookup, including targeted refreshes", async () => {
  const limited = structuredClone(budgets);
  limited.resources.graphql.remaining = 200;
  for (const number of [undefined, "42"]) {
    const calls = [];
    const queue = await fetchQueue(
      "github/gh-aw",
      number,
      args => {
        calls.push(args);
        assert.deepEqual(args, ["api", "rate_limit"]);
        return limited;
      },
      now
    );
    assert.deepEqual(calls, [["api", "rate_limit"]]);
    assert.equal(queue.rate_limit.low, true);
    assert.deepEqual(queue.prs, []);
    assert.deepEqual(queue.skipped, [{ skip_reason: "rate_limit" }]);
    assert.match(formatSummary(queue), /\| graphql \| 200 \/ 5000 \| 500 \|/);
    assert.match(formatSummary(queue), /Agent skipped: GitHub API budget is low/);
  }
});

test("exhausting the budget after enumeration discards the queue before details", async () => {
  let checks = 0;
  const limited = structuredClone(budgets);
  limited.resources.core.remaining = 100;
  const queue = await fetchQueue(
    "github/gh-aw",
    undefined,
    args => {
      if (args[0] === "pr") return [pr, { ...pr, number: 43 }];
      assert.deepEqual(args, ["api", "rate_limit"]);
      return ++checks === 1 ? budgets : limited;
    },
    now
  );
  assert.equal(checks, 2);
  assert.equal(queue.rate_limit.low, true);
  assert.deepEqual(queue.prs, []);
});

test("rate-limit read failures and missing budgets are errors, not no-ops", async () => {
  await assert.rejects(
    () =>
      checkRateLimit(() => {
        throw new Error("HTTP 403");
      }),
    /HTTP 403/
  );
  for (const response of [{}, { resources: { ...budgets.resources, core: undefined } }, { resources: { ...budgets.resources, core: { limit: 5000, remaining: -1, reset: 1791057600 } } }]) {
    await assert.rejects(() => checkRateLimit(() => response), /rate limit/);
  }
});
