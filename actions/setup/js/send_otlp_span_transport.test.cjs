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
    stdout: Buffer.from(headers + "arbitrary collector body\n200\napplication/json"),
    stderr: Buffer.alloc(0),
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
    expect(args.slice(args.indexOf("--dump-header") + 1, args.indexOf("--dump-header") + 2)).toEqual(["-"]);
    expect(spawn.mock.calls[0][2].stdio).toEqual(["pipe", "pipe", "pipe"]);
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

  it.each([0, 200, 401, 403, 503])("preserves known HTTP %s status if the subprocess deadline is reached", async status => {
    spawn.mockReturnValue({
      status: null,
      error: Object.assign(new Error("spawnSync curl ETIMEDOUT"), { code: "ETIMEDOUT" }),
      stdout: Buffer.from(status ? `HTTP/1.1 ${status} Response\r\n\r\n{}\n200\napplication/json` : "untrusted body\n200\napplication/json"),
      stderr: Buffer.alloc(0),
    });
    await expect(sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
    expect(spawn).toHaveBeenCalledTimes(status === 0 || status === 503 ? 3 : 1);
    if (status) expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
    else expect(append.mock.calls[0][1]).not.toContain('"status"');
    expect(JSON.stringify(warn.mock.calls)).toContain(status === 200 ? "acceptance unknown" : status === 401 || status === 403 ? "authorization failed" : "retrying");
  });

  it.each(["", "HTTP/1.1 200 OK\r\nContent-Type: application/json", "HTTP/1.1 100 Continue\r\n\r\n", "HTTP/1.1 302 Found\r\nLocation: /next\r\n\r\nHTTP/1.1 200", "HTTP/1.1 302 Found\r\nLocation: /next\r\n\r\nHTTP/1.1 503"])(
    "never treats unknown or incomplete final status as success: %j",
    async headers => {
      spawn.mockReturnValue(overflow(headers));
      await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 });
      expect(spawn).toHaveBeenCalledTimes(3);
      expect(JSON.stringify(warn.mock.calls)).not.toContain("acceptance unknown");
      expect(append).toHaveBeenCalledOnce();
    }
  );

  it.each([200, 401, 403, 503])("does not interpret HTTP-looking body bytes after final HTTP %s headers", async status => {
    spawn.mockReturnValue(overflow(`HTTP/1.1 ${status} Response\r\n\r\nHTTP/1.1 200 OK\r\n\r\nHTTP/1.1 503`));
    await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 });
    expect(spawn).toHaveBeenCalledTimes(status === 503 ? 3 : 1);
    expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
    expect(JSON.stringify(warn.mock.calls)).toContain(status === 200 ? "acceptance unknown" : status === 503 ? "retrying" : "authorization failed");
  });

  it("uses the final complete response status after informational and redirect headers", async () => {
    spawn.mockReturnValue(overflow("HTTP/1.1 302 Found\r\nLocation: /next\r\n\r\nHTTP/1.1 100 Continue\r\n\r\nHTTP/2 503\r\n\r\n"));
    await sendOTLPSpan("https://collector.invalid", {}, { skipJSONL: true, baseDelayMs: 1 });
    expect(spawn).toHaveBeenCalledTimes(3);
    expect(append.mock.calls[0][1]).toContain('"status":503');
  });

  it.each(["http/json", "http/protobuf"].flatMap(protocol => [false, true].flatMap(redirect => [false, true].map(partial => [protocol, redirect, partial]))))(
    "parses real curl %s response bodies (redirect: %s, partial rejection: %s)",
    async (protocol, redirect, partial) => {
      vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
      const type = protocol === "http/json" ? "application/json" : "application/x-protobuf";
      const body = protocol === "http/json" ? Buffer.from(partial ? '{"partialSuccess":{"rejectedSpans":"2"}}' : "{}") : Buffer.from(partial ? [0x0a, 0x02, 0x08, 0x02] : []);
      const server = new Worker(
        `
        const { parentPort } = require("worker_threads");
        const http = require("http");
        const server = http.createServer((req, res) => {
          req.resume();
          req.on("end", () => {
            if (${redirect} && req.url !== "/final") {
              res.writeHead(307, { Location: "/final" }).end("ignored redirect body");
              return;
            }
            res.writeContinue();
            res.writeHead(200, { "Content-Type": "${type}" });
            res.end(Buffer.from("${body.toString("base64")}", "base64"));
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
        spawn.mockImplementation((command, args, options) => realSpawnSync(command, ["--noproxy", "*", ...args], options));
        await sendOTLPSpan(`http://127.0.0.1:${port}`, {}, { skipJSONL: true, baseDelayMs: 1 });
        expect(spawn).toHaveBeenCalledOnce();
        expect(warn).toHaveBeenCalledTimes(partial ? 1 : 0);
        expect(append).toHaveBeenCalledTimes(partial ? 1 : 0);
        if (partial) expect(append.mock.calls[0][1]).toContain("rejected_spans=2");
      } finally {
        await server.terminate();
      }
    }
  );

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
        expect(result.stdout.toString()).toMatch(new RegExp(`^HTTP/1\\.1 ${status} `));
      }
      expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
      expect(JSON.stringify(warn.mock.calls)).toContain(status === 200 ? "acceptance unknown" : status === 503 ? "retrying" : "authorization failed");
    } finally {
      await server.terminate();
    }
  });

  it.each(["traces", "logs"].flatMap(signal => ["http/json", "http/protobuf"].flatMap(protocol => [0, 200, 401, 403, 503].map(status => [signal, protocol, status]))))(
    "bounds real curl %s %s stalled HTTP %s responses without retrying known acceptance",
    async (signal, protocol, status) => {
      vi.stubEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol);
      const server = new Worker(
        `
        const { parentPort } = require("worker_threads");
        const http = require("http");
        const server = http.createServer((req, res) => {
          req.resume();
          req.on("end", () => {
            if (${status}) {
              res.writeHead(${status}, { "Content-Type": "application/json" });
              res.write("{}");
            }
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
          const index = args.indexOf("--max-time");
          expect(index).toBeGreaterThan(-1);
          expect(Number(args[index + 1]) * 1000).toBeLessThan(options.timeout);
          // Shorten the actual curl deadline, not the production configuration.
          const boundedArgs = [...args];
          boundedArgs[index + 1] = "0.1";
          const result = realSpawnSync(command, ["--noproxy", "*", ...boundedArgs], options);
          results.push(result);
          return result;
        });
        await expect(sendOTLPSpan(`http://127.0.0.1:${port}`, {}, { signal, skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
        expect(results).toHaveLength(status === 0 || status === 503 ? 3 : 1);
        for (const result of results) {
          expect(result.error).toBeUndefined();
          expect(result.status).toBe(28);
        }
        expect(append).toHaveBeenCalledOnce();
        if (status) expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
        else expect(append.mock.calls[0][1]).not.toContain('"status"');
        expect(JSON.stringify(warn.mock.calls)).toContain(status === 200 ? "acceptance unknown" : status === 401 || status === 403 ? "authorization failed" : "retrying");
        if (status === 200) expect(warn.mock.calls[0][0]).toContain(`OTLP ${signal} response diagnostics unavailable`);
      } finally {
        await server.terminate();
      }
    }
  );

  it.each([200, 401, 403, 503])("retains real curl HTTP %s with a 64 KiB body and long Content-Type", async status => {
    const server = new Worker(
      `
      const { parentPort } = require("worker_threads");
      const http = require("http");
      const server = http.createServer((req, res) => {
        req.resume();
        req.on("end", () => {
          res.writeHead(${status}, { "Content-Type": "application/json; private=" + "s".repeat(8192) });
          res.end(" ".repeat(${MAX_OTLP_RESPONSE_BYTES}));
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
        const result = realSpawnSync(command, ["--noproxy", "*", ...args], options);
        results.push(result);
        return result;
      });
      await expect(sendOTLPSpan(`http://127.0.0.1:${port}`, {}, { skipJSONL: true, baseDelayMs: 1 })).resolves.toBeUndefined();
      expect(results).toHaveLength(status === 503 ? 3 : 1);
      for (const result of results) expect(result.error?.code).toBe("ENOBUFS");
      expect(append.mock.calls[0][1]).toContain(`"status":${status}`);
      expect(JSON.stringify(warn.mock.calls)).toContain(status === 200 ? "acceptance unknown" : status === 503 ? "retrying" : "authorization failed");
      expect(JSON.stringify(warn.mock.calls)).not.toContain("private=");
    } finally {
      await server.terminate();
    }
  });
});
