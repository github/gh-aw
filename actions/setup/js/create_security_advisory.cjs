// @ts-check
/// <reference types="@actions/github-script" />

const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { validateValueAgainstSchema } = require("./schema-validation.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const tools = require("./safe_outputs_tools.json");

const tool = tools.find(tool => tool.name === "create_security_advisory");
if (!tool) throw new Error(`${ERR_CONFIG}: Missing create_security_advisory tool schema`);
const inputSchema = tool.inputSchema;

/** @param {any} message */
function advisoryPayload(message) {
  const payload = Object.fromEntries(
    Object.keys(inputSchema.properties)
      .filter(key => message[key] !== undefined)
      .map(key => [key, message[key]])
  );
  const schemaError = validateValueAgainstSchema(payload, inputSchema, { includeReceived: false });
  if (schemaError) throw new Error(`${ERR_VALIDATION}: create_security_advisory ${schemaError.path} ${schemaError.message}`);

  if (!payload.summary.trim() || payload.summary.length > 1024 || !payload.description.trim() || payload.description.length > 65000) {
    throw new Error(`${ERR_VALIDATION}: create_security_advisory summary or description is empty or exceeds its length limit`);
  }
  if (payload.vulnerabilities.length < 1 || payload.vulnerabilities.length > 100) {
    throw new Error(`${ERR_VALIDATION}: create_security_advisory requires between 1 and 100 vulnerabilities`);
  }
  for (const vulnerability of payload.vulnerabilities) {
    const textFields = [vulnerability.package.name, vulnerability.vulnerable_version_range, vulnerability.patched_versions, ...(vulnerability.vulnerable_functions || [])];
    if (textFields.some(value => typeof value === "string" && value.length > 1024) || vulnerability.vulnerable_functions?.length > 100) {
      throw new Error(`${ERR_VALIDATION}: create_security_advisory vulnerability details exceed their limits`);
    }
    if ((typeof vulnerability.package.name === "string" && !vulnerability.package.name.trim()) || vulnerability.vulnerable_functions?.some(value => !value.trim())) {
      throw new Error(`${ERR_VALIDATION}: create_security_advisory package and function names must not be empty`);
    }
  }
  if (payload.cwe_ids && (payload.cwe_ids.length > 100 || payload.cwe_ids.some(value => !/^CWE-[1-9][0-9]*$/.test(value)))) {
    throw new Error(`${ERR_VALIDATION}: create_security_advisory requires valid CWE IDs (for example CWE-79), at most 100`);
  }
  payload.summary = sanitizeTitle(payload.summary, "", 1024);
  payload.description = sanitizeContent(payload.description);
  if (!payload.summary.trim() || !payload.description.trim() || payload.description.length > 65000) {
    throw new Error(`${ERR_VALIDATION}: create_security_advisory content is empty or exceeds its length limit after sanitization`);
  }
  return payload;
}

/** @type {import('./types/handler-factory').HandlerFactoryFunction} */
async function main(config = {}) {
  const staged = isStagedMode(config);
  const max = Number(config.max ?? 1);
  if (!Number.isSafeInteger(max) || max < 1) {
    throw new Error(`${ERR_CONFIG}: create_security_advisory max must be a positive integer`);
  }
  if (!staged && (typeof config["github-token"] !== "string" || !config["github-token"].trim())) {
    throw new Error(`${ERR_CONFIG}: create_security_advisory requires an external github-token with Repository security advisories write permission; GITHUB_TOKEN is not supported`);
  }
  const githubClient = staged ? null : await createAuthenticatedGitHubClient(config);
  let processedCount = 0;

  return async function handleCreateSecurityAdvisory(message) {
    const payload = advisoryPayload(message);
    if (processedCount >= max) {
      return { success: false, skipped: true, reasonCode: "MAX_COUNT_REACHED", error: `Max count of ${max} reached` };
    }
    const request = { ...context.repo, ...payload, private_fork: false };
    if (staged) {
      processedCount++;
      logStagedPreviewInfo(`Would create a draft security advisory with request:\n${JSON.stringify(request, null, 2)}`);
      return { success: true, staged: true };
    }
    try {
      // The create endpoint always creates a draft. Never forward state, repository,
      // or private-fork controls from the agent.
      const { data } = await githubClient.rest.securityAdvisories.createRepositoryAdvisory(request);
      processedCount++;
      if (data.state !== "draft" || typeof data.ghsa_id !== "string" || typeof data.html_url !== "string") {
        throw new Error(`${ERR_API}: GitHub did not return a draft security advisory`);
      }
      core.info(`Created draft security advisory ${data.ghsa_id}`);
      return { success: true, ghsa_id: data.ghsa_id, url: data.html_url };
    } catch (error) {
      // API errors can include the confidential request body; expose only status.
      const status = typeof error === "object" && error !== null && "status" in error ? error.status : "unknown";
      const errorMessage = `${ERR_API}: Failed to create draft security advisory (HTTP ${status}). Verify the external token or GitHub App has Repository security advisories write permission and repository access.`;
      core.error(errorMessage);
      return { success: false, error: errorMessage };
    }
  };
}

module.exports = { main };
