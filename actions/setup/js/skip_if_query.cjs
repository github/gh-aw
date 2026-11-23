// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Mini GitHub issue/PR search query parser and count evaluator for skip-if gates.
 *
 * The search API has a much lower rate limit (30 req/min) than REST (core) or
 * GraphQL. Simple queries (repo-scoped issue/PR listing with state, label,
 * author, assignee, or mention filters) are evaluated through GraphQL or REST
 * instead. Anything the parser cannot translate exactly falls back to search.
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/** @typedef {"graphql" | "rest" | "search"} QueryBackend */

/**
 * @typedef {Object} ParsedQuery
 * @property {"issue" | "pr" | null} type
 * @property {"open" | "closed" | "merged" | null} state
 * @property {string[]} labels - Labels combined with AND semantics
 * @property {{ owner: string, repo: string } | null} repo
 * @property {string | null} author
 * @property {string | null} assignee
 * @property {string | null} mentions
 */

/**
 * @typedef {{ ok: true, query: ParsedQuery } | { ok: false, reason: string }} ParseResult
 */

/**
 * @typedef {Object} CountResult
 * @property {number} totalCount - Number of matches (a lower bound when `exact` is false)
 * @property {boolean} exact - Whether totalCount is the full match count
 * @property {QueryBackend} backend
 */

/** Preferred backend order when there is no rate-limit pressure. */
/** @type {QueryBackend[]} */
const BACKEND_PREFERENCE = ["graphql", "rest", "search"];

/** Rate-limit resource consumed by each backend. */
const BACKEND_RESOURCE = { graphql: "graphql", rest: "core", search: "search" };

/** A backend is under pressure when less than this fraction of its quota remains. */
const LOW_QUOTA_FRACTION = 0.1;

/** Maximum REST pages fetched while counting before deferring to another backend. */
const REST_MAX_PAGES = 5;
const REST_PAGE_SIZE = 100;

const LOGIN_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/;
const REPO_PATTERN = /^([A-Za-z0-9-]+)\/([A-Za-z0-9._-]+)$/;

/**
 * Splits a search query into whitespace-separated tokens, keeping quoted
 * segments intact. Returns null when quotes are unbalanced.
 * @param {string} query
 * @returns {string[] | null}
 */
function tokenizeSearchQuery(query) {
  /** @type {string[]} */
  const tokens = [];
  let current = "";
  let inQuotes = false;
  for (const ch of query) {
    if (ch === '"') {
      inQuotes = !inQuotes;
      current += ch;
    } else if (!inQuotes && /\s/.test(ch)) {
      if (current) tokens.push(current);
      current = "";
    } else {
      current += ch;
    }
  }
  if (inQuotes) return null;
  if (current) tokens.push(current);
  return tokens;
}

/**
 * @param {string} value
 * @returns {string}
 */
function unquote(value) {
  return value.length >= 2 && value.startsWith('"') && value.endsWith('"') ? value.slice(1, -1) : value;
}

/**
 * Parses a GitHub issue/PR search query into a structured filter when every
 * qualifier can be evaluated without the search API.
 * @param {string} query
 * @returns {ParseResult}
 */
