// @ts-check
"use strict";

const ESLINT_WORKERS = Object.freeze(["eslint-miner", "eslint-refiner", "eslint-monster"]);
const RULE_WORKERS = Object.freeze(["eslint-refiner", "eslint-monster"]);
const FACTORY_POOL = "eslint-factory";
const RULES_PER_DAY = 3;
const RULE_ID = /^[A-Za-z0-9][A-Za-z0-9-]*$/;
const SAFETY_CLAUSE =
  "Stay within the assigned resource scope and effect contract; do not dispatch or admit other work. If no resource-writing output is needed, finish the Claim as failed instead of completing this write-capable Work with only noop.";

/** @param {string} profile */
function factoryContract(profile) {
  const outputs = ["noop", "report_incomplete", "report_missing_tool", "report_missing_data"].map(type => ({ type, min: 0, max: 1 }));
  if (profile === "eslint-miner") outputs.push({ type: "create_pull_request", min: 0, max: 1 });
  else if (profile === "eslint-refiner") {
    outputs.push({ type: "create_issue", min: 0, max: 3 }, { type: "create_discussion", min: 0, max: 1 }, { type: "persist_eslint_memory", min: 1, max: 1 });
  } else if (profile === "eslint-monster") {
    outputs.push({ type: "create_issue", min: 0, max: 1 }, { type: "close_issue", min: 0, max: 10 }, { type: "update_issue", min: 0, max: 10 }, { type: "assign_to_agent", min: 0, max: 1 }, { type: "create_discussion", min: 0, max: 1 });
  } else throw new Error("ESLint worker profile is not approved");
  return { version: 1, outputs };
}

/** @param {string} repository */
function assertRepository(repository) {
  if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error("ESLint repository must be owner/name");
}

/**
 * Days since the epoch of a canonical YYYY-MM-DD UTC date.
 * @param {string} date
 */
function factoryDay(date) {
  const timestamp = typeof date === "string" && /^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date) ? Date.parse(`${date}T00:00:00.000Z`) : NaN;
  if (!Number.isFinite(timestamp) || timestamp < 0 || new Date(timestamp).toISOString().slice(0, 10) !== date) throw new Error("ESLint date must be a valid YYYY-MM-DD UTC date on or after 1970-01-01");
  return timestamp / 86400000;
}

/**
 * Rule ids registered in the plugin's `rules: {...}` object of eslint-factory/src/index.ts, sorted.
 * @param {string} source
 * @returns {string[]}
 */
function parseESLintRuleIds(source) {
  const ids = [...String(source).matchAll(/^\s+"([^"]+)":\s*[A-Za-z0-9_]+Rule,?\s*$/gm)].map(([, id]) => id);
  if (ids.length === 0) throw new Error("ESLint rule registry contains no rules");
  const seen = new Set();
  for (const id of ids) {
    if (!RULE_ID.test(id)) throw new Error(`ESLint rule id is invalid: ${id}`);
    if (seen.has(id)) throw new Error(`ESLint rule registry repeats ${id}`);
    seen.add(id);
  }
  return ids.sort();
}

/**
 * The day's rotating cohort: RULES_PER_DAY ids taken cyclically from the ordered list.
 * @param {string} date
 * @param {readonly string[]} rules
 */
function rulesForDay(date, rules) {
  const day = factoryDay(date);
  const offset = (day * RULES_PER_DAY) % rules.length;
  return Array.from({ length: Math.min(RULES_PER_DAY, rules.length) }, (_, index) => rules[(offset + index) % rules.length]);
}

/** @param {unknown} rules */
function assertRules(rules) {
  if (!Array.isArray(rules) || rules.length === 0 || new Set(rules).size !== rules.length || !rules.every(rule => typeof rule === "string" && RULE_ID.test(rule))) {
    throw new Error("ESLint rules must be a non-empty array of unique rule ids");
  }
}

/**
 * @param {string} profile
 * @param {{id: string, source: string, test: string, config_id: string}} rule
 */
function rulePlan(profile, rule) {
  if (profile === "eslint-refiner")
    return `Review only the ESLint rule ${rule.id} (${rule.source} and ${rule.test}) for false positives, missed edge cases, and weak diagnostics against actions/setup/js for the immutable factory date. ${SAFETY_CLAUSE}`;
  return `Remediate only diagnostics reported by ${rule.config_id} in actions/setup/js for the immutable factory date (warnings count as diagnostics). ${SAFETY_CLAUSE}`;
}

/** @param {{date: string, repository: string, repositoryId: string, rules: string[]}} options */
function buildESLintFactoryPlan({ date, repository, repositoryId, rules }) {
  assertRepository(repository);
  factoryDay(date);
  if (typeof repositoryId !== "string" || !/^[1-9][0-9]{0,255}$/.test(repositoryId)) throw new Error("ESLint repository ID must be a verified positive decimal identity");
  assertRules(rules);
  const cohort = rulesForDay(date, [...rules].sort());
  const resourceScope = () => ({ version: 1, resources: [{ host: "github.com", repository, repository_id: repositoryId }] });
  /**
   * @param {string} nodeKey
   * @param {string} profile
   * @param {Record<string, unknown>} payload
   */
  const node = (nodeKey, profile, payload) => ({
    graph_id: `eslint-factory-cohort:${date}`,
    node_key: nodeKey,
    pool: FACTORY_POOL,
    priority: 3,
    fairness_key: "",
    worker_profile: profile,
    payload: { ...payload, factory_date: date, factory_profile: profile, resource_scope: resourceScope(), effect_contract: factoryContract(profile) },
    depends_on: [],
  });
  const nodes = [node("eslint-miner", "eslint-miner", { plan: `Run this workflow's existing ESLint mission for the immutable factory date. ${SAFETY_CLAUSE}` })];
  for (const id of cohort) {
    const rule = { id, source: `eslint-factory/src/rules/${id}.ts`, test: `eslint-factory/src/rules/${id}.test.ts`, config_id: `gh-aw-custom/${id}` };
    for (const profile of RULE_WORKERS) nodes.push(node(`${profile}:${id}`, profile, { plan: rulePlan(profile, rule), rule }));
  }
  return { version: 1, date, rules: cohort, nodes, dispatch: { pool: FACTORY_POOL, max_claims: nodes.length, max_dispatches: nodes.length } };
}

module.exports = { ESLINT_WORKERS, RULE_WORKERS, FACTORY_POOL, RULES_PER_DAY, parseESLintRuleIds, rulesForDay, buildESLintFactoryPlan };
