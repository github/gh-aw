const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");

function screenshotDirectory() {
  const runnerTemp = process.env.RUNNER_TEMP;
  if (!runnerTemp) throw new Error("RUNNER_TEMP is required for Cracktro screenshot staging");
  return path.join(runnerTemp, "gh-aw", "safeoutputs", "upload-artifacts", "cracktro-review");
}

function startCracktroPreview() {
  fs.mkdirSync(screenshotDirectory(), { recursive: true });
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
  const deadline = setTimeout(() => server.kill("SIGTERM"), 10 * 60 * 1000);
  deadline.unref();
  server.once("close", () => clearTimeout(deadline));
  return server;
}

module.exports = { startCracktroPreview };

if (require.main === module) {
  if (process.argv.length === 2) {
    startCracktroPreview();
  } else if (process.argv.length === 3 && process.argv[2] === "background") {
    const directory = screenshotDirectory();
    const launcher = spawn(process.execPath, [__filename], {
      detached: true,
      stdio: "ignore",
      shell: false,
    });
    launcher.once("spawn", () => {
      console.log(`Cracktro preview launcher PID: ${launcher.pid}`);
      console.log(`Cracktro screenshot directory: ${directory}`);
      launcher.unref();
    });
    launcher.once("error", error => {
      console.error(`Cracktro preview launcher failed: ${error.message}`);
      process.exitCode = 1;
    });
  } else {
    throw new Error("Cracktro preview accepts only the optional background argument");
  }
}
