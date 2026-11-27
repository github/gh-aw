"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { main, MAX_BYTES, MAX_LINES } = require("./collect_work_queue_intents.cjs");

function fixture(callback) {
  const temporaryRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-work-queue-collection-"));
  try {
    const root = fs.realpathSync(temporaryRoot);
    const sourceDir = path.join(root, "source");
    const outputDir = path.join(root, "output");
    fs.mkdirSync(sourceDir, { recursive: true });
    fs.mkdirSync(outputDir);
    const failures = [];
    callback({ root, sourceDir, outputDir, failures, core: { setFailed: value => failures.push(value) } });
  } finally {
    fs.rmSync(temporaryRoot, { recursive: true, force: true });
  }
}

function registerTests({ describe, it }) {
  describe("bounded queue-control artifact collection", () => {
    it("uses canonical system-temp fixture paths and cleans callback failures", () => {
      /** @type {string | undefined} */
      let root;
      assert.throws(
        () =>
          fixture(options => {
            root = options.root;
            assert.ok(root);
            assert.equal(path.dirname(root), fs.realpathSync(os.tmpdir()));
            assert.ok(path.basename(root).startsWith("gh-aw-work-queue-collection-"));
            assert.equal(options.sourceDir, path.join(root, "source"));
            assert.equal(options.outputDir, path.join(root, "output"));
            throw new Error("expected callback failure");
          }),
        /expected callback failure/
      );
      assert.ok(root);
      assert.equal(fs.existsSync(root), false);
    });

    it("cleans its allocated fixture when directory setup fails", () => {
      const mkdir = fs.mkdirSync;
      /** @type {string | undefined} */
      let root;
      let called = false;
      try {
        fs.mkdirSync = directory => {
          root = path.dirname(directory);
          throw new Error("expected setup failure");
        };
        assert.throws(
          () =>
            fixture(() => {
              called = true;
            }),
          /expected setup failure/
        );
      } finally {
        fs.mkdirSync = mkdir;
      }
      assert.equal(called, false);
      assert.ok(root);
      assert.equal(fs.existsSync(root), false);
    });

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
