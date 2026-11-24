// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const { parseStrictJSON, utf8Compare } = require("./work_queue_codec.cjs");
const { API_VERSION, nativeId } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

/**
 * @typedef {Record<string, unknown> & {
 *   env?: NodeJS.ProcessEnv,
 *   context?: {repo: {owner: string, repo: string}},
 *   core?: {setOutput: (name: string, value: unknown) => unknown, info: (message: string) => unknown, warning?: (message: string) => unknown, setFailed?: (message: string) => unknown},
 *   github?: object,
 *   githubClient?: object,
 *   queueClient?: object,
 *   dispatchClient?: object,
 *   getOctokit?: (token: string, options?: {baseUrl?: string, credentialSource?: string}) => object | Promise<object>,
 *   config?: Record<string, unknown> & {"github-token"?: string},
 *   intentPath?: string
 * }} CompilerControlOptions
 */

const REPOSITORY = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9._-]+$/;

function validToken(token) {
  return typeof token === "string" && token.length > 0 && token.length <= 16384 && !/[\x00-\x1f\x7f]/.test(token);
}

/** Repository → env variable holding a read token for external dependency reads. */
function credentialBindings(raw) {
  const value = raw === undefined || raw === "" ? {} : parseStrictJSON(raw);
  if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).length > 64) throw new Error(`${SAFE_OUTPUT_E001}: work_queue_dependency_credentials_invalid`);
  const normalized = new Map();
  for (const [repository, variable] of Object.entries(value)) {
    if (!REPOSITORY.test(repository) || typeof variable !== "string" || !/^GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_(?:0|[1-9][0-9]*)$/.test(variable)) throw new Error("work_queue_dependency_credentials_invalid");
    const key = repository.toLowerCase();
    if (normalized.has(key)) throw new Error("work_queue_dependency_credentials_conflict");
    normalized.set(key, { repository, variable });
  }
  return normalized;
}

const LOGIN = /^[A-Za-z0-9](?:[A-Za-z0-9]|-(?=[A-Za-z0-9])){0,38}$/;

/**
 * A client restricted to GET reads of one repository, its Issues and its pull requests.
 * `collaborators` (the queue's own repository through the job client only) also exposes
 * the collaborator permission read used to verify continuation approvals.
 */
function readOnlyRepositoryClient(client, scope, { collaborators = false } = {}) {
  const [owner, repo] = scope.repository.split("/");
  const parameters = { owner, repo, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } };
  const call = (namespace, name, selector, normalize) => async input => {
    const keys = new Set(["owner", "repo", "headers", "request", ...(selector ? [selector] : [])]);
    if (!input || Object.keys(input).some(key => !keys.has(key)) || `${input.owner}/${input.repo}`.toLowerCase() !== scope.repository.toLowerCase()) throw new Error("work_queue_dependency_client_scope_invalid");
    const method = client?.rest?.[namespace]?.[name];
    if (typeof method !== "function") throw new Error("external_read_credentials_missing");
    return method.call(client.rest[namespace], { ...parameters, headers: { ...parameters.headers }, request: { ...parameters.request }, ...(selector ? { [selector]: normalize(input[selector]) } : {}) });
  };
  const number = value => Number(nativeId(value, "external resource number"));
  const login = value => {
    if (typeof value !== "string" || !LOGIN.test(value)) throw new Error("work_queue_dependency_client_scope_invalid");
    return value;
  };
  return Object.freeze({
    rest: Object.freeze({
      repos: Object.freeze({ get: call("repos", "get"), ...(collaborators ? { getCollaboratorPermissionLevel: call("repos", "getCollaboratorPermissionLevel", "username", login) } : {}) }),
      issues: Object.freeze({ get: call("issues", "get", "issue_number", number) }),
      pulls: Object.freeze({ get: call("pulls", "get", "pull_number", number) }),
    }),
  });
}

/**
 * External dependency reads: the queue's own repository through the job client,
 * plus every repository bound to a GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_<n>.
 * @param {CompilerControlOptions & {githubClient: object, context: {repo: {owner: string, repo: string}}}} options
 */
