// @ts-check
"use strict";

const { assertClaimAuthorized, currentClaimHandle, recordClaimEffect, claimIdentity, assertClaimIdentity } = require("./work_queue_claim_scope.cjs");
const { digest } = require("./work_queue_codec.cjs");

const READ_METHODS = new Set(["get", "list", "getSarif", "getAnalysis", "getComment", "getCommit", "getRef", "getTree", "getBlob", "getArtifact", "getBranch", "getByUsername", "getWorkflowRun", "getWorkflowRunAttempt"]);
const NODE_QUERY = `query WorkQueueEffectTargets($ids: [ID!]!) {
  nodes(ids: $ids) {
    __typename
    id
    ... on Repository { nameWithOwner }
    ... on Issue { number repository { nameWithOwner } }
    ... on PullRequest { number repository { nameWithOwner } }
    ... on Discussion { number repository { nameWithOwner } }
    ... on DiscussionCategory { repository { nameWithOwner } }
    ... on IssueComment { issue { number repository { nameWithOwner } } }
    ... on PullRequestReview { pullRequest { number repository { nameWithOwner } } }
    ... on PullRequestReviewComment { pullRequest { number repository { nameWithOwner } } }
    ... on DiscussionComment { discussion { number repository { nameWithOwner } } }
  }
}`;

function repositoryFromRoute(route, parameters) {
  let url = String(route || "").replace(/^[A-Z]+\s+/, "");
  if (/^https?:\/\//i.test(url)) {
    const absolute = new URL(url);
    if (absolute.origin !== new URL(process.env.GITHUB_API_URL || "https://api.github.com").origin) throw new Error("Claim effects cannot use an unapproved API origin");
    url = absolute.pathname;
  }
  const match = url.match(/\/repos\/([^/?{}]+)\/([^/?{}]+)/);
  if (match) return `${decodeURIComponent(match[1])}/${decodeURIComponent(match[2])}`;
  if (typeof parameters.owner !== "string" || typeof parameters.repo !== "string") throw new Error("Claim effect has no independently resolved repository target");
  return `${parameters.owner}/${parameters.repo}`;
}

function assertApiOrigin(route, parameters, defaults = {}) {
  const approved = new URL(process.env.GITHUB_API_URL || "https://api.github.com");
  for (const base of [parameters.baseUrl, defaults.baseUrl]) {
    if (base !== undefined && new URL(base).href.replace(/\/$/, "") !== approved.href.replace(/\/$/, "")) {
      throw new Error("Claim clients cannot override the approved API base URL");
    }
  }
  const url = String(route || "").replace(/^[A-Z]+\s+/, "");
  if (url.startsWith("//") || (/^[a-z][a-z0-9+.-]*:/i.test(url) && new URL(url).origin !== approved.origin)) {
    throw new Error("Claim clients cannot use an unapproved API origin");
  }
}

