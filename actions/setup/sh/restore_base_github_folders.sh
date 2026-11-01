#!/usr/bin/env bash
set +o histexpand

#
# restore_base_github_folders.sh - Restore agent config folders/files from the base
#                                   branch snapshot after PR checkout
#
# After checkout_pr_branch runs the workspace contains PR-branch content,
# which may include attacker-controlled skill/instruction files for fork PRs.
# This script overwrites agent-specific folders and root instruction files with
# the trusted snapshot saved by save_base_github_folders.sh during the activation
# job.  It also removes .mcp.json from the workspace root, which may contain
# untrusted MCP server configuration from the PR branch.
#
# For each item:
#   - If present in the base snapshot: restore it (overwrite PR-branch content)
#   - If absent from the base snapshot but present in the workspace: remove it
#     (prevent the PR branch from injecting files that the base branch doesn't have)
#
# Archive metadata is checked before extraction. Only configured paths, regular
# files, directories, and symlinks confined to a restored folder are accepted.
#
# The lists of folders and files MUST match those used in save_base_github_folders.sh
# and are passed via the same environment variables:
#
#   GH_AW_AGENT_FOLDERS  - space-separated list of directories to restore
#                          (e.g. ".agents .claude .codex .gemini .github")
#   GH_AW_AGENT_FILES    - space-separated list of root files to restore
#                          (e.g. "AGENTS.md CLAUDE.md GEMINI.md")
#
# Exit codes:
#   0 - Success

set -euo pipefail

WORKSPACE="${GITHUB_WORKSPACE:-$(pwd)}"
SRC="/tmp/gh-aw/base"

# Parse the engine-registry-derived lists from environment variables.
# These must match the values used in save_base_github_folders.sh.
IFS=' ' read -ra FOLDERS <<< "${GH_AW_AGENT_FOLDERS:-}"
IFS=' ' read -ra ROOT_FILES <<< "${GH_AW_AGENT_FILES:-}"

for ITEM in "${FOLDERS[@]+"${FOLDERS[@]}"}" "${ROOT_FILES[@]+"${ROOT_FILES[@]}"}"; do
  if [[ ! "${ITEM}" =~ ^[a-zA-Z0-9_.-]+$ || "${ITEM}" == "." || "${ITEM}" == ".." ]]; then
    echo "Invalid agent configuration item: ${ITEM}" >&2
    exit 1
  fi
done

mkdir -p "${SRC}"
SNAPSHOT_DIR="${SRC}/.restore.$$"
(umask 077; mkdir "${SNAPSHOT_DIR}")
trap 'rm -rf "${SNAPSHOT_DIR}"' EXIT
if [ -f "${SRC}/snapshot.tar" ]; then
  # Validate actual tar metadata, not human-readable listings, before extraction.
  node - "${SRC}/snapshot.tar" "${GH_AW_AGENT_FOLDERS:-}" "${GH_AW_AGENT_FILES:-}" <<'NODE'
