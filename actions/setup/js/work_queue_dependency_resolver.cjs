// @ts-check
"use strict";

const { API_VERSION, immutableRef, nativeId } = require("./work_queue_native.cjs");
const { canonical, digest } = require("./work_queue_codec.cjs");
const { gateKey } = require("./work_queue_graph.cjs");

function dependencyKey(resource) {
  if (resource.repository_id !== undefined && resource.resource_id !== undefined) {
    return gateKey({ ...resource, repository_id: nativeId(resource.repository_id, "external repository ID"), resource_id: nativeId(resource.resource_id, "external resource ID") }, resource.condition);
  }
  const identity = {
    kind: resource.kind,
    host: resource.host,
    condition: resource.condition,
    ...(resource.repository_id === undefined ? {} : { repository_id: nativeId(resource.repository_id, "external repository ID") }),
    ...(resource.resource_id === undefined ? {} : { resource_id: nativeId(resource.resource_id, "external resource ID") }),
  };
  if (resource.repository_id === undefined || resource.resource_id === undefined) {
    identity.repository = resource.repository.toLowerCase();
    identity.number = nativeId(resource.number, "external resource number");
  }
  return canonical(identity);
}

function allowedScope(resource, scopes) {
  if (!resource || !["issue", "pull_request"].includes(resource.kind) || typeof resource.host !== "string" || !/^[a-z0-9.-]+$/.test(resource.host) || !/^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(resource.repository))
    throw new TypeError("external_resource_invalid");
  nativeId(resource.number, "external resource number");
  if (!(resource.kind === "issue" ? ["completed", "closed"] : ["merged"]).includes(resource.condition)) throw new TypeError("external_condition_invalid");
  const scope = scopes.find(scope => scope.host === resource.host && scope.repository.toLowerCase() === resource.repository.toLowerCase());
  if (!scope || typeof scope.access_generation !== "string" || !scope.access_generation) throw new Error("external_not_allowlisted");
  return scope;
}

function failureCode(error) {
  const status = error?.status;
  if (status === 401 || status === 403) return "external_access_denied";
  if (status === 404) return "external_not_found";
  if (status === 429) return "external_rate_limited";
  return "external_unavailable";
}

function mergeRevision(value) {
  try {
    return immutableRef(value);
  } catch {
    return null;
  }
}

function predicate(resource, data) {
  if (resource.kind === "issue") {
    if (data.pull_request !== undefined) throw new Error("external_resource_type_mismatch");
    if (!["open", "closed"].includes(data.state)) return { state: "unknown", read_status: "external_predicate_unknown" };
    if (data.state === "open") return { state: "waiting", read_status: "external_reopened" };
    if (resource.condition === "closed" || data.state_reason === "completed") return { state: "ready", read_status: "ok" };
    if (data.state_reason === null || data.state_reason === undefined) return { state: "unknown", read_status: "external_predicate_unknown" };
    return { state: "failed", read_status: "external_closed_not_completed" };
  }
  if (typeof data.merged !== "boolean") return { state: "unknown", read_status: "external_predicate_unknown" };
  if (data.merged === true) {
    const mergedAt = typeof data.merged_at === "string" ? Date.parse(data.merged_at) : NaN;
    if (!Number.isSafeInteger(mergedAt) || mergedAt < 0 || !mergeRevision(data.merge_commit_sha)) return { state: "unknown", read_status: "external_merge_evidence_missing" };
    return { state: "ready", read_status: "ok" };
  }
  if (data.state === "closed") return { state: "failed", read_status: "external_closed_unmerged" };
  return data.state === "open" ? { state: "waiting", read_status: "ok" } : { state: "unknown", read_status: "external_predicate_unknown" };
}

function isFreshObservation(observation, resource, scope, now, maxAge) {
  return (
    observation?.state === "ready" &&
    observation.access_generation === scope.access_generation &&
    dependencyKey(observation.resource) === dependencyKey(resource) &&
    Number.isSafeInteger(observation.observed_at) &&
    observation.observed_at <= now &&
    now - observation.observed_at <= maxAge &&
    observation.invalidated !== true
  );
}

