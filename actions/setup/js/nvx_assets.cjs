// @ts-check
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync, spawn } = require("node:child_process");
const { resolveCopilotSDKMCPConfigPath } = require("./copilot_sdk_mcp_config_path.cjs");

const GUEST_ROOT = "/workspace/.gh-aw-nvx";
const STATE_INPUTS = ["aw-prompts", "mcp-config", "agent", "threat-detection", ".pi", "pi-agent-dir", "models.json", "github-event.json", "awf-config.json", "engine-command.sh"];
const STATE_OUTPUT_DIRECTORIES = ["agent", "threat-detection", "sandbox/agent/logs"];
const STATE_OUTPUT_FILES = ["pi-streaming.jsonl", "claude-debug.log", "agent-step-summary.md", "step-summary.md"];
const SAFEOUTPUTS_OUTPUT_FILES = ["outputs.jsonl"];
const SAFEOUTPUTS_OUTPUT_DIRECTORIES = ["upload-artifacts"];

/** @param {string} source @param {string} destination */
function copyTree(source, destination) {
  try {
    if (fs.lstatSync(source).isSymbolicLink()) throw new Error(`NVX asset source must not be a symlink: ${source}`);
    assertContainedLinks(source, fs.realpathSync(source));
    fs.cpSync(source, destination, { recursive: true, verbatimSymlinks: true });
  } catch (error) {
    throw new Error(`Failed to export NVX asset ${source}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
}

/** @param {string} entry @param {string} root */
function assertContainedLinks(entry, root) {
  try {
    const stat = fs.lstatSync(entry);
    if (stat.isSymbolicLink()) {
      const target = fs.realpathSync(entry);
      if (target !== root && !target.startsWith(`${root}${path.sep}`)) throw new Error(`NVX asset symlink escapes its source: ${entry}`);
      if (path.isAbsolute(fs.readlinkSync(entry))) throw new Error(`NVX asset symlink must be relative: ${entry}`);
    } else if (stat.isDirectory()) {
      for (const name of fs.readdirSync(entry)) assertContainedLinks(path.join(entry, name), root);
    } else if (!stat.isFile()) {
      throw new Error(`NVX asset must be a regular file or directory: ${entry}`);
    }
  } catch (error) {
    throw new Error(`Invalid NVX asset ${entry}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
}

/** @param {string} file @returns {string | undefined} */
function readELFInterpreter(file) {
  const descriptor = fs.openSync(file, "r");
  try {
    const header = Buffer.alloc(64);
    if (fs.readSync(descriptor, header, 0, 64, 0) !== 64 || !header.subarray(0, 4).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46]))) return undefined;
    if (header[5] !== 1 || (header[4] !== 1 && header[4] !== 2)) throw new Error(`Unsupported NVX ELF format: ${file}`);
    const is64 = header[4] === 2;
    const offset = is64 ? Number(header.readBigUInt64LE(32)) : header.readUInt32LE(28);
    const size = header.readUInt16LE(is64 ? 54 : 42);
    const count = header.readUInt16LE(is64 ? 56 : 44);
    if (!Number.isSafeInteger(offset) || size < (is64 ? 56 : 32) || count > 1024) throw new Error(`Invalid NVX ELF program headers: ${file}`);
    const programHeader = Buffer.alloc(size);
    for (let index = 0; index < count; index++) {
      if (fs.readSync(descriptor, programHeader, 0, size, offset + index * size) !== size) throw new Error(`Truncated NVX ELF program header: ${file}`);
      if (programHeader.readUInt32LE(0) !== 3) continue;
      const interpreterOffset = is64 ? Number(programHeader.readBigUInt64LE(8)) : programHeader.readUInt32LE(4);
      const interpreterSize = is64 ? Number(programHeader.readBigUInt64LE(32)) : programHeader.readUInt32LE(16);
      if (!Number.isSafeInteger(interpreterOffset) || interpreterSize < 2 || interpreterSize > 4096) throw new Error(`Invalid NVX ELF interpreter: ${file}`);
      const interpreter = Buffer.alloc(interpreterSize);
      if (fs.readSync(descriptor, interpreter, 0, interpreterSize, interpreterOffset) !== interpreterSize) throw new Error(`Truncated NVX ELF interpreter: ${file}`);
      const result = interpreter.toString("utf8").replace(/\0$/, "");
      if (!path.isAbsolute(result) || result.includes("\0")) throw new Error(`Invalid NVX ELF interpreter path: ${file}`);
      return result;
    }
    return undefined;
  } catch (error) {
    throw new Error(`Failed to inspect NVX executable ${file}`, { cause: error });
  } finally {
    fs.closeSync(descriptor);
  }
}