const fs = require("node:fs");
const path = require("node:path").posix;
const [archive, folderList, fileList] = process.argv.slice(2);
const folders = new Set(folderList.split(" ").filter(Boolean));
const files = new Set(fileList.split(" ").filter(Boolean));
const entries = new Map();
const fd = fs.openSync(archive, "r");
const archiveSize = fs.fstatSync(fd).size;
const text = (buffer, start, length) => buffer.subarray(start, start + length).toString("utf8").split("\0")[0];
const fail = (message) => { throw new Error("Unsafe base snapshot: " + message); };
const octal = (value) => {
  if (!/^[0-7]+$/.test(value.trim())) fail("invalid numeric header");
  const number = Number.parseInt(value.trim(), 8);
  if (!Number.isSafeInteger(number)) fail("oversized numeric header");
  return number;
};
const safeName = (name) => {
  if (name.startsWith("/") || name.split("/").includes("..")) fail("invalid path " + name);
  return path.normalize(name).replace(/\/$/, "");
};
let offset = 0;
let extended = {};
let global = {};
let terminated = false;
while (offset + 512 <= archiveSize) {
  const header = Buffer.alloc(512);
  if (fs.readSync(fd, header, 0, 512, offset) !== 512) fail("truncated header");
  if (header.every((byte) => byte === 0)) {
    if (offset + 1024 > archiveSize || Object.keys(extended).length) fail("invalid archive terminator");
    const tail = Buffer.alloc(65536);
    for (let position = offset + 512; position < archiveSize;) {
      const count = fs.readSync(fd, tail, 0, Math.min(tail.length, archiveSize - position), position);
      if (!count || !tail.subarray(0, count).every((byte) => byte === 0)) fail("data after archive terminator");
      position += count;
    }
    terminated = true;
    break;
  }
  const checksum = header.reduce((sum, byte, index) => sum + (index >= 148 && index < 156 ? 32 : byte), 0);
  if (checksum !== octal(text(header, 148, 8))) fail("invalid header checksum");
  const type = text(header, 156, 1) || "0";
  let size = octal(text(header, 124, 12));
  const prefix = text(header, 257, 6) === "ustar" ? text(header, 345, 155) : "";
  const headerName = (prefix ? prefix + "/" : "") + text(header, 0, 100);
  if (["x", "g", "L", "K"].includes(type)) {
    if (offset + 512 + size > archiveSize || size > 1024 * 1024) fail("invalid extended header");
    const body = Buffer.alloc(size);
    fs.readSync(fd, body, 0, size, offset + 512);
    if (type === "L" || type === "K") {
      extended[type === "L" ? "path" : "linkpath"] = text(body, 0, size).replace(/\n$/, "");
    } else {
      let cursor = 0;
      while (cursor < size) {
        const space = body.indexOf(32, cursor);
        const digits = body.subarray(cursor, space).toString();
        if (space < cursor || !/^[0-9]+$/.test(digits)) fail("invalid PAX record");
        const length = Number(digits);
        const end = cursor + length;
        if (!Number.isSafeInteger(length) || end > size || end <= space + 1 || body[end - 1] !== 10) fail("invalid PAX length");
        const record = body.subarray(space + 1, end - 1).toString("utf8");
        const equals = record.indexOf("=");
        if (equals < 1) fail("invalid PAX attribute");
        const key = record.slice(0, equals);
        if (key.startsWith("GNU.sparse") || key === "SCHILY.filetype") fail("unsupported extended entry");
        (type === "g" ? global : extended)[key] = record.slice(equals + 1);
        cursor = end;
      }
    }
  } else {
    const metadata = { ...global, ...extended };
    const name = safeName(metadata.path ?? headerName);
    const link = metadata.linkpath ?? text(header, 157, 100);
    if (metadata.size !== undefined) {
      if (!/^[0-9]+$/.test(metadata.size)) fail("invalid PAX size");
      size = Number(metadata.size);
      if (!Number.isSafeInteger(size)) fail("oversized PAX size");
    }
    if (!["0", "5", "2"].includes(type)) fail("unsupported entry " + name);
    if (type !== "0" && size !== 0) fail("non-file entry has content");
    if (name !== "." || type !== "5") {
      const root = name.split("/")[0];
      if ((!folders.has(root) && !files.has(root)) || (files.has(root) && (name !== root || type !== "0"))) fail("unexpected entry " + name);
      if (name === root && folders.has(root) && type !== "5") fail("non-directory configuration root");
      if (entries.has(name)) fail("duplicate entry " + name);
      if (type === "2" && path.isAbsolute(link)) fail("absolute symlink " + name);
      entries.set(name, { type, link });
    }
    extended = {};
  }
  offset += 512 + Math.ceil(size / 512) * 512;
  if (offset > archiveSize) fail("truncated entry");
}
fs.closeSync(fd);
if (!terminated) fail("missing archive terminator");
for (const [name, entry] of entries) {
  let parent = path.dirname(name);
  while (parent !== ".") {
    if (entries.has(parent) && entries.get(parent).type !== "5") fail("non-directory parent of " + name);
    parent = path.dirname(parent);
  }
  if (entry.type !== "2") continue;
  // Resolve symlinks before "..", including intermediate symlink components.
  const parts = name.split("/").slice(0, -1);
  let pending = entry.link.split("/");
  let hops = 0;
  while (pending.length) {
    const component = pending.shift();
    if (component === "" || component === ".") continue;
    if (component === "..") {
      if (parts.length <= 1) fail("symlink escapes restored folder " + name);
      parts.pop();
      continue;
    }
    const target = entries.get([...parts, component].join("/"));
    if (target?.type === "2") {
      if (++hops > 40) fail("cyclic symlink " + name);
      pending = [...target.link.split("/"), ...pending];
    } else {
      parts.push(component);
    }
  }
}
NODE
  tar -xpf "${SRC}/snapshot.tar" -C "${SNAPSHOT_DIR}"
fi

for FOLDER in "${FOLDERS[@]+"${FOLDERS[@]}"}"; do
  SNAPSHOT="${SNAPSHOT_DIR}/${FOLDER}"
  DEST="${WORKSPACE}/${FOLDER}"
  if [ -d "${SNAPSHOT}" ]; then
    rm -rf "${DEST}"
    cp -a "${SNAPSHOT}" "${DEST}"
    echo "Restored ${FOLDER} from base branch snapshot"
  elif [ -e "${DEST}" ] || [ -L "${DEST}" ]; then
    # PR branch injected this directory but base doesn't have it — remove it
    rm -rf "${DEST}"
    echo "Removed PR-injected ${FOLDER} (not present in base branch)"
  else
    echo "No base branch snapshot for ${FOLDER}, skipping"
  fi
done

for FILE in "${ROOT_FILES[@]+"${ROOT_FILES[@]}"}"; do
  SNAPSHOT="${SNAPSHOT_DIR}/${FILE}"
  DEST="${WORKSPACE}/${FILE}"
  if [ -f "${SNAPSHOT}" ]; then
    rm -rf "${DEST}"
    cp -a "${SNAPSHOT}" "${DEST}"
    echo "Restored ${FILE} from base branch snapshot"
  elif [ -e "${DEST}" ] || [ -L "${DEST}" ]; then
    # PR branch injected this file but base doesn't have it — remove it
    rm -rf "${DEST}"
    echo "Removed PR-injected ${FILE} (not present in base branch)"
  else
    echo "No base branch snapshot for ${FILE}, skipping"
  fi
done

# Remove .mcp.json — may contain untrusted MCP server config from the PR branch
if [ -e "${WORKSPACE}/.mcp.json" ] || [ -L "${WORKSPACE}/.mcp.json" ]; then
  rm -rf "${WORKSPACE}/.mcp.json"
  echo "Removed .mcp.json from workspace"
fi
