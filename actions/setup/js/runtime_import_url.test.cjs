import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import dns from "node:dns";
import http from "node:http";
import https from "node:https";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { inspect } from "node:util";

const { displayImportUrl, isPublicImportAddress, validateImportUrl, readPublicImportUrl } = require("./runtime_import_url.cjs");
const { fetchUrlContent, processRuntimeImport, processRuntimeImports } = require("./runtime_import.cjs");

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
    tempDir = fs.mkdtempSync(path.join(process.cwd(), ".public-import-test-"));
    response = new PassThrough();
    response.statusCode = 200;
    request = new EventEmitter();
    lookup = vi.spyOn(dns, "lookup").mockImplementation((_host, _options, callback) => {
      callback(null, [{ address: "93.184.216.34", family: 4 }]);
    });
    vi.spyOn(http, "get").mockImplementation(() => {
      throw new Error("HTTP must not be used");
    });
    get = vi.spyOn(https, "get").mockImplementation((url, options, callback) => {
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
    "https://127.1/private",
    "https://2130706433/private",
    "https://0x7f000001/private",
    "https://0177.0.0.1/private",
    "https://[::1]/private",
    "https://[::ffff:7f00:1]/private",
    "https://169.254.169.254/latest/meta-data",
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
    await expect(fetchUrlContent("https://example.com/file", tempDir)).rejects.toThrow(/resolve only to public IP addresses/);
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
    await expect(fetchUrlContent("https://example.com/file", tempDir)).rejects.toThrow(/resolve only to public IP addresses/);
    expect(destination).toBeUndefined();
  });

  it("fails closed on empty DNS answers or resolution errors", async () => {
    lookup.mockImplementationOnce((_host, _options, callback) => callback(null, []));
    await expect(fetchUrlContent("https://empty.example/file", tempDir)).rejects.toThrow(/public IP addresses/);
    lookup.mockImplementationOnce((_host, _options, callback) => callback(new Error("DNS unavailable")));
    await expect(fetchUrlContent("https://error.example/file", tempDir)).rejects.toThrow(/DNS unavailable/);
  });

  it("pins the validated DNS answers to the socket without a second resolution", async () => {
    lookup.mockImplementationOnce((_host, _options, callback) => callback(null, [{ address: "93.184.216.34", family: 4 }]));
    lookup.mockImplementation((_host, _options, callback) => callback(null, [{ address: "127.0.0.1", family: 4 }]));
    response.end("hello world");
    expect(await fetchUrlContent("https://example.com/file", tempDir)).toBe("hello world");
    expect(lookup).toHaveBeenCalledTimes(1);
    expect(destination).toEqual([{ address: "93.184.216.34", family: 4 }]);
    expect(get.mock.calls[0][0].hostname).toBe("example.com");
    expect(get.mock.calls[0][1].agent).toBe(false);
  });

  it("preserves HTTPS hostname and certificate verification with pinned IPv6", async () => {
    lookup.mockImplementation((_host, _options, callback) => callback(null, [{ address: "2606:4700:4700::1111", family: 6 }]));
    response.end("secure");
    expect(await readPublicImportUrl(validateImportUrl("https://example.com/file"), new AbortController().signal)).toBe("secure");
    expect(get.mock.calls[0][0].hostname).toBe("example.com");
    expect(get.mock.calls[0][1]).not.toHaveProperty("rejectUnauthorized");
    expect(destination).toEqual([{ address: "2606:4700:4700::1111", family: 6 }]);
  });

  it.each([301, 302, 303, 307, 308, 404])("rejects HTTP %s without following redirects or caching", async status => {
    response.statusCode = status;
    response.headers = { location: "https://169.254.169.254/latest/meta-data" };
    response.end("not imported");
    await expect(fetchUrlContent("https://example.com/file", tempDir)).rejects.toThrow(`HTTP ${status}`);
    expect(get).toHaveBeenCalledTimes(1);
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("rejects a response error mid-transfer without caching partial data", async () => {
    const pending = fetchUrlContent("https://example.com/file", tempDir);
    await vi.waitFor(() => expect(destination).toBeDefined());
    response.write("partial");
    response.destroy(new Error("simulated socket failure"));
    await expect(pending).rejects.toThrow(/Failed to fetch URL.*simulated socket failure/);
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("keeps the timeout active while reading a stalled response body", async () => {
    vi.useFakeTimers();
    const pending = fetchUrlContent("https://example.com/file", tempDir);
    const rejected = expect(pending).rejects.toThrow(/Failed to fetch URL.*request aborted/);
    await vi.advanceTimersByTimeAsync(30_000);
    await rejected;
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("times out a stalled DNS resolution without reaching a response", async () => {
    vi.useFakeTimers();
    lookup.mockImplementation(() => {});
    const pending = fetchUrlContent("https://example.com/file", tempDir);
    const rejected = expect(pending).rejects.toThrow(/Failed to fetch URL.*request aborted/);
    await vi.advanceTimersByTimeAsync(30_000);
    await rejected;
    expect(destination).toBeUndefined();
    expect(fs.readdirSync(tempDir)).toEqual([]);
  });

  it("supports the single-address socket lookup convention", async () => {
    response.end("single-address");
    const pending = fetchUrlContent("https://example.com/file", tempDir);
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

  it("rejects an optional private destination without contacting it", async () => {
    await expect(processRuntimeImport("https://169.254.169.254/latest/meta-data", true, tempDir)).rejects.toThrow(/public IP address/);
    expect(get).not.toHaveBeenCalled();
  });

  it("does not read legacy URL disk caches", async () => {
    const url = "https://example.com/file";
    const oldHash = crypto.createHash("sha256").update(url).digest("hex");
    fs.writeFileSync(path.join(tempDir, `url-${oldHash}.cache`), "old data");
    response.end("new data");
    expect(await fetchUrlContent(url, tempDir)).toBe("new data");
    expect(get).toHaveBeenCalledTimes(1);
  });

  function expectSafeDiagnostics(error) {
    const diagnostics = inspect(error) + JSON.stringify([global.core.info.mock.calls, global.core.warning.mock.calls, global.core.debug.mock.calls]);
    expect(diagnostics).not.toMatch(/query-secret|fragment-secret|password-secret/);
    return diagnostics;
  }

  it("displays only origin and pathname, including for rejected credentials and malformed URLs", () => {
    expect(displayImportUrl("https://user:password-secret@example.com/file?token=query-secret#fragment-secret")).toBe("https://example.com/file");
    expect(displayImportUrl("https://[invalid]?token=query-secret#fragment-secret")).toBe("(invalid URL)");
  });

  it("preserves query-dependent requests without logging query or fragment credentials", async () => {
    const first = "https://example.com/file?token=query-secret#fragment-secret";
    const second = "https://example.com/file?token=other-query-secret#fragment-secret";
    response.end("first");
    expect(await fetchUrlContent(first, tempDir)).toBe("first");
    response = new PassThrough();
    response.statusCode = 200;
    response.end("second");
    expect(await fetchUrlContent(second, tempDir)).toBe("second");
    expect(get.mock.calls.map(([url]) => url.href)).toEqual([first, second]);
    expectSafeDiagnostics();
    expect(global.core.info).toHaveBeenCalledWith("Fetching content from URL: https://example.com/file");
  });

  it("redacts HTTP failures in direct and wrapped errors", async () => {
    response.statusCode = 404;
    response.end();
    const url = "https://example.com/file?token=query-secret#fragment-secret";
    const direct = await readPublicImportUrl(validateImportUrl(url), new AbortController().signal).catch(error => error);
    expect(expectSafeDiagnostics(direct)).toContain("https://example.com/file: HTTP 404");
    response = new PassThrough();
    response.statusCode = 404;
    response.end();
    const wrapped = await processRuntimeImports(`{{#runtime-import ${url}}}`, tempDir).catch(error => error);
    expect(expectSafeDiagnostics(wrapped)).toContain("Failed to process runtime import for https://example.com/file");
    expect(wrapped).toBeInstanceOf(Error);
  });

  it("redacts URLs embedded in transport errors without retaining an unsafe cause", async () => {
    const url = "https://example.com/file?token=query-secret#fragment-secret";
    get.mockImplementationOnce(() => {
      throw new Error(`socket failure for ${url}`);
    });
    const error = await fetchUrlContent(url, tempDir).catch(error => error);
    expect(expectSafeDiagnostics(error)).toContain("socket failure for https://example.com/file");
    expect(error).toBeInstanceOf(Error);
    expect(error.cause).toBeUndefined();
  });

  it.each(["https://[invalid]?token=query-secret#fragment-secret", "https://user:password-secret@example.com/file?token=query-secret#fragment-secret", "https://127.0.0.1/file?token=query-secret#fragment-secret"])(
    "redacts rejected destinations in required errors and optional warnings: %s",
    async url => {
      const error = await processRuntimeImports(`{{#runtime-import ${url}}}`, tempDir).catch(error => error);
      expect(error).toBeInstanceOf(Error);
      expectSafeDiagnostics(error);
      await expect(processRuntimeImport(url, true, tempDir)).rejects.toThrow(/ERR_VALIDATION/);
      expectSafeDiagnostics();
      expect(get).not.toHaveBeenCalled();
    }
  );

  it.each([
    [0, 1],
    [1, 4],
    [2, 1],
  ])("redacts line range diagnostics for %s-%s", async (start, end) => {
    response.end("line one\nline two");
    const url = "https://example.com/file?token=query-secret#fragment-secret";
    const error = await processRuntimeImport(url, false, tempDir, start, end).catch(error => error);
    expect(error).toBeInstanceOf(Error);
    expect(expectSafeDiagnostics(error)).toContain("URL https://example.com/file");
  });

  it("rejects expressions before stripping frontmatter without leaking URL diagnostics", async () => {
    response.end("---\nname: example\n---\n${{ secrets.TOKEN }}");
    const url = "https://example.com/file?token=query-secret#fragment-secret";
    const error = await processRuntimeImports(`{{#runtime-import ${url}}}`, tempDir).catch(error => error);
    expect(error).toBeInstanceOf(Error);
    expect(expectSafeDiagnostics(error)).toContain("must not contain expressions or template directives");
  });

  it("rejects remote recursive imports without expanding them or leaking diagnostics", async () => {
    const url = "https://example.com/file?token=query-secret#fragment-secret";
    response.end(`{{#runtime-import ${url}}}`);
    const error = await processRuntimeImports(`{{#runtime-import ${url}}}`, tempDir).catch(error => error);
    expect(error).toBeInstanceOf(Error);
    expect(expectSafeDiagnostics(error)).toContain("must not contain expressions or template directives");
    expect(get).toHaveBeenCalledTimes(1);
  });

  it("redacts cached and already resolved import diagnostics without changing lookup keys", async () => {
    const url = "https://example.com/file?token=query-secret#fragment-secret";
    const content = `{{#runtime-import ${url}}}`;
    expect(await processRuntimeImports(content, tempDir, new Set(), new Map([[url, "cached"]]))).toBe("cached");
    expect(await processRuntimeImports(content, tempDir, new Set(), new Map(), [], null, new Map(), new Set([url]))).toBe("");
    expectSafeDiagnostics();
    expect(global.core.info).toHaveBeenCalledWith("Reusing cached content for https://example.com/file");
    expect(global.core.info).toHaveBeenCalledWith("Skipping already resolved import for https://example.com/file");
  });

  it("rejects credential expressions delivered by an actual URL import", async () => {
    process.env["INPUT_GITHUB-TOKEN"] = "not-for-model";
    const url = `https://example.com/${path.basename(tempDir)}.md`;
    response.end("${{ github.actor && env.INPUT_GITHUB-TOKEN || 'fallback' }}");
    try {
      await expect(processRuntimeImport(url, false, tempDir)).rejects.toThrow(/must not contain expressions or template directives/);
    } finally {
      delete process.env["INPUT_GITHUB-TOKEN"];
    }
  });

  it.each([
    "${{ github.actor }}",
    "${{ github.actor == 'bot' && github.repository }}",
    "${{ needs.pre_activation.outputs.value }}",
    "<!-- ${{ github.actor }} -->",
    "---\ntitle: ${{ github.actor }}\n---\nPlain text",
    "Plain text\n${{ github.actor }}",
    "${{ unterminated",
    "{{#if github.actor}}text{{/if}}",
    "{{#runtime-import local.md}}",
    "{{#import?: local.md}}",
    "$<!-- hidden -->{{ github.actor }}",
    "{{<!-- hidden -->#runtime-import local.md}}",
    "${GH_AW_EXPR_1234ABCD}",
    "__GH_AW_EXPERIMENTS_STYLE__",
    "$<!-- hidden -->{GH_AW_EXPR_1234ABCD}",
  ])("rejects all remote expressions/directives even outside selected lines and when optional: %s", async content => {
    for (const optional of [false, true]) {
      response = new PassThrough();
      response.statusCode = 200;
      response.end(content);
      await expect(processRuntimeImport("https://example.com/static.md", optional, tempDir, 1, 1)).rejects.toThrow(/must not contain expressions or template directives/);
    }
  });

  it("preserves plain HTTPS imports, line ranges and sanitization without expression evaluation", async () => {
    response.end("Ignored\n---\nname: static\n---\nHello <!-- hidden -->world\nLast");
    const content = await processRuntimeImport("https://example.com/static.md", false, tempDir, 2, 5);
    expect(content).toBe("Hello world");
  });

  it("does not suppress private DNS destinations for optional imports", async () => {
    lookup.mockImplementationOnce((_host, _options, callback) => callback(null, [{ address: "127.0.0.1", family: 4 }]));
    await expect(processRuntimeImport("https://example.com/static.md", true, tempDir)).rejects.toThrow(/resolve only to public IP addresses/);
    expect(destination).toBeUndefined();
  });

  it("warns and omits optional content on ordinary HTTPS failures", async () => {
    response.statusCode = 404;
    response.end();
    expect(await processRuntimeImport("https://example.com/missing.md", true, tempDir)).toBe("");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringMatching(/Optional runtime import URL failed.*HTTP 404/));
  });
});
