// @ts-check
import { describe, expect, it, vi } from "vitest";
import { isTransientQueueReadError, readQueueMetadata } from "./work_queue_read_retry.cjs";
import { createWorkQueueLogger } from "./work_queue_logging.cjs";

describe("bounded queue metadata reads", () => {
  it.each([
    { status: 500 },
    { response: { status: 503 } },
    { code: "ETIMEDOUT" },
    { code: "ECONNRESET" },
    { cause: { cause: { code: "UND_ERR_SOCKET" } } },
    ...["UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT"].map(code => ({ cause: { code } })),
  ])("recovers a transient GET without changing its response: %j", async failure => {
    const response = { status: 200, data: { id: "7" } };
    const read = vi.fn().mockRejectedValueOnce(failure).mockResolvedValue(response);
    const sleepFn = vi.fn(async () => {});
    const log = { failure: vi.fn() };
    expect(await readQueueMetadata(read, log, "repository.read", sleepFn)).toBe(response);
    expect(read).toHaveBeenCalledTimes(2);
    expect(sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    expect(log.failure).toHaveBeenCalledExactlyOnceWith("repository.read.retry", failure, true);
  });

  it.each([
    undefined,
    null,
    new Error("other side closed"),
    { code: "EACCES" },
    { code: "ERR_TRANSPORT_INTEGRITY", status: 503 },
    { status: 503, cause: { code: "ERR_TRANSPORT_INTEGRITY" } },
    { status: 503, response: { headers: { "retry-after": "60" } } },
    { status: 403, code: "ETIMEDOUT", response: { status: 503 } },
    { cause: { status: 429, code: "ECONNRESET" } },
    { status: 503, cause: { status: 401 } },
    ...[200, 400, 401, 403, 404, 422, 429, 600, "503"].map(status => ({ status, cause: { code: "UND_ERR_SOCKET" } })),
  ])("does not retry authorization, resource, quota, integrity or unclassified failures: %j", async failure => {
    const read = vi.fn().mockRejectedValue(failure);
    const sleepFn = vi.fn(async () => {});
    const log = { failure: vi.fn() };
    expect(isTransientQueueReadError(failure)).toBe(false);
    await expect(readQueueMetadata(read, log, "repository.read", sleepFn)).rejects.toBe(failure);
    expect(read).toHaveBeenCalledTimes(1);
    expect(sleepFn).not.toHaveBeenCalled();
    expect(log.failure).toHaveBeenCalledExactlyOnceWith("repository.read.failed", failure, true);
  });

  it("stops after three reads and propagates the original error without logging provider data", async () => {
    const failure = Object.assign(new Error("private-token-and-body"), {
      status: 503,
      response: { headers: { authorization: "private-token", "x-github-request-id": "1B41:A4F74:930C3C:1DF33D1:6AD4920C" } },
    });

    const read = vi.fn().mockRejectedValue(failure);
    const sleepFn = vi.fn(async () => {});
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    try {
      await expect(readQueueMetadata(read, createWorkQueueLogger("native"), "repository.read", sleepFn)).rejects.toBe(failure);
      expect(read).toHaveBeenCalledTimes(3);
      expect(sleepFn.mock.calls).toEqual([[1000], [1000]]);
      const output = stderr.mock.calls.map(call => call[0]).join("");
      expect(output).toContain("repository.read.retry");
      expect(output).toContain("repository.read.failed");
      expect(output).toContain("http_status=503");
      expect(output).not.toContain("private");
    } finally {
      stderr.mockRestore();
    }
  });

  it("reports bounded attempt numbers and retry timing even without DEBUG", async () => {
    const failure = { status: 500 };
    const read = vi.fn().mockRejectedValue(failure);
    const log = { failure: vi.fn(), diagnostic: vi.fn() };
    await expect(readQueueMetadata(read, log, "repository.read", async () => {})).rejects.toBe(failure);
    expect(log.diagnostic.mock.calls).toEqual([
      ["repository.read.attempt", { attempt: 1, max_attempts: 3, retry: true, delay_ms: 1000 }],
      ["repository.read.attempt", { attempt: 2, max_attempts: 3, retry: true, delay_ms: 1000 }],
      ["repository.read.attempt", { attempt: 3, max_attempts: 3, retry: false, delay_ms: 0 }],
    ]);
  });

  it("does not retry returned responses or validation and bounds cyclic cause chains", async () => {
    const response = { status: 200, data: { id: "foreign" } };
    const read = vi.fn().mockResolvedValue(response);
    const sleepFn = vi.fn(async () => {});
    expect(await readQueueMetadata(read, { failure: vi.fn() }, "repository.read", sleepFn)).toBe(response);
    expect(read).toHaveBeenCalledTimes(1);
    expect(sleepFn).not.toHaveBeenCalled();
    const cyclic = { status: 503, cause: null };
    cyclic.cause = cyclic;
    expect(isTransientQueueReadError(cyclic)).toBe(false);
  });
});
