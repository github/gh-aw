// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
const { claimArtifactPath, readClaimScopeContext } = require("./work_queue_claim_scope.cjs");
const { readGuardedFile, writeGuardedFile, withGuardedDirectory, normalizeGuardedPath, validateUploadSourcePath } = require("./symlink_guard.cjs");
const { validateConnectorFile } = require("./connector_file.cjs");

const STAGING_RELATIVE = path.join("gh-aw", "safeoutputs", "upload-artifacts");
const DEFAULT_MAX_SIZE_BYTES = 100 * 1024 * 1024;
const EMPTY_SELECTION_MARKER = ".gh-aw-empty-selection";
const RUNTIME_POLICY_VALUE_PREFIX = "__GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_";

function isWithinRoot(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

function securityError(message) {
  return Object.assign(new Error(message), { securityBoundary: true });
}

function isSecurityError(error) {
  if (error?.securityBoundary) return true;
  const message = error instanceof Error ? error.message : String(error);
  return /symbolic link|symlink|changed (?:while|during)|escapes (?:its )?root|path traversal|not a regular file/i.test(message);
}

function matchesAnyPattern(relativePath, patterns) {
  return patterns.some(pattern => typeof pattern === "string" && globPatternToRegex(pattern).test(relativePath));
}

function listFiles(root, relativeDirectory = "") {
  const absoluteRoot = path.resolve(root);
  const directory = path.resolve(absoluteRoot, relativeDirectory);
  if (!isWithinRoot(absoluteRoot, directory)) throw securityError("Artifact staging path escapes its root");
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
    const absolutePath = path.resolve(absoluteRoot, relativePath);
    if (!isWithinRoot(absoluteRoot, absolutePath)) throw securityError("Artifact staging path escapes its root");
    const stat = fs.lstatSync(absolutePath);
    if (stat.isSymbolicLink()) throw securityError("Artifact staging path contains a symbolic link");
    if (stat.isDirectory()) files.push(...listFiles(absoluteRoot, relativePath));
    else if (stat.isFile() && entry.name !== EMPTY_SELECTION_MARKER) files.push(relativePath.split(path.sep).join("/"));
  }
  return files;
}

function listFilesWithSource(sourcePath, relativePrefix) {
  const files = [];
  const visit = (currentPath, prefix) => {
    const stat = fs.lstatSync(currentPath);
    if (stat.isSymbolicLink()) throw securityError("Artifact staging path contains a symbolic link");
    if (stat.isFile()) {
      if (path.basename(currentPath) !== EMPTY_SELECTION_MARKER) {
        files.push({ relativePath: prefix.split(path.sep).join("/"), sourcePath: currentPath });
      }
      return;
    }
    if (!stat.isDirectory()) return;

    let entries;
    try {
      entries = fs.readdirSync(currentPath, { withFileTypes: true });
    } catch (error) {
      throw new Error(`Unable to read artifact source directory ${currentPath}`, { cause: error });
    }
    for (const entry of entries) {
      const childPath = path.join(currentPath, entry.name);
      if (entry.isSymbolicLink()) throw securityError("Artifact staging path contains a symbolic link");
      const childPrefix = prefix ? path.join(prefix, entry.name) : entry.name;
      visit(childPath, childPrefix);
    }
  };
  visit(sourcePath, relativePrefix);
  return files;
}

function configuredPatterns(config, key) {
  return Array.isArray(config[key]) ? config[key].filter(pattern => typeof pattern === "string") : [];
}

function filterCandidates(candidates, allowedPaths, includeGroups, excludes) {
  return candidates.filter(({ relativePath }) => {
    if (allowedPaths.length > 0 && !matchesAnyPattern(relativePath, allowedPaths)) return false;
    if (includeGroups.some(patterns => patterns.length > 0 && !matchesAnyPattern(relativePath, patterns))) return false;
    return excludes.length === 0 || !matchesAnyPattern(relativePath, excludes);
  });
}

