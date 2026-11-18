const { spawnSync } = require("node:child_process");

// Keep Linux PATH/HOME intact while forwarding job and action state to WSL.
function runInWSL(command, script, args = [], overrides = {}) {
  const env = { ...process.env, ...overrides };
  const pathVariables = new Set([
    "RUNNER_TEMP",
    "GITHUB_ENV",
    "GITHUB_STATE",
    "GITHUB_OUTPUT",
    "GITHUB_STEP_SUMMARY",
    "GITHUB_EVENT_PATH",
    "GITHUB_WORKSPACE",
    "GITHUB_ACTION_PATH",
    "INPUT_DESTINATION",
    "GH_AW_SETUP_ROOT",
    "STATE_GH_AW_SETUP_ROOT",
  ]);
  const variables = Object.keys(env)
    .filter(name => /^(?:GITHUB_|GH_|INPUT_|STATE_|RUNNER_|ACTIONS_|OTEL_|SECRET_|DEBUG|SETUP_START_MS)/.test(name) && /^[A-Za-z_][A-Za-z0-9_]*$/.test(name))
    .map(name => `${name}${pathVariables.has(name) ? "/p" : ""}`);
  env.WSLENV = [...(env.WSLENV || "").split(":").filter(Boolean), ...variables].join(":");
  const distribution = ["--distribution", "Ubuntu", "--user", "root", "--exec"];
  const converted = spawnSync("wsl.exe", [...distribution, "wslpath", "-u", script], { env, encoding: "utf8" });
  if (converted.error || converted.status !== 0) return { error: converted.error || new Error("Unable to resolve setup path in WSL"), status: 1 };
  return spawnSync("wsl.exe", [...distribution, command, converted.stdout.trim(), ...args], { env, stdio: "inherit" });
}

module.exports = { runInWSL };
