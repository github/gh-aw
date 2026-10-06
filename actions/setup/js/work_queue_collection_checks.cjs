"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { main, MAX_BYTES, MAX_LINES } = require("./collect_work_queue_intents.cjs");
const { readClaimControlMessages } = require("./work_queue_control_delivery.cjs");
const { verifyClaimDelivery } = require("./work_queue_delivery.cjs");

function fixture(callback) {
  const root = path.join(process.cwd(), ".queue-validation-cache", `collection-${randomUUID()}`);
  const sourceDir = path.join(root, "source");
  const outputDir = path.join(root, "output");
  fs.mkdirSync(sourceDir, { recursive: true });
  fs.mkdirSync(outputDir);
  const failures = [];
  try {
    callback({ root, sourceDir, outputDir, failures, core: { setFailed: value => failures.push(value) } });
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}

function registerTests({ describe, it }) {
  describe("bounded queue-control artifact collection", () => {
    it("reports missing runtime temporary-directory configuration before touching artifacts", () => {
      fixture(options => {
        const previous = process.env.RUNNER_TEMP;
        delete process.env.RUNNER_TEMP;
        try {
          assert.throws(() => main({ ...options, sourceDir: undefined }), /work_queue_collection_runner_temp_missing/);
        } finally {
          if (previous === undefined) delete process.env.RUNNER_TEMP;
          else process.env.RUNNER_TEMP = previous;
        }
      });
    });

    it("transports exact intent and finish bytes without publishing or fabricating authority", () => {
      fixture(options => {
        for (const filename of ["work-queue.intents.jsonl", "work-queue.finish.jsonl"]) {
          const data = `{"version":3,"intent_id":"i","kind":"finish","parameters":{"outcome":"completed"},"claim_handle":"h1"}\n`;
          fs.writeFileSync(path.join(options.sourceDir, filename), data);
          fs.writeFileSync(path.join(options.sourceDir, "unrelated.json"), "not transported");
        }
        assert.equal(main(options).length, 2);
        assert.equal(options.failures.length, 0);
        for (const filename of fs.readdirSync(options.outputDir)) {
          assert.deepEqual(fs.readFileSync(path.join(options.outputDir, filename)), fs.readFileSync(path.join(options.sourceDir, filename)));
        }
        assert.deepEqual(main({ ...options, sourceDir: path.join(options.root, "absent") }), []);
      });
    });

    it("rejects symlinks and linked destinations while retaining valid sibling transport", () => {
      fixture(options => {
        const protectedPath = path.join(options.root, "protected");
        fs.writeFileSync(protectedPath, "untouched");
        fs.symlinkSync(protectedPath, path.join(options.sourceDir, "work-queue.intents.jsonl"));
        fs.writeFileSync(path.join(options.sourceDir, "work-queue.finish.jsonl"), "{}\n");
        assert.deepEqual(
          main(options).map(value => value.status),
          ["rejected", "collected"]
        );
        assert.equal(fs.readFileSync(protectedPath, "utf8"), "untouched");
        fs.unlinkSync(path.join(options.sourceDir, "work-queue.intents.jsonl"));
        fs.writeFileSync(path.join(options.sourceDir, "work-queue.intents.jsonl"), "{}\n");
        fs.linkSync(protectedPath, path.join(options.outputDir, "work-queue.intents.jsonl"));
        assert.deepEqual(
          main(options).map(value => value.status),
          ["rejected", "collected"]
        );
        assert.equal(fs.readFileSync(protectedPath, "utf8"), "untouched");
      });
    });

    it("accounts for per-Claim staged controls instead of falsely proving a no-write Result", async () => {
      let directory;
      let control;
      fixture(options => {
        directory = options.root;
        const assignment = {
          version: 3,
          dispatch_id: "d",
          request_id: "r",
          commit_id: "c",
          policy_epoch: "p",
          pool: "default",
          worker_profile: "default",
          claims: ["h1", "h2"].map(handle => ({ handle, claim_id: `c:${handle}`, work_id: `w:${handle}`, work: { kind: "data" }, result_refs: [] })),
        };
        const filename = path.join(options.sourceDir, "work-queue.intents.jsonl");
        fs.writeFileSync(
          filename,
          [
            '{"version":3,"intent_id":"valid","kind":"submit","parameters":{"nodes":[]},"claim_handle":"h1"}',
            '{"version":3,"intent_id":"invalid","kind":"submit","parameters":{},"claim_handle":null}',
            '{"version":3,"intent_id":"missing","kind":"dispatch_next","parameters":{}}',
          ].join("\n")
        );
        control = readClaimControlMessages(assignment, filename);
        assert.equal(control.messages.length, 1);
        assert.equal(control.messages[0].claim_handle, "h1");
        assert.equal(control.errors.length, 2);
      });
      assert.equal(fs.existsSync(directory), false);
      const assignment = {
        version: 3,
        dispatch_id: "d",
        request_id: "r",
        commit_id: "c",
        policy_epoch: "p",
        pool: "default",
        worker_profile: "default",
        claims: ["h1", "h2"].map(handle => ({ handle, claim_id: `c:${handle}`, work_id: `w:${handle}`, work: { effect_contract: { kind: "none" } }, result_refs: [] })),
      };
      const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
      assert.equal((await verifyClaimDelivery({ assignment, claim_handle: "h1", messages: control.messages, authorize })).verification, "unknown");
      assert.equal((await verifyClaimDelivery({ assignment, claim_handle: "h2", authorize })).verification, "verified");
    });

    it("rejects directories, byte overflow and count overflow before trusted ingestion", () => {
      fixture(options => {
        const filename = path.join(options.sourceDir, "work-queue.intents.jsonl");
        fs.mkdirSync(filename);
        assert.equal(main(options)[0].status, "rejected");
        fs.rmdirSync(filename);
        fs.writeFileSync(filename, Buffer.alloc(MAX_BYTES + 1));
        assert.equal(main(options)[0].status, "rejected");
        fs.writeFileSync(filename, "{}\n".repeat(MAX_LINES + 1));
        assert.equal(main(options)[0].status, "rejected");
        assert.equal(fs.existsSync(path.join(options.outputDir, "work-queue.intents.jsonl")), false);
      });
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
