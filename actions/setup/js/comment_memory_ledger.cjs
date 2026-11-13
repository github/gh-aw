// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { Ledger, buildSegment, configuredLedgerLimits } = require("./ledger_store.cjs");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { appendLedgerRecords } = require("./push_ledger_changes.cjs");
const { loadSafeOutputsConfig, resolveCommentMemoryTarget } = require("./setup_comment_memory_files.cjs");
const { resolveCommentMemoryConfig, extractCommentMemoryEntries, COMMENT_MEMORY_MAX_SCAN_PAGES, COMMENT_MEMORY_MAX_FILE_BYTES, COMMENT_MEMORY_MAX_TOTAL_BYTES } = require("./comment_memory_helpers.cjs");
const { buildManagedMemoryBody } = require("./comment_memory.cjs");
const { enforceCommentLimits } = require("./comment_limit_helpers.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

const LEDGER_TAG = "gh-aw-comment-ledger";

function commentConfig() {
  const config = resolveCommentMemoryConfig(loadSafeOutputsConfig());
  if (!config) throw new TypeError("Comment-backed ledgers require tools.comment-memory");
  return config;
}

async function materializeCommentLedger({ githubClient, config, sourceDir, commentMemoryConfig = commentConfig() }) {
  const target = resolveCommentMemoryTarget(commentMemoryConfig);
  if (!target) throw new TypeError("Comment-backed ledger has no target issue/PR");
  const shardDir = path.join(sourceDir, "ledger", "shards");
  fs.mkdirSync(shardDir, { recursive: true });
  let totalBytes = 0;
  for (let page = 1; page <= COMMENT_MEMORY_MAX_SCAN_PAGES; page++) {
    const { data } = await githubClient.rest.issues.listComments({ owner: target.owner, repo: target.repo, issue_number: target.number, per_page: 100, page });
    if (!Array.isArray(data)) throw new TypeError("Invalid comment-backed ledger response");
    for (const comment of data) {
      if (typeof comment.body !== "string" || !comment.body.includes("<!-- gh-aw-agentic-workflow:")) continue;
      const entries = extractCommentMemoryEntries(comment.body, message => core.warning(message), LEDGER_TAG);
      for (const entry of entries) {
        if (entry.memoryId !== config.name) continue;
        const bytes = Buffer.byteLength(entry.content);
        totalBytes += bytes;
        if (bytes > COMMENT_MEMORY_MAX_FILE_BYTES || totalBytes > COMMENT_MEMORY_MAX_TOTAL_BYTES) throw new RangeError("Comment-backed ledger exceeds comment-memory size limits; use git storage for larger ledgers");
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
    if (data.length < 100) return { target, totalBytes };
  }
  throw new RangeError("Comment-backed ledger scan limit reached; refusing incomplete history");
}

async function persistCommentLedgerAppends({ appends, config, githubClient, commentMemoryConfig = commentConfig() }) {
  if (isStagedMode(commentMemoryConfig)) return { persisted: 0, already_present: 0, reconciled: 0, staged: true };
  const sourceDir = fs.mkdtempSync(path.join(os.tmpdir(), "comment-ledger-"));
  let ledger;
  try {
    const { target, totalBytes } = await materializeCommentLedger({ githubClient, config, sourceDir, commentMemoryConfig });
    const limits = configuredLedgerLimits(config, appends.length);
    ledger = new Ledger({ memoryDir: sourceDir, maxRecordBytes: limits.maxRecordBytes, maxSegmentBytes: config.max_segment_kb * 1024, maxPatchBytes: limits.maxPatchBytes });
    const result = appendLedgerRecords(ledger, appends, config);
    ledger.close();
    if (!result.persisted) return result;
    // Each batch creates an immutable comment, so independent appends never overwrite history.
    const content = fs.readFileSync(ledger.writerPath, "utf8");
    const bytes = Buffer.byteLength(content);
    if (bytes > COMMENT_MEMORY_MAX_FILE_BYTES || totalBytes + bytes > COMMENT_MEMORY_MAX_TOTAL_BYTES) throw new RangeError("Comment-backed ledger exceeds comment-memory size limits; use git storage for larger ledgers");
    const body = buildManagedMemoryBody(content, config.name, {
      includeFooter: false,
      runUrl: buildWorkflowRunUrl(context, context.repo),
      workflowName: process.env.GH_AW_WORKFLOW_NAME || "Workflow",
      memoryTag: LEDGER_TAG,
      memoryBlock: { type: "code_block", language: `${LEDGER_TAG}:${config.name}`, fenceLength: 6, value: content, agentTainted: true, sizeProtected: true },
    });
    const [published] = extractCommentMemoryEntries(body, undefined, LEDGER_TAG);
    if (published?.content !== content.trimEnd()) throw new TypeError("Comment-backed ledger JSONL was changed by publication sanitization; refusing to publish altered records");
    enforceCommentLimits(body);
    const { data } = await githubClient.rest.issues.createComment({ owner: target.owner, repo: target.repo, issue_number: target.number, body });
    return { ...result, url: data.html_url };
  } finally {
    ledger?.close();
    fs.rmSync(sourceDir, { recursive: true, force: true });
  }
}

module.exports = { LEDGER_TAG, materializeCommentLedger, persistCommentLedgerAppends };