async function resolveRestResource(client, route, parameters, namespace) {
  let pathname = String(route || "").replace(/^[A-Z]+\s+/, "");
  if (/^https?:\/\//i.test(pathname)) pathname = new URL(pathname).pathname;
  const concreteNumber = pathname.match(/\/(?:issues|pulls|discussions)\/([1-9][0-9]*)(?:\/|$)/)?.[1];
  const resource = {};
  for (const field of ["comment_id", "review_id", "release_id", "tag_name", "ref", "path", "workflow_id", "check_run_id", "deployment_id"]) {
    if (Object.prototype.hasOwnProperty.call(parameters, field)) resource[field] = parameters[field];
  }
  let number = concreteNumber || parameters.issue_number || parameters.pull_number || parameters.discussion_number;
  const commentRoute = pathname.match(/\/(issues|pulls)\/comments\/([1-9][0-9]*)(?:\/|$)/);
  const commentId = commentRoute?.[2] || parameters.comment_id;
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
    number = Number(number);
    if (!Number.isSafeInteger(number) || number < 1) throw new Error("Claim resource number is invalid");
  }
  const run = pathname.match(/\/actions\/runs\/([1-9][0-9]*)(?:\/|$)/)?.[1] || parameters.run_id;
  if (run !== undefined) resource.target_run_id = run;
  return { number, resource };
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
    if (token.startsWith('"')) return JSON.parse(token);
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
      name();
      if (tokens[cursor] === ":") {
        cursor++;
        name();
      }
      const args = argumentsForField();
      if (depth === 0) roots.push(args);
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

/** @param {any} client @param {Record<string, any>} options */
function wrapClaimEffectClient(client, options) {
  const factoryIdentity = claimIdentity(options.claim_handle);
  const cache = new WeakMap();
  const checkContext = () => {
    if (currentClaimHandle() !== options.claim_handle) throw new Error("GitHub effect client cannot escape its trusted Claim context");
    assertClaimIdentity(factoryIdentity);
  };
  const authorize = async (repository, number, resource = {}) => {
    checkContext();
    if (options.targetRepository && repository !== options.targetRepository) throw new Error("Claim adapter effect target conflicts with its fixed repository");
    await assertClaimAuthorized(
      { ...resource, type: "work_queue_resource_effect", claim_handle: options.claim_handle, repo: repository, ...(number ? { item_number: number } : {}) },
      {
        authorize: options.authorize,
        github: options.authorizeGithub || client,
        context: options.context,
        effect: true,
        resource: { ...resource, repository, ...(number ? { number } : {}) },
      }
    );
  };
  const mutate = async (target, receiver, args, repository, number, kind) => {
    const parameters = typeof args[0] === "string" ? args[1] || {} : args[0] || {};
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
    for (const argumentsForMutation of mutationArguments(query, variables)) {
      const ids = new Set();
      collectNodeIds(argumentsForMutation, ids);
      let namedTargets = 0;
      const namedRepositories = async (value, depth = 0) => {
        if (depth > 16) throw new Error("Claim effect target depth exceeded");
        if (!value || typeof value !== "object") return;
        for (const [key, nested] of Object.entries(value)) {
          if (key === "repositoryNameWithOwner") {
            if (typeof nested !== "string") throw new Error("Claim GraphQL repository target is invalid");
            await authorize(nested);
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
      const response = await client.graphql(NODE_QUERY, { ids: targets });
      if (!Array.isArray(response?.nodes) || response.nodes.length !== targets.length) throw new Error("Claim GraphQL effect targets could not be independently resolved");
      for (let index = 0; index < targets.length; index++) {
        const node = response.nodes[index];
        if (!node || node.id !== targets[index]) throw new Error("Claim GraphQL effect node identity mismatch");
        const resource = node.issue || node.pullRequest || node.discussion || node;
        const repository = resource.repository?.nameWithOwner || (node.__typename === "Repository" ? node.nameWithOwner : null);
        if (!repository) throw new Error("Unsupported Claim GraphQL effect resource");
        await authorize(repository, resource.number);
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
        }
        if (names.includes("graphql")) {
          const query = typeof args[0] === "string" ? args[0] : args[0]?.query;
          const variables = typeof args[0] === "string" ? args[1] || {} : args[0] || {};
          if (typeof query !== "string") throw new Error("Claim GraphQL request has no explicit operation");
          if (typeof args[0] === "string" && Object.prototype.hasOwnProperty.call(variables, "query")) throw new Error("Claim GraphQL variables cannot override the authorized operation");
          assertApiOrigin(variables.url, variables, target.endpoint?.DEFAULTS);
          if (/\bmutation\b/.test(query.replace(/#[^\n]*/g, ""))) {
            const immutableArgs = structuredClone(args);
            const immutableVariables = typeof immutableArgs[0] === "string" ? immutableArgs[1] || {} : immutableArgs[0];
            return gateGraphMutation(query, immutableVariables).then(async repositories => {
              const effect = recordClaimEffect({ repository: repositories.size === 1 ? [...repositories][0] : null, number: null, kind: "graphql", outcome: "unknown" });
              const response = await Reflect.apply(target, receiver, immutableArgs);
              if (effect) {
                effect.id = graphEffectIdentity(query, immutableVariables, response);
                effect.outcome = "succeeded";
              }
              return response;
            });
          }
          return Reflect.apply(target, receiver, args);
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
            const repository = repositoryFromRoute(route, parameters);
            const immutableParameters = typeof immutableArgs[0] === "string" ? immutableArgs[1] || {} : immutableArgs[0] || {};
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
            return resolveRestResource(client, route, immutableParameters, names[1]).then(async ({ number, resource }) => {
              await authorize(repository, number, resource);
              if (names[1] === "git" && name === "createTree") {
                if (!Array.isArray(immutableParameters.tree) || immutableParameters.tree.length > 4096) throw new Error("Claim git tree requires a bounded concrete path set");
                for (const entry of immutableParameters.tree) {
                  if (!entry || typeof entry.path !== "string" || !entry.path) throw new Error("Claim git tree has an unresolved effect path");
                  await authorize(repository, number, { ...resource, path: entry.path });
                }
              }
              return mutate(target, receiver, immutableArgs, repository, number, kind);
            });
          }
        }
        return Reflect.apply(target, receiver, args);
      },
    });
    cache.set(value, proxy);
    return proxy;
  };
  return wrap(client, []);
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
