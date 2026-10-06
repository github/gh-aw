// @ts-check
"use strict";

const { parseStrictJSON, utf8Compare } = require("./work_queue_codec.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { readStagedIntentBatch } = require("./work_queue_intents.cjs");
const { DEFAULT_INTENT_PATH } = require("./work_queue_mcp_server.cjs");
const { API_VERSION, nativeId } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

function credentialBindings(raw) {
  const value = raw === undefined ? {} : parseStrictJSON(raw);
  if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).length > 64) throw new Error("work_queue_dependency_credentials_invalid");
  const normalized = new Map();
  for (const [repository, variable] of Object.entries(value)) {
    if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9._-]+$/.test(repository) || typeof variable !== "string" || !/^GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_(?:0|[1-9][0-9]*)$/.test(variable)) throw new Error("work_queue_dependency_credentials_invalid");
    const key = repository.toLowerCase();
    if (normalized.has(key)) throw new Error("work_queue_dependency_credentials_conflict");
    normalized.set(key, variable);
  }
  return normalized;
}

function readOnlyRepositoryClient(client, scope) {
  const [owner, repo] = scope.repository.split("/");
  const parameters = { owner, repo, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } };
  const get = (namespace, selector) => async input => {
    const keys = new Set(["owner", "repo", "headers", "request", ...(selector ? [selector] : [])]);
    if (
      !input ||
      Object.keys(input).some(key => !keys.has(key)) ||
      `${input.owner}/${input.repo}`.toLowerCase() !== scope.repository.toLowerCase() ||
      (input.headers && (Object.keys(input.headers).some(key => key !== "X-GitHub-Api-Version") || input.headers["X-GitHub-Api-Version"] !== API_VERSION)) ||
      (input.request && (Object.keys(input.request).some(key => !["retries", "timeout"].includes(key)) || input.request.retries !== 0 || input.request.timeout !== 15000))
    )
      throw new Error("work_queue_dependency_client_scope_invalid");
    const method = client?.rest?.[namespace]?.get;
    if (typeof method !== "function") throw new Error("external_read_credentials_missing");
    return method.call(client.rest[namespace], { ...parameters, headers: { ...parameters.headers }, request: { ...parameters.request }, ...(selector ? { [selector]: nativeId(input[selector], "external resource number") } : {}) });
  };
  return Object.freeze({
    rest: Object.freeze({
      repos: Object.freeze({ get: get("repos") }),
      issues: Object.freeze({ get: get("issues", "issue_number") }),
      pulls: Object.freeze({ get: get("pulls", "pull_number") }),
    }),
  });
}

/** @param {Record<string, any>} options */
function createCompilerDependencyResolver(options) {
  const env = options.env || process.env;
  const server = new URL(env.GITHUB_SERVER_URL || "https://github.com");
  const api = new URL(env.GITHUB_API_URL || (server.hostname === "github.com" ? "https://api.github.com" : `https://${server.hostname}/api/v3`));
  if (
    server.protocol !== "https:" ||
    server.username ||
    server.password ||
    server.port ||
    server.search ||
    server.hash ||
    server.pathname !== "/" ||
    api.protocol !== "https:" ||
    api.username ||
    api.password ||
    api.port ||
    api.search ||
    api.hash ||
    ![server.hostname, `api.${server.hostname}`].includes(api.hostname) ||
    !["/", "/api/v3", "/api/v3/"].includes(api.pathname)
  )
    throw new Error("work_queue_dependency_host_invalid");
  const bindings = credentialBindings(env.GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS);
  const generation = options.state?.credential_generation;
  if (typeof generation !== "string" || !generation || !options.state?.policy?.pools) throw new Error("work_queue_policy_missing");
  const repositories = new Map();
  for (const pool of Object.values(options.state.policy.pools)) {
    for (const repository of pool.allowed_repositories) {
      if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9._-]+$/.test(repository)) throw new Error("work_queue_dependency_repository_invalid");
      repositories.set(repository.toLowerCase(), repository);
    }
  }
  const ownRepository = `${options.context.repo.owner}/${options.context.repo.repo}`.toLowerCase();
  const scopes = Object.freeze([...repositories.values()].sort(utf8Compare).map(repository => Object.freeze({ host: server.hostname, repository, access_generation: generation })));
  const clients = new Map();
  const tokens = new Map();
  for (const [repository, variable] of bindings) {
    const token = env[variable];
    if (token !== undefined && (typeof token !== "string" || token.length > 16384 || /[\x00-\x1f\x7f]/.test(token))) throw new Error("work_queue_dependency_credentials_invalid");
    if (token) tokens.set(repository, token);
  }
  const getClient = async scope => {
    const approved = scopes.find(candidate => candidate.host === scope?.host && candidate.repository.toLowerCase() === scope?.repository?.toLowerCase() && candidate.access_generation === scope?.access_generation);
    if (!approved) throw new Error("external_not_allowlisted");
    const key = approved.repository.toLowerCase();
    if (!clients.has(key)) {
      let client;
      if (tokens.has(key)) {
        const factory = options.getOctokit || global.getOctokit;
        if (typeof factory !== "function") throw new Error("external_read_credentials_missing");
        client = await factory(tokens.get(key), { baseUrl: api.href.replace(/\/$/, "") });
      } else if (key === ownRepository) client = options.githubClient;
      else return null;
      if (!client) throw new Error("external_read_credentials_missing");
      clients.set(key, readOnlyRepositoryClient(client, approved));
    }
    return clients.get(key);
  };
  return Object.freeze({ scopes, getClient });
}

/** @param {Record<string, any>} [options] */
async function main(options = {}) {
  /** @type {Record<string, any>} */
  const configured = { ...options, githubClient: options.githubClient || options.github || global.github, context: options.context || global.context };
  const intents = readStagedIntentBatch(configured.intentPath || process.env.GH_AW_WORK_QUEUE_INTENTS || DEFAULT_INTENT_PATH).intents;
  if (intents.some(intent => ["submit", "dispatch_next"].includes(intent.kind)) && !isStagedMode(configured) && !isStagedMode(configured.config)) {
    const latest = await loadQueue({ ...configured, policyProposal: undefined, initializationContext: undefined });
    configured.dependencyResolver = createCompilerDependencyResolver({ ...configured, state: latest.projection });
  }
  return require("./work_queue_dispatch.cjs").main(configured);
}

module.exports = { credentialBindings, readOnlyRepositoryClient, createCompilerDependencyResolver, main };
