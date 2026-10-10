#!/usr/bin/env node

import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "../..");
const jarHash = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88";
const jar = process.env.TLA2TOOLS_JAR;
const java = process.env.JAVA_BIN || "java";
if (!jar) throw new Error("Set TLA2TOOLS_JAR to the official v1.7.4 tla2tools.jar.");
const hash = data => createHash("sha256").update(data).digest("hex");
if (hash(readFileSync(jar)) !== jarHash) throw new Error("TLC JAR checksum mismatch.");
const version = spawnSync(java, ["-version"], { encoding: "utf8", timeout: 10000 });
if (version.error || version.status !== 0) throw new Error(`Java unavailable: ${version.error || version.stderr}`);
const output = process.argv[2] ? resolve(process.argv[2]) : mkdtempSync(join(tmpdir(), "outcome-formal-"));
mkdirSync(output, { recursive: true });

const kinds = {
  create_pull_request: "pr",
  create_issue: "issue",
  add_comment: "comment",
  close_issue: "close",
  close_pull_request: "close",
  add_labels: "labels",
  replace_label: "replace",
  push_to_pull_request_branch: "push",
  dispatch_workflow: "dispatch",
  create_discussion: "unsupported",
  close_discussion: "unsupported",
  update_discussion: "unsupported",
  hide_comment: "unsupported",
  create_pull_request_review_comment: "unsupported",
  resolve_pull_request_review_thread: "unsupported",
  noop: "skipped",
  missing_tool: "skipped",
  missing_data: "skipped",
  report_incomplete: "skipped",
  update_project: "generic",
  update_issue: "update",
  update_pull_request: "update",
  add_reviewer: "requested",
  submit_pull_request_review: "submitted",
  mark_pull_request_as_ready_for_review: "ready",
  assign_milestone: "milestone",
  assign_to_agent: "assignment",
};
const validTime = value => Number.isFinite(Date.parse(value));
const after = (at, since) => validTime(at) && validTime(since) && Date.parse(at) > Date.parse(since);
function actor(user) {
  const login = typeof user?.login === "string" ? user.login.toLowerCase() : "";
  if (!login) return "unknown";
  return String(user.type || "").toLowerCase() === "bot" || login.endsWith("[bot]") || ["github-actions", "copilot-swe-agent"].includes(login) ? "bot" : "human";
}
const labelNames = values =>
  values.map(value =>
    String(typeof value === "object" ? value.name : value)
      .trim()
      .toLowerCase()
  );
function executionID(raw) {
  if (typeof raw === "string" && !/^\d+$/.test(raw.trim())) return null;
  const value = typeof raw === "number" || typeof raw === "string" ? Number(raw) : null;
  return Number.isSafeInteger(value) && value > 0 ? value : null;
}

