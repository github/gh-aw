// @ts-check
/// <reference types="@actions/github-script" />

/** @type {import('./types/handler-factory').HandlerFactoryFunction} */
async function main(config = {}) {
  const { max: maxCount = 1 } = config;
  const normalizedMax = typeof maxCount === "string" && /^\d+$/.test(maxCount) ? Number(maxCount) : maxCount;
  if (!Number.isSafeInteger(normalizedMax) || normalizedMax < 1) {
    throw new Error("request_continuation max must be a positive integer");
  }
  let processedCount = 0;

  return async function handleRequestContinuation(message) {
    if (processedCount >= normalizedMax) {
      return { success: false, error: `Max count of ${normalizedMax} reached` };
    }
    if (typeof message.message !== "string" || !message.message.trim()) {
      return { success: false, error: "Missing required field: message" };
    }
    processedCount++;
    // Do not echo potentially sensitive agent content into Actions logs.
    core.info("request_continuation: follow-up requested; continuation not confirmed");
    return {
      success: true,
      message: message.message,
      timestamp: new Date().toISOString(),
    };
  };
}

module.exports = { main };
