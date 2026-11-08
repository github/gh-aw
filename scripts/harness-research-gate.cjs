"use strict";

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { buildSegment, canonicalJSON } = require("../actions/setup/js/ledger_store.cjs");

const LEDGER = "harness-proposals";
const WORKFLOW = "daily-harness-researcher.lock.yml";
const LEDGER_SEGMENT_BYTES = 1024 * 1024;
const AREAS = ["harness", "agent-design", "graders", "indexers", "memory", "tools", "orchestration"];
const ID = /^sha256:[a-f0-9]{64}$/;
const MARKER = /^```harness-proposal\r?\n([\s\S]*?)\r?\n```[ \t]*$/gm;
const LEGACY_MARKER = /<!-- harness-proposal:([\s\S]*?) -->/g;
const SYNONYMS = {
  evaluator: "grader",
  evaluators: "grader",
  graders: "grader",
  grading: "grader",
  evaluation: "grader",
  evaluations: "grader",
  indexing: "index",
  indexer: "index",
  indexers: "index",
  caching: "cache",
  cached: "cache",
  memories: "memory",
  orchestration: "orchestrate",
  coordinating: "orchestrate",
  coordination: "orchestrate",
  deduplication: "deduplicate",
  dedup: "deduplicate",
  retries: "retry",
  retrying: "retry",
  parallelism: "parallel",
};
const STOP = new Set(["a", "an", "the", "to", "for", "of", "in", "on", "with", "and", "by", "via", "add", "use", "using", "implement", "improve", "new", "automated", "automatic"]);

function requireThat(condition, message) {
  if (!condition) throw new Error(message);
}

function text(value, name, min = 12, max = 2000) {
  requireThat(typeof value === "string" && value.trim().length >= min && value.length <= max, `Invalid ${name}`);
  requireThat(!/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/.test(value), `Control characters in ${name}`);
  return value.trim();
}

function normalize(value, max = 2000) {
  return [
    ...new Set(
      text(value, "technical description", 3, max)
        .normalize("NFKC")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, " ")
        .split(" ")
        .filter(Boolean)
        .map(token => SYNONYMS[token] || token)
        .filter(token => !STOP.has(token))
    ),
  ]
    .sort()
    .join(" ");
}

function canonicalProposal(proposal) {
  requireThat(proposal && Object.keys(proposal).sort().join(",") === "area,component,mechanism,objective", "Proposal requires only area/component/mechanism/objective");
  requireThat(AREAS.includes(proposal.area), "Invalid technical area");
  const result = { area: proposal.area };
  for (const field of ["component", "mechanism", "objective"]) {
    result[field] = normalize(proposal[field]);
    requireThat(result[field].length >= 3, `Empty normalized ${field}`);
  }
  requireThat(result.mechanism.split(" ").length >= 3, "Mechanism must describe a specific technique");
  return result;
}

function ideaId(proposal) {
  return `sha256:${crypto
    .createHash("sha256")
    .update(canonicalJSON({ version: 1, ...canonicalProposal(proposal) }))
    .digest("hex")}`;
}

function similarity(left, right) {
  const a = new Set(left.split(" "));
  const b = new Set(right.split(" "));
  const intersection = [...a].filter(token => b.has(token)).length;
  return intersection / Math.max(1, new Set([...a, ...b]).size);
}

function equivalent(left, right) {
  const a = canonicalProposal(left);
  const b = canonicalProposal(right);
  return ideaId(left) === ideaId(right) || (similarity(a.component, b.component) >= 0.5 && similarity(a.mechanism, b.mechanism) >= 0.65);
}

function metadata(body) {
  const content = String(body || "");
  const matches = [...content.matchAll(MARKER), ...content.replace(MARKER, "").matchAll(LEGACY_MARKER)];
  requireThat(matches.length <= 1, "Multiple proposal metadata markers");
  return matches.length ? JSON.parse(matches[0][1]) : null;
}

function metadataBlock(record) {
  return `\`\`\`harness-proposal\n${JSON.stringify(record).replace(/`/g, "\\u0060")}\n\`\`\``;
}

function validDate(value) {
  return typeof value === "string" && /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value;
}

