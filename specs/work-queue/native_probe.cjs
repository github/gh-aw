"use strict";

// Local adapter: no tokens, network, publication or workflow execution.
const fs = require("node:fs");
const { createHash } = require("node:crypto");
const readline = require("node:readline");
const { performance } = require("node:perf_hooks");
const queue = require("../../actions/setup/js/work_queue_replay.cjs");
const codec = require("../../actions/setup/js/work_queue_codec.cjs");
const scheduler = require("../../actions/setup/js/work_queue_scheduler.cjs");
const policy = require("../../actions/setup/js/work_queue_policy.cjs");

function execute(input) {
  if (input.action === "canonical") return { canonical: codec.canonical(codec.parseStrictJSON(input.data)) };
  if (input.action === "validate_commit") {
    const commit = codec.parseStrictJSON(input.data);
    queue.validateCommit(commit);
    return { canonical: codec.canonical(commit) };
  }
  if (input.action === "validate_policy") {
    policy.validatePolicy(codec.parseStrictJSON(input.data));
    return { valid: true };
  }
  let data = input.data;
  if (input.ledger_file) {
    if (fs.statSync(input.ledger_file).size > 80 * 1024 * 1024) throw new Error("resource_limit: adapter input exceeds 80 MiB");
    data = fs.readFileSync(input.ledger_file, "utf8");
  }
  let start = performance.now();
  const commits = queue.parseTransactionLog(data);
  const parse_ms = performance.now() - start;
  start = performance.now();
  const state = queue.replayTransactions(commits);
  const cold_replay_ms = performance.now() - start;
  start = performance.now();
  const selection = scheduler.selectionOnly(queue.planNext(state, input.pool, input.at));
  const selection_ms = performance.now() - start;
  start = performance.now();
  const packing = scheduler.planDispatch(state, input.parameters, { requestId: input.request_id, commitId: input.commit_id, at: input.at });
  const packing_ms = performance.now() - start;
  start = performance.now();
  const serialized = queue.serializeTransactionLog(commits);
  const serialization_ms = performance.now() - start;
  const result = {
    selection,
    packing,
    metrics: {
      parse_ms, cold_replay_ms, selection_ms, packing_ms, serialization_ms,
      input_bytes: Buffer.byteLength(data), canonical_bytes: Buffer.byteLength(serialized),
      canonical_sha256: createHash("sha256").update(serialized).digest("hex"),
      rss_peak_bytes: process.resourceUsage().maxRSS * 1024,
      heap_used_bytes: process.memoryUsage().heapUsed,
      stats: state.stats,
    },
  };
  if (input.action !== "benchmark") result.projection = queue.serializeProjection(state);
  if (input.include_canonical) result.canonical_ledger = serialized;
  return result;
}

const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
lines.on("line", line => {
  try {
    process.stdout.write(`${JSON.stringify(execute(JSON.parse(line)))}\n`);
  } catch (error) {
    process.stdout.write(`${JSON.stringify({ error: error.message })}\n`);
  }
});
