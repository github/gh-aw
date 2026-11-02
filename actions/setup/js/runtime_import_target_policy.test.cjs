import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import http from "node:http";
import https from "node:https";
import dns from "node:dns";

const { processRuntimeImport, processRuntimeImports } = require("./runtime_import.cjs");

describe("runtime import target policy", () => {
  let directory;
  let fetch;
  let httpGet;
  let httpsGet;
  let lookup;
  const targets = [
    "http://example.com/file.md",
    "http://127.0.0.1/file.md",
    "http://169.254.169.254/latest/meta-data",
    "HTTP://example.com/file.md",
    "https://[::1]/file.md",
    "http://example.com/file.md?token=query-secret#fragment-secret",
    "https://user:password-secret@example.com/file.md",
    "https:example.com/file.md",
    "https://example.com/${GH_AW_EXPR_1234ABCD}",
    "https://example.com/__GH_AW_EXPERIMENTS_STYLE__",
    "file:///etc/passwd",
    "ftp://example.com/file.md",
    "data:text/plain,content",
    "//example.com/file.md",
    "\\\\example.com\\file.md",
  ];

  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "local-import-test-"));
    fs.mkdirSync(path.join(directory, ".github", "workflows"), { recursive: true });
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn() };
    fetch = vi.spyOn(global, "fetch").mockRejectedValue(new Error("network must not be used"));
    httpGet = vi.spyOn(http, "get").mockImplementation(() => {
      throw new Error("network must not be used");
    });
    httpsGet = vi.spyOn(https, "get").mockImplementation(() => {
      throw new Error("network must not be used");
    });
    lookup = vi.spyOn(dns, "lookup").mockImplementation(() => {
      throw new Error("DNS must not be used");
    });
  });

  afterEach(() => {
    expect(fetch).not.toHaveBeenCalled();
    expect(httpGet).not.toHaveBeenCalled();
    expect(httpsGet).not.toHaveBeenCalled();
    expect(lookup).not.toHaveBeenCalled();
    vi.restoreAllMocks();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it.each(targets)("rejects non-local targets even when optional: %s", async target => {
    for (const optional of [false, true]) {
      await expect(processRuntimeImport(target, optional, directory)).rejects.toThrow(/ERR_VALIDATION/);
    }
  });

  it.each(["runtime-import", "runtime-import?", "import", "import?"])("rejects URL macros including deprecated and optional forms: %s", async macro => {
    const target = "http://example.com/file.md?token=query-secret#fragment-secret";
    const error = await processRuntimeImports(`{{#${macro} ${target}:1-3}}`, directory).catch(error => error);
    expect(error).toBeInstanceOf(Error);
    expect(error.message).toContain("Runtime imports require local files or literal public HTTPS URLs");
    expect(error.message).not.toMatch(/query-secret|fragment-secret/);
  });

  it.each(["runtime-import", "runtime-import?", "import", "import?:"])("rejects expressions in URLs before network access: %s", async macro => {
    const target = "https://example.com/${{ github.actor }}";
    await expect(processRuntimeImports(`{{#${macro} ${target}}}`, directory)).rejects.toThrow(/literal, unauthenticated HTTPS URL/);
  });

  it("rejects expressions in cached remote content", async () => {
    const target = "https://example.com/cached.md";
    await expect(processRuntimeImports(`{{#runtime-import ${target}}}`, directory, new Set(), new Map([[target, "${{ github.actor }}"]]))).rejects.toThrow(/must not contain expressions or template directives/);
  });

  it("rejects URL targets before consulting the resolved-import or content cache", async () => {
    const target = "http://example.com/cached.md";
    await expect(processRuntimeImports(`{{#runtime-import ${target}}}`, directory, new Set(), new Map([[target, "cached content"]]), [], null, new Map(), new Set([target]))).rejects.toThrow(
      /Runtime imports require local files or literal public HTTPS URLs/
    );
  });

  it("rejects URLs nested in otherwise valid local imports", async () => {
    fs.writeFileSync(path.join(directory, ".github", "workflows", "outer.md"), "{{#runtime-import? http://example.com/nested.md}}");
    await expect(processRuntimeImports("{{#runtime-import outer.md}}", directory)).rejects.toThrow(/Runtime imports require local files or literal public HTTPS URLs/);
  });

  it("preserves local line ranges, nested imports and missing optional files", async () => {
    fs.writeFileSync(path.join(directory, ".github", "workflows", "outer.md"), "Header\n{{#runtime-import inner.md:2-2}}\nFooter");
    fs.writeFileSync(path.join(directory, ".github", "workflows", "inner.md"), "First\nSelected\nLast");
    expect(await processRuntimeImports("{{#runtime-import outer.md}}\n{{#runtime-import? missing.md}}", directory)).toBe("Header\nSelected\nFooter\n");
  });
});
