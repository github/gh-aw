// @ts-check
"use strict";

const { API_VERSION, nativeId } = require("./work_queue_native.cjs");
const { canonical, digest } = require("./work_queue_codec.cjs");
const { gateKey } = require("./work_queue_graph.cjs");

const COMMIT_SHA = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/;

/** Stable key of an external reference, with or without its numeric identities. */
function dependencyKey(resource) {
  if (resource.repository_id !== undefined && resource.resource_id !== undefined) {
    return gateKey({ ...resource, repository_id: nativeId(resource.repository_id, "external repository ID"), resource_id: nativeId(resource.resource_id, "external resource ID") }, resource.condition);
  }
  return canonical({ kind: resource.kind, host: resource.host, condition: resource.condition, repository: resource.repository.toLowerCase(), number: nativeId(resource.number, "external resource number") });
}

/** The readable repository scope of a reference; throws external_not_allowlisted otherwise. */
function allowedScope(resource, scopes) {
  if (!resource || !["issue", "pull_request"].includes(resource.kind) || typeof resource.host !== "string" || !/^[a-z0-9.-]+$/.test(resource.host) || !/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(resource.repository))
    throw new TypeError("external_resource_invalid");
  nativeId(resource.number, "external resource number");
  if (!(resource.kind === "issue" ? ["completed", "closed"] : ["merged"]).includes(resource.condition)) throw new TypeError("external_condition_invalid");
  const scope = scopes.find(candidate => candidate.host === resource.host && candidate.repository.toLowerCase() === resource.repository.toLowerCase());
  if (!scope) throw new Error("external_not_allowlisted");
  return scope;
}

/**
 * Observation fields for one resource read. `ready` is the only satisfying state.
 * @returns {{state: string, state_reason?: string, merged?: boolean, merge_commit?: string}}
 */
function predicate(resource, data) {
  if (resource.kind === "issue") {
    if (data.pull_request !== undefined) throw new Error("external_resource_type_mismatch");
    if (data.state === "open") return { state: "waiting", state_reason: "reopened" };
    if (data.state !== "closed") return { state: "unknown" };
    const reason = ["completed", "not_planned"].includes(data.state_reason) ? { state_reason: data.state_reason } : {};
    if (resource.condition === "closed" || data.state_reason === "completed") return { state: "ready", ...reason };
    return data.state_reason === "not_planned" ? { state: "failed", ...reason } : { state: "unknown" };
  }
  if (typeof data.merged !== "boolean") return { state: "unknown" };
  if (data.merged) return typeof data.merge_commit_sha === "string" && COMMIT_SHA.test(data.merge_commit_sha) ? { state: "ready", merged: true, merge_commit: data.merge_commit_sha } : { state: "unknown", merged: true };
  if (data.state === "closed") return { state: "failed", merged: false };
  return data.state === "open" ? { state: "waiting", merged: false } : { state: "unknown", merged: false };
}

/**
 * Read external references. Each result carries the resolved identity (`binding`)
 * or, when unreadable, `error`; reads never throw for API failures.
 * @param {Array<Record<string, any>>} resources references with `condition`
 * @param {{scopes: Array<{host: string, repository: string}>, getClient: (scope: any) => Promise<any>, maxResources?: number, maxReads?: number}} options
 * @returns {Promise<{results: Map<string, {resource: Record<string, any>, binding?: Record<string, any>, observation?: Record<string, any>, error?: string}>, reads: number}>}
 */
async function resolveDependencies(resources, options) {
  const maxResources = options.maxResources ?? 128;
  const maxReads = options.maxReads ?? maxResources * 2;
  if (!Number.isSafeInteger(maxResources) || maxResources < 1 || maxResources > 256 || !Number.isSafeInteger(maxReads) || maxReads < 1 || maxReads > 512) throw new TypeError("external_resolver_budget_invalid");
  if (!Array.isArray(resources)) throw new TypeError("external_dependencies_invalid");
  const normalized = new Map();
  for (const resource of resources) normalized.set(dependencyKey(resource), resource);
  if (normalized.size > maxResources) throw new TypeError("external_resolver_budget_exceeded");
  const repositories = new Map();
  let reads = 0;
  const results = new Map();
  const read = async request => {
    if (reads >= maxReads) throw new Error("external_read_budget_exhausted");
    reads++;
    const response = await request();
    if (response?.status !== undefined && response.status !== 200) throw Object.assign(new Error("external read failed"), { status: response.status });
    return response.data;
  };
  for (const [key, resource] of normalized) {
    try {
      const scope = allowedScope(resource, options.scopes || []);
      const client = await options.getClient(scope);
      if (!client) throw new Error("external_read_credentials_missing");
      const [owner, repo] = scope.repository.split("/");
      const parameters = { owner, repo, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } };
      const repoKey = scope.repository.toLowerCase();
      if (!repositories.has(repoKey)) repositories.set(repoKey, await read(() => client.rest.repos.get(parameters)));
      const repository = repositories.get(repoKey);
      if (typeof repository?.full_name !== "string" || repository.full_name.toLowerCase() !== repoKey) throw new Error("external_redirect_not_allowlisted");
      const repositoryId = nativeId(repository.id, "external repository ID");
      if (resource.repository_id !== undefined && nativeId(resource.repository_id) !== repositoryId) throw new Error("external_identity_mismatch");
      const number = nativeId(resource.number, "external resource number");
      const data = await read(() => (resource.kind === "issue" ? client.rest.issues.get({ ...parameters, issue_number: Number(number) }) : client.rest.pulls.get({ ...parameters, pull_number: Number(number) })));
      const resourceId = nativeId(data?.id, "external resource ID");
      if (nativeId(data?.number, "external resource number") !== number || (resource.resource_id !== undefined && nativeId(resource.resource_id) !== resourceId)) throw new Error("external_identity_mismatch");
      if (resource.kind === "pull_request" && String(data?.base?.repo?.id) !== repositoryId) throw new Error("external_resource_type_mismatch");
      const binding = { kind: resource.kind, host: resource.host, repository: repository.full_name, repository_id: repositoryId, resource_id: resourceId, number };
      const fields = predicate(resource, data);
      const operation = { kind: "Observation", resource: binding, condition: resource.condition, ...fields };
      results.set(key, { resource, binding, observation: { ...operation, observation_id: `observation:${digest(operation)}` } });
    } catch (error) {
      const message = error instanceof Error ? error.message : "";
      results.set(key, { resource, error: /^external_[a-z_]+$/.test(message) ? message : failureCode(error) });
    }
  }
  return { results, reads };
}

function failureCode(error) {
  const status = error?.status;
  if (status === 401 || status === 403) return "external_access_denied";
  if (status === 404) return "external_not_found";
  if (status === 429) return "external_rate_limited";
  return "external_unavailable";
}

/**
 * Fresh Observation operations for fully identified external edges. Unreadable
 * edges produce no Observation (the edge stays unsatisfied).
 * @param {Array<{kind: string, resource: Record<string, any>, condition: string}>} edges
 * @param {Parameters<typeof resolveDependencies>[1]} options
 */
async function resolveExternalEdges(edges, options) {
  const resolved = await resolveDependencies(
    edges.map(edge => ({ ...edge.resource, condition: edge.condition })),
    options
  );
  return [...resolved.results.values()].flatMap(result => (result.observation ? [result.observation] : []));
}

module.exports = { allowedScope, dependencyKey, failureCode, predicate, resolveDependencies, resolveExternalEdges };