function resolvePathCandidates(requestPath, root, workspaceRoot) {
  if (typeof requestPath !== "string") return { files: [], pathInfo: null };
  const sourceRoot = path.resolve(root);
  const normalizedWorkspace = normalizeGuardedPath(workspaceRoot);

  if (path.isAbsolute(requestPath)) {
    const absolutePath = normalizeGuardedPath(path.resolve(requestPath));
    const allowedRoot = isWithinRoot(sourceRoot, absolutePath) ? sourceRoot : isWithinRoot(normalizedWorkspace, absolutePath) ? normalizedWorkspace : null;
    if (!allowedRoot) return { files: [], pathInfo: null };
    if (allowedRoot === normalizedWorkspace && validateUploadSourcePath(absolutePath)) return { files: [], pathInfo: null };
    const relativePrefix = path.basename(absolutePath);
    if (!relativePrefix || relativePrefix === "." || relativePrefix === path.sep) return { files: [], pathInfo: null };
    if (!fs.existsSync(absolutePath)) return { files: [], pathInfo: null };
    const stat = fs.lstatSync(absolutePath);
    if (stat.isSymbolicLink()) throw securityError("upload_artifact staging path contains a symbolic link");
    return {
      files: listFilesWithSource(absolutePath, relativePrefix),
      pathInfo: { relativePath: relativePrefix, isDirectory: stat.isDirectory(), isRegularFile: stat.isFile() },
    };
  }

  const relativePath = requestPath === "." ? "" : requestPath;
  const stagedPath = path.resolve(sourceRoot, relativePath);
  if (!isWithinRoot(sourceRoot, stagedPath)) throw securityError("upload_artifact staging path escapes its root");
  if (fs.existsSync(stagedPath)) {
    const stat = fs.lstatSync(stagedPath);
    if (stat.isSymbolicLink()) throw securityError("upload_artifact staging path contains a symbolic link");
    return {
      files: listFilesWithSource(stagedPath, relativePath),
      pathInfo: { relativePath, isDirectory: stat.isDirectory(), isRegularFile: stat.isFile() },
    };
  }

  const workspacePath = path.resolve(normalizedWorkspace, relativePath);
  if (!isWithinRoot(normalizedWorkspace, workspacePath) || !fs.existsSync(workspacePath)) return { files: [], pathInfo: null };
  const stat = fs.lstatSync(workspacePath);
  if (stat.isSymbolicLink()) throw securityError("upload_artifact source path contains a symbolic link");
  const deniedPath = validateUploadSourcePath(workspacePath);
  if (deniedPath) return { files: [], pathInfo: null };
  return {
    files: listFilesWithSource(workspacePath, relativePath),
    pathInfo: { relativePath, isDirectory: stat.isDirectory(), isRegularFile: stat.isFile() },
  };
}

function resolveRequestSelection(request, root, config, workspaceRoot = process.env.GITHUB_WORKSPACE || process.cwd()) {
  const hasPath = Object.hasOwn(request, "path");
  const hasFilters = Object.hasOwn(request, "filters");
  if (hasPath === hasFilters) return { files: [], zeroMatchPath: null };

  const allowedPaths = configuredPatterns(config, "allowed-paths");
  if (hasPath) {
    const { files, pathInfo } = resolvePathCandidates(request.path, root, workspaceRoot);
    if (!pathInfo) return { files: [], zeroMatchPath: null };
    if (files.length === 0) return { files: [], zeroMatchPath: pathInfo.isDirectory ? pathInfo : null };
    const selected = filterCandidates(files, allowedPaths, [], []);
    return {
      files: selected,
      zeroMatchPath: selected.length === 0 && (pathInfo.isDirectory || pathInfo.isRegularFile) ? pathInfo : null,
    };
  }

  const filters = request.filters;
  if (!filters || typeof filters !== "object" || Array.isArray(filters)) return { files: [], zeroMatchPath: null };
  if (
    (Object.hasOwn(filters, "include") && (!Array.isArray(filters.include) || filters.include.some(value => typeof value !== "string"))) ||
    (Object.hasOwn(filters, "exclude") && (!Array.isArray(filters.exclude) || filters.exclude.some(value => typeof value !== "string")))
  ) {
    return { files: [], zeroMatchPath: null };
  }

  const requestIncludes = Array.isArray(filters.include) ? filters.include : [];
  const requestExcludes = Array.isArray(filters.exclude) ? filters.exclude : [];
  const configuredIncludes = configuredPatterns(config, "filters-include");
  const configuredExcludes = configuredPatterns(config, "filters-exclude");
  const candidates = listFiles(root).map(relativePath => ({ relativePath, sourcePath: path.resolve(root, relativePath) }));
  return {
    files: filterCandidates(candidates, allowedPaths, [configuredIncludes, requestIncludes], [...configuredExcludes, ...requestExcludes]),
    zeroMatchPath: null,
  };
}

