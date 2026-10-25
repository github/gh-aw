// @ts-check

const MAX_HARNESS_ATTEMPTS = 101;
const MAX_HARNESS_FAILURE_CLASSES = 16;
const FAILURE_CLASS_PATTERN = /^[a-z][a-z0-9_]{0,63}$/;

/** @param {unknown} value @returns {value is Record<string, any>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {Record<string, any>} record @param {string} snakeCase @param {string} camelCase */
function readField(record, snakeCase, camelCase) {
  return Object.hasOwn(record, snakeCase) ? record[snakeCase] : record[camelCase];
}

/**
 * Validate and project a harness retry record without retaining arbitrary fields.
 * @param {unknown} value
 * @returns {{attempts: number, exitCode: number, attemptResults: {attempt: number, exitCode: number, failureClasses: string[]}[]} | undefined}
 */
function normalizeHarnessRetryRecord(value) {
  if (!isRecord(value)) return undefined;
  const attempts = value.attempts;
  const exitCode = readField(value, "exit_code", "exitCode");
  const rawResults = readField(value, "attempt_results", "attemptResults");
  if (!Number.isSafeInteger(attempts) || attempts < 0 || attempts > MAX_HARNESS_ATTEMPTS || !Number.isSafeInteger(exitCode) || !Array.isArray(rawResults) || rawResults.length !== attempts) {
    return undefined;
  }
  const attemptResults = [];
  for (const [index, result] of rawResults.entries()) {
    if (!isRecord(result)) return undefined;
    const resultExitCode = readField(result, "exit_code", "exitCode");
    const failureClasses = readField(result, "failure_classes", "failureClasses");
    if (
      result.attempt !== index + 1 ||
      !Number.isSafeInteger(resultExitCode) ||
      !Array.isArray(failureClasses) ||
      failureClasses.length > MAX_HARNESS_FAILURE_CLASSES ||
      !failureClasses.every(value => typeof value === "string" && FAILURE_CLASS_PATTERN.test(value))
    ) {
      return undefined;
    }
    attemptResults.push({ attempt: result.attempt, exitCode: resultExitCode, failureClasses: [...failureClasses] });
  }
  return { attempts, exitCode, attemptResults };
}

module.exports = { normalizeHarnessRetryRecord, MAX_HARNESS_ATTEMPTS, MAX_HARNESS_FAILURE_CLASSES };
