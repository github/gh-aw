// @ts-check

/**
 * Escape a value for a GitHub Actions workflow command payload.
 *
 * @param {string} value
 * @returns {string}
 */
function escapeWorkflowCommandValue(value) {
  return value.replace(/%/g, "%25").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
}

/**
 * Mask a secret in the surrounding GitHub Actions step when masking is available.
 *
 * Plain Node entry points such as setup.sh-loaded scripts do not have the real
 * @actions/core object, but GitHub Actions still processes add-mask workflow
 * commands emitted by the process.
 *
 * @param {unknown} value
 */
function maskSecret(value) {
  if (value === undefined || value === null) return;
  const secret = String(value);
  if (!secret) return;

  const setSecret = global.core?.setSecret;
  if (typeof setSecret === "function" && !("__ghAwUnavailable" in setSecret && setSecret.__ghAwUnavailable)) {
    setSecret.call(global.core, secret);
    return;
  }

  // shim.cjs marks its throwing placeholder so Actions-side plain Node callers
  // can still mask via workflow commands without enabling masking in MCP shims.
  if (process.env.GITHUB_ACTIONS === "true") {
    process.stdout.write(`::add-mask::${escapeWorkflowCommandValue(secret)}\n`);
  }
}

module.exports = { escapeWorkflowCommandValue, maskSecret };