function resolveRequestFiles(request, root, config) {
  return resolveRequestSelection(request, root, config)
    .files.map(file => file.relativePath)
    .sort();
}

function getMessageStageRoot(message, stagingRoot, assignment, queueEnabled) {
  if (!queueEnabled || typeof message.claim_handle !== "string") return stagingRoot;
  if (!assignment) throw new Error("Cannot export staged Claim files without their trusted assignment");
  return claimArtifactPath(stagingRoot, message.claim_handle, assignment);
}

function getConnectorStagedFiles(message, toolConfig, root) {
  const stagedFiles = toolConfig?.staged_files;
  if (!stagedFiles || typeof stagedFiles !== "object" || Array.isArray(stagedFiles)) return [];
  const files = [];
  for (const [input, policy] of Object.entries(stagedFiles)) {
    const filename = message[input];
    if (typeof filename !== "string" || path.basename(filename) !== filename) continue;
    const absolutePath = path.resolve(root, filename);
    if (!isWithinRoot(path.resolve(root), absolutePath)) continue;
    try {
      const stat = fs.lstatSync(absolutePath);
      if (stat.isSymbolicLink()) throw securityError("Connector staging path contains a symbolic link");
      if (stat.isFile()) files.push({ filename, policy, sourcePath: absolutePath });
    } catch (error) {
      if (isSecurityError(error)) throw error;
    }
  }
  return files;
}

function prepareTransferDirectory({ items, config, stagingRoot, transferRoot, assignment, queueEnabled = process.env.GH_AW_WORK_QUEUE_ENABLED === "true", workspaceRoot = process.env.GITHUB_WORKSPACE || process.cwd() }) {
  const sourceRoot = path.resolve(stagingRoot);
  const destinationRoot = path.resolve(transferRoot);
  if (sourceRoot === destinationRoot || isWithinRoot(sourceRoot, destinationRoot) || isWithinRoot(destinationRoot, sourceRoot)) {
    throw new Error("Artifact transfer directory must be separate from agent-writable staging");
  }

  const snapshots = new Map();
  const zeroMatchPaths = new Map();
  const maxSizeBytes = Number.isSafeInteger(config.upload_artifact?.["max-size-bytes"]) && config.upload_artifact["max-size-bytes"] > 0 ? config.upload_artifact["max-size-bytes"] : DEFAULT_MAX_SIZE_BYTES;

  for (const item of items) {
    if (!item || typeof item.type !== "string") continue;
    const type = normalizeSafeOutputName(item.type);
    const toolConfig = config[type] || config[type.replace(/_/g, "-")];
    if (!toolConfig || typeof toolConfig !== "object" || Array.isArray(toolConfig)) continue;

    if (type === "upload_artifact") {
      let selection;
      try {
        const root = getMessageStageRoot(item, sourceRoot, assignment, queueEnabled);
        selection = resolveRequestSelection(item, root, toolConfig, workspaceRoot);
      } catch (error) {
        if (isSecurityError(error)) throw error;
        continue;
      }

      const relativeRoot = path.relative(sourceRoot, getMessageStageRoot(item, sourceRoot, assignment, queueEnabled));
      const requestSnapshots = [];
      let totalSize = 0;
      let validRequest = true;
      for (const file of selection.files) {
        const relativePath = path.join(relativeRoot, file.relativePath).split(path.sep).join("/");
        if (validateUploadSourcePath(normalizeGuardedPath(file.sourcePath))) {
          validRequest = false;
          break;
        }
        let snapshot = snapshots.get(relativePath);
        if (snapshot && snapshot.sourcePath !== path.resolve(file.sourcePath)) {
          validRequest = false;
          break;
        }
        if (!snapshot) {
          try {
            snapshot = { content: readGuardedFile(file.sourcePath, maxSizeBytes - totalSize), sourcePath: path.resolve(file.sourcePath) };
          } catch (error) {
            if (isSecurityError(error)) throw error;
            validRequest = false;
            break;
          }
        }
        totalSize += snapshot.content.length;
        if (totalSize > maxSizeBytes) {
          validRequest = false;
          break;
        }
        requestSnapshots.push([relativePath, snapshot]);
      }
      if (validRequest) {
        for (const [relativePath, snapshot] of requestSnapshots) snapshots.set(relativePath, snapshot);
        if (selection.zeroMatchPath) {
          const zeroMatchPath = path.join(relativeRoot, selection.zeroMatchPath.relativePath);
          zeroMatchPaths.set(zeroMatchPath.split(path.sep).join("/"), selection.zeroMatchPath.isDirectory);
        }
      }
      continue;
    }

    for (const file of getConnectorStagedFiles(item, toolConfig, sourceRoot)) {
      const relativePath = path.relative(sourceRoot, file.sourcePath).split(path.sep).join("/");
      try {
        let snapshot = snapshots.get(relativePath);
        if (snapshot && snapshot.sourcePath !== path.resolve(file.sourcePath)) continue;
        if (!snapshot) snapshot = { content: readGuardedFile(file.sourcePath, file.policy.maxBytes), sourcePath: path.resolve(file.sourcePath) };
        validateConnectorFile(file.filename, snapshot.content, file.policy);
        snapshots.set(relativePath, snapshot);
      } catch (error) {
        if (isSecurityError(error)) throw error;
      }
    }
  }

  for (const [relativePath, isDirectory] of zeroMatchPaths) {
    const markerPath = isDirectory ? path.join(relativePath, EMPTY_SELECTION_MARKER) : relativePath;
    snapshots.set(markerPath.split(path.sep).join("/"), { content: Buffer.alloc(0), sourcePath: null });
  }

  try {
    withGuardedDirectory(path.dirname(destinationRoot), true, parent => {
      fs.rmSync(path.join(parent, path.basename(destinationRoot)), { recursive: true, force: true });
    });
  } catch (error) {
    throw new Error(`Unable to clear artifact transfer directory ${destinationRoot}`, { cause: error });
  }
  for (const [relativePath, snapshot] of [...snapshots.entries()].sort(([left], [right]) => left.localeCompare(right))) {
    const destinationPath = path.resolve(destinationRoot, relativePath);
    if (!isWithinRoot(destinationRoot, destinationPath)) throw securityError("Artifact transfer path escapes its destination");
    try {
      writeGuardedFile(destinationPath, snapshot.content);
    } catch (error) {
      throw new Error(`Unable to stage artifact file ${relativePath}`, { cause: error });
    }
  }
  return { files: [...snapshots.keys()].sort(), count: snapshots.size };
}