function createCompilerDependencyResolver(options) {
  const env = options.env || process.env;
  const server = new URL(env.GITHUB_SERVER_URL || "https://github.com");
  const api = new URL(env.GITHUB_API_URL || (server.hostname === "github.com" ? "https://api.github.com" : `https://${server.hostname}/api/v3`));
  if (server.protocol !== "https:" || api.protocol !== "https:" || server.username || server.password || api.username || api.password || ![server.hostname, `api.${server.hostname}`].includes(api.hostname))
    throw new Error("work_queue_dependency_host_invalid");
  const bindings = credentialBindings(env.GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS);
  const ownRepository = `${options.context.repo.owner}/${options.context.repo.repo}`;
  const repositories = new Map([[ownRepository.toLowerCase(), ownRepository], ...[...bindings.values()].map(/** @returns {[string, string]} */ binding => [binding.repository.toLowerCase(), binding.repository])]);
  const scopes = Object.freeze([...repositories.values()].sort(utf8Compare).map(repository => Object.freeze({ host: server.hostname, repository })));
  const clients = new Map();
  const getClient = async scope => {
    const key = String(scope?.repository).toLowerCase();
    const approved = scopes.find(candidate => candidate.host === scope?.host && candidate.repository.toLowerCase() === key);
    if (!approved) throw new Error("external_not_allowlisted");
    if (!clients.has(key)) {
      const token = bindings.get(key) && env[bindings.get(key).variable];
      let client;
      if (token) {
        if (!validToken(token)) throw new Error("work_queue_dependency_credentials_invalid");
        const factory = options.getOctokit || global.getOctokit;
        if (typeof factory !== "function") throw new Error("external_read_credentials_missing");
        client = await factory(token, { baseUrl: api.href.replace(/\/$/, "") });
      } else if (key === ownRepository.toLowerCase()) client = options.githubClient;
      else return null;
      // Continuation approvals are verified only through the job's own-repository client.
      clients.set(key, readOnlyRepositoryClient(client, approved, { collaborators: !token }));
    }
    return clients.get(key);
  };
  return Object.freeze({ scopes, getClient });
}

/**
 * Compiler entry point of the "Process trusted work queue controls" step: build the
 * queue client (GH_AW_WORK_QUEUE_TOKEN), the dispatch client (`github-token` of the
 * dispatch-workflow config) and the dependency resolver, then run the controls.
 * @param {CompilerControlOptions} [options]
 */
async function main(options = {}) {
  const env = options.env || process.env;
  /** @type {CompilerControlOptions & {githubClient: object, context: {repo: {owner: string, repo: string}}}} */
  const configured = { ...options, githubClient: options.githubClient || options.github || global.github, context: options.context || global.context };
  const preview = isStagedMode(configured) || isStagedMode(configured.config);
  const factory = configured.getOctokit || global.getOctokit;
  const credentialSource = env.GH_AW_GITHUB_TOKEN_SOURCE || "unknown";
  if (!preview && configured.queueClient === undefined && env.GH_AW_WORK_QUEUE_TOKEN !== undefined) {
    if (!validToken(env.GH_AW_WORK_QUEUE_TOKEN)) throw new Error("work_queue_publisher_credential_token_invalid");
    if (typeof factory !== "function") throw new Error("work_queue_publisher_credential_client_unavailable");
    configured.queueClient = await factory(env.GH_AW_WORK_QUEUE_TOKEN, { credentialSource });
  }
  const token = configured.config?.["github-token"];
  if (!preview && configured.dispatchClient === undefined && token !== undefined) {
    if (!validToken(token)) throw new Error("work_queue_dispatch_credential_token_invalid");
    if (typeof factory !== "function") throw new Error("work_queue_dispatch_credential_client_unavailable");
    configured.dispatchClient = credentialSource === "unknown" ? await factory(token) : await factory(token, { credentialSource });
  }
  if (!preview) configured.dependencyResolver = createCompilerDependencyResolver(configured);
  return require("./work_queue_dispatch.cjs").main(/** @type {any} */ configured);
}

module.exports = { createCompilerDependencyResolver, credentialBindings, main, readOnlyRepositoryClient };
