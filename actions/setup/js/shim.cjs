// @ts-check

/**
 * shim.cjs
 *
 * Provides minimal `global.core` and `global.context` shims so that modules
 * written for the GitHub Actions `github-script` context (which rely on the
 * built-in `core` and `context` globals) work correctly when executed as plain
 * Node.js processes, such as inside the safe-outputs and mcp-scripts MCP servers.
 *
 * When `global.core` / `global.context` is already set (i.e. running inside
 * `github-script`) the respective block is a no-op.
 */

const setSecret = /** @param {string} _value */ _value => {
  throw new Error("core.setSecret is unavailable outside the github-script runtime");
};
Object.defineProperty(setSecret, "__ghAwUnavailable", { value: true });

/**
 * @param {string} name
 * @returns {never}
 */
function unavailableCore(name) {
  throw new Error(`${name} is unavailable outside the github-script runtime`);
}

if (!global.core) {
  /**
   * Write shim log lines to stderr so MCP servers that speak JSON-RPC on stdout
   * never interleave protocol frames with diagnostic output.
   * @param {string} level
   * @param {string} message
   */
  const writeShimLog = (level, message) => {
    process.stderr.write(`[${level}] ${message}\n`);
  };

  class CoreShim {
    get summary() {
      return unavailableCore("core.summary");
    }
    get markdownSummary() {
      return unavailableCore("core.markdownSummary");
    }
    get platform() {
      return unavailableCore("core.platform");
    }
  }

  global.core = Object.assign(new CoreShim(), {
    debug: /** @param {string} message */ message => writeShimLog("debug", message),
    info: /** @param {string} message */ message => writeShimLog("info", message),
    notice: /** @param {string | Error} message */ message => writeShimLog("notice", String(message)),
    warning: /** @param {string | Error} message */ message => writeShimLog("warning", String(message)),
    error: /** @param {string | Error} message */ message => writeShimLog("error", String(message)),
    setFailed: /** @param {string | Error} message */ message => {
      writeShimLog("error", String(message));
      if (typeof process !== "undefined") {
        if (process.exitCode === null || process.exitCode === undefined || process.exitCode === 0) {
          process.exitCode = 1;
        }
      }
    },
    setOutput: /** @param {string} name @param {unknown} value */ (name, value) => {
      writeShimLog("output", `${name}=${value}`);
    },
    setSecret,
    ExitCode: { Success: 0, Failure: 1 },
    exportVariable: () => unavailableCore("core.exportVariable"),
    addPath: () => unavailableCore("core.addPath"),
    getInput: () => unavailableCore("core.getInput"),
    getMultilineInput: () => unavailableCore("core.getMultilineInput"),
    getBooleanInput: () => unavailableCore("core.getBooleanInput"),
    setCommandEcho: () => unavailableCore("core.setCommandEcho"),
    isDebug: () => unavailableCore("core.isDebug"),
    startGroup: () => unavailableCore("core.startGroup"),
    endGroup: () => unavailableCore("core.endGroup"),
    group: () => unavailableCore("core.group"),
    saveState: () => unavailableCore("core.saveState"),
    getState: () => unavailableCore("core.getState"),
    getIDToken: () => unavailableCore("core.getIDToken"),
    toPosixPath: () => unavailableCore("core.toPosixPath"),
    toWin32Path: () => unavailableCore("core.toWin32Path"),
    toPlatformPath: () => unavailableCore("core.toPlatformPath"),
  });
} else if (typeof global.core.setSecret !== "function") {
  global.core.setSecret = setSecret;
}

if (!global.context) {
  // Build a context object from GitHub Actions environment variables,
  // mirroring the shape of @actions/github's Context class.
  /** @type {Record<string, unknown>} */
  let payload = {};
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (eventPath) {
    try {
      const fs = require("fs");
      payload = JSON.parse(fs.readFileSync(eventPath, "utf8"));
    } catch {
      // Ignore errors reading the event payload – it may not be present when
      // the MCP server is started outside of a GitHub Actions runner.
    }
  }

  const repository = process.env.GITHUB_REPOSITORY || "";
  const slashIdx = repository.indexOf("/");
  // When GITHUB_REPOSITORY is absent or lacks a '/' separator, both fields
  // fall back to empty strings so callers can detect the missing value.
  const owner = slashIdx >= 0 ? repository.slice(0, slashIdx) : "";
  const repo = slashIdx >= 0 ? repository.slice(slashIdx + 1) : "";

  class ShimContext {
    eventName = process.env.GITHUB_EVENT_NAME || "";
    sha = process.env.GITHUB_SHA || "";
    ref = process.env.GITHUB_REF || "";
    workflow = process.env.GITHUB_WORKFLOW || "";
    action = process.env.GITHUB_ACTION || "";
    actor = process.env.GITHUB_ACTOR || "";
    job = process.env.GITHUB_JOB || "";
    runNumber = parseInt(process.env.GITHUB_RUN_NUMBER || "0", 10);
    runId = parseInt(process.env.GITHUB_RUN_ID || "0", 10);
    runAttempt = parseInt(process.env.GITHUB_RUN_ATTEMPT || "0", 10);
    apiUrl = process.env.GITHUB_API_URL || "https://api.github.com";
    serverUrl = process.env.GITHUB_SERVER_URL || "https://github.com";
    graphqlUrl = process.env.GITHUB_GRAPHQL_URL || "https://api.github.com/graphql";
    payload = payload;
    repo = { owner, repo };

    get issue() {
      const item = payload.issue || payload.pull_request || payload;
      if (!item || typeof item !== "object" || !("number" in item) || typeof item.number !== "number" || !Number.isSafeInteger(item.number) || item.number < 1) {
        throw new Error("context.issue is unavailable without a valid issue or pull request event");
      }
      return { owner, repo, number: item.number };
    }
  }
  global.context = new ShimContext();
}
