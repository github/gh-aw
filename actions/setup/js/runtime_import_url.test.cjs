import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import dns from "node:dns";
import http from "node:http";
import https from "node:https";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import crypto from "node:crypto";

const { isPublicImportAddress, validateImportUrl, readPublicImportUrl } = require("./runtime_import_url.cjs");
const { fetchUrlContent, processRuntimeImport } = require("./runtime_import.cjs");

describe("runtime import URL boundaries", () => {
  let tempDir;
  let lookup;
  let get;
  let response;
  let request;
  let destination;

  beforeEach(() => {
    destination = undefined;
    global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn() };
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "public-import-test-"));
    response = new PassThrough();
    response.statusCode = 200;
    request = new EventEmitter();
    lookup = vi.spyOn(dns, "lookup").mockImplementation((_host, _options, callback) => {
      callback(null, [{ address: "93.184.216.34", family: 4 }]);
    });
    get = vi.spyOn(http, "get").mockImplementation((url, options, callback) => {
      queueMicrotask(() => {
        options.lookup(url.hostname, { all: true }, (error, addresses) => {
          if (error) {
            request.emit("error", error);
            return;
          }
          destination = addresses;
          callback(response);
        });
      });
      options.signal.addEventListener(
        "abort",
        () => {
          const error = new Error("request aborted");
          request.emit("error", error);
          response.destroy(destination ? error : undefined);
        },
        { once: true }
      );
      return request;
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
    response.destroy();
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  it.each([
    "0.0.0.0",
    "10.1.2.3",
    "100.64.1.2",
    "127.0.0.1",
    "169.254.169.254",
    "172.16.0.1",
    "192.0.0.8",
    "192.168.1.2",
    "192.0.2.1",
    "192.88.99.1",
    "198.18.0.1",
    "198.51.100.1",
    "203.0.113.1",
    "224.0.0.1",
    "255.255.255.255",
    "168.63.129.16",
    "::",
    "::1",
    "::ffff:127.0.0.1",
    "::ffff:7f00:1",
    "::ffff:93.184.216.34",
    "fd00:ec2::254",
    "fc00::1",
    "fe80::1",
    "ff02::1",
    "64:ff9b::a00:1",
    "2001::a00:1",
    "2002:7f00:1::",
    "2001:db8::1",
    "3fff::1",
  ])("rejects non-public or special-purpose address %s", address => {
    expect(isPublicImportAddress(address)).toBe(false);
  });

  it.each(["93.184.216.34", "8.8.8.8", "172.32.0.1", "2001:4860:4860::8888", "2606:4700:4700::1111"])("preserves ordinary public unicast %s", address => {
    expect(isPublicImportAddress(address)).toBe(true);
  });

  it.each([
    "http://127.1/private",
    "http://2130706433/private",
    "http://0x7f000001/private",
    "http://0177.0.0.1/private",
    "http://[::1]/private",
    "http://[::ffff:7f00:1]/private",
    "http://169.254.169.254/latest/meta-data",
    "https://user:password@example.com/file",
    "file:///etc/passwd",
    "ftp://example.com/file",
  ])("rejects URL %s before a connection", async url => {
    await expect(fetchUrlContent(url, tempDir)).rejects.toThrow(/ERR_VALIDATION/);
    expect(get).not.toHaveBeenCalled();
    expect(lookup).not.toHaveBeenCalled();
  });

  it.each(["127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "::ffff:10.0.0.1", "fd00:ec2::254"])("rejects a DNS name resolving to %s", async address => {
    lookup.mockImplementationOnce((_host, _options, callback) => callback(null, [{ address, family: address.includes(":") ? 6 : 4 }]));
    await expect(fetchUrlContent("http://example.com/file", tempDir)).rejects.toThrow(/resolve only to public IP addresses/);
    expect(destination).toBeUndefined();
    expect(response.readableLength).toBe(0);
  });

  it("rejects mixed public and private DNS answers", async () => {
    lookup.mockImplementationOnce((_host, _options, callback) =>
      callback(null, [
        { address: "93.184.216.34", family: 4 },
        { address: "::1", family: 6 },
      ])
    );
    await expect(fetchUrlContent("http://example.com/file", tempDir)).rejects.toThrow(/resolve only to public IP addresses/);
    expect(destination).toBeUndefined();
  });

  it("fails closed on empty DNS answers or resolution errors", async () => {
    lookup.mockImplementationOnce((_host, _options, callback) => callback(null, []));
    await expect(fetchUrlContent("http://empty.example/file", tempDir)).rejects.toThrow(/public IP addresses/);
    lookup.mockImplementationOnce((_host, _options, callback) => callback(new Error("DNS unavailable")));
    await expect(fetchUrlContent("http://error.example/file", tempDir)).rejects.toThrow(/DNS unavailable/);
  });

  it("pins the validated DNS answers to the socket without a second resolution", async () => {
    lookup.mockImplementationOnce((_host, _options, callback) => callback(null, [{ address: "93.184.216.34", family: 4 }]));
    lookup.mockImplementation((_host, _options, callback) => callback(null, [{ address: "127.0.0.1", family: 4 }]));
    response.end("hello world");
    expect(await fetchUrlContent("http://example.com/file", tempDir)).toBe("hello world");
    expect(lookup).toHaveBeenCalledTimes(1);
    expect(destination).toEqual([{ address: "93.184.216.34", family: 4 }]);
    expect(get.mock.calls[0][0].hostname).toBe("example.com");
    expect(get.mock.calls[0][1].agent).toBe(false);
  });

  it("preserves HTTPS hostname and certificate verification with pinned IPv6", async () => {
    lookup.mockImplementation((_host, _options, callback) => callback(null, [{ address: "2606:4700:4700::1111", family: 6 }]));
    const secureGet = vi.spyOn(https, "get").mockImplementation(get.getMockImplementation());
    response.end("secure");
    expect(await readPublicImportUrl(validateImportUrl("https://example.com/file"), new AbortController().signal)).toBe("secure");
    expect(secureGet.mock.calls[0][0].hostname).toBe("example.com");
    expect(secureGet.mock.calls[0][1]).not.toHaveProperty("rejectUnauthorized");
    expect(destination).toEqual([{ address: "2606:4700:4700::1111", family: 6 }]);
  });

  it.each([301, 302, 303, 307, 308, 404])("rejects HTTP %s without following redirects or caching", async status => {
    response.statusCode = status;
    response.headers = { location: "http://169.254.169.254/latest/meta-data" };
    response.end("not imported");
    await expect(fetchUrlContent("http://example.com/file", tempDir)).rejects.toThrow(`HTTP ${status}`);
    expect(get).toHaveBeenCalledTimes(1);
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("rejects a response error mid-transfer without caching partial data", async () => {
    const pending = fetchUrlContent("http://example.com/file", tempDir);
    await vi.waitFor(() => expect(destination).toBeDefined());
    response.write("partial");
    response.destroy(new Error("simulated socket failure"));
    await expect(pending).rejects.toThrow(/Failed to fetch URL.*simulated socket failure/);
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("keeps the timeout active while reading a stalled response body", async () => {
    vi.useFakeTimers();
    const pending = fetchUrlContent("http://example.com/file", tempDir);
    const rejected = expect(pending).rejects.toThrow(/Failed to fetch URL.*request aborted/);
    await vi.advanceTimersByTimeAsync(30_000);
    await rejected;
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("times out a stalled DNS resolution without reaching a response", async () => {
    vi.useFakeTimers();
    lookup.mockImplementation(() => {});
    const pending = fetchUrlContent("http://example.com/file", tempDir);
    const rejected = expect(pending).rejects.toThrow(/Failed to fetch URL.*request aborted/);
    await vi.advanceTimersByTimeAsync(30_000);
    await rejected;
    expect(destination).toBeUndefined();
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("supports the single-address socket lookup convention", async () => {
    response.end("single-address");
    const pending = fetchUrlContent("http://example.com/file", tempDir);
    const options = get.mock.calls[0][1];
    await expect(
      new Promise((resolve, reject) => {
        options.lookup("example.com", { all: false }, (error, address, family) => {
          if (error) reject(error);
          else resolve({ address, family });
        });
      })
    ).resolves.toEqual({ address: "93.184.216.34", family: 4 });
    expect(await pending).toBe("single-address");
  });

  it("warns and skips an optional private destination without contacting it", async () => {
    expect(await processRuntimeImport("http://169.254.169.254/latest/meta-data", true, tempDir)).toBe("");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringMatching(/Optional runtime import URL failed.*public IP address/));
    expect(get).not.toHaveBeenCalled();
  });

  it("uses only the new public-destination cache namespace", async () => {
    const url = "http://example.com/file";
    const oldHash = crypto.createHash("sha256").update(url).digest("hex");
    fs.writeFileSync(path.join(tempDir, `url-${oldHash}.cache`), "old data");
    response.end("new data");
    expect(await fetchUrlContent(url, tempDir)).toBe("new data");
    expect(await fetchUrlContent(url, tempDir)).toBe("new data");
    expect(get).toHaveBeenCalledTimes(1);
  });

  it("rejects credential expressions delivered by an actual URL import", async () => {
    process.env["INPUT_GITHUB-TOKEN"] = "not-for-model";
    const url = `http://example.com/${path.basename(tempDir)}.md`;
    const hash = crypto
      .createHash("sha256")
      .update("public-v1:" + url)
      .digest("hex");
    response.end("${{ github.actor && env.INPUT_GITHUB-TOKEN || 'fallback' }}");
    try {
      await expect(processRuntimeImport(url, false, tempDir)).rejects.toThrow(/unauthorized GitHub Actions expressions/);
    } finally {
      delete process.env["INPUT_GITHUB-TOKEN"];
      fs.rmSync(path.join("/tmp/gh-aw/url-cache", `url-${hash}.cache`), { force: true });
    }
  });
});
