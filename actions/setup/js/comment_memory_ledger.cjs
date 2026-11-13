// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { Ledger, buildSegment, configuredLedgerLimits } = require("./ledger_store.cjs");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { appendLedgerRecords } = require("./push_ledger_changes.cjs");
const { loadSafeOutputsConfig, resolveCommentMemoryTarget } = require("./setup_comment_memory_files.cjs");
const { resolveCommentMemoryConfig, COMMENT_MEMORY_MAX_SCAN_PAGES, COMMENT_MEMORY_MAX_FILE_BYTES, COMMENT_MEMORY_MAX_TOTAL_BYTES } = require("./comment_memory_helpers.cjs");
const { buildManagedMemoryBody } = require("./comment_memory.cjs");
const { enforceCommentLimits } = require("./comment_limit_helpers.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { resolveDispatchCredentialPrincipal } = require("./work_queue_dispatch_credential.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");

const LEDGER_TAG = "gh-aw-comment-ledger";

/**
 * Ledger history must not silently lose an unterminated shard or interpret a
 * nested example inside editable memory as a persistence-authored shard.
 * @param {string} body
 * @param {string} ledgerName
 * @returns {Array<{memoryId: string, content: string}>}
 */
function extractCommentLedgerEntries(body, ledgerName) {
  const entries = [];
  const language = `${LEDGER_TAG}:${ledgerName}`;
  let open;
  for (const line of body.split(/\r?\n/)) {
    if (!open) {
      const match = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
      if (!match) continue;
      const info = match[2].trim();
      const selected = info === language || info.startsWith(`${language} `) || info.startsWith(`${language}\t`);
      if (selected && (info !== language || match[1][0] !== "`" || match[1].length < 6)) throw new TypeError("Malformed comment-backed ledger fence");
      open = { fence: match[1], selected, lines: [] };
      continue;
    }
    const closing = /^ {0,3}(`+|~+)[ \t]*$/.exec(line);
    if (closing && closing[1][0] === open.fence[0] && closing[1].length >= open.fence.length) {
      if (open.selected) {
        if (closing[1] !== open.fence) throw new TypeError("Malformed comment-backed ledger fence");
        entries.push({ memoryId: ledgerName, content: open.lines.join("\n").trim() });
      }
      open = undefined;
    } else if (open.selected) {
      if (/^ {0,3}(`{3,}|~{3,})/.test(line)) throw new TypeError("Malformed comment-backed ledger fence");
      open.lines.push(line);
    }
  }
  if (open?.selected) throw new TypeError("Malformed comment-backed ledger fence");
  return entries;
}

function commentConfig() {
  const config = resolveCommentMemoryConfig(loadSafeOutputsConfig());
  if (!config) throw new TypeError("Comment-backed ledgers require tools.comment-memory");
  return config;
}

async function materializeCommentLedger({ githubClient, config, sourceDir, commentMemoryConfig = commentConfig() }) {
  const target = resolveCommentMemoryTarget(commentMemoryConfig);
  if (!target) throw new TypeError("Comment-backed ledger has no target issue/PR");
  let authorId;
  try {
    const credential = { kind: process.env.GH_AW_COMMENT_MEMORY_TOKEN_SOURCE === "github_actions" ? "github_token" : "authenticated" };
    const selectedToken = process.env.GH_AW_COMMENT_MEMORY_TOKEN;
    if (credential.kind !== "authenticated" && !selectedToken) throw new Error("Comment-backed ledger installation identity requires a bound token");
    if (selectedToken) {
      githubClient = await createAuthenticatedGitHubClient({ "github-token": selectedToken });
      const authentication = await githubClient.auth({ type: "token" });
      if (authentication?.type !== "token" || authentication.token !== selectedToken) throw new Error("Comment-backed ledger credential does not match the selected client");
    }
    authorId = await resolveDispatchCredentialPrincipal(githubClient, credential);
  } catch (error) {
    throw new Error("Cannot verify the authenticated comment-backed ledger writer", { cause: error });
  }
  const shardDir = path.join(sourceDir, "ledger", "shards");
  fs.mkdirSync(shardDir, { recursive: true });
  let totalBytes = 0;
  for (let page = 1; page <= COMMENT_MEMORY_MAX_SCAN_PAGES; page++) {
    const { data } = await githubClient.rest.issues.listComments({ owner: target.owner, repo: target.repo, issue_number: target.number, per_page: 100, page });
    if (!Array.isArray(data)) throw new TypeError("Invalid comment-backed ledger response");
    for (const comment of data) {
      if (String(comment.user?.id) !== authorId) continue;
      if (typeof comment.body !== "string" || !comment.body.includes("<!-- gh-aw-agentic-workflow:")) continue;
      const entries = extractCommentLedgerEntries(comment.body, config.name);
      let batchBytes = 0;
      for (const entry of entries) {
        if (entry.memoryId !== config.name) continue;
        const bytes = Buffer.byteLength(entry.content) + 1;
        batchBytes += bytes;
        totalBytes += bytes;
        if (batchBytes > COMMENT_MEMORY_MAX_FILE_BYTES || totalBytes > COMMENT_MEMORY_MAX_TOTAL_BYTES) throw new RangeError("Comment-backed ledger exceeds comment-memory size limits; use git storage for larger ledgers");
        let segment;
        try {
          segment = buildSegment([...iterateJsonl(entry.content, { strict: true })], { maxSegmentBytes: config.max_segment_kb * 1024 });
        } catch (error) {
          throw new TypeError("Invalid comment-backed ledger JSONL shard", { cause: error });
        }
        const file = path.join(shardDir, `${segment.id}.jsonl`);
        if (fs.existsSync(file)) {
          if (!fs.readFileSync(file).equals(segment.content)) throw new TypeError("Conflicting comment-backed ledger shard");
        } else {
          fs.writeFileSync(file, segment.content, { flag: "wx", mode: 0o600 });
        }
      }
    }
    if (data.length < 100) return { target, totalBytes, githubClient };
  }
  throw new RangeError("Comment-backed ledger scan limit reached; refusing incomplete history");
}

