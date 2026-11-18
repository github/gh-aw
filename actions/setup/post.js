// Setup Activation Action - Post Step
// Sends a conclusion span and reclaims only captured invocation-owned state.

const path = require("path");
const fs = require("fs");
const { spawnSync } = require("child_process");

function cleanupRuntimeTmp() {
  if (!process.env.GH_AW_RUNTIME_TMP_DIR) return;
  const cleanupScript = path.join(__dirname, "sh", "cleanup_runtime_tmp.sh");
  const result = spawnSync("bash", [cleanupScript], { stdio: "inherit" });
  if (result.error || result.status !== 0) {
    console.warn(`Warning: unable to run scoped AWF runtime temp cleanup: ${result.error?.message || result.status}`);
  }
}

function isDebugModeEnabled() {
  const toBool = value => {
    const normalized = String(value || "").toLowerCase();
    return normalized === "1" || normalized === "true";
  };
  return toBool(process.env.RUNNER_DEBUG) || toBool(process.env.ACTIONS_STEP_DEBUG);
}

function listTmpGhAwFiles(tmpDir, maxDepth, maxFiles) {
  if (!fs.existsSync(tmpDir)) {
    console.log(`[debug] ${tmpDir} does not exist; skipping file listing`);
    return;
  }

  const files = [];
  let readErrors = 0;

  const walk = (currentDir, depth) => {
    if (depth >= maxDepth || files.length >= maxFiles) {
      return;
    }

    let entries;
    try {
      entries = fs.readdirSync(currentDir, { withFileTypes: true });
    } catch (err) {
      readErrors += 1;
      console.log(`[debug] failed to read ${currentDir}: ${err.message}`);
      return;
    }

    entries.sort((a, b) => a.name.localeCompare(b.name));

    for (const entry of entries) {
      if (files.length >= maxFiles) {
        return;
      }

      const fullPath = path.join(currentDir, entry.name);
      if (entry.isDirectory()) {
        walk(fullPath, depth + 1);
        continue;
      }

      files.push(path.relative(tmpDir, fullPath) || ".");
    }
  };

  walk(tmpDir, 0);

  const truncated = files.length >= maxFiles;
  console.log(`[debug] listing files under ${tmpDir} (max depth ${maxDepth}, max files ${maxFiles})`);
  if (files.length === 0) {
    console.log("[debug] no files found");
  } else {
    for (const file of files) {
      console.log(`[debug] - ${file}`);
    }
  }
  if (truncated) {
    console.log(`[debug] output truncated at ${maxFiles} files`);
  }
  if (readErrors > 0) {
    console.log(`[debug] encountered ${readErrors} directory read error(s)`);
  }
}

// Wait for the OTLP span before completing the post step.
(async () => {
  if (process.platform === "win32") {
    const result = require("./wsl.js").runInWSL("node", __filename);
    if (result.error || result.status !== 0) console.warn(`Warning: private WSL runtime cleanup failed: ${result.error?.message || result.status}`);
    return;
  }
  // Send a gh-aw.<jobName>.conclusion span to the configured OTLP endpoint, if any.
  // Delegates to action_conclusion_otlp.cjs so that script mode (clean.sh) and
  // dev/release mode share the same implementation.  Non-fatal: errors are
  // handled inside sendJobConclusionSpan via console.warn.
  try {
    const setupRoot = process.env.STATE_GH_AW_SETUP_ROOT;
    const setupId = process.env.STATE_GH_AW_SETUP_ID;
    const destination = process.env.STATE_GH_AW_SETUP_DESTINATION;
    if (!setupRoot || !setupId || !destination || path.isAbsolute(destination)) throw new Error("No valid invocation-owned setup state");
    const { validateSetup, stagedModulePath } = require("./js/runtime_isolation.cjs");
    validateSetup(setupRoot, process.env.STATE_GH_AW_RUNTIME_TOKEN, setupId);
    const conclusionScript = stagedModulePath(`${setupRoot}${path.sep}${destination}`, setupRoot, "action_conclusion_otlp.cjs");
    const { run } = require(conclusionScript);
    await run();
  } catch {
    // Non-fatal: silently ignore any OTLP export errors in the post step.
  }

  const tmpDir = process.env.STATE_GH_AW_RUNTIME_ROOT;
  const maxDebugDepth = 4;
  const maxDebugFiles = 200;

  if (tmpDir && isDebugModeEnabled()) {
    try {
      require("./js/runtime_isolation.cjs").validate(tmpDir, process.env.STATE_GH_AW_RUNTIME_TOKEN, process.env.STATE_GH_AW_RUNTIME_ID);
      listTmpGhAwFiles(tmpDir, maxDebugDepth, maxDebugFiles);
    } catch {
      // Never inspect paths whose invocation ownership cannot be established.
    }
  }

  cleanupRuntimeTmp();
  require("./js/runtime_isolation.cjs").cleanup();
})();
