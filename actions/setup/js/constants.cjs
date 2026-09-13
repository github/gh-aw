// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Constants
 *
 * This module provides shared constants used across JavaScript actions.
 * Where a constant has a counterpart in pkg/constants/constants.go, their values should be kept in sync.
 * Some constants are specific to the JavaScript implementation and do not have Go equivalents.
 */

/**
 * AgentOutputFilename is the filename of the agent output JSON file
 * @type {string}
 */
const AGENT_OUTPUT_FILENAME = "agent_output.json";

/**
 * Base path for temporary gh-aw files
 * @type {string}
 */
const TMP_GH_AW_PATH = "/tmp/gh-aw";

// ---------------------------------------------------------------------------
// GitHub reviewer bot
// ---------------------------------------------------------------------------

/**
 * GitHub login name for the Copilot pull request reviewer bot
 * @type {string}
 */
const COPILOT_REVIEWER_BOT = "copilot-pull-request-reviewer[bot]";

/**
 * GitHub.com GraphQL node ID for the Copilot pull request reviewer bot.
 * Used as a fallback when the node ID cannot be resolved at runtime (e.g. network error).
 * For GHES and other GitHub instances the node ID differs; prefer runtime resolution via
 * the REST users API ({@link https://docs.github.com/en/rest/users/users#get-a-user}).
 * @type {string}
 */
const COPILOT_REVIEWER_BOT_ID = "BOT_kgDOCnlnWA";

// ---------------------------------------------------------------------------
// Documentation URLs
// ---------------------------------------------------------------------------

/**
 * FAQ URL explaining why create-pull-request workflows may fail due to
 * GitHub Actions not being permitted to create or approve pull requests
 * @type {string}
 */
const FAQ_CREATE_PR_PERMISSIONS_URL = "https://github.github.com/gh-aw/reference/faq/#why-is-my-create-pull-request-workflow-failing-with-github-actions-is-not-permitted-to-create-or-approve-pull-requests";

// ---------------------------------------------------------------------------
// Array size limits
// ---------------------------------------------------------------------------

/**
 * Maximum number of labels that can be applied to an issue, PR, or discussion
 * @type {number}
 */
const MAX_LABELS = 10;

/**
 * Maximum number of assignees for an issue or pull request
 * @type {number}
 */
const MAX_ASSIGNEES = 5;

// ---------------------------------------------------------------------------
// File paths
// ---------------------------------------------------------------------------

/**
 * Path to the MCP gateway JSONL log file
 * @type {string}
 */
const GATEWAY_JSONL_PATH = `${TMP_GH_AW_PATH}/mcp-logs/gateway.jsonl`;

/**
 * Path to the MCP RPC messages JSONL log file
 * @type {string}
 */
const RPC_MESSAGES_PATH = `${TMP_GH_AW_PATH}/mcp-logs/rpc-messages.jsonl`;

/**
 * Path to the safe-output manifest JSONL file
 * @type {string}
 */
const MANIFEST_FILE_PATH = `${TMP_GH_AW_PATH}/safe-output-items.jsonl`;

/**
 * Path to the temporary ID map JSON file.
 * This file stores the mapping of temporary IDs (e.g., aw_abc123) to their resolved
 * GitHub resource references ({repo, number}) for review and audit purposes.
 * The file is uploaded as part of the safe-outputs-items artifact.
 * @type {string}
 */
const TEMPORARY_ID_MAP_FILE_PATH = `${TMP_GH_AW_PATH}/temporary-id-map.json`;

/**
 * Path to the safe-output error report JSON file.
 * This file records structured diagnostics (error code, message, failing safe-output
 * types) whenever the safe-output handler manager fails, so that failures of the
 * "Process Safe Outputs" step remain diagnosable after the job logs expire.
 * The file is uploaded as part of the safe-outputs-items artifact.
 * @type {string}
 */
