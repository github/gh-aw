// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
const { claimArtifactPath, readClaimScopeContext } = require("./work_queue_claim_scope.cjs");

const STAGING_RELATIVE = path.join("gh-aw", "safeoutputs", "upload-artifacts");

function isWithinRoot(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative !== "" && relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

function containsSymlink(root, candidate) {
  let current = root;
  for (const component of path.relative(root, candidate).split(path.sep).filter(Boolean)) {
    current = path.join(current, component);
    try {
      if (fs.lstatSync(current).isSymbolicLink()) return true;
    } catch {
      return false;
    }
  }
  return false;
}

function matchesAnyPattern(relativePath, patterns) {
  return patterns.some(pattern => typeof pattern === "string" && globPatternToRegex(pattern).test(relativePath));
}

function listFiles(root, relativeDirectory = "") {
  const directory = path.resolve(root, relativeDirectory);
  if (!isWithinRoot(root, directory) && directory !== root) throw new Error("Artifact staging path escapes its root");
  if (!fs.existsSync(directory)) return [];

  const files = [];
  let entries;
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    throw new Error(`Unable to read artifact staging directory ${directory}`, { cause: error });
  }
  for (const entry of entries) {
    const relativePath = path.join(relativeDirectory, entry.name);
    const absolutePath = path.resolve(root, relativePath);
    if (!isWithinRoot(root, absolutePath)) throw new Error("Artifact staging path escapes its root");
    const stat = fs.lstatSync(absolutePath);
    if (stat.isSymbolicLink()) continue;
    if (stat.isDirectory()) files.push(...listFiles(root, relativePath));
    else if (stat.isFile()) files.push(relativePath.split(path.sep).join("/"));
  }
  return files;
}

function resolveRequestFiles(request, root, config) {
  const allowedPaths = Array.isArray(config["allowed-paths"]) ? config["allowed-paths"] : [];
  const hasPath = typeof request.path === "string";
  const hasFilters = request.filters !== undefined;
  if (hasPath === hasFilters) throw new Error("upload_artifact request must specify exactly one of path or filters");

  let candidates;
  if (hasPath) {
    const relativePath = request.path;
    if (path.isAbsolute(relativePath)) throw new Error("upload_artifact staging path must be relative");
    const resolved = path.resolve(root, relativePath);
    if (!isWithinRoot(root, resolved)) throw new Error("upload_artifact staging path escapes its root");
    if (containsSymlink(root, resolved)) throw new Error("upload_artifact staging path contains a symbolic link");
    const stat = fs.existsSync(resolved) ? fs.lstatSync(resolved) : null;
    if (!stat || stat.isSymbolicLink()) throw new Error(`upload_artifact staging path is unavailable: ${relativePath}`);
    candidates = stat.isDirectory() ? listFiles(root, relativePath) : stat.isFile() ? [path.relative(root, resolved).split(path.sep).join("/")] : [];
  } else {
    const files = listFiles(root);
    const filters = request.filters || {};
    const include = Array.isArray(filters.include) ? filters.include : Array.isArray(config["filters-include"]) ? config["filters-include"] : [];
    const exclude = Array.isArray(filters.exclude) ? filters.exclude : Array.isArray(config["filters-exclude"]) ? config["filters-exclude"] : [];
    candidates = files.filter(file => (include.length === 0 || matchesAnyPattern(file, include)) && (exclude.length === 0 || !matchesAnyPattern(file, exclude)));
  }

  if (allowedPaths.length > 0) candidates = candidates.filter(file => matchesAnyPattern(file, allowedPaths));
  return Array.from(new Set(candidates)).sort();
}

function getMessageStageRoot(message, stagingRoot, assignment) {
  if (typeof message.claim_handle !== "string") return stagingRoot;
  if (!assignment) throw new Error("Cannot export staged Claim files without their trusted assignment");
  return claimArtifactPath(stagingRoot, message.claim_handle, assignment);
}

function getConnectorStagedFiles(message, toolConfig, root) {
  const stagedFiles = toolConfig?.staged_files;
  if (!stagedFiles || typeof stagedFiles !== "object" || Array.isArray(stagedFiles)) return [];
  const files = [];
  for (const input of Object.keys(stagedFiles)) {
    const filename = message[input];
    if (typeof filename !== "string" || path.basename(filename) !== filename) continue;
    const absolutePath = path.resolve(root, filename);
    if (!isWithinRoot(root, absolutePath)) continue;
    try {
      if (fs.lstatSync(absolutePath).isFile()) files.push(filename);
    } catch {
      continue;
    }
  }
  return files;
}

