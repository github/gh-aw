// @ts-check
"use strict";

const { assertClaimAuthorized, currentClaimHandle, recordClaimEffect, claimIdentity, assertClaimIdentity, currentClaimResourceEffects } = require("./work_queue_claim_scope.cjs");
const { canonical, digest, parseStrictJSON } = require("./work_queue_codec.cjs");
const { canonicalResourceTarget, nativeDecimalIdentity, resolveRepositoryTarget, resolveParentResourceTarget } = require("./work_queue_effect_resource.cjs");

const READ_METHODS = new Set(["get", "list", "getSarif", "getAnalysis", "getComment", "getCommit", "getRef", "getTree", "getBlob", "getArtifact", "getBranch", "getByUsername", "getWorkflowRun", "getWorkflowRunAttempt"]);
/** @type {WeakMap<object, {identity: ReturnType<typeof claimIdentity>, options: Record<string, unknown>}>} */
const claimClients = new WeakMap();
const NODE_QUERY = `query WorkQueueEffectTargets($ids: [ID!]!) {
  nodes(ids: $ids) {
    __typename
    id
    ... on Repository { nameWithOwner databaseId }
    ... on Ref { name prefix repository { nameWithOwner databaseId } }
    ... on Issue { databaseId number repository { nameWithOwner databaseId } }
    ... on PullRequest { databaseId number repository { nameWithOwner databaseId } }
    ... on Discussion { number repository { nameWithOwner databaseId } }
    ... on DiscussionCategory { repository { nameWithOwner databaseId } }
    ... on IssueComment { databaseId issue { __typename databaseId number repository { nameWithOwner databaseId } } }
    ... on PullRequestReview { pullRequest { __typename databaseId number repository { nameWithOwner databaseId } } }
    ... on PullRequestReviewComment { databaseId pullRequest { __typename databaseId number repository { nameWithOwner databaseId } } }
    ... on DiscussionComment { discussion { number repository { nameWithOwner databaseId } } }
  }
}`;

/** @typedef {{DEFAULTS?: Record<string, unknown>, merge?: (...args: unknown[]) => Record<string, unknown>, parse?: (options: Record<string, unknown>) => {url: string, method: string, body?: unknown, headers?: unknown}}} NativeEndpoint */
/** @typedef {Function & {endpoint?: NativeEndpoint, defaults?: (options: Record<string, unknown>) => NativeTransport}} NativeTransport */