// This adapter projects raw inputs, never expected classifications, into facts.
function project(fixture) {
  const { item, responses = {}, errors = {} } = fixture;
  const kind = kinds[item.type];
  if (!kind) throw new Error(`Unmapped fixture type: ${item.type}`);
  const number = item.number || Number(String(item.url || "").match(/\/(?:issues|pull)\/(\d+)/)?.[1] || 0);
  const facts = {
    kind,
    reference: number > 0 || kind === "dispatch",
    primary: "ok",
    state: "open",
    actor: "human",
    execution: "present",
    mutation: "retained",
    engaged: false,
    reactions: false,
    effortKnown: true,
    humanActivity: false,
    timestampKnown: validTime(item.timestamp),
    afterAction: true,
    supplementary: "ok",
  };
  let endpoint = `${["pr", "push", "ready", "submitted", "requested"].includes(kind) || ["close_pull_request", "update_pull_request"].includes(item.type) ? "pulls" : "issues"}/${number}`;
  if (kind === "labels") endpoint += "/labels";
  if (kind === "comment") {
    const id = String(item.url || "").match(/(?:#issuecomment-|\/comments\/)(\d+)/)?.[1];
    facts.reference = facts.reference && Boolean(id);
    endpoint = `issues/comments/${id}`;
  }
  if (kind === "dispatch") {
    const id = item.metadata?.run_id;
    facts.execution = Number.isSafeInteger(id) && id > 0 ? "present" : "missing";
    endpoint = `actions/runs/${id}`;
  }
  if (kind === "requested") endpoint += "/reviews";
  if (kind === "ready") endpoint += "/reviews";
  facts.primary = errors[endpoint] === 404 ? "404" : errors[endpoint] || responses[endpoint] == null ? "error" : "ok";
  const object = responses[endpoint] || {};
  facts.state = object.merged ? "merged" : object.state || "open";
  const comments = responses[`issues/${number}/comments`];
  facts.engaged = Array.isArray(comments) && comments.some(comment => actor(comment.user) === "human" && after(comment.created_at, kind === "comment" ? object.created_at : item.timestamp));
  facts.supplementary = errors[`issues/${number}/comments`] || !Array.isArray(comments) ? "error" : "ok";
  if (kind === "issue" && object.state === "closed") {
    facts.state = ["completed", "not_planned"].includes(object.state_reason) ? object.state_reason : "closed";
  }
  if (kind === "close" || (kind === "issue" && facts.state === "not_planned")) {
    const events = responses[`issues/${number}/events`] || [];
    facts.actor = actor(events.filter(event => event.event === "closed").at(-1)?.actor);
  }
  if (kind === "comment") facts.reactions = Number(object.reactions?.total_count || 0) > 0;
  if (kind === "pr") {
    // Optional effort endpoint failures do not replace the authoritative result.
    facts.supplementary = "ok";
    const reviews = responses[`pulls/${number}/reviews`];
    const commits = responses[`pulls/${number}/commits`];
    facts.effortKnown =
      Array.isArray(comments) &&
      Array.isArray(reviews) &&
      Array.isArray(commits) &&
      comments.every(comment => actor(comment.user) !== "unknown" && validTime(comment.created_at)) &&
      reviews.every(review => review.state === "PENDING" || (actor(review.user) !== "unknown" && validTime(review.submitted_at))) &&
      commits.every(commit => actor(commit.author) !== "unknown" && (validTime(commit.commit?.committer?.date) || validTime(commit.commit?.author?.date)));
    facts.humanActivity =
      facts.engaged ||
      (Array.isArray(reviews) && reviews.some(review => review.state !== "PENDING" && actor(review.user) === "human" && after(review.submitted_at, item.timestamp))) ||
      (Array.isArray(commits) && commits.some(commit => actor(commit.author) === "human" && (after(commit.commit?.committer?.date, item.timestamp) || after(commit.commit?.author?.date, item.timestamp))));
  }
  if (["labels", "replace"].includes(kind)) {
    const before = item.before_state?.labels ?? item.labelsBefore;
    const additions = kind === "replace" ? item.after_state?.labels : item.labelsAdded?.length ? item.labelsAdded : item.labels;
    facts.execution = Array.isArray(before) && Array.isArray(additions) ? "present" : "missing";
    if (facts.execution === "present") {
      const old = labelNames(before);
      const next = labelNames(additions);
      const added = next.filter(label => !old.includes(label));
      const removed = kind === "replace" ? old.filter(label => !next.includes(label)) : [];
      if (!added.length && !removed.length) facts.execution = "empty";
      const current = labelNames(kind === "replace" ? object.labels || [] : Array.isArray(object) ? object : []);
      facts.mutation =
        added.every(label => current.includes(label)) && removed.every(label => !current.includes(label))
          ? "retained"
          : added.every(label => !current.includes(label)) && removed.every(label => current.includes(label))
            ? "reverted"
            : "replaced";
    }
  }
  if (kind === "push") {
    const shas = [...(item.metadata?.pushed_commit_shas || []), ...(item.metadata?.commit_sha ? [item.metadata.commit_sha] : [])];
    const validSHA = sha => /^[a-fA-F0-9]{7,40}$/.test(sha || "");
    facts.execution = shas.length && shas.every(validSHA) && validSHA(object.merge_commit_sha) ? "present" : "missing";
    for (const sha of shas) {
      const compare = `compare/${sha}...${object.merge_commit_sha}`;
      if (errors[compare] || responses[compare] == null) {
        facts.mutation = "api_error";
        break;
      }
      if (!["ahead", "identical"].includes(responses[compare].status)) {
        facts.mutation = "unverified";
        break;
      }
    }
  }
  if (kind === "dispatch") {
    facts.state = object.status !== "completed" ? "pending" : object.conclusion === "success" ? "success" : ["failure", "timed_out", "cancelled", "action_required"].includes(object.conclusion) ? "failure" : "no_effect";
  }
  if (kind === "update") {
    facts.execution = item.before_state && item.after_state ? "present" : "missing";
    const normalize = (field, value) =>
      field === "labels" ? labelNames(value || []).sort() : field === "assignees" ? (value || []).map(entry => (typeof entry === "object" ? entry.login : entry)).sort() : typeof value === "string" ? value.trim() : (value ?? "");
    const equal = (field, a, b) => JSON.stringify(normalize(field, a)) === JSON.stringify(normalize(field, b));
    const current = {
      ...object,
      base: object.base?.ref || "",
      head_sha: object.head?.sha || "",
      body_hash: hash(
        String(object.body || "")
          .replace(/\r\n/g, "\n")
          .split("\n")
          .map(line => line.replace(/[ \t]+$/g, ""))
          .join("\n")
          .trim()
      ),
    };
    const changed = Object.keys(item.after_state || {}).filter(field => !equal(field, item.before_state?.[field], item.after_state[field]));
    if (facts.execution === "present" && !changed.length) facts.execution = "empty";
    facts.mutation = changed.every(field => equal(field, current[field], item.after_state[field])) ? "retained" : changed.every(field => equal(field, current[field], item.before_state?.[field])) ? "reverted" : "replaced";
  }
  if (kind === "milestone") {
    const expected = executionID(item.metadata?.milestone_number);
    facts.execution = expected !== null ? "present" : "missing";
    facts.mutation = object.milestone?.number === expected ? "retained" : "replaced";
  }
  if (kind === "ready") {
    const reviews = Array.isArray(object) ? object : [];
    const review = reviews.find(review => review.state !== "PENDING" && actor(review.user) === "human" && after(review.submitted_at, item.timestamp));
    facts.engaged = Boolean(review);
    facts.actor = actor(review?.user);
    facts.afterAction = Boolean(review) && after(review.submitted_at, item.timestamp);
    const pr = responses[`pulls/${number}`];
    facts.state = pr?.merged ? "merged" : pr?.state || "open";
    if (!review && (errors[`pulls/${number}`] || !pr)) facts.primary = "error";
  }
  if (kind === "assignment") {
    const timeline = responses[`issues/${number}/timeline`];
    const linked = (timeline || []).find(
      event =>
        event.event === "cross-referenced" &&
        event.source?.issue?.pull_request &&
        event.source.issue.number > 0 &&
        ["copilot-swe-agent", "github-actions[bot]"].includes(String(event.source.issue.user?.login || "").toLowerCase()) &&
        after(event.created_at, item.timestamp)
    );
    facts.execution = linked ? "present" : "missing";
    facts.actor = linked ? "agent" : "unknown";
    facts.afterAction = Boolean(linked) && after(linked.created_at, item.timestamp);
    if (errors[`issues/${number}/timeline`] || !Array.isArray(timeline)) facts.primary = "error";
    if (linked) {
      const linkedEndpoint = `pulls/${linked.source.issue.number}`;
      const pr = responses[linkedEndpoint];
      if (errors[linkedEndpoint] || !pr) facts.primary = "error";
      facts.state = pr?.merged ? "merged" : pr?.state || "open";
    }
  }
  if (kind === "requested" || kind === "submitted") {
    const endpointReviews = `pulls/${number}/reviews`;
    const reviews = responses[endpointReviews];
    if (errors[endpointReviews] || !Array.isArray(reviews)) facts.primary = "error";
    const submitted = (reviews || []).filter(review => review.submitted_at && review.state && review.state !== "PENDING");
    const state = review => ({ APPROVED: "approved", DISMISSED: "dismissed", CHANGES_REQUESTED: "changes" })[review?.state] || (review ? "submitted" : "absent");
    if (kind === "requested") {
      const users = (item.metadata?.requested_reviewers || []).map(login => login.toLowerCase());
      const teams = (item.metadata?.requested_team_reviewers || []).map(team => team.toLowerCase());
      const requestedEndpoint = `pulls/${number}/requested_reviewers`;
      const requested = responses[requestedEndpoint];
      if (errors[requestedEndpoint] || !requested) facts.primary = "error";
      const eligible = submitted.filter(review => validTime(review.submitted_at) && (!item.timestamp || (validTime(item.timestamp) && Date.parse(review.submitted_at) >= Date.parse(item.timestamp))));
      const latest = new Map();
      for (const review of eligible) {
        const login = String(review.user?.login || "").toLowerCase();
        if (users.includes(login) && (!latest.has(login) || Date.parse(review.submitted_at) >= Date.parse(latest.get(login).submitted_at))) latest.set(login, review);
      }
      const relevant = [...latest.values()];
      facts.execution = users.length ? "present" : teams.length ? "team" : "missing";
      facts.state = relevant.some(review => review.state === "APPROVED") ? "approved" : relevant.length ? "submitted" : "absent";
      facts.engaged = eligible.length > 0;
      facts.mutation =
        (requested?.users || []).some(user => users.includes(String(user.login || "").toLowerCase())) || (requested?.teams || []).some(team => teams.includes(String(team.slug || team.name || "").toLowerCase())) ? "pending" : "open";
    } else {
      const review = submitted.find(review => Number(review.id) === executionID(item.metadata?.review_id));
      facts.execution = review ? "present" : "missing";
      facts.state = state(review);
      facts.mutation = object.merged ? "merged" : object.state === "closed" ? "closed" : "open";
      const latest = [...submitted].sort((a, b) => Date.parse(a.submitted_at) - Date.parse(b.submitted_at)).at(-1);
      facts.engaged = Boolean(review && latest && Number(review.id) === Number(latest.id));
      if (review?.state === "CHANGES_REQUESTED" && object.merged) {
        const commits = responses[`pulls/${number}/commits`];
        if (errors[`pulls/${number}/commits`] || !Array.isArray(commits)) facts.primary = "error";
        facts.engaged = (commits || []).some(commit => {
          const time = commit.commit?.committer?.date || commit.commit?.author?.date;
          return validTime(time) && validTime(review.submitted_at) && Date.parse(time) >= Date.parse(review.submitted_at);
        });
      }
    }
  }
  return facts;
}

function tla(value) {
  if (typeof value === "boolean") return value ? "TRUE" : "FALSE";
  if (typeof value === "string") return JSON.stringify(value);
  if (Array.isArray(value)) return `{${value.map(tla).join(",\n")}}`;
  return `[${Object.entries(value)
    .map(([key, field]) => `${key} |-> ${tla(field)}`)
    .join(", ")}]`;
}
const fixturePath = join(root, "pkg/cli/testdata/outcome_conformance.json");
const fixtures = JSON.parse(readFileSync(fixturePath, "utf8"));
const projected = fixtures;
const pairs = projected.map(fixture => ({
  observation: project(fixture),
  result: {
    status: fixture.expected.outcome_status,
    strength: fixture.expected.evidence_strength,
    signal: fixture.expected.signal,
    zeroTouch: fixture.zero_touch ?? false,
  },
}));
const model = readFileSync(join(directory, "OutcomeEvaluation.tla"), "utf8");
const config = readFileSync(join(directory, "OutcomeEvaluation.cfg"), "utf8");
writeFileSync(join(output, "OutcomeEvaluation.tla"), model);
writeFileSync(
  join(output, "OutcomeFixtures.tla"),
  [
    "------------------------- MODULE OutcomeFixtures -------------------------",
    "EXTENDS OutcomeEvaluation",
    `FixturePairs == ${tla(pairs)}`,
    "FixtureObservations == {pair.observation : pair \\in FixturePairs}",
    "=============================================================================",
  ].join("\n")
);
writeFileSync(
  join(output, "fixture-projection.json"),
  JSON.stringify(
    projected.map((fixture, index) => ({ name: fixture.name, ...pairs[index] })),
    null,
    2
  ) + "\n"
);

const controls = [
  ["existence", "AcceptanceEvidence"],
  ["bot", "OpenCreationsPending"],
  ["execution", "AcceptanceEvidence"],
  ["deletion", "DeletionRequiresPrimary404"],
  ["zero_touch", "ZeroTouchRequiresCompleteEvidence"],
  ["normalization", "TypedExportPreserved"],
  ["summary", "SummaryReconciles"],
  ["review", "ExecutionAttributionRequired"],
  ["team", "ExecutionAttributionRequired"],
  ["time", "ReviewRequiresPostActionHuman"],
];
const jobs = [
  { name: "baseline", module: "OutcomeEvaluation", config, invariant: null },
  { name: "fixtures", module: "OutcomeFixtures", invariant: null, config: config.replace("BoundedObservations", "FixtureObservations").replace("NoExpectations", "FixturePairs") },
  ...controls.map(([bug, invariant]) => ({
    name: bug,
    module: "OutcomeEvaluation",
    invariant,
    config: config.replace('Bug = "none"', `Bug = "${bug}"`).replace(/INVARIANTS[\s\S]*?PROPERTY EventuallyPublished/, `INVARIANTS\n    TypeOK\n    ${invariant}`),
  })),
];
const report = {
  tool: { release: "v1.7.4", sha256: jarHash, java: version.stderr.trim() },
  sha256: { model: hash(model), config: hash(config), checker: hash(readFileSync(fileURLToPath(import.meta.url))), fixtures: hash(readFileSync(fixturePath)), projection: hash(readFileSync(join(output, "fixture-projection.json"))) },
  fixtureCoverage: { total: fixtures.length, projected: projected.length },
  settings: { workers: 2, heap: "1GiB", seed: 1, timeoutMs: 60000 },
  results: [],
};
for (const job of jobs) {
  writeFileSync(join(output, `${job.name}.cfg`), job.config);
  const command = ["-Xmx1g", "-XX:+UseParallelGC", "-cp", resolve(jar), "tlc2.TLC", "-workers", "2", "-seed", "1", "-metadir", join(output, `${job.name}-states`), "-config", `${job.name}.cfg`, `${job.module}.tla`];
  const run = spawnSync(java, command, { cwd: output, encoding: "utf8", timeout: 60000, maxBuffer: 16 * 1024 * 1024 });
  const log = `${run.stdout || ""}${run.stderr || ""}`;
  writeFileSync(join(output, `${job.name}.log`), log);
  const valid =
    !run.error &&
    (job.invariant
      ? run.status === 12 && log.includes(`Invariant ${job.invariant} is violated.`)
      : run.status === 0 && log.includes("Model checking completed. No error has been found.") && /\d+ states generated, \d+ distinct states found, 0 states left on queue\./.test(log));
  report.results.push({
    name: job.name,
    exitCode: run.status,
    expectedInvariant: job.invariant,
    verified: valid,
    states: log.match(/\d+ states generated, \d+ distinct states found, \d+ states left on queue\./)?.[0],
    command: [java, ...command],
    logSHA256: hash(log),
  });
  writeFileSync(join(output, "report.json"), JSON.stringify(report, null, 2) + "\n");
  if (!valid) throw new Error(`${job.name} did not produce the expected TLC verdict: ${run.error || log}\nArtifacts: ${output}`);
  console.log(`${job.name}: ${job.invariant ? `expected violation of ${job.invariant}` : report.results.at(-1).states}`);
}
console.log(`Verified ${projected.length}/${fixtures.length} fixture projections. Report: ${join(output, "report.json")}`);