async function persistCommentLedgerAppends({ appends, config, githubClient, commentMemoryConfig = commentConfig() }) {
  if (isStagedMode(commentMemoryConfig)) return { persisted: 0, already_present: 0, reconciled: 0, staged: true };
  const sourceDir = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-"));
  let ledger;
  try {
    const { target, totalBytes, githubClient: commentClient } = await materializeCommentLedger({ githubClient, config, sourceDir, commentMemoryConfig });
    const shardDir = path.join(sourceDir, "ledger", "shards");
    const previousFiles = new Set(fs.readdirSync(shardDir));
    const limits = configuredLedgerLimits(config, appends.length);
    ledger = new Ledger({ memoryDir: sourceDir, maxRecordBytes: limits.maxRecordBytes, maxSegmentBytes: config.max_segment_kb * 1024, maxPatchBytes: limits.maxPatchBytes });
    const result = appendLedgerRecords(ledger, appends, config);
    ledger.close();
    if (!result.persisted) return result;
    // Each batch creates an immutable comment, so independent appends never overwrite history.
    const contents = fs
      .readdirSync(shardDir)
      .filter(file => !previousFiles.has(file) && file.endsWith(".jsonl"))
      .sort()
      .map(file => fs.readFileSync(path.join(shardDir, file), "utf8"));
    const bytes = contents.reduce((total, content) => total + Buffer.byteLength(content), 0);
    if (bytes > COMMENT_MEMORY_MAX_FILE_BYTES || totalBytes + bytes > COMMENT_MEMORY_MAX_TOTAL_BYTES) throw new RangeError("Comment-backed ledger exceeds comment-memory size limits; use git storage for larger ledgers");
    /** @type {import("./report_ast.cjs").ReportBlock[]} */
    const blocks = contents.map(content => ({ type: "code_block", language: `${LEDGER_TAG}:${config.name}`, fenceLength: 6, value: content, agentTainted: true, sizeProtected: true }));
    const body = buildManagedMemoryBody("", config.name, {
      includeFooter: false,
      runUrl: buildWorkflowRunUrl(context, context.repo),
      workflowName: process.env.GH_AW_WORKFLOW_NAME || "Workflow",
      memoryTag: LEDGER_TAG,
      memoryBlock: blocks.length === 1 ? blocks[0] : { type: "details", summary: "JSONL shards", agentTainted: false, children: blocks },
    });
    const published = extractCommentLedgerEntries(body, config.name);
    if (published.length !== contents.length || published.some((entry, index) => entry.memoryId !== config.name || entry.content !== contents[index].trimEnd()))
      throw new TypeError("Comment-backed ledger JSONL was changed by publication sanitization; refusing to publish altered records");
    enforceCommentLimits(body);
    const { data } = await commentClient.rest.issues.createComment({ owner: target.owner, repo: target.repo, issue_number: target.number, body });
    return { ...result, url: data.html_url };
  } finally {
    ledger?.close();
    fs.rmSync(sourceDir, { recursive: true, force: true });
  }
}

module.exports = { LEDGER_TAG, materializeCommentLedger, persistCommentLedgerAppends };