function parseSearchQuery(query) {
  const tokens = tokenizeSearchQuery(query);
  if (!tokens) return { ok: false, reason: "unbalanced quotes" };
  if (tokens.length === 0) return { ok: false, reason: "empty query" };

  /** @type {ParsedQuery} */
  const parsed = { type: null, state: null, labels: [], repo: null, author: null, assignee: null, mentions: null };

  /**
   * @param {"type" | "state"} field
   * @param {any} value
   * @returns {string | null} conflict reason
   */
  const setOnce = (field, value) => {
    if (parsed[field] !== null && parsed[field] !== value) return `conflicting ${field} qualifiers`;
    parsed[field] = value;
    return null;
  };

  for (const token of tokens) {
    if (token.startsWith("-")) return { ok: false, reason: `negated qualifier "${token}"` };
    const colon = token.indexOf(":");
    if (colon <= 0) return { ok: false, reason: `free-text term "${token}"` };

    const key = token.slice(0, colon).toLowerCase();
    const rawValue = unquote(token.slice(colon + 1));
    const value = rawValue.toLowerCase();
    if (!rawValue) return { ok: false, reason: `empty qualifier "${token}"` };
    if (rawValue.includes('"')) return { ok: false, reason: `unsupported quoting "${token}"` };

    /** @type {string | null} */
    let conflict = null;
    switch (key) {
      case "is":
        if (value === "issue" || value === "pr" || value === "pull-request") conflict = setOnce("type", value === "issue" ? "issue" : "pr");
        else if (value === "open" || value === "closed") conflict = setOnce("state", value);
        else if (value === "merged") conflict = setOnce("state", "merged") || setOnce("type", "pr");
        else return { ok: false, reason: `unsupported qualifier "${token}"` };
        break;
      case "type":
        if (value === "issue" || value === "pr" || value === "pull-request") conflict = setOnce("type", value === "issue" ? "issue" : "pr");
        else return { ok: false, reason: `unsupported qualifier "${token}"` };
        break;
      case "state":
        if (value === "open" || value === "closed") conflict = setOnce("state", value);
        else return { ok: false, reason: `unsupported qualifier "${token}"` };
        break;
      case "label":
        // Comma-separated labels use OR semantics in search; REST label filters are AND-only.
        if (rawValue.includes(",")) return { ok: false, reason: `OR label list "${token}"` };
        if (!parsed.labels.includes(rawValue)) parsed.labels.push(rawValue);
        break;
      case "repo": {
        const match = REPO_PATTERN.exec(rawValue);
        if (!match) return { ok: false, reason: `unsupported repository "${rawValue}"` };
        const repo = { owner: match[1], repo: match[2] };
        // Multiple repo: qualifiers use OR semantics.
        if (parsed.repo && (parsed.repo.owner.toLowerCase() !== repo.owner.toLowerCase() || parsed.repo.repo.toLowerCase() !== repo.repo.toLowerCase())) {
          return { ok: false, reason: "multiple repo qualifiers" };
        }
        parsed.repo = repo;
        break;
      }
      case "author":
      case "assignee":
      case "mentions": {
        if (!LOGIN_PATTERN.test(rawValue)) return { ok: false, reason: `unsupported ${key} "${rawValue}"` };
        if (parsed[key] !== null && parsed[key]?.toLowerCase() !== value) return { ok: false, reason: `multiple ${key} qualifiers` };
        parsed[key] = rawValue;
        break;
      }
      case "sort":
        // Sorting does not affect the match count.
        break;
      default:
        return { ok: false, reason: `unsupported qualifier "${token}"` };
    }
    if (conflict) return { ok: false, reason: conflict };
  }

  if (parsed.type === "issue" && parsed.state === "merged") return { ok: false, reason: "is:merged applies only to pull requests" };
  if (!parsed.repo) return { ok: false, reason: "query is not scoped to a single repository" };

  return { ok: true, query: parsed };
}

/**
 * Returns the backends able to evaluate the parsed query exactly, in preference order.
 * @param {ParseResult} parseResult
 * @returns {QueryBackend[]}
 */
function getCapableBackends(parseResult) {
  if (!parseResult.ok) return ["search"];
  const q = parseResult.query;
  /** @type {QueryBackend[]} */
  const backends = [];
  // GraphQL label filters use OR semantics, so only a single label is exact.
  // Pull request connections cannot filter by author/assignee/mentions.
  const hasUserFilters = Boolean(q.author || q.assignee || q.mentions);
  if (q.labels.length <= 1 && (q.type === "issue" || !hasUserFilters)) backends.push("graphql");
  backends.push("rest", "search");
  return backends;
}

