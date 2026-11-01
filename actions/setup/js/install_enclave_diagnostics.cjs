const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

function installEnclaveDiagnostics() {
  if (process.getuid() === 0) {
    throw new Error("The enclave diagnostic bootstrap must run rootless");
  }
  if (!process.env.RUNNER_TEMP || !process.env.DIAGNOSTICS_STAGING) {
    throw new Error("Runner diagnostic staging is required");
  }
  if (!["x64", "arm64"].includes(process.arch)) {
    throw new Error("Unsupported collector Node architecture");
  }
  const runnerTemp = path.resolve(process.env.RUNNER_TEMP);
  const directory = path.resolve(process.env.DIAGNOSTICS_STAGING);
  if (path.dirname(directory) !== runnerTemp) {
    throw new Error("Collector staging must be a direct child of RUNNER_TEMP");
  }
  try {
    fs.mkdirSync(directory, { mode: 0o700 });
  } catch (cause) {
    throw new Error("Collector installation staging could not be created", { cause });
  }
  const download = (url, destination, limit) => {
    try {
      execFileSync("/usr/bin/curl", ["--proto", "=https", "--proto-redir", "=https", "-fsSL", "--max-time", "120", "--max-filesize", String(limit), url, "-o", destination], { stdio: "inherit", timeout: 130000 });
    } catch (cause) {
      throw new Error("Trusted collector download failed", { cause });
    }
  };
  download(`https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-${process.arch}.tar.xz`, path.join(directory, "node.tar.xz"), 128 * 1024 * 1024);
  download("https://raw.githubusercontent.com/github/gh-aw-firewall/3929c9a15c0f239b772affcaae6e55fec92f41c2/scripts/ci/collect-enclave-smoke-debug.cjs", path.join(directory, "collector.cjs"), 1024 * 1024);
}

module.exports = { installEnclaveDiagnostics };

if (require.main === module) {
  try {
    installEnclaveDiagnostics();
  } catch {
    console.error("::error::Trusted enclave collector bootstrap failed");
    process.exitCode = 1;
  }
}