/** @param {string} entry @param {string} runtime @param {string} guestRuntime */
function stageExecutableLibraries(entry, runtime, guestRuntime) {
  try {
    const stat = fs.lstatSync(entry);
    if (stat.isSymbolicLink()) return;
    if (stat.isDirectory()) {
      for (const name of fs.readdirSync(entry)) stageExecutableLibraries(path.join(entry, name), runtime, guestRuntime);
      return;
    }
    if (!stat.isFile() || (stat.mode & 0o111) === 0) return;
    const interpreter = readELFInterpreter(entry);
    if (!interpreter) return;
    // Musl engine artifacts are selected explicitly for musl guests and must
    // retain the guest's loader instead of being rewritten with host glibc.
    if (path.basename(interpreter).startsWith("ld-musl-")) return;
    const output = execFileSync("/usr/bin/ldd", [entry], { encoding: "utf8", timeout: 15_000, env: { PATH: "/usr/bin:/bin", LC_ALL: "C" } });
    if (/=>\s+not found/.test(output)) throw new Error(`NVX executable has missing host libraries: ${entry}`);
    const libraries = [interpreter, ...Array.from(output.matchAll(/^\s*(?:\S+\s+=>\s+)?(\/.+?)\s+\(0x[0-9a-f]+\)/gm), match => match[1])];
    const libDir = path.join(runtime, "lib");
    fs.mkdirSync(libDir, { recursive: true });
    for (const source of libraries) {
      const destination = path.join(libDir, path.basename(source));
      if (fs.existsSync(destination)) {
        if (!fs.readFileSync(source).equals(fs.readFileSync(destination))) throw new Error(`Conflicting NVX runtime library: ${source}`);
      } else {
        fs.copyFileSync(source, destination);
        fs.chmodSync(destination, fs.statSync(source).mode & 0o777);
      }
    }
    // Relocate the ELF itself so Node/Bun forks retain a runnable execPath.
    const originalRPath = execFileSync("/usr/bin/patchelf", ["--print-rpath", entry], { encoding: "utf8", timeout: 15_000 }).trim();
    const guestLibDir = path.join(guestRuntime, "lib");
    // patchelf 0.18 corrupts some Node binaries if both sections grow together,
    // or if the interpreter is expanded before RPATH.
    execFileSync("/usr/bin/patchelf", ["--force-rpath", "--set-rpath", [guestLibDir, originalRPath].filter(Boolean).join(":"), entry], { timeout: 15_000 });
    execFileSync("/usr/bin/patchelf", ["--set-interpreter", path.join(guestLibDir, path.basename(interpreter)), entry], { timeout: 15_000 });
  } catch (error) {
    throw new Error(`Failed to export NVX executable libraries for ${entry}`, { cause: error });
  }
}

