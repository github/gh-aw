// Setup Activation Action - Post Step
// Reclaim only the allocation recorded by this exact setup action invocation.
const path = require("path");
const fs = require("fs");
const { spawnSync } = require("child_process");

for (const suffix of ["DIR", "ID", "DEV", "INO", "UID", "OWNER"]) {
  const key = `GH_AW_RUNTIME_${suffix}`;
  if (process.env[`STATE_${key}`]) process.env[key] = process.env[`STATE_${key}`];
  else delete process.env[key];
}

function runCleanup(script, args = []) {
  const result = spawnSync("bash", [path.join(__dirname, "sh", script), ...args], { stdio: "inherit" });
  if (result.error || result.status !== 0) {
    console.warn(`Warning: ${script} cleanup failed: ${result.error?.message || result.status}`);
    return false;
  }
  return true;
}

function runWSLServiceCleanup() {
  const runtimeDir = process.env.GH_AW_RUNTIME_DIR;
  const runtimeId = process.env.GH_AW_RUNTIME_ID;
  const runtimeUid = process.env.GH_AW_RUNTIME_UID;
  if (process.platform !== "win32" || !runtimeDir || !runtimeId || !runtimeUid || !fs.existsSync(path.join(runtimeDir, ".gh-aw-processes.jsonl"))) return;

  const script = path.join(__dirname, "sh", "runtime_lifecycle.sh");
  const toWSL = value => {
    const result = spawnSync("wsl.exe", ["--distribution", "Ubuntu", "--user", "root", "--exec", "wslpath", "-u", value], { encoding: "utf8", windowsHide: true, timeout: 10000, maxBuffer: 16384 });
    if (result.error || result.status !== 0) throw new Error(result.error?.message || "wslpath failed");
    return result.stdout.trim();
  };
  try {
    const result = spawnSync(
      "wsl.exe",
      [
        "--distribution",
        "Ubuntu",
        "--user",
        "root",
        "--exec",
        "env",
        `GH_AW_RUNTIME_DIR=${toWSL(runtimeDir)}`,
        `GH_AW_RUNTIME_ID=${runtimeId}`,
        `GH_AW_RUNTIME_UID=${runtimeUid}`,
        "GH_AW_WSL_CLEANUP=true",
        "bash",
        toWSL(script),
        "stop-services",
      ],
      { stdio: "inherit", windowsHide: true, timeout: 30000 }
    );
    if (result.error || result.status !== 0) {
      console.warn(`Warning: WSL service cleanup failed: ${result.error?.message || result.status}`);
    }
  } catch (error) {
    console.warn(`Warning: WSL service cleanup failed: ${error.message}`);
  }
}

(async () => {
  try {
    const { run } = require(path.join(__dirname, "js", "action_conclusion_otlp.cjs"));
    await run();
  } catch {
    // Telemetry must not prevent reclamation, including after setup fails.
  }
  if (process.platform !== "win32" || runCleanup("runtime_lifecycle.sh", ["validate"])) {
    runWSLServiceCleanup();
  }
  runCleanup("runtime_lifecycle.sh", ["cleanup"]);
  // AWF scratch is nested in the validated root. Never follow ambient scratch
  // variables, which may belong to a later setup action in the same job.
})();
