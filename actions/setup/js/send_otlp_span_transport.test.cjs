import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { Worker } from "worker_threads";
import childProcess from "child_process";
import fs from "fs";

const require = createRequire(import.meta.url);
const { sendOTLPSpan } = require("./send_otlp_span.cjs");
const { MAX_OTLP_RESPONSE_BYTES } = require("./otlp_response.cjs");
// Preload the lazy encoder before the readFileSync mock prevents Node from loading it.
require("./otlp_protobuf.cjs");
const realSpawnSync = childProcess.spawnSync;
let spawn, warn, append;

beforeEach(() => {
  for (const key of ["HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"]) vi.stubEnv(key, "");
  vi.stubEnv("HTTPS_PROXY", "http://proxy.invalid");
  vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/json");
  vi.spyOn(fs, "mkdirSync").mockImplementation(() => {});
  vi.spyOn(fs, "readFileSync").mockImplementation(() => {
    throw new Error("No prior failures");
  });
  vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});
  append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  spawn = vi.spyOn(childProcess, "spawnSync");
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
});

function overflow(headers) {
  return {
    status: null,
    error: Object.assign(new Error("spawnSync curl ENOBUFS"), { code: "ENOBUFS" }),
    // A truncated body is not a trusted status channel, even with a valid suffix.
    stdout: Buffer.from("arbitrary collector body\n200\napplication/json"),
    stderr: Buffer.alloc(0),
    output: [null, null, null, Buffer.from(headers)],
  };
}

describe("bounded curl response status", () => {
  it.each(["traces", "logs"].flatMap(signal => ["http/json", "http/protobuf"].map(protocol => [signal, protocol])))("does not retry oversized known-2xx %s %s exports", async (signal, protocol) => {
    vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
    spawn.mockReturnValue(overflow("HTTP/2 200\r\ncontent-type: application/json\r\n\r\n"));
    await expect(sendOTLPSpan("https://collector.invalid", {}, { signal, skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
    expect(spawn).toHaveBeenCalledOnce();
    expect(warn.mock.calls[0][0]).toContain("acceptance unknown");
    expect(append.mock.calls[0][1]).toContain('"status":200');
    const args = spawn.mock.calls[0][1];
    expect(args).toContain("--suppress-connect-headers");
    expect(args.slice(args.indexOf("--dump-header") + 1, args.indexOf("--dump-header") + 2)).toEqual(["/dev/fd/3"]);
    expect(spawn.mock.calls[0][2].stdio).toEqual(["pipe", "pipe", "pipe", "pipe"]);
    expect(spawn.mock.calls[0][2].maxBuffer).toBeLessThan(2 * MAX_OTLP_RESPONSE_BYTES);
  });

  it.each([401, 403, 503])("preserves oversized HTTP %s retry policy", async status => {
    spawn.mockReturnValue(overflow(`HTTP/1.1 ${status} Error\r\nContent-Type: text/plain\r\n\r\n`));
    await expect(sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
    expect(spawn).toHaveBeenCalledTimes(status === 503 ? 3 : 1);
    expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
    expect(warn.mock.calls[0][0]).toContain(status === 503 ? "retrying" : "authorization failed");
    expect(JSON.stringify(warn.mock.calls)).not.toContain("acceptance unknown");
  });

  it.each(["", "HTTP/1.1 200 OK\r\nContent-Type: application/json", "HTTP/1.1 100 Continue\r\n\r\n", "HTTP/1.1 302 Found\r\nLocation: /next\r\n\r\nHTTP/1.1 200", "HTTP/1.1 200 OK\r\n\r\nHTTP/1.1 503"])(
    "never treats unknown or incomplete final status as success: %j",
    async headers => {
      spawn.mockReturnValue(overflow(headers));
      await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 });
      expect(spawn).toHaveBeenCalledTimes(3);
      expect(JSON.stringify(warn.mock.calls)).not.toContain("acceptance unknown");
      expect(append).toHaveBeenCalledOnce();
    }
  );

  it("uses the final complete response status after informational and redirect headers", async () => {
    spawn.mockReturnValue(overflow("HTTP/1.1 302 Found\r\nLocation: /next\r\n\r\nHTTP/1.1 100 Continue\r\n\r\nHTTP/2 503\r\n\r\n"));
    await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 });
    expect(spawn).toHaveBeenCalledTimes(3);
    expect(append.mock.calls[0][1]).toContain('"status":503');
  });

  it.each([200, 401, 403, 503].flatMap(status => [false, true].map(forceOverflow => [status, forceOverflow])))("retains real curl HTTP %s status on a chunked oversized body (force ENOBUFS: %s)", async (status, forceOverflow) => {
    // The server must run outside this thread because spawnSync blocks its loop.
    const server = new Worker(
      `
      const { parentPort } = require("worker_threads");
      const http = require("http");
      const server = http.createServer((req, res) => {
        req.resume();
        req.on("end", () => {
          res.writeHead(${status}, { "Content-Type": "application/json" });
          res.end(" ".repeat(2 * 1024 * 1024));
        });
      });
      server.listen(0, "127.0.0.1", () => parentPort.postMessage(server.address().port));
      `,
      { eval: true }
    );
    try {
      const port = await new Promise((resolve, reject) => {
        server.once("message", resolve);
        server.once("error", reject);
      });
      const results = [];
      spawn.mockImplementation((command, args, options) => {
        // Emulate curl versions that don't enforce max-filesize for chunked
        // bodies, ensuring Node's real ENOBUFS path is exercised on modern curl.
        const index = args.indexOf("--max-filesize");
        const boundedArgs = forceOverflow ? [...args.slice(0, index), ...args.slice(index + 2)] : args;
        const result = realSpawnSync(command, ["--noproxy", "*", ...boundedArgs], options);
        results.push(result);
        return result;
      });
      await expect(sendOTLPSpan(`http://127.0.0.1:${port}`, {}, { skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
      expect(results).toHaveLength(status === 503 ? 3 : 1);
      for (const result of results) {
        if (forceOverflow) expect(result.error?.code).toBe("ENOBUFS");
        else expect(result.status).toBe(63);
        expect(result.output[3].toString()).toContain(` ${status} `);
      }
      expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
      expect(JSON.stringify(warn.mock.calls)).toContain(status === 200 ? "acceptance unknown" : status === 503 ? "retrying" : "authorization failed");
    } finally {
      await server.terminate();
    }
  });
});
