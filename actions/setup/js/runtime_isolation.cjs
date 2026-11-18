const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { spawnSync } = require("node:child_process");

const ROOT_PATTERN = /^\/tmp\/gh-aw-run\.([A-Za-z0-9]+)$/;
const TOKEN_PATTERN = /^[a-f0-9]{64}$/;
const OWNER_FILE = ".gh-aw-runtime-owner";

function identity(stat) {
  return `${stat.dev}:${stat.ino}`;
}

function ownershipDigest(token) {
  return crypto.createHash("sha256").update(token).digest("hex");
}

function currentUID() {
  if (!process.getuid) throw new Error("Runtime ownership checks require a POSIX host or WSL");
  return process.getuid();
}

function resourceSuffix(root) {
  const match = ROOT_PATTERN.exec(root);
  if (!match) throw new Error("Invalid private gh-aw runtime path");
  return match[1];
}

function setupParentFor(env) {
  return env.RUNNER_OS === "Windows" && env.WSL_DISTRO_NAME ? "/tmp" : fs.realpathSync(env.RUNNER_TEMP);
}

function validateSetup(root, token, expectedIdentity, env = process.env) {
  return validate(root, token, expectedIdentity, setupParentFor(env));
}

function validate(root, token, expectedIdentity, setupParent) {
  const validPath = setupParent ? path.dirname(root || "") === fs.realpathSync(setupParent) && /^gh-aw-setup\.[A-Za-z0-9]+$/.test(path.basename(root || "")) : ROOT_PATTERN.test(root || "");
  if (!validPath || !TOKEN_PATTERN.test(token || "")) {
    throw new Error("Missing or invalid private gh-aw runtime ownership");
  }
  const stat = fs.lstatSync(root);
  const owner = fs.lstatSync(path.join(root, OWNER_FILE));
  const uid = currentUID();
  if (
    !stat.isDirectory() ||
    stat.isSymbolicLink() ||
    stat.uid !== uid ||
    (stat.mode & 0o777) !== 0o700 ||
    (expectedIdentity && identity(stat) !== expectedIdentity) ||
    !owner.isFile() ||
    owner.isSymbolicLink() ||
    owner.uid !== uid ||
    (owner.mode & 0o777) !== 0o600 ||
    owner.nlink !== 1 ||
    fs.readFileSync(path.join(root, OWNER_FILE), "utf8").trim() !== ownershipDigest(token)
  ) {
    throw new Error("Refusing replaced, unsafe, or unowned gh-aw runtime directory");
  }
  return identity(stat);
}

function append(file, values) {
  if (file)
    fs.appendFileSync(
      file,
      Object.entries(values)
        .map(([key, value]) => `${key}=${value}\n`)
        .join("")
    );
}

function allocate(env = process.env) {
  const created = [];
  try {
    return allocateRoots(env, created);
  } catch (error) {
    for (const root of created.reverse()) {
      try {
        fs.rmSync(root, { recursive: true, force: true });
      } catch {
        // Never elevate permissions or scan for leftover allocations.
      }
    }
    throw error;
  }
}

function allocateRoots(env, created) {
  const setupParent = setupParentFor(env);
  if (/[\r\n]/.test(setupParent)) throw new Error("RUNNER_TEMP cannot contain line breaks");
  let root = env.GH_AW_RUNTIME_ROOT;
  let token = env.GH_AW_RUNTIME_TOKEN;
  if (!root) {
    root = fs.mkdtempSync("/tmp/gh-aw-run.");
    created.push(root);
    fs.chmodSync(root, 0o700);
    token = crypto.randomBytes(32).toString("hex");
    fs.writeFileSync(path.join(root, OWNER_FILE), ownershipDigest(token), { mode: 0o600, flag: "wx" });
  }
  const runtimeIdentity = validate(root, token, env.GH_AW_RUNTIME_ID);
  const suffix = resourceSuffix(root);
  if (env.GH_AW_RESOURCE_SUFFIX && env.GH_AW_RESOURCE_SUFFIX !== suffix) throw new Error("Runtime resource suffix does not match owned runtime root");
  let setupRoot = env.GH_AW_SETUP_ROOT;
  if (!setupRoot) {
    setupRoot = fs.mkdtempSync(path.join(setupParent, "gh-aw-setup."));
    created.push(setupRoot);
    fs.chmodSync(setupRoot, 0o700);
    fs.writeFileSync(path.join(setupRoot, OWNER_FILE), ownershipDigest(token), { mode: 0o600, flag: "wx" });
  }
  const values = {
    GH_AW_RUNTIME_ROOT: root,
    GH_AW_RUNTIME_TOKEN: token,
    GH_AW_RESOURCE_SUFFIX: suffix,
    GH_AW_RUNTIME_ID: runtimeIdentity,
    GH_AW_SETUP_ROOT: setupRoot,
    GH_AW_SETUP_ID: validate(setupRoot, token, env.GH_AW_SETUP_ID, setupParent),
  };
  Object.assign(env, values);
  if (env.GITHUB_ACTIONS === "true") process.stderr.write(`::add-mask::${token}\n`);
  const persistedValues = { ...values };
  if (env.RUNNER_OS === "Windows" && env.WSL_DISTRO_NAME) {
    const translated = spawnSync("wslpath", ["-w", setupRoot], { encoding: "utf8" });
    if (translated.error || translated.status !== 0) throw new Error("Unable to persist native Windows setup path");
    persistedValues.GH_AW_SETUP_ROOT = translated.stdout.trim();
  }
  append(env.GITHUB_ENV, persistedValues);
  append(env.GITHUB_STATE, persistedValues);
  return values;
}