/** @param {unknown} value @returns {Record<string, unknown>} */
function requestObject(value) {
  if (value === undefined) return {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Claim transport requires resolved object parameters");
  return Object.fromEntries(Object.entries(value));
}

/** @param {string} value @param {string | URL} [base] */
function nativeUrl(value, base) {
  try {
    return new URL(value, base);
  } catch (cause) {
    throw new Error("Claim API target is not a valid URL", { cause });
  }
}

/** @param {string} route */
function nativePathname(route) {
  const url = route.replace(/^[A-Z]+\s+/, "");
  const pathname = /^https?:\/\//i.test(url) ? nativeUrl(url).pathname : url.split(/[?#]/)[0];
  try {
    return pathname.split("/").map(decodeURIComponent).join("/");
  } catch (cause) {
    throw new Error("Claim native target has malformed path encoding", { cause });
  }
}

/** @param {unknown} value @returns {unknown} */
function snapshotBody(value) {
  if (value === undefined || value === "") return undefined;
  if (typeof value === "string") return parseStrictJSON(value);
  if (!value || typeof value !== "object" || value instanceof Uint8Array) throw new Error("Claim effects require a resolved JSON request body");
  return parseStrictJSON(JSON.stringify(value));
}

/** @param {NativeTransport} target @param {unknown[]} args @param {string} [fallbackRoute] */
function requestSnapshot(target, args, fallbackRoute = "") {
  const endpoint = target.endpoint;
  const defaults = endpoint?.DEFAULTS || {};
  const supplied = requestObject(typeof args[0] === "string" ? args[1] : args[0]);
  const merge = endpoint?.merge;
  const parse = endpoint?.parse;
  const native = typeof merge === "function" && typeof parse === "function";
  const merged = native ? merge(...args) : { ...defaults, ...supplied };
  const route = merged.url || (typeof args[0] === "string" ? args[0] : defaults.url || fallbackRoute);
  const method = String(merged.method || (typeof args[0] === "string" ? args[0].match(/^([A-Z]+)\s/)?.[1] : undefined) || defaults.method || "POST").toUpperCase();
  const parsed = native ? parse(merged) : { url: String(route), method, body: Object.hasOwn(merged, "data") ? merged.data : supplied, headers: merged.headers };
  const body = snapshotBody(parsed.body);
  const fields = body && typeof body === "object" && !Array.isArray(body) ? requestObject(body) : {};
  const parameters = { ...merged };
  for (const field of ["branch", "ref", "path", "tree", "head", "head_repo"]) delete parameters[field];
  Object.assign(parameters, fields);
  // Forward the same resolved wire payload after authorization, even if SDK
  // defaults change during a native read or Claim authorization callback.
  const forwarded = native ? [{ ...merged, ...fields, url: parsed.url, method: parsed.method, headers: parsed.headers, data: body === undefined ? "" : JSON.stringify(body) }] : args;
  return { route: parsed.url, method: String(parsed.method).toUpperCase(), parameters, body, forwarded, native };
}

/** @param {NativeTransport} target @param {unknown[]} args */
function graphRequestSnapshot(target, args) {
  const source = typeof args[0] === "string" ? { query: args[0], ...requestObject(args[1]) } : requestObject(args[0]);
  if (typeof source.query !== "string") throw new Error("Claim GraphQL request has no explicit operation");
  const transport = new Set(["method", "baseUrl", "url", "headers", "request", "query", "mediaType", "operationName"]);
  /** @type {Record<string, unknown> & {variables: Record<string, unknown>}} */
  const options = { variables: {} };
  for (const [key, value] of Object.entries(source)) {
    if (transport.has(key)) options[key] = value;
    else options.variables[key] = value;
  }
  const snapshot = requestSnapshot(target, [options], "/graphql");
  const approved = nativeUrl(process.env.GITHUB_API_URL || "https://api.github.com");
  const actual = nativeUrl(snapshot.route, approved);
  if (actual.origin !== approved.origin || actual.pathname !== `${approved.pathname.replace(/\/$/, "")}/graphql` || actual.search || actual.hash || snapshot.method !== "POST")
    throw new Error("Claim GraphQL clients require the approved GraphQL transport");
  const body = requestObject(snapshot.body);
  if (body.query !== source.query || canonical(requestObject(body.variables)) !== canonical(options.variables)) throw new Error("Claim GraphQL defaults cannot override the authorized operation or variables");
  if (snapshot.native && typeof target.defaults !== "function") throw new Error("Claim GraphQL transport cannot pin its resolved request body");
  const forwardedTarget =
    snapshot.native && typeof target.defaults === "function" ? target.defaults({ url: actual.href, method: "POST", headers: snapshot.parameters.headers, request: snapshot.parameters.request, data: JSON.stringify(body) }) : target;
  return { query: source.query, variables: options.variables, target: forwardedTarget };
}

function repositoryFromRoute(route, parameters) {
  if (/^https?:\/\//i.test(String(route))) {
    const absolute = nativeUrl(route);
    if (absolute.origin !== nativeUrl(process.env.GITHUB_API_URL || "https://api.github.com").origin) throw new Error("Claim effects cannot use an unapproved API origin");
  }
  const url = nativePathname(String(route || ""));
  const prefix = nativeUrl(process.env.GITHUB_API_URL || "https://api.github.com").pathname.replace(/\/$/, "");
  const pathname = prefix && url.startsWith(`${prefix}/`) ? url.slice(prefix.length) : url;
  const match = pathname.match(/^\/repos\/([^/{}]+)\/([^/{}]+)(?:\/|$)/);
  if (match) return `${match[1]}/${match[2]}`;
  if ((pathname && !/^\/repos\/\{owner\}\/\{repo\}(?:\/|$)/.test(pathname)) || typeof parameters.owner !== "string" || typeof parameters.repo !== "string") throw new Error("Claim effect has no independently resolved repository target");
  return `${parameters.owner}/${parameters.repo}`;
}

function assertApiOrigin(route, parameters, defaults = {}) {
  const approved = nativeUrl(process.env.GITHUB_API_URL || "https://api.github.com");
  for (const base of [parameters.baseUrl, defaults.baseUrl]) {
    if (base !== undefined && nativeUrl(base).href.replace(/\/$/, "") !== approved.href.replace(/\/$/, "")) {
      throw new Error("Claim clients cannot override the approved API base URL");
    }
  }
  const url = String(route || "").replace(/^[A-Z]+\s+/, "");
  if (url.startsWith("//") || (/^[a-z][a-z0-9+.-]*:/i.test(url) && nativeUrl(url).origin !== approved.origin)) {
    throw new Error("Claim clients cannot use an unapproved API origin");
  }
}

/** @param {readonly Record<string, unknown>[] | null} [compiledResources] */
async function resolveRestResource(client, route, parameters, namespace, compiledResources = null) {
  const pathname = nativePathname(String(route || ""));
  const concrete = Boolean(pathname) && !pathname.includes("{");
  const contents = /\/contents\//.test(pathname);
  const refs = /\/git\/refs(?:\/|$)/.test(pathname);
  const pullCreation = /\/pulls\/?$/.test(pathname) || (!pathname && namespace === "pulls");
  const concreteNumber = pathname.match(/\/(?:issues|pulls|discussions)\/([1-9][0-9]*)(?:\/|$)/)?.[1];
  const resource = {};
  for (const field of ["review_id", "release_id", "tag_name", "workflow_id", "check_run_id", "deployment_id"]) {
    if (Object.prototype.hasOwnProperty.call(parameters, field)) resource[field] = parameters[field];
  }
  // Only selectors consumed by this native endpoint can supply authority.
  // Other JSON fields may be sent, but cannot turn an unrelated effect into
  // a file, ref, run, comment, or Subject-bound mutation.
  if ((refs || /\/code-scanning\/sarifs\/?$/.test(pathname) || /\/code-coverage\/report\/?$/.test(pathname) || (!pathname && ["git", "codeScanning"].includes(namespace))) && Object.hasOwn(parameters, "ref")) resource.ref = parameters.ref;
  if ((contents || (!pathname && namespace === "repos")) && Object.hasOwn(parameters, "path")) resource.path = parameters.path;
  if (pullCreation && compiledResources?.some(target => target.ref !== undefined)) {
    if (typeof parameters.head !== "string" || !parameters.head || parameters.head.includes(":") || parameters.head_repo !== undefined) throw new Error("Claim pull head requires an independently resolved native branch repository");
    resource.ref = `refs/heads/${parameters.head}`;
  }
  const concretePath = pathname.match(/\/contents\/(.+)$/)?.[1];
  if (concretePath && !concretePath.includes("{")) {
    const path = concretePath;
    if (resource.path !== undefined && resource.path !== path) throw new Error("Claim native file path conflicts with its request selector");
    resource.path = path;
  }
  const concreteRef = pathname.match(/\/git\/refs\/(.+)$/)?.[1];
  if (concreteRef && !concreteRef.includes("{")) {
    const ref = concreteRef;
    if (resource.ref !== undefined && resource.ref.replace(/^refs\//, "") !== ref.replace(/^refs\//, "")) throw new Error("Claim native ref conflicts with its request selector");
    resource.ref = ref;
  }
  if (refs && resource.ref === undefined) throw new Error("Claim native ref has no resolved effect selector");
  if (resource.path !== undefined && parameters.branch !== undefined) {
    if (typeof parameters.branch !== "string" || !parameters.branch) throw new Error("Claim native branch is not a resolved string");
    const ref = `refs/heads/${parameters.branch}`;
    if (resource.ref !== undefined && resource.ref !== ref) throw new Error("Claim native branch conflicts with its request selector");
    resource.ref = ref;
  }
  let number = concreteNumber || (concrete ? undefined : parameters.issue_number || parameters.pull_number || parameters.discussion_number);
  const commentRoute = pathname.match(/\/(issues|pulls)\/comments\/([1-9][0-9]*)(?:\/|$)/);
  const commentId = commentRoute?.[2] || (concrete ? undefined : parameters.comment_id);
  if (commentId) {
    const repository = repositoryFromRoute(route, parameters);
    const [owner, repo] = repository.split("/");
    const pulls = commentRoute ? commentRoute[1] === "pulls" : namespace === "pulls";
    const getComment = pulls ? client.rest?.pulls?.getReviewComment : client.rest?.issues?.getComment;
    if (typeof getComment !== "function") throw new Error("Claim comment target cannot be independently resolved");
    const { data } = await getComment({ owner, repo, comment_id: commentId });
    if (!data || String(data.id) !== String(commentId)) throw new Error("Claim comment target identity mismatch");
    const parent = data[pulls ? "pull_request_url" : "issue_url"];
    const prefix = `${(process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "")}/repos/${repository}/${pulls ? "pulls" : "issues"}/`;
    if (typeof parent !== "string" || !parent.startsWith(prefix) || !/^[1-9][0-9]*$/.test(parent.slice(prefix.length))) throw new Error("Claim comment target has no independently resolved parent");
    number = parent.slice(prefix.length);
    resource.comment_id = commentId;
  }
  if (number !== undefined) {
    number = Number(nativeDecimalIdentity(number));
    if (!Number.isSafeInteger(number) || number < 1) throw new Error("Claim resource number is invalid");
    const repository = repositoryFromRoute(route, parameters);
    const [owner, repo] = repository.split("/");
    const pulls = namespace === "pulls" || /\/pulls\//.test(pathname);
    const getParent = pulls ? client.rest?.pulls?.get : client.rest?.issues?.get;
    if (typeof getParent === "function") {
      const { data } = await getParent({ owner, repo, [pulls ? "pull_number" : "issue_number"]: number });
      if (!data || data.number !== number) throw new Error("Claim effect parent identity mismatch");
      Object.assign(resource, await resolveParentResourceTarget(client, { repository, kind: pulls ? "pull_request" : "issue", number }, data));
    }
  }
  const run = pathname.match(/\/actions\/runs\/([1-9][0-9]*)(?:\/|$)/)?.[1] || (concrete ? undefined : parameters.run_id);
  if (run !== undefined) resource.target_run_id = run;
  if (typeof resource.ref === "string" && /^(?:heads|tags)\//.test(resource.ref)) resource.ref = `refs/${resource.ref}`;
  const target = { ...resource, repository: repositoryFromRoute(route, parameters), ...(number === undefined ? {} : { number }) };
  return { number, resource: resource.repository_id === undefined ? await resolveRepositoryTarget(client, target) : canonicalResourceTarget(target) };
}

// Resolve only arguments actually consumed by each root mutation. Unused variables
// must never supply the apparent authority for a different mutation target.
function mutationArguments(query, variables) {
  const tokens = [];
  let offset = 0;
  while (offset < query.length) {
    const rest = query.slice(offset);
    const ignored = rest.match(/^(?:[\s,]+|#[^\n]*(?:\n|$))/);
    if (ignored) {
      offset += ignored[0].length;
      continue;
    }
    const token = rest.match(/^(?:"(?:[^"\\\r\n]|\\.)*"|[_A-Za-z][_0-9A-Za-z]*|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|[!$():=[\]{}])/);
    if (!token || rest.startsWith('"""')) throw new Error("Unsupported Claim GraphQL operation syntax");
    tokens.push(token[0]);
    offset += token[0].length;
    if (tokens.length > 32768) throw new Error("Claim GraphQL operation size exceeded");
  }
  let cursor = 0;
  const expect = token => {
    if (tokens[cursor++] !== token) throw new Error("Malformed Claim GraphQL operation");
  };
  const name = () => {
    const token = tokens[cursor++];
    if (!/^[_A-Za-z][_0-9A-Za-z]*$/.test(token || "")) throw new Error("Malformed Claim GraphQL field");
    return token;
  };
  const value = (depth = 0) => {
    if (depth > 16) throw new Error("Claim GraphQL argument depth exceeded");
    if (tokens[cursor] === "$") {
      cursor++;
      const variable = name();
      if (!Object.prototype.hasOwnProperty.call(variables, variable)) throw new Error("Missing Claim GraphQL variable");
      return variables[variable];
    }
    if (tokens[cursor] === "{") {
      cursor++;
      const result = Object.create(null);
      while (tokens[cursor] !== "}") {
        const key = name();
        expect(":");
        const variableBound = tokens[cursor] === "$";
        if (Object.prototype.hasOwnProperty.call(result, key)) throw new Error("Duplicate Claim GraphQL argument");
        result[key] = value(depth + 1);
        if (/(?:^id$|Id$|Ids$|_id$|_ids$)/.test(key) && !variableBound) throw new Error("Claim GraphQL effects require variable-bound node targets");
        if (key === "repositoryNameWithOwner" && !variableBound) throw new Error("Claim GraphQL effects require variable-bound repositories");
      }
      expect("}");
      return result;
    }
    if (tokens[cursor] === "[") {
      cursor++;
      const result = [];
      while (tokens[cursor] !== "]") result.push(value(depth + 1));
      expect("]");
      return result;
    }
    const token = tokens[cursor++];
    if (!token || /[{}():!$=[\]]/.test(token[0])) throw new Error("Malformed Claim GraphQL value");
    if (token.startsWith('"')) return parseStrictJSON(token);
    return token === "null" ? null : token === "true" ? true : token === "false" ? false : token;
  };
  const argumentsForField = () => {
    const result = Object.create(null);
    if (tokens[cursor] !== "(") return result;
    cursor++;
    while (tokens[cursor] !== ")") {
      const key = name();
      expect(":");
      const variableBound = tokens[cursor] === "$";
      if (Object.prototype.hasOwnProperty.call(result, key)) throw new Error("Duplicate Claim GraphQL argument");
      result[key] = value();
      if (/(?:^id$|Id$|Ids$|_id$|_ids$)/.test(key) && !variableBound) throw new Error("Claim GraphQL effects require variable-bound node targets");
      if (key === "repositoryNameWithOwner" && !variableBound) throw new Error("Claim GraphQL effects require variable-bound repositories");
    }
    expect(")");
    return result;
  };
  expect("mutation");
  if (tokens[cursor] !== "{" && tokens[cursor] !== "(") name();
  if (tokens[cursor] === "(") {
    let depth = 0;
    do {
      const token = tokens[cursor++];
      if (token === "(") depth++;
      if (token === ")") depth--;
      if (token === undefined) throw new Error("Malformed Claim GraphQL variable definitions");
    } while (depth);
  }
  const roots = [];
  const selection = (depth = 0) => {
    if (depth > 32) throw new Error("Claim GraphQL selection depth exceeded");
    expect("{");
    while (tokens[cursor] !== "}") {
      let field = name();
      if (tokens[cursor] === ":") {
        cursor++;
        field = name();
      }
      const args = argumentsForField();
      if (depth === 0) roots.push({ field, args });
      if (tokens[cursor] === "{") selection(depth + 1);
    }
    expect("}");
  };
  selection();
  if (cursor !== tokens.length || !roots.length) throw new Error("Claim GraphQL effects require one explicit mutation operation");
  return roots;
}

function collectNodeIds(value, ids, depth = 0) {
  if (depth > 16) throw new Error("Claim effect target depth exceeded");
  if (!value || typeof value !== "object") return;
  for (const [key, nested] of Object.entries(value)) {
    if (/^(?:clientMutationId|client_mutation_id)$/i.test(key)) continue;
    if (/(?:^id$|Id$|Ids$|_id$|_ids$)/.test(key)) {
      for (const id of Array.isArray(nested) ? nested : [nested]) {
        if (typeof id !== "string" || !id || Buffer.byteLength(id) > 256) throw new Error("Claim effect node target is invalid");
        ids.add(id);
      }
    } else if (nested && typeof nested === "object") {
      collectNodeIds(nested, ids, depth + 1);
    }
    if (ids.size > 128) throw new Error("Claim effect target count exceeded");
  }
}

/**
 * @template {object} Client
 * @param {Client} client
 * @param {{claim_handle: string, targetRepository?: string, signal?: AbortSignal, authorize?: (request: Record<string, unknown>) => unknown, authorizeGithub?: object, context?: object}} options
 * @returns {Client}
 */
function wrapClaimEffectClient(client, options) {
  const factoryIdentity = claimIdentity(options.claim_handle);
  const existing = claimClients.get(client);
  if (existing && Object.keys(options).every(key => existing.options[key] === Reflect.get(options, key))) {
    assertClaimIdentity(existing.identity);
    return client;
  }
  const cache = new WeakMap();
  const checkContext = () => {
    if (currentClaimHandle() !== options.claim_handle) throw new Error("GitHub effect client cannot escape its trusted Claim context");
    assertClaimIdentity(factoryIdentity);
  };
  const read = (target, receiver, args) => {
    const signal = options.signal;
    if (!signal) return Reflect.apply(target, receiver, args);
    signal.throwIfAborted();
    const index = typeof args[0] === "string" || typeof args[0] === "function" ? 1 : 0;
    const scopedArgs = [...args];
    if (typeof scopedArgs[index] === "function") scopedArgs.splice(index, 0, {});
    const parameters = scopedArgs[index] || {};
    scopedArgs[index] = { ...parameters, request: { ...parameters.request, signal } };
    const result = Reflect.apply(target, receiver, scopedArgs);
    if (result && typeof result.then === "function")
      return result.then(value => {
        signal.throwIfAborted();
        return value;
      });
    return result;
  };
  /** @param {string} repository @param {number | string | undefined} [number] @param {Record<string, unknown>} [resource] */
  const authorize = async (repository, number, resource = {}) => {
    checkContext();
    if (options.targetRepository && repository !== options.targetRepository) throw new Error("Claim adapter effect target conflicts with its fixed repository");
    const messageResource = { ...resource };
    delete messageResource.run_id;
    await assertClaimAuthorized(
      { ...messageResource, type: "work_queue_resource_effect", claim_handle: options.claim_handle, repo: repository, ...(number ? { item_number: number } : {}) },
      {
        authorize: options.authorize,
        github: options.authorizeGithub || client,
        context: options.context,
        effect: true,
        resource: canonicalResourceTarget({ ...resource, repository, ...(number ? { number } : {}) }),
      }
    );
  };
  const mutate = async (target, receiver, args, repository, number, kind, body) => {
    const parameters = body && typeof body === "object" && !Array.isArray(body) ? requestObject(body) : {};
    const expected = {};
    for (const field of ["title", "body", "state", "state_reason", "milestone", "labels", "assignees"]) {
      if (Object.prototype.hasOwnProperty.call(parameters, field)) expected[field] = structuredClone(parameters[field]);
    }
    const effect = recordClaimEffect({ repository, number: number || null, kind, expected, outcome: "unknown" });
    const response = await Reflect.apply(target, receiver, args);
    const data = response?.data;
    if (effect) {
      effect.outcome = "succeeded";
      if (data?.number) effect.number = data.number;
      if (data?.id) effect.id = String(data.id);
      if (kind.startsWith("git_") && (data?.sha || data?.object?.sha)) effect.id = String(data.sha || data.object.sha);
    }
    return response;
  };
  const gateGraphMutation = async (query, variables) => {
    const repositories = new Set();
    for (const { field, args: argumentsForMutation } of mutationArguments(query, variables)) {
      const input = argumentsForMutation.input || {};
      const refs = [];
      const paths = [];
      if (field === "createCommitOnBranch") {
        if (!input.branch || typeof input.branch !== "object") throw new Error("Claim GraphQL commit has no resolved branch");
        if (input.branch.repositoryNameWithOwner !== undefined) {
          if (typeof input.branch.branchName !== "string" || !input.branch.branchName) throw new Error("Claim GraphQL commit has no resolved branch name");
          refs.push(`refs/heads/${input.branch.branchName}`);
        } else if (typeof input.branch.id !== "string" || !input.branch.id) throw new Error("Claim GraphQL commit has no resolved branch target");
        if (input.fileChanges !== undefined) {
          if (!input.fileChanges || typeof input.fileChanges !== "object") throw new Error("Claim GraphQL commit has no resolved file changes");
          for (const changes of [input.fileChanges.additions, input.fileChanges.deletions]) {
            if (changes === undefined) continue;
            if (!Array.isArray(changes) || changes.length > 4096) throw new Error("Claim GraphQL commit requires bounded file changes");
            for (const change of changes) {
              if (!change || typeof change.path !== "string" || !change.path) throw new Error("Claim GraphQL commit has an unresolved file path");
              paths.push(change.path);
            }
          }
          if (paths.length > 4096) throw new Error("Claim GraphQL commit path count exceeded");
        }
      }
      if (field === "createRef" || field === "updateRefs") {
        const updates = field === "createRef" ? [input] : input.refUpdates;
        if (!Array.isArray(updates) || !updates.length || updates.length > 128) throw new Error("Claim GraphQL ref requires bounded native names");
        for (const update of updates) {
          if (!update || typeof update.name !== "string" || !update.name.startsWith("refs/") || update.name.length <= 5) throw new Error("Claim GraphQL ref has no resolved native name");
          refs.push(update.name);
        }
      }
      const authorizeTarget = async (repository, number, target) => {
        for (const ref of refs.length ? refs : [target.ref]) {
          if (target.ref !== undefined && ref !== target.ref) throw new Error("Claim GraphQL ref conflicts with its native node target");
          for (const path of paths.length ? paths : [undefined]) await authorize(repository, number, { ...target, ...(ref === undefined ? {} : { ref }), ...(path === undefined ? {} : { path }) });
        }
      };
      const ids = new Set();
      collectNodeIds(argumentsForMutation, ids);
      let namedTargets = 0;
      const namedRepositories = async (value, depth = 0) => {
        if (depth > 16) throw new Error("Claim effect target depth exceeded");
        if (!value || typeof value !== "object") return;
        for (const [key, nested] of Object.entries(value)) {
          if (key === "repositoryNameWithOwner") {
            if (typeof nested !== "string") throw new Error("Claim GraphQL repository target is invalid");
            await authorizeTarget(nested, undefined, await resolveRepositoryTarget(client, { repository: nested }));
            repositories.add(nested);
            namedTargets++;
          } else if (nested && typeof nested === "object") await namedRepositories(nested, depth + 1);
        }
      };
      await namedRepositories(argumentsForMutation);
      if (!ids.size) {
        if (namedTargets) continue;
        throw new Error("Claim GraphQL effect has no resolved node targets");
      }
      const targets = [...ids];
      const resolveNodes = Reflect.get(client, "graphql");
      if (typeof resolveNodes !== "function") throw new Error("Claim GraphQL effect targets could not be independently resolved");
      const nodeArgs = [NODE_QUERY, { ids: targets }];
      const nodeRequest = graphRequestSnapshot(resolveNodes, nodeArgs);
      const response = requestObject(await Reflect.apply(nodeRequest.target, client, nodeArgs));
      if (!Array.isArray(response?.nodes) || response.nodes.length !== targets.length) throw new Error("Claim GraphQL effect targets could not be independently resolved");
      for (let index = 0; index < targets.length; index++) {
        const node = requestObject(response.nodes[index]);
        if (!node || node.id !== targets[index]) throw new Error("Claim GraphQL effect node identity mismatch");
        const resource = requestObject(node.issue || node.pullRequest || node.discussion || node);
        const nativeRepository = requestObject(resource.repository);
        const repository = nativeRepository.nameWithOwner || (node.__typename === "Repository" ? node.nameWithOwner : null);
        if (typeof repository !== "string" || !repository) throw new Error("Unsupported Claim GraphQL effect resource");
        /** @type {Record<string, string>} */
        const target = {
          ...(typeof resource.__typename === "string" && ["Issue", "PullRequest"].includes(resource.__typename) ? { kind: resource.__typename === "Issue" ? "issue" : "pull_request" } : {}),
          ...(resource.databaseId === undefined || node.__typename === "Repository" ? {} : { resource_id: nativeDecimalIdentity(resource.databaseId) }),
          ...(node.__typename === "Repository" ? { repository_id: nativeDecimalIdentity(node.databaseId) } : nativeRepository.databaseId === undefined ? {} : { repository_id: nativeDecimalIdentity(nativeRepository.databaseId) }),
          ...(typeof node.__typename === "string" && ["IssueComment", "PullRequestReviewComment"].includes(node.__typename) && node.databaseId !== undefined ? { comment_id: nativeDecimalIdentity(node.databaseId) } : {}),
          ...((process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "") === "https://api.github.com" ? { host: "github.com" } : {}),
        };
        if (node.__typename === "Ref") {
          if (typeof node.name !== "string" || !node.name || typeof node.prefix !== "string" || !node.prefix.startsWith("refs/") || !node.prefix.endsWith("/")) throw new Error("Claim GraphQL Ref has no resolved native name");
          target.ref = `${node.prefix}${node.name}`;
        }
        await authorizeTarget(repository, resource.number === undefined ? undefined : nativeDecimalIdentity(resource.number), target);
        repositories.add(repository);
      }
    }
    return repositories;
  };
  const wrap = (value, names, owner = undefined) => {
    if (!value || !["object", "function"].includes(typeof value)) return value;
    if (cache.has(value)) return cache.get(value);
    const facade = typeof value === "function" ? function () {} : {};
    const proxy = new Proxy(facade, {
      ownKeys() {
        return [...new Set([...Reflect.ownKeys(facade), ...Reflect.ownKeys(value)])];
      },
      getOwnPropertyDescriptor(_target, key) {
        const invariant = Reflect.getOwnPropertyDescriptor(facade, key);
        if (invariant && !invariant.configurable) return invariant;
        const descriptor = Reflect.getOwnPropertyDescriptor(value, key);
        if (!descriptor) return invariant;
        if ("value" in descriptor) return { ...descriptor, value: wrap(descriptor.value, [...names, String(key)], value), configurable: true };
        return { ...descriptor, get: () => wrap(Reflect.get(value, key, value), [...names, String(key)], value), configurable: true };
      },
      has(_target, key) {
        return key in value;
      },
      get(_target, key) {
        if (typeof value === "function") {
          if (key === "call") return (_receiver, ...args) => Reflect.apply(proxy, undefined, args);
          if (key === "apply") return (_receiver, args) => Reflect.apply(proxy, undefined, args || []);
          if (key === "bind")
            return (_receiver, ...bound) =>
              (...args) =>
                Reflect.apply(proxy, undefined, [...bound, ...args]);
        }
        const nested = Reflect.get(value, key, value);
        return wrap(nested, [...names, String(key)], value);
      },
      apply(_target, receiver, args) {
        const target = value;
        receiver = owner || receiver;
        checkContext();
        const name = names[names.length - 1];
        if (name === "defaults") return wrap(Reflect.apply(target, receiver, args), names.slice(0, -1));
        if (names[0] === "paginate") {
          const metadata = typeof args[0] === "function" ? args[0].endpoint?.DEFAULTS : args[0];
          const method = typeof metadata === "string" ? metadata.match(/^([A-Z]+)\s/)?.[1] : metadata?.method;
          if (method !== "GET" || (args[1]?.method && String(args[1].method).toUpperCase() !== "GET")) throw new Error("Claim effect clients only paginate explicit read endpoints");
          assertApiOrigin(args[1]?.url || (typeof metadata === "string" ? metadata : metadata?.url), args[1] || {}, metadata || {});
          return read(target, receiver, args);
        }
        if (names.includes("graphql")) {
          const query = typeof args[0] === "string" ? args[0] : args[0]?.query;
          const variables = typeof args[0] === "string" ? args[1] || {} : args[0] || {};
          if (typeof query !== "string") throw new Error("Claim GraphQL request has no explicit operation");
          if (typeof args[0] === "string" && Object.prototype.hasOwnProperty.call(variables, "query")) throw new Error("Claim GraphQL variables cannot override the authorized operation");
          assertApiOrigin(variables.url, variables, target.endpoint?.DEFAULTS);
          const immutableArgs = structuredClone(args);
          const snapshot = graphRequestSnapshot(target, immutableArgs);
          if (/\bmutation\b/.test(query.replace(/#[^\n]*/g, ""))) {
            const immutableVariables = snapshot.variables;
            return gateGraphMutation(query, immutableVariables).then(async repositories => {
              const effect = recordClaimEffect({ repository: repositories.size === 1 ? [...repositories][0] : null, number: null, kind: "graphql", outcome: "unknown" });
              const response = await Reflect.apply(snapshot.target, receiver, immutableArgs);
              if (effect) {
                effect.id = graphEffectIdentity(query, immutableVariables, response);
                effect.outcome = "succeeded";
              }
              return response;
            });
          }
          return read(snapshot.target, receiver, immutableArgs);
        }
        if (names[0] === "rest" || names[0] === "request") {
          const parameters = typeof args[0] === "string" ? args[1] || {} : args[0] || {};
          const defaults = target.endpoint?.DEFAULTS || {};
          const route = parameters.url || (typeof args[0] === "string" ? args[0] : defaults.url || "");
          const explicitMethod = typeof args[0] === "string" ? args[0].match(/^([A-Z]+)\s/)?.[1] : undefined;
          const method = String(parameters.method || explicitMethod || defaults.method || (READ_METHODS.has(name) ? "GET" : "POST")).toUpperCase();
          assertApiOrigin(route, parameters, defaults);
          if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
            const immutableArgs = structuredClone(args);
            const snapshot = requestSnapshot(target, immutableArgs);
            const actualRoute = snapshot.route;
            assertApiOrigin(actualRoute, snapshot.parameters, defaults);
            const repository = repositoryFromRoute(actualRoute, snapshot.parameters);
            const immutableParameters = snapshot.parameters;
            const gitKind = names[1] === "git" ? { createBlob: "git_blob", createTree: "git_tree", createCommit: "git_commit", createRef: "git_ref", updateRef: "git_ref" }[name] : null;
            const kind =
              gitKind ||
              (/\/code-coverage\/report(?:$|\?)/.test(route) ? "code_coverage" : null) ||
              (names[1] === "codeScanning" && name === "uploadSarif"
                ? "sarif"
                : parameters.comment_id || /comment/i.test(name) || /\/comments(?:\/|$)/.test(route)
                  ? "comment"
                  : names[1] === "pulls"
                    ? "pull_request"
                    : names[1] === "issues"
                      ? "issue"
                      : /\/repos\/[^/]+\/[^/]+\/check-runs(?:\/|$)/.test(route)
                        ? "check_run"
                        : /\/repos\/[^/]+\/[^/]+\/releases(?:\/|$)/.test(route)
                          ? "release"
                          : /\/repos\/[^/]+\/[^/]+\/deployments(?:\/|$)/.test(route)
                            ? "deployment"
                            : /\/repos\/[^/]+\/[^/]+\/issues(?:\/|$)/.test(route)
                              ? "issue"
                              : /\/repos\/[^/]+\/[^/]+\/pulls(?:\/|$)/.test(route)
                                ? "pull_request"
                                : "unknown");
            const compiledResources =
              currentClaimResourceEffects(names.join(".")) ||
              currentClaimResourceEffects(`request:${snapshot.method} ${String(route).replace(/^[A-Z]+\s+/, "")}`) ||
              currentClaimResourceEffects(`request:${snapshot.method} ${String(actualRoute).replace(/^[A-Z]+\s+/, "")}`);
            return resolveRestResource(client, actualRoute, immutableParameters, names[1], compiledResources).then(async ({ number, resource }) => {
              const resources = compiledResources || [resource];
              const pathname = nativePathname(String(actualRoute));
              const treeMutation = (names[1] === "git" && name === "createTree") || /\/git\/trees\/?$/.test(pathname);
              const stagedGitObject = (names[1] === "git" && ["createBlob", "createTree", "createCommit"].includes(name)) || /\/git\/(?:blobs|trees|commits)\/?$/.test(pathname);
              const releaseAsset = /\/releases\/[1-9][0-9]*\/assets\/?$/.test(pathname);
              const semanticFileAdapter =
                stagedGitObject ||
                (names[1] === "git" && ["createRef", "updateRef"].includes(name)) ||
                (["POST", "PATCH"].includes(snapshot.method) && /\/git\/refs(?:\/|$)/.test(pathname)) ||
                kind === "sarif" ||
                kind === "code_coverage" ||
                (names[1] === "pulls" && name === "create") ||
                (snapshot.method === "POST" && /\/pulls\/?$/.test(pathname)) ||
                /\/(?:code-scanning\/sarifs|code-coverage\/report)\/?$/.test(pathname) ||
                releaseAsset;
              for (const target of resources) {
                if (resources.length !== 1 || target !== resource) {
                  for (const [field, value] of Object.entries(resource)) {
                    if (target[field] !== value) throw new Error("Native adapter operation conflicts with its independently resolved Claim target");
                  }
                  for (const field of Object.keys(target)) {
                    const semanticSelector = (field === "path" && semanticFileAdapter) || (field === "ref" && (stagedGitObject || releaseAsset));
                    if (resource[field] === undefined && !semanticSelector) throw new Error("Native adapter operation omits its protected Claim target selector");
                  }
                }
                if (!treeMutation || target !== resource) await authorize(repository, number, target);
              }
              if (treeMutation) {
                if (!Array.isArray(immutableParameters.tree) || immutableParameters.tree.length < 1 || immutableParameters.tree.length > 4096) throw new Error("Claim git tree requires a bounded concrete path set");
                for (const entry of immutableParameters.tree) {
                  if (!entry || typeof entry.path !== "string" || !entry.path) throw new Error("Claim git tree has an unresolved effect path");
                  const targets = resources.filter(target => !Object.hasOwn(target, "path") || target.path === entry.path);
                  if (!targets.length) throw new Error("Claim git tree path is outside its protected adapter target set");
                  for (const target of targets) await authorize(repository, number, { ...target, path: entry.path });
                }
              }
              return mutate(target, receiver, snapshot.forwarded, repository, number, kind, snapshot.body);
            });
          }
          return read(target, receiver, args);
        }
        return Reflect.apply(target, receiver, args);
      },
    });
    cache.set(value, proxy);
    return proxy;
  };
  const wrapped = wrap(client, []);
  claimClients.set(wrapped, { identity: factoryIdentity, options: { ...options } });
  return wrapped;
}

async function withClaimEffectClients(options, callback) {
  const github = global.github;
  const getOctokit = global.getOctokit;
  global.github = wrapClaimEffectClient(github, options);
  if (typeof getOctokit === "function") global.getOctokit = (...args) => wrapClaimEffectClient(getOctokit(...args), options);
  try {
    return await callback();
  } finally {
    global.github = github;
    global.getOctokit = getOctokit;
  }
}

function graphEffectIdentity(query, variables, response) {
  return digest({ query, variables, response });
}

module.exports = { wrapClaimEffectClient, withClaimEffectClients, graphEffectIdentity };
