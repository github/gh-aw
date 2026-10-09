// @ts-check
"use strict";

const { DEFAULT_LIMITS } = require("./work_queue_limits.cjs");
const { validatePolicy } = require("./work_queue_policy.cjs");

const ENGINES = Object.freeze(["agy", "aider", "claude", "codex", "copilot", "crush", "cursor", "deepseek-harness", "gemini", "goose", "kiro", "opencode", "pi", "pydantic-ai"]);
const SUPPORTED = Object.freeze(["claude", "codex", "copilot", "pi"]);
const POOL = "engine-conformance";
const DAILY_LIMIT = 3;

function dayNumber(date) {
  if (typeof date !== "string" || !/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date)) throw new Error("Conformance date must be YYYY-MM-DD UTC");
  const stamp = Date.parse(`${date}T00:00:00.000Z`);
  if (!Number.isFinite(stamp) || stamp < 0 || new Date(stamp).toISOString().slice(0, 10) !== date) throw new Error("Invalid conformance UTC date");
  return stamp / 86400000;
}

function assertRepository(repository) {
  if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error("Conformance repository must be owner/name");
}

function changedEngines(paths) {
  if (!Array.isArray(paths) || paths.some(path => typeof path !== "string")) throw new Error("Changed paths must be an array of strings");
  const all = paths.some(
    path =>
      path === ".github/workflows/shared/engine-conformance.md" ||
      path === ".github/workflows/shared/engine-conformance-worker.md" ||
      path === "pkg/workflow/engine.go" ||
      path === "pkg/workflow/agentic_engine.go" ||
      path === "pkg/workflow/behavior_defined_engine.go" ||
      path.startsWith("actions/setup/js/work_queue_")
  );
  return new Set(
    ENGINES.filter(
      engine =>
        all ||
        paths.some(
          path =>
            path === `.github/workflows/engine-conformance-${engine}.md` ||
            path === `.github/workflows/shared/${engine}.md` ||
            (path === `.github/workflows/shared/agy-conformance.md` && engine === "agy") ||
            (path.startsWith("pkg/workflow/") && new RegExp(`(^|-)${engine}($|[-.])`).test(path.slice("pkg/workflow/".length).toLowerCase().replaceAll("_", "-")))
        )
    )
  );
}

/** @param {{date: string, repository: string, repositoryId: string, changedPaths?: string[]}} options */
function buildEngineConformancePlan({ date, repository, repositoryId, changedPaths = [] }) {
  assertRepository(repository);
  if (typeof repositoryId !== "string" || !/^[1-9][0-9]{0,255}$/.test(repositoryId)) throw new Error("Invalid conformance repository ID");
  const day = dayNumber(date);
  const recent = changedEngines(changedPaths);
  const selected = [SUPPORTED[day % SUPPORTED.length]];
  const changed = ENGINES.filter(engine => recent.has(engine) && !selected.includes(engine));
  if (changed.length) selected.push(changed[day % changed.length]);
  else selected.push(SUPPORTED[(day + 1) % SUPPORTED.length]);
  for (let index = 0; index < ENGINES.length && selected.length < DAILY_LIMIT; index++) {
    const candidate = ENGINES[(day + index) % ENGINES.length];
    if (!selected.includes(candidate)) selected.push(candidate);
  }
  return {
    version: 1,
    date,
    selected,
    nodes: selected.map(engine => ({
      graph_id: `engine-conformance:${date}`,
      node_key: engine,
      pool: POOL,
      priority: SUPPORTED.includes(engine) ? 1 : recent.has(engine) ? 2 : 3,
      fairness_key: engine,
      worker_profile: engine,
      payload: {
        plan: "Run the existing host-asserted conformance suite once for the assigned engine; do not modify repository files or dispatch other workflows.",
        engine,
        conformance_date: date,
        resource_scope: { version: 1, resources: [{ host: "github.com", repository, repository_id: repositoryId }] },
        effect_contract: { kind: "none" },
      },
      depends_on: [],
    })),
    dispatch: { pool: POOL, max_claims: DAILY_LIMIT, max_dispatches: DAILY_LIMIT },
  };
}

/** @param {{repository: string, ref: string, producerPrincipal: string, workerPrincipal: string}} options */
function buildEngineConformancePolicy({ repository, ref, producerPrincipal, workerPrincipal }) {
  assertRepository(repository);
  const policy = {
    mode: "weighted-priority",
    class_weights: [8, 4, 2, 1, 1],
    accounting_weights: Object.fromEntries([["", 1], ...ENGINES.map(engine => [engine, 1])]),
    producers: { [producerPrincipal]: { pools: [POOL], priorities: [1, 2, 3], fairness_keys: [...ENGINES] } },
    pools: {
      [POOL]: {
        default_profile: "copilot",
        profiles: Object.fromEntries(
          ENGINES.map(engine => [
            engine,
            {
              workflow: `.github/workflows/engine-conformance-${engine}.lock.yml`,
              ref,
              principal: workerPrincipal,
              trust_domain: engine,
              credential_scope: "repository",
              effect_scope: repository,
              max_claims: 1,
              share_keys: false,
            },
          ])
        ),
        logical_limit: DAILY_LIMIT,
        native_limit: DAILY_LIMIT,
        per_account_limit: 1,
        allowed_repositories: [repository],
        max_observation_age_ms: 60000,
        retry: { max_attempts: 1, backoff_ms: 1000 },
        reconciliation: { max_attempts: 5, deadline_ms: 300000 },
      },
    },
    limits: { ...DEFAULT_LIMITS, graph_nodes: DAILY_LIMIT, pending_nodes: 60, operations: 32, payload_bytes: 8192 },
  };
  validatePolicy(policy);
  return policy;
}

module.exports = { ENGINES, SUPPORTED, POOL, DAILY_LIMIT, changedEngines, buildEngineConformancePlan, buildEngineConformancePolicy };

if (require.main === module) {
  const [command, repository, ref, producerPrincipal, workerPrincipal, ...extra] = process.argv.slice(2);
  if (command !== "policy" || extra.length || !repository || !ref || !producerPrincipal || !workerPrincipal)
    throw new Error("Usage: node engine_conformance_portfolio.cjs policy OWNER/REPO IMMUTABLE_SHA VERIFIED_PRODUCER_ID VERIFIED_WORKER_ID");
  process.stdout.write(JSON.stringify(buildEngineConformancePolicy({ repository, ref, producerPrincipal, workerPrincipal }), null, 2) + "\n");
}