function resolveRuntimePolicyValues(config, env) {
  const pattern = new RegExp(`${RUNTIME_POLICY_VALUE_PREFIX}(\\d+)__`, "g");
  for (const toolConfig of Object.values(config)) {
    if (!toolConfig || typeof toolConfig !== "object" || Array.isArray(toolConfig)) continue;
    for (const key of ["allowed-paths", "filters-include", "filters-exclude"]) {
      if (!Array.isArray(toolConfig[key])) continue;
      toolConfig[key] = toolConfig[key].map(value =>
        typeof value === "string"
          ? value.replace(pattern, (placeholder, index) => {
              const runtimeValue = env[`GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_${index}`];
              if (typeof runtimeValue !== "string") throw new Error(`Missing runtime safe-output policy value ${index}`);
              return runtimeValue;
            })
          : value
      );
    }
  }
  return config;
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
  config = resolveRuntimePolicyValues(config, env);
  const stagingRoot = options.stagingRoot || env.GH_AW_UPLOAD_ARTIFACT_STAGING_DIR || path.join(env.RUNNER_TEMP || "/tmp", STAGING_RELATIVE);
  const transferRoot = options.transferRoot || env.GH_AW_UPLOAD_ARTIFACT_TRANSFER_DIR || path.join(env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-artifacts-transfer");
  const output = options.output || require("./load_agent_output.cjs").loadAgentOutput({ partitioning: true });
  const assignment = options.assignment || readClaimScopeContext()?.assignment;
  if (!output.success) {
    return prepareTransferDirectory({ items: [], config, stagingRoot, transferRoot, assignment, queueEnabled: env.GH_AW_WORK_QUEUE_ENABLED === "true" });
  }
  return prepareTransferDirectory({
    items: output.items,
    config,
    stagingRoot,
    transferRoot,
    assignment,
    queueEnabled: env.GH_AW_WORK_QUEUE_ENABLED === "true",
    workspaceRoot: env.GITHUB_WORKSPACE || process.cwd(),
  });
}

module.exports = { prepare, prepareTransferDirectory, resolveRequestFiles, resolveRuntimePolicyValues };