const SAFE_OUTPUT_ERRORS_FILE_PATH = `${TMP_GH_AW_PATH}/safe-output-errors.json`;

/**
 * Path to the OTLP telemetry mirror file.
 * Every OTLP span payload is appended here as a JSON line for artifact inspection.
 * @type {string}
 */
const OTEL_JSONL_PATH = `${TMP_GH_AW_PATH}/otel.jsonl`;

/**
 * Path to the GitHub API rate-limit log file.
 * Each entry records the x-ratelimit-* headers (or rate-limit API snapshot)
 * at the time of a GitHub REST API call, enabling post-run rate-limit analysis.
 * @type {string}
 */
const GITHUB_RATE_LIMITS_JSONL_PATH = `${TMP_GH_AW_PATH}/github_rate_limits.jsonl`;

/**
 * Filename of the threat detection log written by the detection engine via tee.
 * The detection copilot's stdout (containing THREAT_DETECTION_RESULT) is piped
 * through `tee -a` to this file inside the threat-detection directory.
 * @type {string}
 */
const DETECTION_LOG_FILENAME = "detection.log";

/**
 * Filename of the structured threat detection result written by the Codex engine
 * via `--output-last-message`. When present, the parser reads this file directly
 * instead of scraping the detection log, eliminating false parse_error warnings
 * caused by noisy SSE/tracing output in the log stream.
 * @type {string}
 */
const DETECTION_RESULT_FILENAME = "detection_result.json";

/**
 * Maximum retry attempts for the workflow-run creation time lookup.
 * The operational-value grader cannot bind a run opportunity without this
 * timestamp, so transient lookup failures are retried before giving up.
 * @type {number}
 */
const RUN_CREATED_AT_MAX_RETRIES = 2;

/**
 * Initial backoff delay, in milliseconds, for the workflow-run creation time lookup.
 * @type {number}
 */
const RUN_CREATED_AT_INITIAL_DELAY_MS = 500;

/**
 * Maximum backoff delay, in milliseconds, for the workflow-run creation time lookup.
 * @type {number}
 */
const RUN_CREATED_AT_MAX_DELAY_MS = 2000;

/**
 * Directory name holding deterministic grader output files.
 * Counterpart of GradersDirName in pkg/constants/job_constants.go.
 * @type {string}
 */
const GRADERS_DIR_NAME = "graders";

/**
 * Filename of the deterministic grader results document.
 * Counterpart of GraderResultsFilename in pkg/constants/job_constants.go.
 * @type {string}
 */
const GRADER_RESULTS_FILENAME = "grader_results.json";

/**
 * Filename of the deterministic grader manifest.
 * Counterpart of GraderManifestFilename in pkg/constants/job_constants.go.
 * @type {string}
 */
const GRADER_MANIFEST_FILENAME = "grader_manifest.json";

module.exports = {
  AGENT_OUTPUT_FILENAME,
  TMP_GH_AW_PATH,
  COPILOT_REVIEWER_BOT,
  COPILOT_REVIEWER_BOT_ID,
  FAQ_CREATE_PR_PERMISSIONS_URL,
  MAX_LABELS,
  MAX_ASSIGNEES,
  GATEWAY_JSONL_PATH,
  RPC_MESSAGES_PATH,
  MANIFEST_FILE_PATH,
  TEMPORARY_ID_MAP_FILE_PATH,
  SAFE_OUTPUT_ERRORS_FILE_PATH,
  OTEL_JSONL_PATH,
  GITHUB_RATE_LIMITS_JSONL_PATH,
  DETECTION_LOG_FILENAME,
  DETECTION_RESULT_FILENAME,
  RUN_CREATED_AT_MAX_RETRIES,
  RUN_CREATED_AT_INITIAL_DELAY_MS,
  RUN_CREATED_AT_MAX_DELAY_MS,
  GRADERS_DIR_NAME,
  GRADER_RESULTS_FILENAME,
  GRADER_MANIFEST_FILENAME,
};
