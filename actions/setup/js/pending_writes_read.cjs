// @ts-check
"use strict";

class PendingReadUnavailableError extends Error {
  constructor(cause) {
    super("Pending-write target cannot be read with credentials available before the gate; no future write-App token or alternate credential was used", { cause });
    this.code = "ERR_PENDING_READ_UNAVAILABLE";
  }
}

/**
 * Select once from existing credentials: handler token, matching checkout read
 * token, then the workflow token. Never retry authorization with another token.
 */
function pendingReadAccess(config, repository, options) {
  const configured = config["github-token"] || options.readTokens?.[repository];
  const token = configured || options.readToken;
  if (typeof token !== "string" && token !== undefined) throw new PendingReadUnavailableError(undefined);
  const client = token && typeof globalThis.getOctokit === "function" ? globalThis.getOctokit(token, { credentialSource: configured ? "pat" : "unknown" }) : options.readClient || globalThis.github;
  return { token, client };
}

async function pendingRead(operation) {
  try {
    return await operation();
  } catch (cause) {
    throw new PendingReadUnavailableError(cause);
  }
}

module.exports = { PendingReadUnavailableError, pendingReadAccess, pendingRead };