/** @param {NodeJS.ProcessEnv} environment @param {string} stateRoot @param {string} guestRoot */
function stage(environment = process.env, stateRoot = "/tmp/gh-aw", guestRoot = GUEST_ROOT) {
  try {
    const workspace = environment.GITHUB_WORKSPACE;
    const runnerTemp = environment.RUNNER_TEMP;
    if (!workspace || !runnerTemp) throw new Error("NVX asset staging requires GITHUB_WORKSPACE and RUNNER_TEMP");
    const hostRoot = path.join(runnerTemp, "gh-aw");
    const root = path.join(workspace, ".gh-aw-nvx");
    // Exclusive creation prevents a checkout-controlled directory or symlink
    // from redirecting compiler asset writes.
    fs.mkdirSync(root, { mode: 0o700 });
    const identity = fs.lstatSync(root);
    fs.writeFileSync(path.join(hostRoot, "nvx-assets.json"), JSON.stringify({ root, dev: identity.dev, ino: identity.ino }), { mode: 0o600, flag: "wx" });
    const runtime = path.join(root, "gh-aw");
    fs.mkdirSync(runtime);
    const state = path.join(root, "state");
    fs.mkdirSync(state);
    fs.mkdirSync(path.join(runtime, "safeoutputs"));
    for (const name of ["actions", "bin", "engine-cli", "mcp-cli"]) {
      const source = path.join(hostRoot, name);
      if (fs.existsSync(source)) copyTree(source, path.join(runtime, name));
    }
    const guestCopilot = path.join(hostRoot, "nvx-engine", "copilot");
    if (fs.existsSync(guestCopilot)) {
      copyTree(guestCopilot, path.join(runtime, "bin", "copilot"));
      fs.chmodSync(path.join(runtime, "bin", "copilot"), 0o755);
      fs.unlinkSync(guestCopilot);
      fs.rmdirSync(path.dirname(guestCopilot));
    }
    const mcpConfigDir = path.join(hostRoot, "mcp-config");
    const mcpConfigs = [environment.GH_AW_MCP_CONFIG, resolveCopilotSDKMCPConfigPath(runnerTemp)];
    for (const mcpConfig of mcpConfigs) {
      if (mcpConfig && path.dirname(mcpConfig) === mcpConfigDir && fs.existsSync(mcpConfig)) {
        if (!fs.lstatSync(mcpConfigDir).isDirectory()) throw new Error("NVX native MCP config directory must not be a symlink");
        fs.mkdirSync(path.join(runtime, "mcp-config"), { recursive: true });
        copyTree(mcpConfig, path.join(runtime, "mcp-config", path.basename(mcpConfig)));
      }
    }
    if (!fs.existsSync(path.join(runtime, "actions", "nvx_assets.cjs"))) throw new Error("NVX guest bootstrap is missing");
    fs.mkdirSync(path.join(runtime, "bin"), { recursive: true });
    fs.copyFileSync(process.execPath, path.join(runtime, "bin", "node"));
    fs.chmodSync(path.join(runtime, "bin", "node"), 0o755);
    if (process.platform === "linux") {
      const guestRuntime = path.join(guestRoot, "gh-aw");
      stageExecutableLibraries(path.join(runtime, "bin"), runtime, guestRuntime);
      if (fs.existsSync(path.join(runtime, "engine-cli"))) stageExecutableLibraries(path.join(runtime, "engine-cli"), runtime, guestRuntime);
    }
    for (const name of STATE_INPUTS) {
      const source = path.join(stateRoot, name);
      if (fs.existsSync(source)) copyTree(source, path.join(state, name));
    }
  } catch (error) {
    throw new Error(`Failed to stage NVX runtime assets: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
}

/** @param {NodeJS.ProcessEnv} environment @param {string} [guestRoot] */
function guestEnvironment(environment, guestRoot = GUEST_ROOT) {
  const runtime = `${guestRoot}/gh-aw`;
  const replacements = [
    [environment.GH_AW_NVX_HOST_ROOT, runtime],
    [environment.GH_AW_NVX_HOST_WORKSPACE, "/workspace"],
    [environment.GH_AW_NVX_HOST_HOME, environment.HOME],
  ];
  const result = { ...environment };
  for (const [name, value] of Object.entries(result)) {
    if (!value) continue;
    for (const [source, target] of replacements) {
      if (source && target && (value === source || value.startsWith(`${source}/`))) {
        result[name] = target + value.slice(source.length);
        break;
      }
    }
  }
  result.RUNNER_TEMP = guestRoot;
  result.GH_AW_NODE_BIN = `${runtime}/bin/node`;
  result.GH_AW_MODELS_JSON_PATH = "/tmp/gh-aw/models.json";
  result.GH_AW_PI_PACKAGE_ROOT = `${runtime}/engine-cli/node_modules/@earendil-works/pi-coding-agent`;
  result.NODE_PATH = `${runtime}/engine-cli/node_modules${result.NODE_PATH ? `:${result.NODE_PATH}` : ""}`;
  result.PATH = `${runtime}/bin:${runtime}/engine-cli/bin:${runtime}/mcp-cli/bin:${result.PATH || "/usr/local/bin:/usr/bin:/bin"}`;
  for (const name of ["GH_AW_NVX_HOST_ROOT", "GH_AW_NVX_HOST_WORKSPACE", "GH_AW_NVX_HOST_HOME"]) delete result[name];
  return result;
}

/** @param {string} command @param {string} guestRoot @param {string} stateLink */
function guest(command, guestRoot = GUEST_ROOT, stateLink = "/tmp/gh-aw") {
  if (!command) throw new Error("NVX guest command is missing");
  if (fs.existsSync(stateLink)) fs.rmdirSync(stateLink);
  fs.symlinkSync(`${guestRoot}/state`, stateLink);
  const child = spawn("/bin/bash", ["-c", command], { env: guestEnvironment(process.env, guestRoot), stdio: "inherit" });
  child.once("error", error => {
    console.error(new Error("Failed to start NVX guest command", { cause: error }));
    process.exitCode = 1;
  });
  child.once("exit", (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    else process.exitCode = code ?? 1;
  });
}

/** @param {string} source @param {string} destination */
function collectTree(source, destination) {
  try {
    const stat = fs.lstatSync(source);
    const destinationStat = lstatIfExists(destination);
    if (stat.isDirectory()) {
      if (destinationStat && !destinationStat.isDirectory()) throw new Error(`Unsafe NVX output destination: ${destination}`);
      fs.mkdirSync(destination, { recursive: true });
      for (const name of fs.readdirSync(source)) collectTree(path.join(source, name), path.join(destination, name));
    } else if (stat.isFile()) {
      if (destinationStat && !destinationStat.isFile()) throw new Error(`Unsafe NVX output destination: ${destination}`);
      fs.copyFileSync(source, destination);
    } else {
      throw new Error(`NVX output must be a regular file or directory: ${source}`);
    }
  } catch (error) {
    throw new Error(`Failed to collect NVX output ${source}: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
}

/** @param {string} source @param {string} destination @param {string} name @param {boolean} directory */
function collectAllowedOutput(source, destination, name, directory) {
  for (const root of [source, destination]) {
    const stat = lstatIfExists(root);
    if (stat && !stat.isDirectory()) throw new Error(`NVX output root must be a directory: ${root}`);
  }
  const parts = name.split("/");
  for (let index = 0; index < parts.length; index++) {
    source = path.join(source, parts[index]);
    destination = path.join(destination, parts[index]);
    const stat = lstatIfExists(source);
    if (!stat) return;
    if (index < parts.length - 1) {
      const destinationStat = lstatIfExists(destination);
      if (!stat.isDirectory() || (destinationStat && !destinationStat.isDirectory())) throw new Error(`NVX output parent must be a directory: ${source}`);
    } else {
      if (directory ? !stat.isDirectory() : !stat.isFile()) throw new Error(`NVX output must be a ${directory ? "directory" : "regular file"}: ${source}`);
      collectTree(source, destination);
    }
  }
}

/** @param {string} entry */
function lstatIfExists(entry) {
  try {
    return fs.lstatSync(entry);
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return undefined;
    throw new Error(`Failed to inspect NVX output destination ${entry}`, { cause: error });
  }
}

/** @param {string} entry */
function removeAssets(entry) {
  try {
    const stat = fs.lstatSync(entry);
    if (stat.isDirectory() && !stat.isSymbolicLink()) {
      fs.chmodSync(entry, 0o700);
      for (const name of fs.readdirSync(entry)) removeAssets(path.join(entry, name));
      fs.rmdirSync(entry);
    } else {
      fs.unlinkSync(entry);
    }
  } catch (error) {
    throw new Error(`Failed to remove NVX asset ${entry}`, { cause: error });
  }
}

/** @param {NodeJS.ProcessEnv} environment @param {string} stateRoot */
function collect(environment = process.env, stateRoot = "/tmp/gh-aw") {
  try {
    if (!environment.RUNNER_TEMP || !environment.GITHUB_WORKSPACE) throw new Error("NVX output collection requires runner paths");
    const marker = path.join(environment.RUNNER_TEMP, "gh-aw", "nvx-assets.json");
    if (!fs.existsSync(marker)) return; // Staging never started.
    const { root, dev, ino } = JSON.parse(fs.readFileSync(marker, "utf8"));
    if (root !== path.join(environment.GITHUB_WORKSPACE, ".gh-aw-nvx")) throw new Error("Unexpected NVX runtime asset path");
    const identity = fs.lstatSync(root);
    if (!identity.isDirectory() || identity.isSymbolicLink() || identity.dev !== dev || identity.ino !== ino) throw new Error("NVX runtime asset directory was replaced");
    try {
      for (const directory of ["state", "gh-aw"]) {
        if (!fs.lstatSync(path.join(root, directory)).isDirectory()) throw new Error(`NVX asset directory must not be a symlink: ${directory}`);
      }
      const guestSafeoutputs = path.join(root, "gh-aw", "safeoutputs");
      if (!fs.lstatSync(guestSafeoutputs).isDirectory()) throw new Error("NVX safeoutputs directory must not be a symlink");
      for (const name of ["config.json", "tools.json", "validation.json"]) {
        if (lstatIfExists(path.join(guestSafeoutputs, name))) throw new Error(`NVX guest must not export safe-output policy: ${name}`);
      }
      for (const name of STATE_OUTPUT_DIRECTORIES) collectAllowedOutput(path.join(root, "state"), stateRoot, name, true);
      for (const name of STATE_OUTPUT_FILES) collectAllowedOutput(path.join(root, "state"), stateRoot, name, false);
      const hostSafeoutputs = path.join(environment.RUNNER_TEMP, "gh-aw", "safeoutputs");
      for (const name of SAFEOUTPUTS_OUTPUT_FILES) collectAllowedOutput(guestSafeoutputs, hostSafeoutputs, name, false);
      for (const name of SAFEOUTPUTS_OUTPUT_DIRECTORIES) collectAllowedOutput(guestSafeoutputs, hostSafeoutputs, name, true);
    } finally {
      removeAssets(root);
      fs.unlinkSync(marker);
    }
  } catch (error) {
    throw new Error(`Failed to collect NVX guest outputs: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
}

if (require.main === module) {
  try {
    const mode = process.argv[2];
    if (mode === "stage") stage();
    else if (mode === "guest") guest(process.argv[3]);
    else if (mode === "collect") collect();
    else throw new Error(`Unknown NVX asset operation: ${mode}`);
  } catch (error) {
    console.error(error);
    process.exitCode = 1;
  }
}

module.exports = { stage, guest, guestEnvironment, collect, collectTree, readELFInterpreter };
