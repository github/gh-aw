// @ts-check

const { sanitizeContent } = require("./sanitize_content.cjs");

const JIRA_API_PATH = "/rest/api/3";
const JIRA_REQUEST_TIMEOUT_MS = 30_000;

function logJiraDebug(message) {
  if (global.core && typeof global.core.debug === "function") {
    global.core.debug(message);
  }
}

function normalizeJiraBaseUrl(value) {
  const raw = typeof value === "string" ? value.trim() : "";
  if (!raw) {
    throw new Error("Jira configuration is missing JIRA_BASE_URL");
  }

  let url;
  try {
    url = new URL(raw);
  } catch {
    throw new Error("JIRA_BASE_URL must be a valid URL");
  }

  const hostname = url.hostname.replace(/^\[|\]$/g, "");
  const isLocal = hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1";
  if (url.protocol !== "https:" && !(isLocal && url.protocol === "http:")) {
    throw new Error("JIRA_BASE_URL must use HTTPS");
  }
  if (url.search || url.hash) {
    throw new Error("JIRA_BASE_URL must not include a query string or fragment");
  }

  url.pathname = url.pathname.replace(/\/+$/, "").replace(/\/rest\/api\/3$/i, "");
  return url.toString().replace(/\/+$/, "");
}

function textToADF(value) {
  const text = String(value).replace(/\r\n?/g, "\n");
  return {
    type: "doc",
    version: 1,
    content: text.split("\n").map(line => ({
      type: "paragraph",
      content: line ? [{ type: "text", text: line }] : [],
    })),
  };
}

function redactJiraSecrets(value, secrets) {
  let result = String(value);
  for (const secret of secrets) {
    if (secret) {
      result = result.split(secret).join("***");
    }
  }
  return result;
}

function formatJiraError(status, statusText, responseBody, secrets) {
  const details = [];
  if (responseBody && typeof responseBody === "object") {
    if (Array.isArray(responseBody.errorMessages)) {
      details.push(...responseBody.errorMessages.filter(message => typeof message === "string"));
    }
    if (responseBody.errors && typeof responseBody.errors === "object" && !Array.isArray(responseBody.errors)) {
      for (const [field, message] of Object.entries(responseBody.errors)) {
        if (typeof message === "string") {
          details.push(`${field}: ${message}`);
        }
      }
    }
  }

  const detail = details.length > 0 ? `: ${details.join("; ")}` : "";
  const safe = sanitizeContent(redactJiraSecrets(`Jira API request failed (${status} ${statusText || "Error"})${detail}`, secrets), 2000);
  return safe || `Jira API request failed (${status})`;
}

function createJiraClient(env = process.env, fetchImpl = global.fetch) {
  const baseUrl = normalizeJiraBaseUrl(env.JIRA_BASE_URL);
  const email = typeof env.JIRA_USER_EMAIL === "string" ? env.JIRA_USER_EMAIL.trim() : "";
  const token = typeof env.JIRA_API_TOKEN === "string" ? env.JIRA_API_TOKEN : "";
  const missingSecrets = [...(email ? [] : ["JIRA_USER_EMAIL"]), ...(token ? [] : ["JIRA_API_TOKEN"])];
  if (missingSecrets.length > 0) {
    throw new Error(
      `Jira configuration is missing required GitHub Actions ${missingSecrets.length === 1 ? "secret" : "secrets"}: ${missingSecrets.join(", ")}. Configure ${missingSecrets.length === 1 ? "it" : "them"} as a repository or organization secret available to this workflow`
    );
  }
  if (typeof fetchImpl !== "function") {
    throw new Error("Jira requests require the fetch API");
  }

  const authorization = `Basic ${Buffer.from(`${email}:${token}`, "utf8").toString("base64")}`;
  const secrets = [token, email, authorization];
  logJiraDebug("Jira client configured with API-token authentication");

  return {
    async request(path, options = {}) {
      const normalizedPath = path.startsWith("/") ? path : `/${path}`;
      const url = `${baseUrl}${JIRA_API_PATH}${normalizedPath}`;
      const method = options.method || "GET";
      logJiraDebug(`Jira API request started: ${method} ${normalizedPath}`);
      let response;
      const abortController = new AbortController();
      const timeout = setTimeout(() => abortController.abort(), JIRA_REQUEST_TIMEOUT_MS);
      try {
        response = await fetchImpl(url, {
          method,
          signal: abortController.signal,
          headers: {
            Accept: "application/json",
            Authorization: authorization,
            "Content-Type": "application/json",
          },
          ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
        });
      } catch {
        logJiraDebug(`Jira API request failed before receiving a response: ${method} ${normalizedPath}`);
        throw new Error("Jira API request failed due to a network error");
      } finally {
        clearTimeout(timeout);
      }

      logJiraDebug(`Jira API response received: ${method} ${normalizedPath} status=${response.status}`);
      const responseText = await response.text();
      let responseBody = null;
      if (responseText) {
        try {
          responseBody = JSON.parse(responseText);
        } catch {
          if (response.ok) {
            logJiraDebug(`Jira API response contained invalid JSON: ${method} ${normalizedPath}`);
            throw new Error(`Jira API returned an invalid JSON response (${response.status})`);
          }
        }
      }

      if (!response.ok) {
        logJiraDebug(`Jira API request rejected: ${method} ${normalizedPath} status=${response.status}`);
        throw new Error(formatJiraError(response.status, response.statusText, responseBody, secrets));
      }
      logJiraDebug(`Jira API request completed: ${method} ${normalizedPath}`);
      return responseBody;
    },
  };
}

module.exports = {
  JIRA_API_PATH,
  JIRA_REQUEST_TIMEOUT_MS,
  createJiraClient,
  formatJiraError,
  normalizeJiraBaseUrl,
  textToADF,
};
