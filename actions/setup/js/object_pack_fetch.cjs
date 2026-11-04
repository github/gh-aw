// @ts-check

const childProcess = require("child_process");
const { execLocalGitSync } = require("./git_helpers.cjs");
const { buildGitAuthEnv } = require("./git_auth_env.cjs");
const { getObjectPackLimits } = require("./object_pack_limits.cjs");

/**
 * Network access belongs only to a clean trusted store, before pack import.
 * Uploaded graph edges are hints, never fetch refspecs or remote authority.
 */
function fetchRecoveryObjects({ cwd, env, remote, ref, checkoutSha, token, inspection, tip, trace }) {
  const limits = getObjectPackLimits(env);
  const deadline = Date.now() + limits.networkTimeoutMs;
  const auth = token ? buildGitAuthEnv(token) : {};
  const git = (args, settings = {}) => execLocalGitSync(args, { cwd, env, maxBuffer: limits.maxPackBytes, timeout: limits.commandTimeoutMs, ...settings });
  git(["config", "remote.recovery.promisor", "true"]);
  git(["config", "remote.recovery.partialclonefilter", "tree:0"]);
  /** @param {string[]} args @param {string} input @param {string} [filter] */
  const fetch = (args, input, filter = "tree:0") => {
    const remaining = deadline - Date.now();
    if (remaining <= 0) throw new Error("Trusted recovery fetch budget exhausted");
    const result = childProcess.spawnSync(
      "git",
      [
        "-c",
        "credential.helper=",
        "-c",
        "core.hooksPath=/dev/null",
        "-c",
        "http.followRedirects=false",
        "-c",
        "fetch.fsckObjects=true",
        "-c",
        `remote.recovery.url=${remote}`,
        "fetch",
        "--no-tags",
        "--no-recurse-submodules",
        "--no-write-fetch-head",
        `--filter=${filter}`,
        ...args,
        "--",
        "recovery",
      ],
      {
        cwd,
        env: { ...env, ...auth, GIT_ALLOW_PROTOCOL: "https", GIT_TERMINAL_PROMPT: "0", GIT_ASKPASS: "/bin/false" },
        input,
        encoding: "utf8",
        maxBuffer: limits.maxCommandOutputBytes,
        timeout: Math.min(limits.commandTimeoutMs, remaining),
        killSignal: "SIGKILL",
      }
    );
    // Never retry a failed request (including authentication and unsupported filters).
    if (result.status !== 0 || result.error || /filtering not recognized|does not support filter/i.test(result.stderr || "")) {
      throw new Error("Trusted recovery fetch failed or filtering is unsupported");
    }
  };
  const objects = inspection.objects;
  const commits = [...objects].filter(([, object]) => object.type === "commit");
  const parents = bytes =>
    [
      ...bytes
        .toString("utf8")
        .split("\n\n", 1)[0]
        .matchAll(/^parent ([0-9a-f]{40,64})$/gm),
    ].map(match => match[1]);
  const frontier = new Set(commits.flatMap(([, object]) => parents(object.bytes)).filter(oid => !objects.has(oid)));
  let pinned;
  for (const [attempt, depth] of limits.fetchDepths.entries()) {
    trace("fetch-step", { attempt, depth });
    const refs = [`${pinned || ref}:refs/heads/recovery-base`];
    if (checkoutSha) refs.push(`${checkoutSha}:refs/heads/recovery-checkout`);
    // Ref operands travel over stdin so neither a branch nor an object ID can
    // become a fetch option. Subsequent depths stay pinned to the first result.
    fetch([`--depth=${depth}`, "--stdin"], `${refs.join("\n")}\n`);
    pinned ||= git(["rev-parse", "refs/heads/recovery-base"]).trim();
    const available = git(["rev-list", "--all"]).trim().split("\n");
    const found = new Set(available);
    if ([...frontier].every(oid => found.has(oid)) && (checkoutSha || available.some(oid => objects.has(oid) || frontier.has(oid)))) break;
    if (attempt === limits.fetchDepths.length - 1) throw new Error("Required commit ancestry exceeds the bounded recovery search");
    // A complete trusted history cannot gain another ancestor by deepening.
    if (git(["rev-parse", "--is-shallow-repository"]).trim() === "false") throw new Error("Required commits are not in authorized history");
  }
  // Ancestry search transfers commits only. Materialize tree snapshots only for
  // authorized baselines and boundary commits actually needed by the upload.
  const roots = new Set();
  for (const oid of new Set([pinned, checkoutSha, ...frontier].filter(Boolean))) {
    const header = git(["cat-file", "commit", oid]);
    const root = /^tree ([0-9a-f]{40,64})\n/.exec(header)?.[1];
    if (!root) throw new Error("Invalid trusted snapshot");
    roots.add(root);
  }
  const snapshots = [...roots];
  for (let offset = 0; offset < snapshots.length; offset += limits.fetchBatchSize) {
    const batch = snapshots.slice(offset, offset + limits.fetchBatchSize);
    trace("fetch-trees", { objects: batch.length });
    fetch(["--refetch", "--stdin"], `${batch.join("\n")}\n`, "blob:none");
  }

  const treeCache = new Map();
  const trustedTrees = new Set();
  const trustedBlobs = new Set();
  const trustedTypes = new Map();
  const inspectTrustedTypes = ids => {
    if (!ids.length) return;
    const records = git(["cat-file", "--batch-check=%(objectname) %(objecttype)"], { input: `${ids.join("\n")}\n` });
    for (const record of records.trim().split("\n")) {
      const [oid, type] = record.split(" ");
      trustedTypes.set(oid, type);
    }
  };
  inspectTrustedTypes([...objects.keys()]);
  const required = new Set();
  let entries = 0;
  const rawObject = (oid, type) => {
    const packed = objects.get(oid);
    if (packed) {
      if (packed.type !== type) throw new Error("Recovery graph object type mismatch");
      return packed.bytes;
    }
    const result = childProcess.spawnSync("git", ["cat-file", type, oid], { cwd, env, encoding: null, maxBuffer: limits.maxPackBytes, timeout: limits.commandTimeoutMs, killSignal: "SIGKILL" });
    if (result.status !== 0 || result.error || !Buffer.isBuffer(result.stdout)) throw new Error("Required trusted graph object is unavailable");
    return result.stdout;
  };
  const treeOf = oid => {
    const match = /^tree ([0-9a-f]{40,64})\n/.exec(rawObject(oid, "commit").toString("utf8"));
    if (!match) throw new Error("Invalid recovery commit tree");
    return match[1];
  };
  const tree = oid => {
    if (!oid) return new Map();
    if (treeCache.has(oid)) return treeCache.get(oid);
    const bytes = rawObject(oid, "tree");
    const trusted = !objects.has(oid) || trustedTypes.get(oid) === "tree";
    if (trusted) trustedTrees.add(oid);
    const result = new Map();
    const width = tip.length / 2;
    let offset = 0;
    while (offset < bytes.length) {
      if (++entries > limits.maxTreeEntries) throw new Error("Recovery tree traversal budget exhausted");
      const space = bytes.indexOf(32, offset);
      const nul = bytes.indexOf(0, space + 1);
      if (space < offset || nul < space || nul + 1 + width > bytes.length) throw new Error("Invalid recovery tree");
      const mode = bytes.subarray(offset, space).toString("ascii");
      if (!["40000", "100644", "100755", "120000", "160000"].includes(mode)) throw new Error("Invalid recovery tree mode");
      const name = bytes.subarray(space + 1, nul).toString("hex");
      const id = bytes.subarray(nul + 1, nul + 1 + width).toString("hex");
      if (result.has(name)) throw new Error("Duplicate recovery tree entry");
      result.set(name, { mode, id });
      if (trusted && mode !== "40000" && mode !== "160000") trustedBlobs.add(id);
      offset = nul + 1 + width;
    }
    treeCache.set(oid, result);
    return result;
  };
  const externalTrees = new Set();
  for (const [oid, object] of objects) {
    if (object.type !== "tree") continue;
    for (const entry of tree(oid).values()) if (entry.mode === "40000" && !objects.has(entry.id)) externalTrees.add(entry.id);
  }
  for (const [oid] of commits) {
    const root = treeOf(oid);
    if (!objects.has(root)) externalTrees.add(root);
  }
  inspectTrustedTypes([...externalTrees]);
  const compared = new Set();
  const compare = (left, right) => {
    if (left === right) return;
    const key = `${left || ""}:${right || ""}`;
    if (compared.has(key)) return;
    compared.add(key);
    const old = tree(left),
      next = tree(right);
    for (const name of new Set([...old.keys(), ...next.keys()])) {
      const a = old.get(name),
        b = next.get(name);
      if (a?.id === b?.id && a?.mode === b?.mode) continue;
      if (a?.mode === "40000" || b?.mode === "40000") compare(a?.mode === "40000" ? a.id : undefined, b?.mode === "40000" ? b.id : undefined);
      for (const entry of [a, b]) if (entry && entry.mode !== "40000" && entry.mode !== "160000") required.add(entry.id);
    }
  };
  // Inspect every packed commit, including intermediate edits and resolved
  // merges, so format-patch never needs an unhydrated old/new blob.
  for (const [, object] of commits) {
    const current = /^tree ([0-9a-f]{40,64})\n/.exec(object.bytes.toString("utf8"))?.[1];
    if (!current) throw new Error("Invalid recovery commit");
    const ancestors = parents(object.bytes);
    if (!ancestors.length) compare(undefined, current);
    for (const parent of ancestors) compare(treeOf(parent), current);
  }
  const validated = new Set();
  const validateTree = oid => {
    if (validated.has(oid)) return;
    validated.add(oid);
    if (!objects.has(oid)) {
      if (trustedTypes.get(oid) !== "tree") throw new Error("Unverified missing agent tree");
      return;
    }
    const contents = tree(oid);
    if (trustedTrees.has(oid)) {
      // Fetched trees are authoritative, including their promised unchanged blobs.
      return;
    }
    for (const entry of contents.values()) {
      if (entry.mode === "40000") validateTree(entry.id);
      else if (entry.mode !== "160000") {
        const blob = objects.get(entry.id);
        if (blob ? blob.type !== "blob" : !trustedBlobs.has(entry.id)) throw new Error("Unverified missing agent object");
      }
    }
  };
  for (const [oid] of commits) validateTree(treeOf(oid));
  const missing = [...required].filter(oid => !objects.has(oid));
  // Only blob identities actually read from fetched trusted trees may be
  // requested. Missing agent-created bytes cannot be filled from upload hints.
  if (missing.some(oid => !trustedBlobs.has(oid))) throw new Error("Missing agent-created object bytes");
  let bytes = 0;
  for (let offset = 0; offset < missing.length; offset += limits.fetchBatchSize) {
    const batch = missing.slice(offset, offset + limits.fetchBatchSize);
    trace("fetch-blobs", { objects: batch.length });
    fetch(["--refetch", "--stdin"], `${batch.join("\n")}\n`, "blob:none");
    const sizes = git(["cat-file", "--batch-check=%(objectsize)"], { input: `${batch.join("\n")}\n` })
      .trim()
      .split("\n");
    for (const size of sizes) {
      if (!/^\d+$/.test(size)) throw new Error("Unable to validate hydrated blobs");
      bytes += Number(size);
      if (!Number.isSafeInteger(bytes) || bytes > limits.maxHydrationBytes) throw new Error("Required baseline blobs exceed the configured hydration budget");
    }
  }
  trace("fetch-hydrated", { objects: missing.length, bytes });
}

module.exports = { fetchRecoveryObjects };
