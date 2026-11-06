// Setup Activation Action - Post Step
// Sends an OTLP conclusion span for the job. Shared legacy runtime paths have
// no per-invocation ownership record and must not be reclaimed by this step.

const path = require("path");
const fs = require("fs");

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
  // Send a gh-aw.<jobName>.conclusion span to the configured OTLP endpoint, if any.
  // Delegates to action_conclusion_otlp.cjs so that script mode (clean.sh) and
  // dev/release mode share the same implementation.  Non-fatal: errors are
  // handled inside sendJobConclusionSpan via console.warn.
  try {
    const { run } = require(path.join(__dirname, "js", "action_conclusion_otlp.cjs"));
    await run();
  } catch {
    // Non-fatal: silently ignore any OTLP export errors in the post step.
  }

  const tmpDir = "/tmp/gh-aw";
  const maxDebugDepth = 4;
  const maxDebugFiles = 200;

  if (isDebugModeEnabled()) {
    listTmpGhAwFiles(tmpDir, maxDebugDepth, maxDebugFiles);
  }

  if (fs.existsSync(tmpDir)) {
    console.warn(`Warning: leaving shared ${tmpDir} untouched; per-invocation ownership cannot be established.`);
  }
})();
