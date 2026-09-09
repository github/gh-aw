// @ts-check

const { ERR_API, ERR_CONFIG, ERR_PARSE } = require("./error_codes.cjs");

const LINEAR_GRAPHQL_ENDPOINT = "https://api.linear.app/graphql";
const LINEAR_ISSUE_PATTERN = /^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[A-Z][A-Z0-9]{0,15}-[1-9][0-9]*)$/i;
const LINEAR_TEAM_NOT_FOUND_MESSAGE = "entity not found: team";

function redactToken(value, token) {
  const text = String(value || "");
  return token ? text.split(token).join("***") : text;
}

async function linearGraphQL(query, variables, token = process.env.GH_AW_LINEAR_TOKEN) {
  if (!token) {
    throw new Error(`${ERR_CONFIG}: Linear API token is not configured`);
  }

  let response;
  try {
    response = await fetch(LINEAR_GRAPHQL_ENDPOINT, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: token,
      },
      body: JSON.stringify({ query, variables }),
      signal: AbortSignal.timeout(30_000),
    });
  } catch {
    throw new Error(`${ERR_API}: Linear request failed: network error`);
  }

  if (!response.ok) {
    const detail = response.status === 429 ? "rate limit exceeded" : `HTTP ${response.status}`;
    throw new Error(`${ERR_API}: Linear request failed: ${detail}`);
  }

  let payload;
  try {
    payload = await response.json();
  } catch {
    throw new Error(`${ERR_PARSE}: Linear returned a malformed JSON response`);
  }

  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    throw new Error(`${ERR_PARSE}: Linear returned an invalid GraphQL response`);
  }
  if (Array.isArray(payload.errors) && payload.errors.length > 0) {
    const message = redactToken(payload.errors[0]?.message || "unknown GraphQL error", token).slice(0, 500);
    if (message.toLowerCase().includes(LINEAR_TEAM_NOT_FOUND_MESSAGE)) {
      throw new Error(`${ERR_CONFIG}: Linear could not access the configured team. Verify safe-outputs.linear-create-issue.team-id or LINEAR_TEAM_ID references a team in the workspace authorized by LINEAR_API_KEY`);
    }
    throw new Error(`${ERR_API}: Linear GraphQL operation failed: ${message}`);
  }

  return payload.data;
}

module.exports = { LINEAR_GRAPHQL_ENDPOINT, LINEAR_ISSUE_PATTERN, linearGraphQL };
