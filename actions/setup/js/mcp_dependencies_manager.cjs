// @ts-check

const { execFileSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { createToolEnvironment } = require("./mcp_handler_process.cjs");

const installedDependencyPromises = new Map();
const perToolInstallPromises = new Map();
let execFileSyncRunner = execFileSync;

/**
 * Keep installer network configuration separate from tool bindings. URL
 * credentials must not leak through otherwise innocuous configuration names.
 * @returns {NodeJS.ProcessEnv}
 */
function createInstallerEnvironment() {
  const env = createToolEnvironment();
  const proxyNames = ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"];
  const registryNames = ["PIP_INDEX_URL", "NPM_CONFIG_REGISTRY", "npm_config_registry"];
  const pathNames = ["NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "PIP_CERT", "NPM_CONFIG_CAFILE", "npm_config_cafile"];
  const names = [...proxyNames, ...registryNames, ...pathNames, "NO_PROXY", "no_proxy", "PIP_EXTRA_INDEX_URL", "GOPROXY"];
  for (const name of names) {
    const value = process.env[name];
    if (value === undefined || value === "") continue;
    let valid = !/[\u0000-\u001f\u007f]/.test(value);
    const protocols = proxyNames.includes(name) ? ["http:", "https:", "socks5:", "socks5h:"] : ["http:", "https:"];
    if (proxyNames.includes(name) || registryNames.includes(name) || name === "PIP_EXTRA_INDEX_URL" || name === "GOPROXY") {
      const urls = name === "GOPROXY" ? value.split(/[,|]/) : name === "PIP_EXTRA_INDEX_URL" ? value.trim().split(/\s+/) : [value];
      valid =
        valid &&
        urls.every(candidate => {
          if (name === "GOPROXY" && ["direct", "off"].includes(candidate)) return true;
          try {
            const url = new URL(candidate);
            return protocols.includes(url.protocol) && Boolean(url.hostname) && !url.username && !url.password && !/[?#]/.test(candidate) && !/\s/.test(candidate);
          } catch {
            return false;
          }
        });
    } else if (name === "NO_PROXY" || name === "no_proxy") {
      valid = valid && /^[a-zA-Z0-9.*,:/[\] _-]+$/.test(value);
    }
    if (!valid) {
      throw new Error(`Unsafe MCP dependency installer configuration in '${name}': use credential-free configuration without URL userinfo, query strings, or fragments`);
    }
    env[name] = value;
  }
  return env;
}

/**
 * Emit dependency-install logs via the provided MCP logger.
 * @param {Object} logger
 * @param {string} level
 * @param {string} message
 */
function logWithCore(logger, level, message) {
  if (logger) {
    const logMethod = typeof logger[level] === "function" ? logger[level] : logger.debug;
    if (typeof logMethod === "function") {
      logMethod(message);
    }
  }
}

function inferDependencyManager(handlerPath) {
  const ext = path.extname(handlerPath).toLowerCase();
  if (ext === ".py") return "pip";
  if (ext === ".go") return "go";
  if (ext === ".sh") return "shell";
  return "npm";
}

function resolveShellPackageManager() {
  const env = createInstallerEnvironment();
  const managers = [
    { command: "apt-get", args: ["install", "-y"] },
    { command: "yum", args: ["install", "-y"] },
    { command: "dnf", args: ["install", "-y"] },
  ];

  for (const manager of managers) {
    try {
      execFileSyncRunner("which", [manager.command], { stdio: "pipe", env });
      return manager;
    } catch {
      // Manager not installed — ignored, try next manager.
    }
  }

  return null;
}

function isTransientInstallFailure(message) {
  return /(timed out|timeout|temporary|network|econnreset|econnrefused|eai_again|etimedout|429|502|503|504)/i.test(message);
}

function isDeterministicInstallFailure(message) {
  return /(not found|no matching distribution|unable to locate package|invalid requirement|permission denied|forbidden|unauthorized|unknown revision|invalid version)/i.test(message);
}

function executeInstallWithRetry(logger, toolName, dependency, command, args, cwd) {
  const env = createInstallerEnvironment();
  const maxRetries = 2;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      logWithCore(logger, "debug", `  [${toolName}] Installing dependency '${dependency}' with: ${command} ${args.join(" ")}`);
      execFileSyncRunner(command, args, {
        cwd,
        stdio: "pipe",
        env,
      });
      logWithCore(logger, "info", `  [${toolName}] Installed dependency '${dependency}'`);
      return;
    } catch (error) {
      const stderr = error && error.stderr ? String(error.stderr) : "";
      const stdout = error && error.stdout ? String(error.stdout) : "";
      const details = [stderr.trim(), stdout.trim(), error ? getErrorMessage(error) : ""].filter(Boolean).join("\n");

      if (isDeterministicInstallFailure(details)) {
        logWithCore(logger, "error", `  [${toolName}] Deterministic dependency install failure for '${dependency}'`);
        throw new Error(`Dependency installation failed for '${dependency}': ${details || "deterministic failure"}`);
      }

      if (!isTransientInstallFailure(details) || attempt === maxRetries) {
        logWithCore(logger, "error", `  [${toolName}] Dependency install failed after ${attempt + 1} attempt(s) for '${dependency}'`);
        throw new Error(`Dependency installation failed for '${dependency}' after ${attempt + 1} attempt(s): ${details || "unknown error"}`);
      }

      logWithCore(logger, "warning", `  [${toolName}] Transient dependency install failure for '${dependency}', retrying (${attempt + 1}/${maxRetries})`);
    }
  }
}

function installDependency(logger, toolName, dependency, manager, basePath) {
  let command = "";
  let args = [];
  let cwd = basePath;

  if (manager === "npm") {
    command = "npm";
    args = ["install", "--ignore-scripts", "--no-save", "--", dependency];
  } else if (manager === "pip") {
    command = "python3";
    args = ["-m", "pip", "install", "--disable-pip-version-check", dependency];
  } else if (manager === "go") {
    const goModPath = path.join(basePath, "go.mod");
    if (!fs.existsSync(goModPath)) {
      try {
        execFileSyncRunner("go", ["mod", "init", "example.com/mcp-scripts"], { cwd: basePath, stdio: "pipe", env: createInstallerEnvironment() });
      } catch {
        // go.mod may have been created concurrently — failure is ignored.
      }
    }
    command = "go";
    args = ["get", dependency];
  } else if (manager === "shell") {
    const shellPM = resolveShellPackageManager();
    if (!shellPM) {
      throw new Error(`Dependency installation failed for '${dependency}': no supported system package manager found (expected apt-get, yum, or dnf)`);
    }
    command = shellPM.command;
    args = [...shellPM.args, dependency];
    cwd = process.cwd();
  } else {
    return;
  }

  executeInstallWithRetry(logger, toolName, dependency, command, args, cwd);
}

function createDependencyInstallGate(logger, toolName, handlerPath, dependencies, basePath) {
  const depList = Array.isArray(dependencies) ? dependencies.filter(dep => typeof dep === "string" && dep.trim() !== "") : [];
  if (depList.length === 0) {
    return async () => {};
  }

  const manager = inferDependencyManager(handlerPath);
  const toolKey = `${toolName}:${handlerPath}`;

  return async () => {
    if (perToolInstallPromises.has(toolKey)) {
      logWithCore(logger, "debug", `  [${toolName}] Reusing dependency install gate for ${toolKey}`);
      return perToolInstallPromises.get(toolKey);
    }

    const installPromise = (async () => {
      logWithCore(logger, "debug", `  [${toolName}] Starting dependency install gate (${depList.length} dependency item(s))`);
      for (const dependency of depList) {
        const key = `${manager}:${dependency}`;
        if (!installedDependencyPromises.has(key)) {
          logWithCore(logger, "debug", `  [${toolName}] No existing install promise for '${dependency}', creating one`);
          installedDependencyPromises.set(
            key,
            Promise.resolve().then(() => installDependency(logger, toolName, dependency, manager, basePath))
          );
        } else {
          logWithCore(logger, "debug", `  [${toolName}] Reusing existing install promise for '${dependency}'`);
        }
        await installedDependencyPromises.get(key);
      }
      logWithCore(logger, "debug", `  [${toolName}] Dependency install gate completed`);
    })();

    perToolInstallPromises.set(toolKey, installPromise);
    return installPromise;
  };
}

function resetDependencyInstallStateForTests() {
  installedDependencyPromises.clear();
  perToolInstallPromises.clear();
}

function setExecFileSyncRunnerForTests(runner) {
  execFileSyncRunner = runner || execFileSync;
}

module.exports = {
  createInstallerEnvironment,
  createDependencyInstallGate,
  inferDependencyManager,
  isTransientInstallFailure,
  isDeterministicInstallFailure,
  resetDependencyInstallStateForTests,
  setExecFileSyncRunnerForTests,
};
