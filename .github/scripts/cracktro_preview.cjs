const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");

function startCracktroPreview() {
  const runnerTemp = process.env.RUNNER_TEMP;
  if (!runnerTemp) throw new Error("RUNNER_TEMP is required for Cracktro screenshot staging");
  fs.mkdirSync(path.join(runnerTemp, "gh-aw", "safeoutputs", "upload-artifacts", "cracktro-review"), { recursive: true });
  const server = spawn("python3", ["-m", "http.server", "4329", "--bind", "127.0.0.1", "--directory", "docs/public"], {
    cwd: path.resolve(__dirname, "../.."),
    stdio: "inherit",
    shell: false,
  });
  server.once("error", error => {
    console.error(`Cracktro preview failed to start: ${error.message}`);
    process.exitCode = 1;
  });
  server.once("exit", (code, signal) => {
    if (signal && signal !== "SIGTERM" && signal !== "SIGINT") {
      console.error(`Cracktro preview terminated by ${signal}`);
    }
    process.exitCode = code ?? (signal === "SIGTERM" || signal === "SIGINT" ? 0 : 1);
  });
  for (const signal of ["SIGTERM", "SIGINT"]) {
    process.once(signal, () => server.kill(signal));
  }
  return server;
}

module.exports = { startCracktroPreview };

if (require.main === module) {
  if (process.argv.length !== 2) throw new Error("Cracktro preview accepts no arguments");
  startCracktroPreview();
}
