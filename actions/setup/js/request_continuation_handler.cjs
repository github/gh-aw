// @ts-check
/// <reference types="@actions/github-script" />

/** @type {import('./types/handler-factory').HandlerFactoryFunction} */
async function main(config = {}) {
  const { max: maxCount = 1 } = config;
  if (!Number.isSafeInteger(maxCount) || maxCount < 1) {
    throw new Error("request_continuation max must be a positive integer");
  }
  let processedCount = 0;

  return async function handleRequestContinuation(message) {
    if (maxCount > 0 && processedCount >= maxCount) {
      return { success: false, error: `Max count of ${maxCount} reached` };
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
