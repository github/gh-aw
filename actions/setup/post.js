// Setup Activation Action - Post Step
// Reclaim only the allocation recorded by this exact setup action invocation.
const path = require("path");
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
  }
}

(async () => {
  try {
    const { run } = require(path.join(__dirname, "js", "action_conclusion_otlp.cjs"));
    await run();
  } catch {
    // Telemetry must not prevent reclamation, including after setup fails.
  }
  runCleanup("runtime_lifecycle.sh", ["cleanup"]);
  // AWF scratch is nested in the validated root. Never follow ambient scratch
  // variables, which may belong to a later setup action in the same job.
})();