function prepareTransferDirectory({ items, config, stagingRoot, transferRoot, assignment }) {
  const sourceRoot = path.resolve(stagingRoot);
  const destinationRoot = path.resolve(transferRoot);
  if (sourceRoot === destinationRoot || isWithinRoot(sourceRoot, destinationRoot) || isWithinRoot(destinationRoot, sourceRoot)) {
    throw new Error("Artifact transfer directory must be separate from agent-writable staging");
  }

  const selected = new Set();
  for (const item of items) {
    if (!item || typeof item.type !== "string") continue;
    const type = normalizeSafeOutputName(item.type);
    const toolConfig = config[type] || config[type.replace(/_/g, "-")];
    if (!toolConfig || typeof toolConfig !== "object" || Array.isArray(toolConfig)) continue;
    const root = getMessageStageRoot(item, sourceRoot, assignment);
    const files =
      type === "upload_artifact"
        ? resolveRequestFiles(item, root, toolConfig).map(file => path.join(path.relative(sourceRoot, root), file))
        : getConnectorStagedFiles(item, toolConfig, root).map(file => path.join(path.relative(sourceRoot, root), file));
    for (const file of files) selected.add(file.split(path.sep).join("/"));
  }

  try {
    fs.rmSync(destinationRoot, { recursive: true, force: true });
  } catch (error) {
    throw new Error(`Unable to clear artifact transfer directory ${destinationRoot}`, { cause: error });
  }
  try {
    fs.mkdirSync(destinationRoot, { recursive: true, mode: 0o700 });
  } catch (error) {
    throw new Error(`Unable to create artifact transfer directory ${destinationRoot}`, { cause: error });
  }
  for (const relativePath of [...selected].sort()) {
    const sourcePath = path.resolve(sourceRoot, relativePath);
    if (!isWithinRoot(sourceRoot, sourcePath)) throw new Error("Artifact transfer path escapes its root");
    if (containsSymlink(sourceRoot, sourcePath)) throw new Error("Artifact transfer path contains a symbolic link");
    const stat = fs.lstatSync(sourcePath);
    if (!stat.isFile() || stat.isSymbolicLink()) throw new Error("Artifact transfer includes a non-regular file");
    const destinationPath = path.resolve(destinationRoot, relativePath);
    if (!isWithinRoot(destinationRoot, destinationPath)) throw new Error("Artifact transfer path escapes its destination");
    try {
      fs.mkdirSync(path.dirname(destinationPath), { recursive: true, mode: 0o700 });
      fs.copyFileSync(sourcePath, destinationPath, fs.constants.COPYFILE_EXCL);
      fs.chmodSync(destinationPath, 0o600);
    } catch (error) {
      throw new Error(`Unable to stage artifact file ${relativePath}`, { cause: error });
    }
  }
  return { files: [...selected].sort(), count: selected.size };
}

async function prepare(options = {}) {
  const env = options.env || process.env;
  let config = options.config;
  if (!config) {
    try {
      config = JSON.parse(env.GH_AW_UPLOAD_ARTIFACT_CONFIG || "{}");
    } catch (error) {
      throw new Error("Unable to parse safe-output artifact staging configuration", { cause: error });
    }
  }
  const stagingRoot = options.stagingRoot || env.GH_AW_UPLOAD_ARTIFACT_STAGING_DIR || path.join(env.RUNNER_TEMP || "/tmp", STAGING_RELATIVE);
  const transferRoot = options.transferRoot || env.GH_AW_UPLOAD_ARTIFACT_TRANSFER_DIR || path.join(env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-artifacts-transfer");
  const output = options.output || require("./load_agent_output.cjs").loadAgentOutput({ partitioning: true });
  if (!output.success) {
    return prepareTransferDirectory({ items: [], config, stagingRoot, transferRoot, assignment: readClaimScopeContext()?.assignment });
  }
  return prepareTransferDirectory({
    items: output.items,
    config,
    stagingRoot,
    transferRoot,
    assignment: options.assignment || readClaimScopeContext()?.assignment,
  });
}

module.exports = { prepare, prepareTransferDirectory, resolveRequestFiles };
