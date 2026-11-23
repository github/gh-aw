// @ts-check
/**
 * Fuzz harness for the skip-if search query parser in skip_if_query.cjs.
 *
 * Checks parser and backend-selection invariants for arbitrary queries:
 * - parsing never throws and is deterministic
 * - accepted queries are fully structured (single repo, valid enums, no OR label lists)
 * - unsupported queries route only to search; supported ones always keep search as fallback
 * - GraphQL is offered only when it is exact (<= 1 label, user filters only on issues)
 * - acceptance is invariant to token order, extra whitespace, and sort: qualifiers
 * - appending free text or a negated qualifier always forces the search API
 * - pressure ordering is always a permutation of the capable backends
 *
 * Used by vitest and by Go's fuzzing framework when run as main module via stdin.
 */

const { tokenizeSearchQuery, parseSearchQuery, getCapableBackends, orderBackendsByPressure } = require("./skip_if_query.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const TYPES = [null, "issue", "pr"];
const STATES = [null, "open", "closed", "merged"];
const LOGIN_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/;

/**
 * Rate-limit scenarios exercised for every query.
 * @type {(Record<string, { limit: number, remaining: number }> | null)[]}
 */
const RESOURCE_SCENARIOS = [
  null,
  {},
  { graphql: { limit: 5000, remaining: 5000 }, core: { limit: 5000, remaining: 5000 }, search: { limit: 30, remaining: 30 } },
  { graphql: { limit: 5000, remaining: 10 }, core: { limit: 5000, remaining: 4000 }, search: { limit: 30, remaining: 30 } },
  { graphql: { limit: 5000, remaining: 0 }, core: { limit: 5000, remaining: 0 }, search: { limit: 30, remaining: 1 } },
  { graphql: { limit: 0, remaining: 0 }, core: { limit: 5000, remaining: 499 }, search: { limit: 30, remaining: 0 } },
];

/**
 * Normalizes a parse result for comparisons that must ignore token order and casing.
 * @param {ReturnType<typeof parseSearchQuery>} result
 * @returns {string}
 */
function normalize(result) {
  if (!result.ok) return "unsupported";
  const q = result.query;
  const lower = (/** @type {string | null} */ v) => (v === null ? null : v.toLowerCase());
  return JSON.stringify({
    type: q.type,
    state: q.state,
    labels: [...q.labels].sort(),
    repo: q.repo && `${q.repo.owner}/${q.repo.repo}`.toLowerCase(),
    author: lower(q.author),
    assignee: lower(q.assignee),
    mentions: lower(q.mentions),
  });
}

/**
 * Returns all invariant violations for a query. Never throws.
 * @param {string} query
 * @returns {string[]}
 */
function checkSkipIfQueryInvariants(query) {
  /** @type {string[]} */
  const violations = [];
  try {
    const result = parseSearchQuery(query);
    if (JSON.stringify(parseSearchQuery(query)) !== JSON.stringify(result)) violations.push("parse is not deterministic");

    const backends = getCapableBackends(result);
    if (!result.ok) {
      if (typeof result.reason !== "string" || !result.reason) violations.push("rejection has no reason");
      if (JSON.stringify(backends) !== '["search"]') violations.push(`unsupported query offered ${backends.join(",")}`);
    } else {
      const q = result.query;
      if (!q.repo || !q.repo.owner || !q.repo.repo) violations.push("accepted query without repository");
      if (!TYPES.includes(q.type)) violations.push(`invalid type ${q.type}`);
      if (!STATES.includes(q.state)) violations.push(`invalid state ${q.state}`);
      if (q.state === "merged" && q.type !== "pr") violations.push("merged state without pr type");
      if (q.labels.some(l => !l || l.includes(","))) violations.push("empty or OR label accepted");
      if ([...q.labels, q.author, q.assignee, q.mentions, q.repo?.owner, q.repo?.repo].some(v => v?.includes('"'))) violations.push("value with stray quote accepted");
      if (new Set(q.labels).size !== q.labels.length) violations.push("duplicate labels");
      for (const login of [q.author, q.assignee, q.mentions]) {
        if (login !== null && !LOGIN_PATTERN.test(login)) violations.push(`invalid login ${login}`);
      }

      if (backends[backends.length - 1] !== "search") violations.push("search is not the final fallback");
      if (!backends.includes("rest")) violations.push("REST not offered for supported query");
      const graphqlExact = q.labels.length <= 1 && (q.type === "issue" || !(q.author || q.assignee || q.mentions));
      if (backends.includes("graphql") !== graphqlExact) violations.push(`graphql offered=${backends.includes("graphql")} exact=${graphqlExact}`);

      for (const suffix of [" zzfreetext", " -label:x", ' "quoted text"', " in:title"]) {
        if (parseSearchQuery(query + suffix).ok) violations.push(`suffix ${JSON.stringify(suffix)} did not force search`);
      }
    }

    for (const resources of RESOURCE_SCENARIOS) {
      const ordered = orderBackendsByPressure(backends, resources);
      if (ordered.length !== backends.length || [...ordered].sort().join() !== [...backends].sort().join()) {
        violations.push(`pressure ordering ${ordered.join(",")} is not a permutation of ${backends.join(",")}`);
      }
    }

    const tokens = tokenizeSearchQuery(query);
    if (tokens === null) {
      if (result.ok) violations.push("accepted query with unbalanced quotes");
    } else {
      const expected = normalize(result);
      const variants = {
        reordered: [...tokens].reverse().join(" "),
        "extra whitespace": ` \t${tokens.join(" \n\t ")}  `,
        "sort qualifier": [...tokens, "sort:updated-desc"].join(" "),
      };
      for (const [name, variant] of Object.entries(variants)) {
        if (normalize(parseSearchQuery(variant)) !== expected) violations.push(`${name} variant changed result`);
      }
    }
  } catch (err) {
    violations.push(`threw: ${getErrorMessage(err)}`);
  }
  return violations;
}

const FRAGMENTS = [
  "is:issue",
  "is:pr",
  "is:pull-request",
  "IS:Open",
  "is:open",
  "is:closed",
  "is:merged",
  "is:draft",
  "type:issue",
  "type:pr",
  "state:open",
  "state:closed",
  "state:merged",
  "label:bug",
  "label:Bug",
  'label:"needs triage"',
  "label:a,b",
  'label:""',
  "label:",
  "-label:bug",
  "repo:octo/demo",
  "repo:Octo/Demo",
  "repo:octo/other",
  "repo:bad",
  "author:octocat",
  "author:app/bot",
  "assignee:@me",
  "assignee:octocat",
  "mentions:hubot",
  "sort:created-desc",
  "org:octo",
  "in:title",
  "no:assignee",
  '"[daily-report]"',
  "free",
  '"unterminated',
  ":",
  "-",
];

/**
 * Builds a query from a byte choice stream and a free-form payload.
 * @param {number[]} bytes
 * @param {string} payload
 * @returns {string}
 */
function buildModelQuery(bytes, payload) {
  const parts = [];
  for (const b of bytes.slice(0, 64)) {
    const pick = b % (FRAGMENTS.length + 4);
    if (pick < FRAGMENTS.length) parts.push(FRAGMENTS[pick]);
    else if (pick === FRAGMENTS.length) parts.push(`label:${payload}`);
    else if (pick === FRAGMENTS.length + 1) parts.push(`author:${payload}`);
    else if (pick === FRAGMENTS.length + 2) parts.push(`repo:${payload}`);
    else parts.push(payload);
  }
  return parts.join((bytes[0] ?? 0) % 3 === 0 ? " \t " : " ");
}

// Read input from stdin for Go-driven fuzzing
if (require.main === module) {
  let input = "";
  process.stdin.on("data", chunk => {
    input += chunk;
  });
  process.stdin.on("end", () => {
    try {
      // Expected JSON: { query?: string, bytes?: number[], payload?: string }
      const { query, bytes, payload } = JSON.parse(input);
      const text = Array.isArray(bytes) ? buildModelQuery(bytes, payload ?? "") : (query ?? "");
      const violations = checkSkipIfQueryInvariants(text);
      process.stdout.write(JSON.stringify({ ok: violations.length === 0, query: text, violations }));
      process.exit(0);
    } catch (err) {
      process.stdout.write(JSON.stringify({ ok: false, violations: [getErrorMessage(err)] }));
      process.exit(1);
    }
  });
}

module.exports = { checkSkipIfQueryInvariants, buildModelQuery, FRAGMENTS };
