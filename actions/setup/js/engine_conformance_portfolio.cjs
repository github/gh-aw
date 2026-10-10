// @ts-check
"use strict";

const { buildAWPolicy } = require("./work_queue_settings.cjs");

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
      worker_profile: `engine-conformance-${engine}`,
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

/** @param {{repository: string, ref: string, settings?: object}} options */
function buildEngineConformancePolicy({ repository, ref, settings }) {
  assertRepository(repository);
  return buildAWPolicy({ repository, ref, workflows: ENGINES.map(engine => `engine-conformance-${engine}`), settings });
}

module.exports = { ENGINES, SUPPORTED, POOL, DAILY_LIMIT, changedEngines, buildEngineConformancePlan, buildEngineConformancePolicy };

if (require.main === module) {
  const [command, repository, ref, ...extra] = process.argv.slice(2);
  if (command !== "policy" || extra.length || !repository || !ref) throw new Error("Usage: node engine_conformance_portfolio.cjs policy OWNER/REPO IMMUTABLE_SHA");
  const { readPortfolioSettings } = require("./work_queue_portfolio_config.cjs");
  process.stdout.write(JSON.stringify(buildEngineConformancePolicy({ repository, ref, settings: readPortfolioSettings() }), null, 2) + "\n");
}