/**
 * Fetches current rate-limit resources. Returns null when unavailable.
 * The rate_limit endpoint does not count against any quota.
 * @returns {Promise<Record<string, { limit: number, remaining: number }> | null>}
 */
async function fetchRateLimitResources() {
  try {
    const { data } = await github.rest.rateLimit.get();
    return /** @type {any} */ data?.resources ?? null;
  } catch (error) {
    core.info(`Rate limit lookup failed: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Orders capable backends by preference, demoting backends under rate-limit pressure.
 * @param {QueryBackend[]} capable
 * @param {Record<string, { limit: number, remaining: number }> | null} resources
 * @returns {QueryBackend[]}
 */
function orderBackendsByPressure(capable, resources) {
  const ordered = BACKEND_PREFERENCE.filter(b => capable.includes(b));
  if (!resources) return ordered;

  /** @param {QueryBackend} backend */
  const headroom = backend => {
    const resource = resources[BACKEND_RESOURCE[backend]];
    if (!resource || !(resource.limit > 0)) return 1;
    return resource.remaining / resource.limit;
  };
  /** @param {QueryBackend} backend */
  const remaining = backend => resources[BACKEND_RESOURCE[backend]]?.remaining ?? Infinity;

  const healthy = ordered.filter(b => remaining(b) > 0 && headroom(b) >= LOW_QUOTA_FRACTION);
  const pressured = ordered.filter(b => !healthy.includes(b)).sort((a, b) => headroom(b) - headroom(a));
  return [...healthy, ...pressured];
}

/**
 * @param {ParsedQuery} q
 * @returns {Promise<CountResult>}
 */
async function countWithGraphQL(q) {
  if (!q.repo) throw new Error("query is not scoped to a repository");
  const { owner, repo } = q.repo;
  const labels = q.labels.length ? q.labels : null;
  const includeIssues = q.type !== "pr";
  const includePRs = q.type !== "issue";

  const issueStates = q.state === "open" ? ["OPEN"] : q.state === "closed" ? ["CLOSED"] : null;
  const prStates = q.state === "open" ? ["OPEN"] : q.state === "closed" ? ["CLOSED", "MERGED"] : q.state === "merged" ? ["MERGED"] : null;
  // Omit unset filters: an explicit `assignee: null` means "unassigned" in GraphQL.
  /** @type {Record<string, string>} */
  const filterBy = {};
  if (q.assignee) filterBy.assignee = q.assignee;
  if (q.author) filterBy.createdBy = q.author;
  if (q.mentions) filterBy.mentioned = q.mentions;

  // GraphQL rejects declared-but-unused variables, so declare only what each field uses.
  const declarations = ["$owner: String!", "$repo: String!", "$labels: [String!]"];
  /** @type {Record<string, unknown>} */
  const variables = { owner, repo, labels };
  const fields = [];
  if (includeIssues) {
    declarations.push("$issueStates: [IssueState!]", "$filterBy: IssueFilters");
    Object.assign(variables, { issueStates, filterBy });
    fields.push("issues(states: $issueStates, labels: $labels, filterBy: $filterBy) { totalCount }");
  }
  if (includePRs) {
    declarations.push("$prStates: [PullRequestState!]");
    variables.prStates = prStates;
    fields.push("pullRequests(states: $prStates, labels: $labels) { totalCount }");
  }

  const query = `query(${declarations.join(", ")}) { repository(owner: $owner, name: $repo) { ${fields.join(" ")} } }`;
  /** @type {any} */
  const result = await github.graphql(query, variables);
  if (!result?.repository) throw new Error(`Repository ${owner}/${repo} not found`);
  const totalCount = (result.repository.issues?.totalCount ?? 0) + (result.repository.pullRequests?.totalCount ?? 0);
  return { totalCount, exact: true, backend: "graphql" };
}

/**
 * Counts matches through REST list endpoints, stopping once the threshold is reached.
 * @param {ParsedQuery} q
 * @param {number} threshold
 * @returns {Promise<CountResult>}
 */
async function countWithRest(q, threshold) {
  if (!q.repo) throw new Error("query is not scoped to a repository");
  const { owner, repo } = q.repo;
  /** @type {"open" | "closed" | "all"} */
  const apiState = q.state === "merged" ? "closed" : (q.state ?? "all");
  const usePulls = q.type === "pr" && q.labels.length === 0 && !q.author && !q.assignee && !q.mentions;
  const needsClientFilter = q.type !== null || q.state === "merged";
  const perPage = needsClientFilter ? REST_PAGE_SIZE : Math.min(REST_PAGE_SIZE, threshold);

  /** @param {any} item */
  const matches = item => {
    const isPR = usePulls || Boolean(item.pull_request);
    if (q.type === "issue" && isPR) return false;
    if (q.type === "pr" && !isPR) return false;
    if (q.state === "merged") return Boolean(usePulls ? item.merged_at : item.pull_request?.merged_at);
    return true;
  };

  // Page-number pagination can shift items across pages when new issues/PRs
  // arrive mid-scan, so count distinct ids rather than raw matches.
  /** @type {Set<number>} */
  const seen = new Set();
  for (let page = 1; page <= REST_MAX_PAGES; page++) {
    /** @type {any[]} */
    let items;
    if (usePulls) {
      ({ data: items } = await github.rest.pulls.list({ owner, repo, state: apiState, per_page: perPage, page }));
    } else {
      /** @type {any} */
      const params = { owner, repo, state: apiState, per_page: perPage, page };
      if (q.labels.length) params.labels = q.labels.join(",");
      if (q.author) params.creator = q.author;
      if (q.assignee) params.assignee = q.assignee;
      if (q.mentions) params.mentioned = q.mentions;
      ({ data: items } = await github.rest.issues.listForRepo(params));
    }
    for (const item of items) {
      if (matches(item)) seen.add(item.id);
    }
    if (items.length < perPage) return { totalCount: seen.size, exact: true, backend: "rest" };
    if (seen.size >= threshold) return { totalCount: seen.size, exact: false, backend: "rest" };
  }
  throw new Error(`inconclusive after ${REST_MAX_PAGES} pages (${seen.size} matches, threshold ${threshold})`);
}

/**
 * @param {string} searchQuery
 * @returns {Promise<CountResult>}
 */
async function countWithSearch(searchQuery) {
  const {
    data: { total_count: totalCount },
  } = await github.rest.search.issuesAndPullRequests({ q: searchQuery, per_page: 1 });
  return { totalCount, exact: true, backend: "search" };
}

/**
 * Counts issues/PRs matching a search query using the cheapest capable backend
 * given current rate-limit pressure, falling back to the next backend on error.
 * @param {string} searchQuery - Fully scoped search query
 * @param {number} threshold - Count at which the gate decision is settled
 * @returns {Promise<CountResult>}
 */
async function countSearchMatches(searchQuery, threshold) {
  const parseResult = parseSearchQuery(searchQuery);
  if (!parseResult.ok) {
    core.info(`Query requires the search API (${parseResult.reason})`);
    return countWithSearch(searchQuery);
  }

  const resources = await fetchRateLimitResources();
  const backends = orderBackendsByPressure(getCapableBackends(parseResult), resources);
  core.info(`Query evaluation backends: ${backends.join(" → ")}`);

  /** @type {unknown} */
  let lastError;
  for (const backend of backends) {
    try {
      if (backend === "graphql") return await countWithGraphQL(parseResult.query);
      if (backend === "rest") return await countWithRest(parseResult.query, threshold);
      return await countWithSearch(searchQuery);
    } catch (error) {
      lastError = error;
      core.info(`${backend} evaluation failed: ${getErrorMessage(error)}`);
    }
  }
  throw lastError;
}

module.exports = {
  tokenizeSearchQuery,
  parseSearchQuery,
  getCapableBackends,
  orderBackendsByPressure,
  countSearchMatches,
};
