// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "ledger_mutation";

/** @type {number} Maximum characters logged for any single audit field */
const MAX_FIELD_LENGTH = 128;

/**
 * Render one audit field for logging without leaking unexpected content.
 * Ledger audit entries only carry redacted metadata (identifiers, hashes and a
 * timestamp), so anything unexpected is truncated before it reaches the log.
 * @param {unknown} value
 * @returns {string}
 */
function renderField(value) {
  if (value === undefined || value === null) return "";
  const text = Array.isArray(value) ? value.map(item => renderField(item)).join(", ") : String(value);
  return text.length > MAX_FIELD_LENGTH ? `${text.slice(0, MAX_FIELD_LENGTH)}…` : text;
}

/**
 * Main handler factory for ledger_mutation.
 *
 * Ledger mutations are performed by the trusted ledger MCP server during the agent
 * job and are already durable by the time this handler runs. The handler therefore
 * performs no side effects: it only logs the redacted audit metadata so ledger
 * writes appear in the safe-outputs job alongside every other safe output.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const maxCount = config.max || 0; // 0 means unlimited

  core.info(`Max count: ${maxCount === 0 ? "unlimited" : maxCount}`);

  let processedCount = 0;

  /**
   * Message handler function that logs a single ledger_mutation message
   * @param {Object} message - The ledger_mutation audit entry to log
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleLedgerMutation(message) {
    if (maxCount > 0 && processedCount >= maxCount) {
      core.warning(`Skipping ledger_mutation: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    const operation = renderField(message.operation);
    if (!operation) {
      core.warning("ledger_mutation entry missing 'operation' field");
      return {
        success: false,
        error: "Missing required field: operation",
      };
    }

    const record = message.record && typeof message.record === "object" ? message.record : {};
    processedCount++;

    const recordId = renderField(record.id);
    const recordType = renderField(record.type);
    const recordSha = renderField(record.sha);
    const payloadSha = renderField(record.payload_sha);
    const parents = renderField(record.parents);
    const timestamp = renderField(message.timestamp) || renderField(record.timestamp);

    core.info(`Recorded ledger ${operation}: record ${recordId || "(unknown)"}`);
    if (recordType) core.info(`   Record type: ${recordType}`);
    if (recordSha) core.info(`   Record SHA: ${recordSha}`);
    if (payloadSha) core.info(`   Payload SHA: ${payloadSha}`);
    if (parents) core.info(`   Parents: ${parents}`);
    if (timestamp) core.info(`   Timestamp: ${timestamp}`);

    return {
      success: true,
      operation,
      record_id: recordId,
      record_type: recordType,
      record_sha: recordSha,
      payload_sha: payloadSha,
      timestamp,
    };
  };
}

module.exports = { main, HANDLER_TYPE };
