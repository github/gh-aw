// Setup Activation Action - Post Step
// Sends an OTLP conclusion span for the job, then removes the /tmp/gh-aw/
// directory created during the main action step.
// Runs in the post-job phase so that temporary files are erased after the
// workflow job completes, regardless of success or failure.
//
// Files inside /tmp/gh-aw/ may be owned by root (written by Docker containers
// or privileged scripts), so we use `sudo rm -rf` — GitHub-hosted runners have
// passwordless sudo.  We fall back to fs.rmSync for self-hosted runners that
// don't have sudo but do have direct write access.

const path = require("path");
const { spawnSync } = require("child_process");
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

// Wrap everything in an async IIFE so that the OTLP span is fully sent before
// the cleanup deletes /tmp/gh-aw/ (which contains aw_info.json and otel.jsonl).
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

  console.log(`Cleaning up ${tmpDir}...`);

  // Try sudo rm -rf first (handles root-owned files on GitHub-hosted runners)
  let result;
  if (process.platform === "win32" && process.env.GH_AW_WINDOWS_WSL === "true") {
    try {
      require("./js/windows_wsl.cjs").cleanupWindowsRuntime();
      result = { status: 0 };
    } catch (error) {
      console.error(`Warning: failed to clean up WSL runtime: ${error.message}`);
      result = { status: 1 };
    }
  } else {
    result = spawnSync("sudo", ["rm", "-rf", tmpDir], { stdio: "inherit" });
  }

  if (result.status === 0) {
    console.log(`Cleaned up ${tmpDir}`);
  } else {
    // Fall back to fs.rmSync for environments without sudo
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
      console.log(`Cleaned up ${tmpDir}`);
    } catch (err) {
      // Log but do not fail — cleanup is best-effort
      console.error(`Warning: failed to clean up ${tmpDir}: ${err.message}`);
    }
  }

  // Clean up AWF chroot directories under /tmp (e.g. /tmp/awf-*-chroot-home
  // and /tmp/awf-chroot-*). These are created by AWF when running with
  // --enable-host-access on GitHub-hosted runners. Files inside may be owned
  // by root (written by Docker containers or privileged AWF processes),
  // causing EACCES failures if cleanup is attempted without sudo.
  for (const pattern of ["awf-*-chroot-home", "awf-chroot-*"]) {
    const awfChrootFindResult = spawnSync("sudo", ["find", "/tmp", "-maxdepth", "1", "-name", pattern, "-type", "d", "-print"], { encoding: "utf8" });
    if (awfChrootFindResult.status !== 0) {
      console.log(`Failed to inspect /tmp/${pattern} directories`);
      continue;
    }

    const awfChrootDirs = awfChrootFindResult.stdout
      .split("\n")
      .map(line => line.trim())
      .filter(Boolean);
    if (awfChrootDirs.length === 0) {
      console.log(`No /tmp/${pattern} directories found`);
      continue;
    }

    const awfChrootCleanupResult = spawnSync("sudo", ["find", "/tmp", "-maxdepth", "1", "-name", pattern, "-type", "d", "-exec", "rm", "-rf", "--", "{}", "+"], { stdio: "inherit" });
    if (awfChrootCleanupResult.status === 0) {
      const awfChrootNoun = awfChrootDirs.length === 1 ? "directory" : "directories";
      console.log(`Cleaned up ${awfChrootDirs.length} /tmp/${pattern} ${awfChrootNoun}`);
    } else {
      console.log(`Failed to clean /tmp/${pattern} directories`);
    }
  }
})();