function validateRecord(record, { historical = false, root } = {}) {
  requireThat(record && [1, 2].includes(record.version), "Invalid proposal record version");
  requireThat(/^\d+$/.test(record.run_id) && /^\d+$/.test(record.run_attempt), "Missing run provenance");
  requireThat(typeof record.analyzed_at === "string" && Number.isFinite(Date.parse(record.analyzed_at)), "Missing timestamp");
  const checkpoint = Object.hasOwn(record, "verified_through_run_id");
  if (checkpoint) requireThat(historical && /^\d+$/.test(record.verified_through_run_id) && BigInt(record.verified_through_run_id) < BigInt(record.run_id), "Only the trusted gate may stamp a verified prior-run checkpoint");
  const recordKeys = Object.keys(record)
    .filter(key => key !== "verified_through_run_id")
    .sort()
    .join(",");
  if (record.kind === "scan") {
    requireThat(record.version === 1 && recordKeys === "analyzed_at,kind,run_attempt,run_id,status,version" && record.status === "complete", "Invalid complete-scan record");
    return record;
  }
  if (!historical) requireThat(record.version === 2, "New proposal decisions require version 2");
  if (!historical) requireThat(Buffer.byteLength(canonicalJSON(record), "utf8") <= 14 * 1024, "Proposal metadata must leave room inside native 16 KiB ledger record limit");
  const extended = record.version === 2;
  const fields = ["aliases", "analyzed_at", "id", "proposal", "reason", "repository_evidence", "run_attempt", "run_id", "scores", "sources", "status", "version"];
  if (extended) fields.push("implementation_hours", "score_reasons", "security", "tradeoffs", "validation");
  requireThat(recordKeys === fields.sort().join(","), "Unexpected proposal record fields");
  requireThat(ID.test(record.id) && record.id === ideaId(record.proposal), "Noncanonical technical idea ID");
  requireThat(Array.isArray(record.aliases) && record.aliases.length <= 8, "Invalid aliases");
  for (const alias of record.aliases) canonicalProposal(alias);
  requireThat(["proposal_attempt", "reserved", "rejected", "implemented", "proposed"].includes(record.status), "Invalid proposal status");
  if (!historical) requireThat(["proposal_attempt", "rejected"].includes(record.status), "Agent cannot claim implementation or PR success");
  text(record.reason, "decision reason");
  requireThat(Array.isArray(record.sources) && record.sources.length >= 2 && record.sources.length <= 6, "Require 2–6 grounded sources");
  let arxiv = false;
  let github = false;
  for (const source of record.sources) {
    requireThat(source && Object.keys(source).sort().join(",") === (extended ? "accessed_at,finding,publication_note,published,relevance,url" : "accessed_at,finding,relevance,url"), "Invalid source evidence fields");
    const url = new URL(source.url);
    requireThat(url.protocol === "https:" && !url.username && !url.password && !url.hash, "Invalid research source URL");
    arxiv ||= ["arxiv.org", "export.arxiv.org"].includes(url.hostname) && /^\/abs\/(?:\d{4}\.\d{4,5}|[a-z-]+\/\d{7})(v\d+)?$/.test(url.pathname);
    github ||= url.hostname === "github.com" && /^\/[\w.-]+\/[\w.-]+(?:\/.*)?$/.test(url.pathname);
    requireThat(["arxiv.org", "export.arxiv.org", "github.com"].includes(url.hostname), "Unapproved source");
    text(source.finding, "source finding", 40);
    text(source.relevance, "source-to-repository mapping", 40);
    requireThat(typeof source.accessed_at === "string" && Number.isFinite(Date.parse(source.accessed_at)), "Missing source access timestamp");
    if (extended) {
      const paper = ["arxiv.org", "export.arxiv.org"].includes(url.hostname);
      requireThat(validDate(source.published) || (!paper && source.published === null), "Paper publication date must be exact YYYY-MM-DD; GitHub may explicitly use null");
      text(source.publication_note, "publication date provenance or unknown-date justification", source.published === null ? 40 : 12);
      if (source.published !== null) requireThat(source.published <= source.accessed_at.slice(0, 10), "Publication date cannot follow access date");
    }
  }
  requireThat(arxiv && github, "Require arXiv and GitHub evidence");
  const scoreNames = extended ? ["actionability", "cost", "evidence", "feasibility", "impact", "novelty"] : ["actionability", "evidence", "impact", "novelty"];
  requireThat(record.scores && Object.keys(record.scores).sort().join(",") === scoreNames.join(","), "Missing independent scoring rubric");
  for (const score of Object.values(record.scores)) requireThat(Number.isInteger(score) && score >= 1 && score <= 5, "Invalid score");
  if (record.status === "proposal_attempt")
    requireThat(
      Object.values(record.scores).every(score => score >= 4),
      "Proposal below independent novelty/evidence/impact/actionability/feasibility/cost thresholds"
    );
  if (extended) {
    requireThat(record.score_reasons && Object.keys(record.score_reasons).sort().join(",") === scoreNames.join(","), "Missing independent score rationales");
    for (const name of scoreNames) text(record.score_reasons[name], `${name} rationale`, 40);
    requireThat(new Set(Object.values(record.score_reasons)).size === scoreNames.length, "Score rationales must be independent");
    requireThat(Number.isFinite(record.implementation_hours) && record.implementation_hours > 0, "Missing numeric implementation cost estimate");
    const cost = record.implementation_hours <= 8 ? 5 : record.implementation_hours <= 24 ? 4 : record.implementation_hours <= 80 ? 3 : record.implementation_hours <= 160 ? 2 : 1;
    requireThat(record.scores.cost === cost, "Cost score must follow the higher-is-cheaper hour bands");
    text(record.security, "security implications", 80);
    text(record.tradeoffs, "implementation tradeoffs", 80);
    const contract = record.validation;
    requireThat(contract && Object.keys(contract).sort().join(",") === "baseline,direction,metric,samples,target", "Missing measurable validation contract");
    text(contract.metric, "validation metric", 12, 200);
    requireThat(["lower", "higher"].includes(contract.direction) && Number.isFinite(contract.baseline) && Number.isFinite(contract.target) && contract.baseline >= 0 && contract.target >= 0, "Invalid numeric validation thresholds");
    requireThat(Number.isInteger(contract.samples) && contract.samples >= 10 && contract.samples <= 1000000, "Validation needs 10–1000000 samples");
    requireThat(contract.direction === "lower" ? contract.target < contract.baseline : contract.target > contract.baseline, "Validation threshold must improve on the baseline");
  }
  requireThat(Array.isArray(record.repository_evidence) && record.repository_evidence.length >= 2 && record.repository_evidence.length <= 6, "Missing repository evidence");
  for (const evidence of record.repository_evidence) {
    requireThat(evidence && Object.keys(evidence).sort().join(",") === "line,path,quote", "Invalid repository evidence fields");
    requireThat(typeof evidence.path === "string" && /^(?:pkg|actions|scripts|docs\/adr)\//.test(evidence.path) && !evidence.path.split("/").includes("..") && !evidence.path.includes("\\"), "Invalid repository evidence path");
    requireThat(Number.isInteger(evidence.line) && evidence.line >= 1, "Invalid repository evidence line");
    text(evidence.quote, "repository evidence quote", 12, 500);
    if (root && !historical) {
      const file = path.join(root, evidence.path);
      requireThat(fs.lstatSync(file).isFile() && !fs.lstatSync(file).isSymbolicLink(), "Evidence must be a trusted regular file");
      const lines = fs.readFileSync(file, "utf8").split("\n");
      requireThat(
        lines
          .slice(evidence.line - 1, evidence.line + 4)
          .join("\n")
          .includes(evidence.quote),
        "Repository evidence does not match trusted source"
      );
    }
  }
  return record;
}

function identities(record) {
  return [record.proposal, ...record.aliases];
}

function duplicate(candidate, history) {
  return history.some(prior => identities(candidate).some(a => identities(prior).some(b => equivalent(a, b))));
}

function validatePatch(patch, candidate) {
  text(patch, "ADR patch", 100, 32768);
  const filename = `docs/adr/harness-${candidate.id.slice(7)}.md`;
  const diffs = [...patch.matchAll(/^diff --git a\/(.+) b\/(.+)$/gm)];
  requireThat(diffs.length === 1 && diffs[0][1] === filename && diffs[0][2] === filename, "Exactly one canonical ADR-only file is required");
  requireThat(/^new file mode 100644$/m.test(patch) && /^--- \/dev\/null$/m.test(patch) && patch.includes(`+++ b/${filename}\n`), "ADR must be a NEW regular file");
  requireThat(!/^[-](?![-]{2}|- $)/m.test(patch) && !/^(?:GIT binary patch|rename |copy |deleted file mode)/m.test(patch), "ADR patch cannot delete, rename, copy, or contain binary data");
  const adr = patch
    .split("\n")
    .filter(line => line.startsWith("+") && !line.startsWith("+++"))
    .map(line => line.slice(1))
    .join("\n");
  requireThat(adr.includes(`Idea-ID: ${candidate.id}`), "ADR idea ID mismatch");
  requireThat(adr.includes(`Proposal-area: ${candidate.proposal.area}`) && adr.includes(`Proposal-component: ${candidate.proposal.component}`), "ADR technical scope mismatch");
  for (const [name, score] of Object.entries(candidate.scores)) requireThat(adr.includes(`${name}=${score}`), "ADR scoring does not match metadata");
  for (const heading of ["Status", "Context", "Decision", "Alternatives", "Consequences", "Security", "Tradeoffs", "Implementation plan", "Validation", "Sources"]) {
    requireThat(new RegExp(`^## ${heading}\\s*$`, "m").test(adr), `ADR missing ${heading}`);
    if (heading !== "Status") {
      const section = adr.split(`## ${heading}\n`)[1]?.split("\n## ")[0];
      text(section, `ADR ${heading} content`, 40, 32768);
    }
  }
  requireThat(/## Status\s+\n?Proposed\b/.test(adr), "ADR status must be Proposed");
  for (const source of candidate.sources)
    requireThat(adr.includes(`${source.url}\nPublished: ${source.published ?? "unknown"}\nPublication note: ${source.publication_note}`) && adr.includes(source.finding), "ADR lacks source-grounded findings or publication dates");
  for (const evidence of candidate.repository_evidence) requireThat(adr.includes(`${evidence.path}:${evidence.line}`) && adr.includes(evidence.quote), "ADR lacks grounded repository citation");
  requireThat(adr.includes(candidate.proposal.mechanism) && adr.includes(candidate.proposal.objective), "ADR lacks technical mechanism/objective");
  requireThat(adr.includes(candidate.security) && adr.includes(candidate.tradeoffs), "ADR security/tradeoffs differ from metadata");
  requireThat(adr.includes(`Estimated implementation hours: ${candidate.implementation_hours}`), "ADR implementation cost differs from metadata");
  const contract = candidate.validation;
  requireThat(
    adr.includes(`Validation: metric=${contract.metric}; baseline=${contract.baseline}; target=${contract.target}; direction=${contract.direction}; samples=${contract.samples}`),
    "ADR numeric validation contract differs from metadata"
  );
  return filename;
}

function validateBatch(output, { history = [], runId, runAttempt = "1", patch, root } = {}) {
  requireThat(output && Array.isArray(output.items) && output.items.length <= 6, "Invalid safe-output batch");
  const allowed = new Set(["create_pull_request", "ledger_append", "noop", "report_incomplete"]);
  for (const item of output.items) requireThat(item && allowed.has(item.type), "Unexpected safe-output operation");
  const prs = output.items.filter(item => item.type === "create_pull_request");
  const terminals = output.items.filter(item => ["create_pull_request", "noop", "report_incomplete"].includes(item.type));
  requireThat(prs.length <= 1 && terminals.length === 1, "Exactly one terminal output and at most one PR");
  if (terminals[0].type === "report_incomplete") text(terminals[0].reason, "native incomplete reason", 12, 1024);
  const records = output.items
    .filter(item => item.type === "ledger_append")
    .map(item => {
      requireThat(item.ledger === LEDGER && item.operation === "append" && !item.record, "Only append-only proposal ledger allowed");
      const record = validateRecord(item.value, { root });
      requireThat(record.run_id === String(runId) && record.run_attempt === String(runAttempt), "Ledger run provenance mismatch");
      return record;
    });
  const decisions = records.filter(record => record.kind !== "scan");
  const scans = records.filter(record => record.kind === "scan");
  requireThat(scans.length <= 1 && decisions.length <= 3 && new Set(decisions.map(record => record.id)).size === decisions.length, "Duplicate canonical IDs in batch");
  for (let index = 0; index < decisions.length; index++) requireThat(!duplicate(decisions[index], [...history, ...decisions.slice(0, index)]), "Known technical idea or semantic alias");
  const attempts = decisions.filter(record => record.status === "proposal_attempt");
  if (!prs.length) {
    requireThat(attempts.length === 0, "Proposal attempt requires a PR request");
    requireThat(terminals[0].type !== "report_incomplete" || records.length === 0, "Incomplete research cannot persist decisions");
    requireThat(terminals[0].type !== "noop" || scans.length === 1, "Complete noop requires durable scan checkpoint");
    return null;
  }
  requireThat(scans.length === 0, "PR attempt is its own checkpoint");
  requireThat(String(runAttempt) === "1", "Repeated workflow executions cannot propose");
  requireThat(attempts.length === 1, "PR requires exactly one conservative proposal attempt");
  const candidate = attempts[0];
  const pr = prs[0];
  requireThat(!["repo", "base", "head_repo", "head-repo", "draft", "labels"].some(key => Object.hasOwn(pr, key)), "PR cannot override repository, base, draft, or labels");
  requireThat(pr.branch === `harness-${candidate.id.slice(7)}`, "PR branch must carry canonical ID");
  requireThat(text(pr.title, "PR title").includes(candidate.id), "PR title requires stable ID");
  requireThat([...String(pr.body || "").matchAll(MARKER)].length === 1, "PR requires sanitizer-preserved visible proposal metadata");
  requireThat(canonicalJSON(metadata(pr.body)) === canonicalJSON(candidate), "PR metadata must equal queued ledger candidate");
  validatePatch(patch, candidate);
  return candidate;
}

async function pages(request, route, field, params = {}, maxPages = 10) {
  const results = [];
  for (let page = 1; page <= maxPages; page++) {
    const response = await request(route, { ...params, per_page: 100, page });
    const rows = field ? response.data[field] : response.data;
    requireThat(Array.isArray(rows), "Malformed authoritative history response");
    if (route === "GET /search/issues") requireThat(response.data.incomplete_results === false, "GitHub indexed search is incomplete");
    results.push(...rows);
    if (rows.length < 100) {
      if (field && response.data.total_count !== undefined) requireThat(results.length >= response.data.total_count, "Incomplete authoritative history");
      return results;
    }
  }
  throw new Error("Authoritative history pagination budget exhausted");
}

async function checkPriorRuns(request, repo, runId) {
  const durable = await loadLedgerRecords(request, repo);
  const checkpoint = (durable || []).reduce((latest, record) => {
    const value = BigInt(record.verified_through_run_id || "0");
    return value > latest ? value : latest;
  }, 0n);
  requireThat(checkpoint < BigInt(runId), "Durable checkpoint cannot cover the current invocation");
  const runs = [];
  let complete = false;
  let previous = null;
  let inspected = 0;
  for (let page = 1; page <= 10; page++) {
    const response = await request(`GET /repos/${repo}/actions/workflows/${WORKFLOW}/runs`, { per_page: 100, page });
    const rows = response.data.workflow_runs;
    requireThat(Array.isArray(rows), "Malformed authoritative workflow history");
    requireThat(Number.isInteger(response.data.total_count) && response.data.total_count >= rows.length, "Malformed workflow history total_count");
    inspected += rows.length;
    let reachedCheckpoint = false;
    for (const run of rows) {
      requireThat(/^\d+$/.test(String(run.id)), "Invalid workflow run identity");
      const id = BigInt(run.id);
      requireThat(previous === null || id <= previous, "Workflow history is not ordered newest first");
      previous = id;
      if (id <= checkpoint) reachedCheckpoint = true;
      else if (id < BigInt(runId)) runs.push(run);
    }
    if (rows.length < 100 && !reachedCheckpoint) requireThat(inspected === response.data.total_count, "Incomplete uncheckpointed workflow history");
    if (reachedCheckpoint || rows.length < 100) {
      complete = true;
      break;
    }
  }
  requireThat(complete, "Uncheckpointed workflow history pagination budget exhausted");
  for (const run of runs) {
    requireThat(run.status === "completed", "Prior invocation still unresolved");
    requireThat(run.run_attempt === undefined || run.run_attempt === 1, "Prior invocation was repeated; operator reconciliation required");
    const jobs = await pages(request, `GET /repos/${repo}/actions/runs/${run.id}/jobs`, "jobs");
    const safe = jobs.find(job => job.name === "safe_outputs");
    const ledger = jobs.find(job => job.name === "push_ledger_changes");
    const process = safe?.steps?.find(step => step.name === "Process Safe Outputs");
    // A skipped native processing step proves the gate failed before any PR
    // attempt, even when the surrounding job or ledger job reports failure.
    if (safe?.conclusion === "skipped" || process?.conclusion === "skipped") continue;
    requireThat(durable !== null, "Durable ledger missing after prior native processing; persistence is uncertain");
    requireThat(safe && (safe.conclusion === "skipped" || ledger?.conclusion === "success"), "Prior ledger persistence uncertain; operator reconciliation required");
    if (safe.conclusion !== "skipped" && safe.conclusion !== "success") {
      requireThat(
        (durable || []).some(record => record.run_id === String(run.id) && ["proposal_attempt", "reserved"].includes(record.status)),
        "Failed PR invocation has no provably durable proposal attempt; persistence is uncertain"
      );
      run.ledger_attempt_required = true;
    }
  }
  runs.verified_through_run_id = runs.reduce((latest, run) => (BigInt(run.id) > BigInt(latest) ? String(run.id) : latest), checkpoint.toString());
  runs.ledger_expected = Array.isArray(durable) && durable.length > 0;
  return runs;
}

function mechanismQueries(candidate, repo) {
  const queries = new Set();
  for (const proposal of identities(candidate)) {
    const canonical = canonicalProposal(proposal);
    const raw = proposal.mechanism
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, " ")
      .split(" ")
      .filter(token => token.length >= 4 && !STOP.has(token));
    for (const terms of [canonical.mechanism.split(" "), raw]) {
      const informative = [...new Set(terms)].sort((a, b) => b.length - a.length || a.localeCompare(b)).slice(0, 3);
      for (let index = 0; index < informative.length; index++) {
        for (let other = index + 1; other < informative.length; other++) queries.add(`repo:${repo} in:title,body ${informative[index]} ${informative[other]}`);
      }
    }
  }
  requireThat(queries.size > 0 && queries.size <= 12, "Candidate mechanism search budget exhausted");
  return [...queries];
}

async function indexedHistory(request, repo, candidate) {
  const queries = [`repo:${repo} is:pr in:body "harness-proposal"`, `repo:${repo} is:pr in:title "[Harness ADR]"`, ...(candidate ? mechanismQueries(candidate, repo) : [])];
  const found = new Map();
  for (const q of queries) {
    const results = await pages(request, "GET /search/issues", "items", { q, sort: "created", order: "desc" });
    for (const issue of results) {
      requireThat(Number.isInteger(issue.number), "Indexed history result lacks issue identity");
      found.set(issue.number, issue);
    }
  }
  return [...found.values()];
}

async function loadLegacyPriorArt(request, repo) {
  const branches = await pages(request, `GET /repos/${repo}/branches`, null);
  const selected = branches.filter(branch => ["memory/harness-experiment-proposer", "ledgers/arxiv-papers"].includes(branch.name) || branch.name.startsWith("experiments/"));
  requireThat(selected.length <= 32, "Prior-art branch budget exhausted");
  const priorArt = [];
  let bytes = 0;
  let count = 0;
  for (const branch of selected) {
    const commit = await request(`GET /repos/${repo}/git/commits/${branch.commit.sha}`);
    const tree = await request(`GET /repos/${repo}/git/trees/${commit.data.tree.sha}`, { recursive: "1" });
    requireThat(!tree.data.truncated && Array.isArray(tree.data.tree), "Incomplete prior-art tree");
    const files = tree.data.tree.filter(entry => {
      if (branch.name === "ledgers/arxiv-papers") return /^ledger\/shards\/[^/]+\.jsonl$/.test(entry.path);
      if (branch.name.startsWith("experiments/")) return ["state.json", "state.jsonl"].includes(entry.path);
      return entry.path === "harness-proposals.json";
    });
    requireThat(files.length > 0, "Known prior-art branch has no readable history");
    for (const file of files) {
      requireThat(file.mode === "100644" && file.type === "blob" && file.size <= 102400, "Invalid prior-art file");
      bytes += file.size;
      requireThat(bytes <= 2 * 1024 * 1024 && ++count <= 128, "Prior-art data budget exhausted");
      const blob = await request(`GET /repos/${repo}/git/blobs/${file.sha}`);
      requireThat(blob.data.encoding === "base64", "Invalid prior-art blob encoding");
      const contents = Buffer.from(blob.data.content, "base64").toString("utf8");
      const values = file.path.endsWith(".jsonl")
        ? contents
            .trim()
            .split("\n")
            .filter(Boolean)
            .map(line => JSON.parse(line))
        : [JSON.parse(contents)];
      if (branch.name === "ledgers/arxiv-papers") buildSegment(values);
      for (const value of values) priorArt.push({ title: branch.name, body: JSON.stringify(value) });
    }
  }
  return priorArt;
}

async function loadLedgerRecords(request, repo) {
  let ref;
  try {
    ref = await request(`GET /repos/${repo}/git/ref/heads/ledgers/${LEDGER}`);
  } catch (error) {
    if (error.status !== 404) throw error;
    return null;
  }
  const commit = await request(`GET /repos/${repo}/git/commits/${ref.data.object.sha}`);
  const tree = await request(`GET /repos/${repo}/git/trees/${commit.data.tree.sha}`, { recursive: "1" });
  requireThat(!tree.data.truncated && Array.isArray(tree.data.tree), "Incomplete ledger tree");
  const files = tree.data.tree.filter(entry => entry.path.startsWith("ledger/shards/") && entry.type !== "tree");
  requireThat(files.length <= 1024, "Ledger file budget exhausted");
  let bytes = 0;
  const records = new Map();
  const recordIds = new Map();
  for (const file of files) {
    requireThat(/^ledger\/shards\/[a-f0-9-]{36}\.jsonl$/.test(file.path) && file.mode === "100644" && file.type === "blob" && file.size <= LEDGER_SEGMENT_BYTES, "Unexpected canonical ledger file");
    bytes += file.size;
    requireThat(bytes <= 100 * 1024 * 1024, "Ledger byte budget exhausted");
    const blob = await request(`GET /repos/${repo}/git/blobs/${file.sha}`);
    requireThat(blob.data.encoding === "base64", "Invalid ledger blob encoding");
    const content = Buffer.from(blob.data.content, "base64").toString("utf8");
    const shard = content
      .trimEnd()
      .split("\n")
      .map(line => {
        const record = JSON.parse(line);
        requireThat(canonicalJSON(record) === line, "Noncanonical ledger record");
        return record;
      });
    buildSegment(shard, { maxSegmentBytes: LEDGER_SEGMENT_BYTES });
    for (const record of shard) {
      requireThat(!recordIds.has(record.id) || recordIds.get(record.id) === record.sha, "Conflicting immutable ledger envelope identity");
      recordIds.set(record.id, record.sha);
      records.set(record.sha, record);
    }
  }
  const durable = [];
  for (const record of records.values()) {
    requireThat(
      record.parents.every(parent => records.has(parent)),
      "Incomplete immutable ledger history"
    );
    requireThat(record.payload.operation === "append", "Unexpected ledger operation");
    const value = validateRecord(record.payload.value, { historical: true });
    durable.push(value);
  }
  return durable;
}

async function loadHistory(request, repo, runId, attestation, candidate) {
  const runs = attestation ? null : await checkPriorRuns(request, repo, runId);
  const ledgerExpected = attestation ? attestation.ledger_expected : runs.ledger_expected;
  const attemptRunIds = attestation ? attestation.attempt_run_ids : runs.filter(run => run.ledger_attempt_required).map(run => String(run.id));
  const issues = await indexedHistory(request, repo, candidate);
  const history = [];
  for (const issue of issues) {
    const record = metadata(issue.body);
    if (record) history.push(validateRecord(record, { historical: true }));
    else if (/\bharness-[a-f0-9]{64}\b|Idea-ID:\s*sha256:|\[Harness ADR\]/i.test(`${issue.title}\n${issue.body}`)) throw new Error("Historical proposal lacks reconstructable technical metadata");
  }
  const durable = await loadLedgerRecords(request, repo);
  if (durable === null) {
    requireThat(!history.length && !ledgerExpected && attemptRunIds.length === 0, "Durable ledger missing after previous proposals/runs");
    return { history, issues };
  }
  for (const record of durable) if (record.kind !== "scan") history.push(record);
  for (const id of attemptRunIds)
    requireThat(
      durable.some(record => record.run_id === id && ["proposal_attempt", "reserved"].includes(record.status)),
      "Failed PR invocation has no provably durable proposal attempt; persistence is uncertain"
    );
  return { history, issues };
}

function checkUnstructuredHistory(candidate, issues) {
  const proposal = canonicalProposal(candidate.proposal);
  const tokens = proposal.mechanism.split(" ");
  for (const issue of issues) {
    // GitHub's indexed search selects candidate-specific records. Token-scan
    // bounded data instead of applying the short technical-field length limit.
    const data = `${issue.title || ""} ${issue.body || ""}`.normalize("NFKC").toLowerCase();
    const bounded = data.length <= 256000 ? data : `${data.slice(0, 128000)} ${data.slice(-128000)}`;
    const body = bounded
      .replace(/[^a-z0-9]+/g, " ")
      .split(" ")
      .filter(Boolean)
      .map(token => SYNONYMS[token] || token)
      .join(" ");
    const words = new Set(body.split(" "));
    requireThat(!(proposal.component.split(" ").some(token => words.has(token)) && tokens.filter(token => words.has(token)).length / tokens.length >= 0.8), "Potential semantic duplicate in GitHub history; human reconciliation required");
  }
}

async function gateOutput(output, { request, context, patch, root, runAttempt = "1", attestation }) {
  requireThat(context.ref === `refs/heads/${context.payload.repository.default_branch}`, "Only trusted default-branch workflows may propose");
  const preliminary = validateBatch(output, { runId: context.runId, runAttempt, patch, root });
  if (attestation && output.items.some(item => item.type === "ledger_append"))
    requireThat(
      attestation.version === 1 &&
        attestation.clear === true &&
        attestation.run_id === String(context.runId) &&
        attestation.sha === context.sha &&
        typeof attestation.ledger_expected === "boolean" &&
        Array.isArray(attestation.attempt_run_ids) &&
        attestation.attempt_run_ids.every(id => /^\d+$/.test(id)) &&
        /^\d+$/.test(attestation.verified_through_run_id) &&
        BigInt(attestation.verified_through_run_id) < BigInt(context.runId),
      "Missing or invalid trusted Actions history attestation"
    );
  let history = [];
  let issues = [];
  if (output.items.some(item => item.type === "ledger_append")) {
    ({ history, issues } = await loadHistory(request, `${context.repo.owner}/${context.repo.repo}`, context.runId, attestation, preliminary));
    if (output.items.some(item => item.type === "create_pull_request")) issues.push(...(await loadLegacyPriorArt(request, `${context.repo.owner}/${context.repo.repo}`)));
  }
  const candidate = validateBatch(output, { history, runId: context.runId, runAttempt, patch, root });
  if (candidate) {
    checkUnstructuredHistory(
      candidate,
      issues.filter(issue => !metadata(issue.body))
    );
    const filename = `docs/adr/harness-${candidate.id.slice(7)}.md`;
    if (root) requireThat(!fs.existsSync(path.join(root, filename)), "ADR already exists on trusted main");
  }
  return candidate;
}

function incompleteOutput() {
  return { items: [{ type: "report_incomplete", reason: "Trusted harness validation or authoritative prior history was incomplete. No ADR publication was attempted; inspect the gate and reconcile uncertain history before retrying." }] };
}

function stampCheckpoint(output, verifiedThrough) {
  requireThat(/^\d+$/.test(verifiedThrough), "Invalid trusted checkpoint");
  for (const item of output.items.filter(item => item.type === "ledger_append")) item.value.verified_through_run_id = verifiedThrough;
  const pr = output.items.find(item => item.type === "create_pull_request");
  if (pr) {
    const attempt = output.items.find(item => item.type === "ledger_append" && item.value.status === "proposal_attempt").value;
    pr.body = pr.body.replace(MARKER, metadataBlock(attempt));
  }
  return output;
}

function writeOutput(outputPath, output) {
  const fd = fs.openSync(outputPath, fs.constants.O_WRONLY | fs.constants.O_TRUNC | fs.constants.O_NOFOLLOW);
  try {
    fs.writeFileSync(fd, JSON.stringify(output));
  } finally {
    fs.closeSync(fd);
  }
}

async function validateArtifact({ github, context, outputPath, root }) {
  const output = JSON.parse(fs.readFileSync(outputPath, "utf8"));
  const attestation = JSON.parse(process.env.GH_AW_HISTORY_ATTESTATION || "null");
  if (output.items?.some(item => ["ledger_append", "create_pull_request"].includes(item.type))) requireThat(attestation, "Trusted Actions history job evidence is required");
  const pr = output.items?.find(item => item.type === "create_pull_request");
  let patch;
  if (pr) {
    const { resolveTransportPaths } = require("../actions/setup/js/resolve_transport_paths.cjs");
    const transport = resolveTransportPaths(pr, `${context.repo.owner}/${context.repo.repo}`);
    requireThat(transport.patchPath && !transport.bundlePath, "Require native am patch transport only");
    requireThat(fs.lstatSync(transport.patchPath).isFile() && !fs.lstatSync(transport.patchPath).isSymbolicLink() && fs.statSync(transport.patchPath).size <= 32768, "Patch must be a bounded regular file");
    patch = fs.readFileSync(transport.patchPath, "utf8");
  }
  let requests = 0;
  const request = (route, params) => {
    requireThat(++requests <= 256, "Authoritative history API budget exhausted");
    return github.request(route, params);
  };
  const candidate = await gateOutput(output, { request, context, patch, root, attestation, runAttempt: process.env.GITHUB_RUN_ATTEMPT || "1" });
  if (output.items.some(item => item.type === "report_incomplete")) return { incomplete: true };
  if (output.items.some(item => item.type === "ledger_append")) writeOutput(outputPath, stampCheckpoint(output, attestation.verified_through_run_id));
  return candidate;
}

async function main({ github, context, outputPath = process.env.GH_AW_AGENT_OUTPUT, root = process.cwd() }) {
  requireThat(fs.lstatSync(outputPath).isFile() && !fs.lstatSync(outputPath).isSymbolicLink() && fs.statSync(outputPath).size <= 256 * 1024, "Invalid agent-output artifact");
  try {
    return await validateArtifact({ github, context, outputPath, root });
  } catch {
    // Replace every queued mutation before native handlers. This reports an
    // actual incomplete result, never a clean noop or a queued proposal success.
    writeOutput(outputPath, incompleteOutput());
    return { incomplete: true };
  }
}

module.exports = {
  canonicalProposal,
  ideaId,
  equivalent,
  metadata,
  metadataBlock,
  validateRecord,
  duplicate,
  validatePatch,
  validateBatch,
  pages,
  checkPriorRuns,
  mechanismQueries,
  indexedHistory,
  loadLegacyPriorArt,
  loadLedgerRecords,
  loadHistory,
  checkUnstructuredHistory,
  gateOutput,
  incompleteOutput,
  stampCheckpoint,
  main,
};

if (require.main === module) {
  requireThat(process.argv[2] === "id" && process.argv[3], "Usage: node scripts/harness-research-gate.cjs id proposal.json");
  console.log(ideaId(JSON.parse(fs.readFileSync(process.argv[3], "utf8"))));
}