function validateDestination(directory, setupRoot) {
  const relative = path.relative(setupRoot, directory);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) throw new Error("Assets must be staged inside the private setup root");
  let current = setupRoot;
  for (const component of relative.split(path.sep)) {
    current = path.join(current, component);
    try {
      const stat = fs.lstatSync(current);
      if (!stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== currentUID()) throw new Error("Unsafe setup destination");
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
}

function stagedModulePath(directory, setupRoot, filename) {
  if (/[\r\n]/.test(directory)) throw new Error("Invalid setup destination");
  validateDestination(directory, setupRoot);
  const resolved = path.resolve(directory);
  if (fs.realpathSync(directory) !== resolved) throw new Error("Unsafe setup destination");
  const modulePath = path.join(resolved, filename);
  const stat = fs.lstatSync(modulePath);
  if (!stat.isFile() || stat.uid !== currentUID()) throw new Error("Unsafe staged module");
  return modulePath;
}

function cleanup(env = process.env) {
  const root = env.STATE_GH_AW_RUNTIME_ROOT || env.GH_AW_RUNTIME_ROOT;
  if (!root) return false;
  // Never combine a saved action root with ownership supplied by a later step.
  const saved = !!env.STATE_GH_AW_RUNTIME_ROOT;
  const token = saved ? env.STATE_GH_AW_RUNTIME_TOKEN : env.GH_AW_RUNTIME_TOKEN;
  const runtimeIdentity = saved ? env.STATE_GH_AW_RUNTIME_ID : env.GH_AW_RUNTIME_ID;
  try {
    if (!runtimeIdentity) throw new Error("Missing runtime directory identity");
    validate(root, token, runtimeIdentity);
    const suffix = resourceSuffix(root);
    const ownerDigest = ownershipDigest(token);
    const containerNames = ["awmg-mcpg", "awmg-cli-proxy", "awmg-proxy"].map(name => `${name}-${suffix}`);
    const ownedContainers = [];
    for (const containerName of containerNames) {
      const inspected = spawnSync("docker", ["container", "inspect", "--format", '{{.Id}} {{index .Config.Labels "com.gh.aw.owner"}}', containerName], { stdio: ["ignore", "pipe", "ignore"], timeout: 30000, encoding: "utf8" });
      const [id, digest] = (inspected.stdout || "").trim().split(" ");
      if (inspected.status === 0 && /^[a-f0-9]{64}$/.test(id) && digest === ownerDigest) ownedContainers.push(id);
    }
    if (ownedContainers.length > 0) spawnSync("docker", ["rm", "-f", ...ownedContainers], { stdio: "ignore", timeout: 30000 });
    fs.rmSync(root, { recursive: true, force: true });
  } catch (error) {
    if (error.code !== "ENOENT") console.warn(`Warning: private runtime cleanup skipped: ${error.message}`);
    return false;
  }
  const setupRoot = saved ? env.STATE_GH_AW_SETUP_ROOT : env.GH_AW_SETUP_ROOT;
  const setupIdentity = saved ? env.STATE_GH_AW_SETUP_ID : env.GH_AW_SETUP_ID;
  if (setupRoot && setupIdentity) {
    try {
      validateSetup(setupRoot, token, setupIdentity, env);
      fs.rmSync(setupRoot, { recursive: true, force: true });
    } catch (error) {
      if (error.code !== "ENOENT") console.warn(`Warning: private setup cleanup skipped: ${error.message}`);
    }
  }
  return true;
}

function relocateRuntimePaths(text, env = process.env) {
  const root = env.GH_AW_RUNTIME_ROOT;
  if (!root) return text;
  if (!ROOT_PATTERN.test(root)) throw new Error("Invalid private gh-aw runtime path");
  return text.replace(/\/tmp\/gh-aw(?=\/|[^A-Za-z0-9_.-]|$)/g, root);
}

function relocateText(text, root, suffix, setupRoot, filename = "asset.cjs") {
  const isJavaScript = /\.(?:cjs|js)$/.test(filename);
  const setupReference = isJavaScript ? "${process.env.GH_AW_SETUP_ROOT}" : "${GH_AW_SETUP_ROOT}";
  return text
    .replace(/((?:\w+\.)?(?:join|resolve))\(os\.tmpdir\(\),\s*(["'])gh-aw(?:\/([^"']*))?\2/g, (_, join, _quote, tail) => `${join}(${JSON.stringify(tail ? `${root}/${tail}` : root)}`)
    .replace(/((?:\w+\.)?(?:join|resolve))\((["'])\/tmp\2,\s*(["'])gh-aw(?:\/([^"']*))?\3/g, (_, join, _baseQuote, _quote, tail) => `${join}(${JSON.stringify(tail ? `${root}/${tail}` : root)}`)
    .replace(/\/tmp\/gh-aw(?=\/|[^A-Za-z0-9_.-]|$)/g, root)
    .replace(/\$\{\{\s*runner\.temp\s*\}\}\/gh-aw\b/g, setupReference)
    .replace(/\$\{(?:process\.env\.)?(?:RUNNER_TEMP|runnerTemp)(?::[^}]*|%[^}]*)?\}\/gh-aw\b/g, setupReference)
    .replace(/\$RUNNER_TEMP\/gh-aw\b/g, setupReference)
    .replace(/((?:\w+\.)?(?:join|resolve))\(([^,\n]*(?:RUNNER_TEMP|runnerTemp)[^,\n]*),\s*(["'])gh-aw(?:\/([^"']*))?\3/g, (_, join, _base, _quote, tail) => `${join}(${JSON.stringify(tail ? `${setupRoot}/${tail}` : setupRoot)}`)
    .replace(/\bawmg-(?:mcpg|cli-proxy|proxy)\b(?!-[A-Za-z0-9$])/g, name => `${name}-${suffix}`);
}

function relocate(directory, root, token, suffix, setupRoot) {
  validate(root, token);
  validateDestination(directory, setupRoot);
  const walk = current => {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      if (entry.isSymbolicLink()) continue;
      const file = path.join(current, entry.name);
      if (entry.isDirectory()) {
        if (!["node_modules", ".git", ".github"].includes(entry.name)) walk(file);
      } else if (entry.isFile() && /\.(?:cjs|js|json|sh|md)$/.test(entry.name) && !entry.name.includes(".test.") && entry.name !== "runtime_isolation.cjs") {
        const source = fs.readFileSync(file, "utf8");
        const target = relocateText(source, root, suffix, setupRoot, entry.name);
        if (source !== target) fs.writeFileSync(file, target);
      }
    }
  };
  walk(directory);
}

module.exports = { allocate, cleanup, validate, validateSetup, relocate, relocateText, relocateRuntimePaths, validateDestination, stagedModulePath };

if (require.main === module) {
  try {
    const command = process.argv[2];
    if (command === "allocate") {
      const values = allocate();
      process.stdout.write([values.GH_AW_RUNTIME_ROOT, values.GH_AW_RUNTIME_TOKEN, values.GH_AW_RESOURCE_SUFFIX, values.GH_AW_RUNTIME_ID, values.GH_AW_SETUP_ROOT, values.GH_AW_SETUP_ID].join("\n") + "\n");
    } else if (command === "cleanup") {
      cleanup();
    } else if (command === "validate") {
      validate(process.env.GH_AW_RUNTIME_ROOT, process.env.GH_AW_RUNTIME_TOKEN, process.env.GH_AW_RUNTIME_ID);
    } else if (command === "validate-setup") {
      validateSetup(process.env.GH_AW_SETUP_ROOT, process.env.GH_AW_RUNTIME_TOKEN, process.env.GH_AW_SETUP_ID);
    } else if (command === "destination") {
      validateDestination(process.argv[3], process.env.GH_AW_SETUP_ROOT);
    } else if (command === "relocate") {
      for (const directory of process.argv.slice(3)) relocate(directory, process.env.GH_AW_RUNTIME_ROOT, process.env.GH_AW_RUNTIME_TOKEN, process.env.GH_AW_RESOURCE_SUFFIX, process.env.GH_AW_SETUP_ROOT);
    } else {
      throw new Error("Expected allocate, relocate, or cleanup");
    }
  } catch (error) {
    console.error(`::error::${error.message}`);
    process.exitCode = 1;
  }
}
