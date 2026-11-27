// Host-side only. The sandbox sees this directory through the read-only
// runner-temp mount, including on ARC where the runner's own Node is not visible.
const fs = require("node:fs");
const path = require("node:path");

const engine = process.argv[2];
if (!["copilot", "claude", "codex"].includes(engine)) throw new Error("Unsupported detection engine");
if (!process.env.RUNNER_TEMP) throw new Error("RUNNER_TEMP must be set");
const root = path.join(process.env.RUNNER_TEMP, "gh-aw");
const runtime = path.join(root, "threat-detection-runtime");
fs.rmSync(runtime, { recursive: true, force: true });
fs.mkdirSync(runtime);
const command = path.join(runtime, "node");
fs.copyFileSync(process.execPath, command);
fs.chmodSync(command, 0o755);
const nativeModel = { copilot: process.env.COPILOT_MODEL, claude: process.env.ANTHROPIC_MODEL };
const launch = {
  command,
  args: [path.join(root, "actions", `${engine}_harness.cjs`), engine],
  convention: engine,
  model: process.env[`GH_AW_MODEL_DETECTION_${engine.toUpperCase()}`]?.trim() || nativeModel[engine],
};
fs.writeFileSync(path.join(runtime, "launch.json"), JSON.stringify(launch), { flag: "wx" });
