// @ts-check
"use strict";

const { DEFAULT_LIMITS } = require("./work_queue_limits.cjs");
const { validatePolicy } = require("./work_queue_policy.cjs");
const { validateDeliveryContract } = require("./work_queue_delivery.cjs");

const ESLINT_WORKERS = Object.freeze(["eslint-miner", "eslint-refiner", "eslint-monster"]);

/** @param {string} profile */
function factoryContract(profile) {
  const outputs = ["noop", "report_incomplete", "missing_tool", "missing_data"].map(type => ({ type, min: 0, max: 1 }));
  if (profile === "eslint-miner") outputs.push({ type: "create_pull_request", min: 0, max: 1 });
  else if (profile === "eslint-refiner") {
    outputs.push({ type: "create_issue", min: 0, max: 3 }, { type: "create_discussion", min: 0, max: 1 }, { type: "persist_eslint_memory", min: 1, max: 1 });
  } else if (profile === "eslint-monster") {
    outputs.push({ type: "create_issue", min: 0, max: 3 }, { type: "close_issue", min: 0, max: 10 }, { type: "update_issue", min: 0, max: 10 }, { type: "assign_to_agent", min: 0, max: 3 }, { type: "create_discussion", min: 0, max: 1 });
  } else throw new Error("ESLint worker profile is not approved");
  return validateDeliveryContract({ version: 1, outputs });
}

/** @param {string} repository */
function assertRepository(repository) {
  if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error("ESLint repository must be owner/name");
}

/** @param {{date: string, repository: string, repositoryId: string}} options */
function buildESLintFactoryPlan({ date, repository, repositoryId }) {
  assertRepository(repository);
  const timestamp = typeof date === "string" && /^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date) ? Date.parse(`${date}T00:00:00.000Z`) : NaN;
  if (!Number.isFinite(timestamp) || timestamp < 0 || new Date(timestamp).toISOString().slice(0, 10) !== date) throw new Error("ESLint date must be a valid YYYY-MM-DD UTC date on or after 1970-01-01");
  if (typeof repositoryId !== "string" || !/^[1-9][0-9]{0,255}$/.test(repositoryId)) throw new Error("ESLint repository ID must be a verified positive decimal identity");
  return {
    version: 1,
    date,
    nodes: ESLINT_WORKERS.map(profile => ({
      graph_id: `eslint-factory-cohort:${date}`,
      node_key: profile,
      pool: "default",
      priority: 3,
      fairness_key: "",
      worker_profile: profile,
      payload: {
        plan: "Run this workflow's existing ESLint mission for the immutable factory date. Stay within the assigned resource scope and effect contract; do not dispatch or admit other work. If no resource-writing output is needed, cancel the Claim instead of completing this write-capable Work with only noop.",
        factory_date: date,
        factory_profile: profile,
        resource_scope: { version: 1, resources: [{ host: "github.com", repository, repository_id: repositoryId }] },
        effect_contract: factoryContract(profile),
      },
      depends_on: [],
    })),
    dispatch: { pool: "default", max_claims: ESLINT_WORKERS.length, max_dispatches: ESLINT_WORKERS.length },
  };
}

/** @param {{repository: string, ref: string, producerPrincipal: string, workerPrincipal: string}} options */
function buildESLintFactoryPolicy({ repository, ref, producerPrincipal, workerPrincipal }) {
  assertRepository(repository);
  const policy = {
    mode: "weighted-priority",
    class_weights: [8, 4, 2, 1, 1],
    accounting_weights: { "": 1 },
    producers: { [producerPrincipal]: { pools: ["default"], priorities: [3], fairness_keys: [""] } },
    pools: {
      default: {
        default_profile: ESLINT_WORKERS[0],
        profiles: Object.fromEntries(
          ESLINT_WORKERS.map(profile => [
            profile,
            { workflow: `.github/workflows/${profile}.lock.yml`, ref, principal: workerPrincipal, trust_domain: profile, credential_scope: "repository", effect_scope: repository, max_claims: 1, share_keys: false },
          ])
        ),
        logical_limit: 3,
        native_limit: 3,
        allowed_repositories: [repository],
        max_observation_age_ms: 60000,
        retry: { max_attempts: 1, backoff_ms: 1000 },
        reconciliation: { max_attempts: 5, deadline_ms: 300000 },
      },
    },
    limits: { ...DEFAULT_LIMITS, graph_nodes: 3, pending_nodes: 30, operations: 32, payload_bytes: 8192 },
  };
  validatePolicy(policy);
  return policy;
}

module.exports = { ESLINT_WORKERS, buildESLintFactoryPlan, buildESLintFactoryPolicy };

if (require.main === module) {
  const [command, repository, ref, producerPrincipal, workerPrincipal, ...extra] = process.argv.slice(2);
  if (command !== "policy" || extra.length || !repository || !ref || !producerPrincipal || !workerPrincipal)
    throw new Error("Usage: node eslint_factory_portfolio.cjs policy OWNER/REPO IMMUTABLE_SHA VERIFIED_PRODUCER_ID VERIFIED_WORKER_ID");
  process.stdout.write(JSON.stringify(buildESLintFactoryPolicy({ repository, ref, producerPrincipal, workerPrincipal }), null, 2) + "\n");
}
