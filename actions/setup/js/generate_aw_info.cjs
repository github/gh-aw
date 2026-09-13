// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { TMP_GH_AW_PATH } = require("./constants.cjs");
const { generateWorkflowOverview } = require("./generate_workflow_overview.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { validateContextVariables } = require("./validate_context_variables.cjs");
const validateLockdownRequirements = require("./validate_lockdown_requirements.cjs");
const { writeMergedModelsJSON } = require("./merge_frontmatter_models.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveRunCreatedAtForInfo } = require("./run_created_at.cjs");
const { ERR_CONFIG, ERR_SYSTEM } = require("./error_codes.cjs");

/**
 * Generate aw_info.json with workflow run metadata.
 * Reads compile-time values from environment variables (GH_AW_INFO_*) and
 * runtime values from the GitHub Actions context. Validates required context
 * variables, writes to /tmp/gh-aw/aw_info.json, sets the model output, and
 * prints the agent overview in the step summary.
 *
 * SEC-005: The `target_repo` field written to aw_info.json is compile-time
 * metadata sourced from GH_AW_INFO_TARGET_REPO. It is not used for cross-repository
 * API calls in this handler; no validateTargetRepo allowlist check is required here.
 *
 * @param {typeof import('@actions/core')} core - GitHub Actions core library
 * @param {any} ctx - GitHub Actions context object
 * @param {any} [githubClient] - Authenticated GitHub client; falls back to global.github
 * @returns {Promise<void>}
 */
async function main(core, ctx, githubClient) {
  // Validate numeric context variables before processing run info.
  // This prevents malicious payloads from hiding special text or code in numeric fields.
  await validateContextVariables(core, ctx);

  // Validate lockdown mode requirements if lockdown is explicitly enabled.
  // This fails fast if lockdown: true is set but no custom GitHub token is configured.
  validateLockdownRequirements(core);

  // Validate required context variables
  const requiredContextFields = ["runId", "runNumber", "sha", "ref", "actor", "eventName", "repo"];
  for (const field of requiredContextFields) {
    if (ctx[field] == null) {
      core.warning(`GitHub Actions context.${field} is not set`);
    }
  }

  // Parse allowed domains from JSON env var
  let allowedDomains = [];
  const allowedDomainsEnv = process.env.GH_AW_INFO_ALLOWED_DOMAINS || "[]";
  try {
    allowedDomains = JSON.parse(allowedDomainsEnv);
  } catch {
    core.warning(`Failed to parse GH_AW_INFO_ALLOWED_DOMAINS: ${allowedDomainsEnv}`);
  }

  // Build awInfo from env vars (compile-time) + context (runtime)
  const model = process.env.GH_AW_INFO_MODEL || "";

  // Reject model names that contain an unresolved GitHub Actions expression.
  // This can happen when a vars.* expression was used for the model in the
  // workflow frontmatter but the variable is not defined in the repository,
  // causing GitHub Actions to pass the literal expression string at runtime.
  if (/\$\{\{/.test(model)) {
    const message = `${ERR_CONFIG}: GH_AW_INFO_MODEL contains an unresolved GitHub Actions expression: ${JSON.stringify(model)}`;
    core.setFailed(message);
    throw new Error(message);
  }

  /** @type {Record<string, unknown>} */
  const awInfo = {
    engine_id: process.env.GH_AW_INFO_ENGINE_ID || "",
    engine_name: process.env.GH_AW_INFO_ENGINE_NAME || "",
    model,
    version: process.env.GH_AW_INFO_VERSION || "",
    agent_version: process.env.GH_AW_INFO_AGENT_VERSION || "",
    workflow_name: process.env.GH_AW_INFO_WORKFLOW_NAME || "",
    experimental: process.env.GH_AW_INFO_EXPERIMENTAL === "true",
    supports_tools_allowlist: process.env.GH_AW_INFO_SUPPORTS_TOOLS_ALLOWLIST === "true",
    cache_memory: process.env.GH_AW_INFO_CACHE_MEMORY === "true",
    run_id: ctx.runId,
    run_number: ctx.runNumber,
    run_attempt: process.env.GITHUB_RUN_ATTEMPT,
    repository: ctx.repo ? ctx.repo.owner + "/" + ctx.repo.repo : "",
    ref: ctx.ref,
    sha: ctx.sha,
    actor: ctx.actor,
    event_name: ctx.eventName,
    target_repo: process.env.GH_AW_INFO_TARGET_REPO || "",
    staged: process.env.GH_AW_INFO_STAGED === "true",
    allowed_domains: allowedDomains,
    firewall_enabled: process.env.GH_AW_INFO_FIREWALL_ENABLED === "true",
    awf_version: process.env.GH_AW_INFO_AWF_VERSION || "",
    awmg_version: process.env.GH_AW_INFO_AWMG_VERSION || "",
    agent_runtime: process.env.GH_AW_INFO_AGENT_RUNTIME || "",
    steps: {
      firewall: process.env.GH_AW_INFO_FIREWALL_TYPE || "",
    },
    created_at: new Date().toISOString(),
  };

  if (process.env.GH_AW_INFO_FETCH_RUN_CREATED_AT === "true") {
    core.setOutput("run_created_at", await resolveRunCreatedAtForInfo(core, ctx, githubClient));
  }

  const frontmatterSource = process.env.GH_AW_INFO_FRONTMATTER_SOURCE || "";
  if (frontmatterSource) {
    awInfo.frontmatter_source = frontmatterSource;
  }

  const frontmatterEmoji = process.env.GH_AW_INFO_FRONTMATTER_EMOJI || "";
  if (frontmatterEmoji) {
    awInfo.frontmatter_emoji = frontmatterEmoji;
  }

  const bodyModified = process.env.GH_AW_INFO_BODY_MODIFIED;
  if (bodyModified === "true" || bodyModified === "false") {
    awInfo.body_modified = bodyModified === "true";
  }

  // Include cli_version only when set (released builds only)
  const cliVersion = process.env.GH_AW_INFO_CLI_VERSION;
  if (cliVersion) {
    awInfo.cli_version = cliVersion;
  }

  // Include deployment_state when triggered by a deployment_status event.
  // This makes the deployment state available to the agent without requiring it to
  // read the raw event payload, and is propagated to child workflows via aw_context.
  const deploymentState = ctx.payload?.deployment_status?.state;
  if (deploymentState && typeof deploymentState === "string") {
    awInfo.deployment_state = deploymentState;
  }

  // Include workflow_run_conclusion when triggered by a workflow_run event.
  // This makes the triggering run conclusion available to the agent without requiring it
  // to read the raw event payload, and is propagated to child workflows via aw_context.
  const workflowRunConclusion = ctx.payload?.workflow_run?.conclusion;
  if (workflowRunConclusion && typeof workflowRunConclusion === "string") {
    awInfo.workflow_run_conclusion = workflowRunConclusion;
  }

  const features = parseFeaturesFromEnv(core);
  if (features) {
    awInfo.features = features;
  }

  const skills = parseSkillsFromEnv(core);
  if (skills) {
    awInfo.skills = skills;
    core.info(`Configured frontmatter skills (${skills.length}): ${skills.join(", ")}`);
  }

  // Include aw_context when the workflow was triggered by a caller that relayed
  // orchestration context via workflow inputs or repository_dispatch client payload.
  // Validates JSON format and structure before populating the context key in aw_info.json.
  const awContextRaw = ctx.payload?.inputs?.aw_context ?? ctx.payload?.client_payload?.aw_context;
  if (awContextRaw != null) {
    try {
      const parsed = typeof awContextRaw === "string" ? JSON.parse(awContextRaw) : awContextRaw;

      // Validate: must be a plain non-null object (not an array or primitive)
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
        core.warning(`aw_context must be a JSON object, got: ${typeof parsed}`);
      } else {
        // Validate: no nested objects (all values must be primitives)
        const nestedKeys = Object.entries(parsed)
          .filter(([, v]) => v !== null && typeof v === "object")
          .map(([k]) => k);
        if (nestedKeys.length > 0) {
          core.warning(`aw_context contains nested objects for keys: ${nestedKeys.join(", ")}. Ignoring aw_context.`);
        } else {
          // Validate: required fields must be present
          const requiredFields = ["run_id", "repo", "workflow_id"];
          const missingFields = requiredFields.filter(f => !(f in parsed));
          if (missingFields.length > 0) {
            core.warning(`aw_context is missing required fields: ${missingFields.join(", ")}. Ignoring aw_context.`);
          } else {
            awInfo.context = parsed;
          }
        }
      }
    } catch {
      core.warning(`Failed to parse aw_context input as JSON: ${String(awContextRaw)}`);
    }
  }

  // Write to /tmp/gh-aw directory to avoid inclusion in PR
  try {
    fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to create directory ${TMP_GH_AW_PATH}: ${getErrorMessage(err)}`, { cause: err });
  }
  writeMergedModelsJSON(core);
  const tmpPath = TMP_GH_AW_PATH + "/aw_info.json";
  try {
    fs.writeFileSync(tmpPath, JSON.stringify(awInfo, null, 2));
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to write file ${tmpPath}: ${getErrorMessage(err)}`, { cause: err });
  }

  if (awInfo.staged) {
    logStagedPreviewInfo("Generating workflow info in staged mode — no changes applied");
  }

  /**
   * Parse optional features map from GH_AW_INFO_FEATURES.
   * @param {typeof import('@actions/core')} core
   * @returns {Record<string, unknown> | null}
   */
  function parseFeaturesFromEnv(core) {
    const featuresEnv = process.env.GH_AW_INFO_FEATURES;
    if (!featuresEnv) {
      return null;
    }
    try {
      const parsed = JSON.parse(featuresEnv);
      if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
        return Object.keys(parsed).length > 0 ? parsed : null;
      }
      core.warning("GH_AW_INFO_FEATURES must be a JSON object, ignoring");
      return null;
    } catch {
      core.warning(`Failed to parse GH_AW_INFO_FEATURES: ${featuresEnv}`);
      return null;
    }
  }

  /**
   * Parse optional skills list from GH_AW_INFO_SKILLS.
   * @param {typeof import('@actions/core')} core
   * @returns {string[] | null}
   */
  function parseSkillsFromEnv(core) {
    const skillsEnv = process.env.GH_AW_INFO_SKILLS;
    if (!skillsEnv) {
      return null;
    }
    try {
      const parsed = JSON.parse(skillsEnv);
      if (!Array.isArray(parsed)) {
        core.warning("GH_AW_INFO_SKILLS must be a JSON array, ignoring");
        return null;
      }
      const skills = [];
      for (const [index, value] of parsed.entries()) {
        if (typeof value === "string" && value.length > 0) {
          skills.push(value);
          continue;
        }
        core.warning(`Ignoring invalid GH_AW_INFO_SKILLS[${index}] value: ${JSON.stringify(value)}`);
      }
      return skills.length > 0 ? skills : null;
    } catch (err) {
      const message = getErrorMessage(err);
      core.warning(`Failed to parse GH_AW_INFO_SKILLS: ${skillsEnv} (${message})`);
      return null;
    }
  }

  core.info("Generated aw_info.json at: " + tmpPath);
  core.info(JSON.stringify(awInfo, null, 2));

  // Set model and engine_id as outputs for reuse in other steps/jobs
  core.setOutput("model", awInfo.model);
  core.setOutput("engine_id", awInfo.engine_id);

  // Generate workflow overview and write to step summary
  await generateWorkflowOverview(core);
}

module.exports = { main };