async function resolveDependencies(resources, options) {
  const maxResources = options.maxResources ?? 128;
  const maxReads = options.maxReads ?? maxResources * 2;
  const now = options.now ?? Date.now();
  if (!Number.isSafeInteger(maxResources) || maxResources < 1 || maxResources > 256 || !Number.isSafeInteger(maxReads) || maxReads < 1 || maxReads > 512 || !Number.isSafeInteger(now) || now < 0)
    throw new TypeError("external_resolver_budget_invalid");
  if (!Array.isArray(resources) || resources.length > 256) throw new TypeError("external_resolver_budget_exceeded");
  const normalized = new Map();
  for (const resource of resources) {
    const scope = allowedScope(resource, options.scopes || []);
    if (resource.repository_id !== undefined) nativeId(resource.repository_id, "external repository ID");
    if (resource.resource_id !== undefined) nativeId(resource.resource_id, "external resource ID");
    const key = dependencyKey(resource);
    const previous = normalized.get(key);
    if (previous && nativeId(previous.resource.number) !== nativeId(resource.number)) throw new Error("external_identity_mismatch");
    normalized.set(key, { resource, scope });
  }
  if (normalized.size > maxResources) throw new TypeError("external_resolver_budget_exceeded");
  const repositories = new Map();
  const reads = new Map();
  let readCount = 0;
  const observations = [];
  const bindings = new Map();
  for (const [key, { resource, scope }] of normalized) {
    let resolved = { ...resource };
    const observation = { resource: resolved, state: "unknown", read_status: "external_unavailable", observed_at: now, source_updated_at: null, access_generation: scope.access_generation };
    try {
      const client = await options.getClient(scope);
      if (!client) throw new Error("external_read_credentials_missing");
      const [owner, repo] = scope.repository.split("/");
      const parameters = { owner, repo, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } };
      const repoKey = `${scope.host}/${scope.repository.toLowerCase()}/${scope.access_generation}`;
      let repository = repositories.get(repoKey);
      if (!repository) {
        if (readCount >= maxReads) throw new Error("external_read_budget_exhausted");
        readCount++;
        repository = await client.rest.repos.get(parameters);
        repositories.set(repoKey, repository);
      }
      if (repository.status !== undefined && repository.status !== 200) throw Object.assign(new Error("external read failed"), { status: repository.status });
      const actualRepo = repository.data?.full_name;
      if (typeof actualRepo !== "string" || !options.scopes.some(candidate => candidate.host === resource.host && candidate.repository.toLowerCase() === actualRepo.toLowerCase() && candidate.access_generation === scope.access_generation))
        throw new Error("external_redirect_not_allowlisted");
      const repositoryId = nativeId(repository.data?.id, "external repository ID");
      if (resource.repository_id !== undefined && resource.repository_id !== repositoryId) throw new Error("external_identity_mismatch");
      const resourceKey = `${repoKey}/${resource.kind}/${resource.number}`;
      let response = reads.get(resourceKey);
      if (!response) {
        if (readCount >= maxReads) throw new Error("external_read_budget_exhausted");
        readCount++;
        response = resource.kind === "issue" ? await client.rest.issues.get({ ...parameters, issue_number: resource.number }) : await client.rest.pulls.get({ ...parameters, pull_number: resource.number });
        reads.set(resourceKey, response);
      }
      if (response.status !== undefined && response.status !== 200) throw Object.assign(new Error("external read failed"), { status: response.status });
      const data = response.data;
      const resourceId = nativeId(data?.id, "external resource ID");
      if (nativeId(data?.number, "external resource number") !== nativeId(resource.number) || (resource.resource_id !== undefined && resource.resource_id !== resourceId)) throw new Error("external_identity_mismatch");
      if (resource.kind === "pull_request" && (!data?.base?.repo || nativeId(data.base.repo.id) !== repositoryId)) throw new Error("external_resource_type_mismatch");
      resolved = { ...resource, repository: actualRepo, repository_id: repositoryId, resource_id: resourceId, number: nativeId(resource.number) };
      observation.resource = resolved;
      Object.assign(observation, predicate(resource, data), { source_updated_at: typeof data.updated_at === "string" ? data.updated_at : null });
      if (["open", "closed"].includes(data.state)) observation.resource_state = data.state;
      if (["completed", "not_planned"].includes(data.state_reason)) observation.state_reason = data.state_reason;
      if (data.state === "open" && resource.kind === "issue") observation.state_reason = "reopened";
      if (typeof data.merged === "boolean") observation.merged = data.merged;
      const mergeCommit = mergeRevision(data.merge_commit_sha);
      if (mergeCommit) observation.merge_commit = mergeCommit;
    } catch (error) {
      const code = typeof error?.message === "string" && /^external_[a-z_]+$/.test(error.message) ? error.message : failureCode(error);
      observation.read_status = code;
      observation.state = ["external_identity_mismatch", "external_resource_type_mismatch", "external_redirect_not_allowlisted"].includes(code) ? "failed" : "unknown";
    }
    observations.push(observation);
    bindings.set(key, observation.resource);
  }
  return { observations, reads: readCount, bindings };
}

async function resolveExternalEdges(edges, options) {
  if (!Array.isArray(edges)) throw new TypeError("external_dependencies_invalid");
  const resources = edges
    .filter(edge => edge.kind !== "work")
    .map(edge => {
      if (edge.kind !== edge.resource?.kind) throw new TypeError("external_resource_type_mismatch");
      return { ...edge.resource, condition: edge.condition };
    });
  const result = await resolveDependencies(resources, options);
  const operations = result.observations.map(observation => {
    const { condition, ...resource } = observation.resource;
    resource.number = nativeId(resource.number, "external resource number");
    nativeId(resource.repository_id, "external repository ID");
    nativeId(resource.resource_id, "external resource ID");
    const operation = {
      kind: "Observation",
      resource,
      condition,
      state: observation.state,
      observed_at: observation.observed_at,
      credential_generation: observation.access_generation,
      read_status: observation.read_status,
    };
    const updated = Date.parse(observation.source_updated_at);
    if (Number.isSafeInteger(updated) && updated >= 0) operation.source_updated_at = updated;
    for (const key of ["state_reason", "resource_state", "merged", "merge_commit"]) {
      if (Object.hasOwn(observation, key)) operation[key] = observation[key];
    }
    return { ...operation, observation_id: `observation:${digest(operation)}` };
  });
  return { ...result, operations };
}

module.exports = { dependencyKey, allowedScope, predicate, failureCode, isFreshObservation, resolveDependencies, resolveExternalEdges };
