import { afterEach, describe, expect, it, vi } from "vitest";
import childProcess from "node:child_process";
import { EventEmitter } from "node:events";
import { PassThrough, Writable } from "node:stream";

const { createQueueGitObjectReader } = require("./work_queue_git_objects.cjs");
const readers = [];
afterEach(() => {
  for (const reader of readers.splice(0)) reader.close();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function fixture(reply) {
  const child = new EventEmitter();
  child.stdout = new PassThrough();
  child.stdin = new Writable({
    write(chunk, encoding, callback) {
      queueMicrotask(() => reply(chunk.toString("utf8").trim(), child));
      callback();
    },
  });
  child.unref = vi.fn();
  child.kill = vi.fn();
  const spawn = vi.spyOn(childProcess, "spawn").mockReturnValue(child);
  const reader = createQueueGitObjectReader(["--git-dir", "/offline/queue"], { GIT_NO_LAZY_FETCH: "1" });
  readers.push(reader);
  return { reader, child, spawn };
}

describe("bounded queue Git object stream", () => {
  const oid = "a".repeat(40);

  it("serializes concurrent objects through one helper and preserves fragmented binary contents", async () => {
    const bytes = Buffer.from([0, 10, 255, 13]);
    const { reader, spawn, child } = fixture((id, child) => {
      child.stdout.write(`${id} blob `);
      child.stdout.write(`${bytes.length}\n`);
      child.stdout.write(bytes.subarray(0, 2));
      child.stdout.write(Buffer.concat([bytes.subarray(2), Buffer.from("\n")]));
    });
    const values = await Promise.all([reader.object(oid, "blob"), reader.object("b".repeat(64), "blob")]);
    expect(values).toEqual([bytes, bytes]);
    expect(spawn).toHaveBeenCalledExactlyOnceWith("git", ["--git-dir", "/offline/queue", "cat-file", "--batch"], { env: { GIT_NO_LAZY_FETCH: "1" }, stdio: ["pipe", "pipe", "ignore"] });
    reader.close();
    expect(child.kill).toHaveBeenCalled();
    await expect(reader.object(oid, "blob")).rejects.toMatchObject({ code: "git_transport_failed" });
  });

  it.each([
    id => `${id} missing\n`,
    id => `${id} tree 1\nx\n`,
    () => `${"b".repeat(40)} blob 1\nx\n`,
    id => `${id} blob 83886081\n`,
    id => `${id} blob 01\nx\n`,
    id => `${id} blob 1\nx!`,
    id => `${id} blob 1\nx\nextra`,
    () => "x".repeat(129),
  ])("rejects malformed, missing, oversized or wrong-type objects without restarting", async response => {
    const { reader, spawn, child } = fixture((id, child) => child.stdout.write(response(id)));
    await expect(reader.object(oid, "blob")).rejects.toMatchObject({ code: "ledger_invalid" });
    await expect(reader.object(oid, "blob")).rejects.toMatchObject({ code: "git_transport_failed" });
    expect(child.kill).toHaveBeenCalled();
    expect(spawn).toHaveBeenCalledTimes(1);
  });

  it.each(["error", "exit", "stdin", "close", "timeout"])("rejects pending and queued reads on %s without exposing process data", async cause => {
    vi.useFakeTimers();
    const { reader, child } = fixture(() => {});
    const first = reader.object(oid, "blob");
    const second = reader.object(oid, "blob");
    const checked = Promise.all([expect(first).rejects.toMatchObject({ code: "git_transport_failed" }), expect(second).rejects.toMatchObject({ code: "git_transport_failed" })]);
    await Promise.resolve();
    if (cause === "timeout") await vi.advanceTimersByTimeAsync(120000);
    else if (cause === "stdin") child.stdin.emit("error", new Error("private credentials"));
    else if (cause === "close") reader.close();
    else child.emit(cause, new Error("private credentials"));
    await checked;
    expect(child.stdout.listenerCount("data")).toBe(0);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("rejects untrusted object selectors before starting Git", async () => {
    const { reader, spawn } = fixture(() => {});
    for (const [id, type] of [
      [`${oid}\nHEAD`, "blob"],
      [oid, "blob\nHEAD"],
      ["HEAD", "commit"],
    ])
      await expect(reader.object(id, type)).rejects.toMatchObject({ code: "ledger_invalid" });
    expect(spawn).not.toHaveBeenCalled();
  });
});
