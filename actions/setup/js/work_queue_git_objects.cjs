// @ts-check
"use strict";

const childProcess = require("node:child_process");
const { queueError } = require("./work_queue_codec.cjs");

const MAX_OBJECT_BYTES = 80 * 1024 * 1024;
const OID = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/;

function createQueueGitObjectReader(args, env) {
  /** @type {import("node:child_process").ChildProcessByStdio<import("node:stream").Writable, import("node:stream").Readable, null> | undefined} */
  let child;
  let closed = false;
  let tail = Promise.resolve();
  /** @type {((error: Error) => void) | undefined} */
  let rejectPending;

  function failure() {
    return queueError("git_transport_failed", "queue Git object reader failed; check the complete queue history and repository access");
  }

  function close() {
    closed = true;
    rejectPending?.(failure());
    child?.kill();
  }

  /** @returns {Promise<Buffer>} */
  async function read(oid, type) {
    if (closed) throw failure();
    if (!child) {
      child = childProcess.spawn("git", [...args, "cat-file", "--batch"], { env, stdio: ["pipe", "pipe", "ignore"] });
      child.on("error", () => {
        closed = true;
        rejectPending?.(failure());
      });
      child.on("exit", () => {
        closed = true;
        rejectPending?.(failure());
      });
      child.unref();
      // The bounded request timer keeps active reads alive, not an idle helper.
      for (const stream of [child.stdin, child.stdout]) if ("unref" in stream && typeof stream.unref === "function") stream.unref();
      child.stdin.on("error", () => close());
    }
    const process = child;
    return new Promise((resolve, reject) => {
      /** @type {Buffer[]} */
      const chunks = [];
      let bytes = 0;
      /** @type {number | undefined} */
      let size;
      let header = Buffer.alloc(0);
      const timer = setTimeout(() => {
        rejectPending?.(failure());
        close();
      }, 120000);
      /** @param {Error | undefined} error @param {Buffer} [value] */
      function finish(error, value = undefined) {
        clearTimeout(timer);
        process.stdout.removeListener("data", receive);
        rejectPending = undefined;
        if (error) reject(error);
        else if (value) resolve(value);
        else reject(failure());
      }
      function invalid() {
        finish(queueError("ledger_invalid", "queue Git object has an invalid identity, type or size"));
        close();
      }
      function receive(chunk) {
        if (size === undefined) {
          header = Buffer.concat([header, chunk]);
          const end = header.indexOf(10);
          if (end === -1) {
            if (header.length > 128) invalid();
            return;
          }
          const expected = new RegExp(`^${oid} ${type} (0|[1-9][0-9]*)$`).exec(header.subarray(0, end).toString("ascii"));
          if (!expected || end > 128) return invalid();
          size = Number(expected[1]);
          if (!Number.isSafeInteger(size) || size > MAX_OBJECT_BYTES) return invalid();
          chunk = header.subarray(end + 1);
          header = Buffer.alloc(0);
        }
        bytes += chunk.length;
        if (bytes > size + 1) return invalid();
        chunks.push(chunk);
        if (bytes === size + 1) {
          const contents = Buffer.concat(chunks, bytes);
          if (contents[size] !== 10) return invalid();
          finish(undefined, contents.subarray(0, size));
        }
      }
      rejectPending = error => finish(error);
      process.stdout.on("data", receive);
      process.stdin.write(`${oid}\n`);
    });
  }

  function object(oid, type) {
    if (!OID.test(oid) || !["commit", "tree", "blob"].includes(type)) return Promise.reject(queueError("ledger_invalid", "invalid queue Git object identity or type"));
    const result = tail.then(() => read(oid, type));
    tail = result.then(
      () => undefined,
      () => undefined
    );
    return result;
  }

  return { object, close };
}

module.exports = { createQueueGitObjectReader };
