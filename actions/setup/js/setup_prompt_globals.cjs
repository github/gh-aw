// @ts-check
/// <reference types="@actions/github-script" />

const { ERR_CONFIG, ERR_PARSE } = require("./error_codes.cjs");

const PROMPT_ENVIRONMENT = [
  "PATH",
  "RUNNER_TEMP",
  "RUNNER_OS",
  "RUNNER_ARCH",
  "GITHUB_WORKSPACE",
  "GITHUB_EVENT_PATH",
  "GITHUB_EVENT_NAME",
  "GITHUB_REPOSITORY",
  "GITHUB_ACTOR",
  "GITHUB_SHA",
  "GITHUB_REF",
  "GITHUB_RUN_ID",
  "GITHUB_RUN_NUMBER",
  "GITHUB_RUN_ATTEMPT",
  "GITHUB_WORKFLOW",
  "GITHUB_JOB",
  "GITHUB_ACTION",
  "GITHUB_SERVER_URL",
  "GITHUB_API_URL",
  "GITHUB_GRAPHQL_URL",
  "GITHUB_STEP_SUMMARY",
  "GITHUB_OUTPUT",
  "GH_AW_ACTIONS_DIR",
];

/**
 * Strip inherited credentials before loading prompt modules. Unlike setupGlobals,
 * prompt rendering receives only logging and event context, never API clients.
 * @param {typeof core} coreModule
 * @param {typeof context} contextModule
 */
function setupPromptGlobals(coreModule, contextModule) {
  const config = process.env.GH_AW_PROMPT_ENV;
  if (!config) {
    throw new Error(`${ERR_CONFIG}: GH_AW_PROMPT_ENV environment variable is not set`);
  }
  let selectedEnvironment;
  try {
    selectedEnvironment = JSON.parse(config);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid GH_AW_PROMPT_ENV JSON`, { cause: error });
  }
  if (!Array.isArray(selectedEnvironment) || !selectedEnvironment.every(name => typeof name === "string")) {
    throw new Error(`${ERR_CONFIG}: GH_AW_PROMPT_ENV must contain an array of environment variable names`);
  }
  const allowedEnvironment = new Set([...PROMPT_ENVIRONMENT, ...selectedEnvironment]);
  for (const name of Object.keys(process.env)) {
    if (!allowedEnvironment.has(name)) delete process.env[name];
  }
  global.core = coreModule;
  global.context = contextModule;
}

module.exports = { setupPromptGlobals };
