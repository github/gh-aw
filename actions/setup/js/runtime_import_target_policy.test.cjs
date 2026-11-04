import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import http from "node:http";
import https from "node:https";
import dns from "node:dns";
import net from "node:net";
import tls from "node:tls";

const { processRuntimeImport, processRuntimeImports, resolveRuntimeImportFilePath, resolveRuntimeImportKey } = require("./runtime_import.cjs");

describe("runtime import target policy", () => {
  let directory;
  let networkSpies;
  const targets = [
    "https://example.com/file.md",
    "HTTPS://example.com/file.md",
    "https://raw.githubusercontent.com/org/repo/main/file.md",
    "https://127.0.0.1/file.md",
    "https://169.254.169.254/latest/meta-data",
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
    "https://example.com/${{ github.actor }}",
    "file:///etc/passwd",
    "ftp://example.com/file.md",
    "data:text/plain,content",
    "//example.com/file.md",
    "\\\\example.com\\file.md",
    "ws://example.com/file.md",
    "wss://example.com/file.md",
    "git+https://example.com/file.md",
  ];

  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "local-import-test-"));
    fs.mkdirSync(path.join(directory, ".github", "workflows"), { recursive: true });
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn() };
    networkSpies = [
      [global, "fetch"],
      [http, "get"],
      [http, "request"],
      [https, "get"],
      [https, "request"],
      [dns, "lookup"],
      [dns.promises, "lookup"],
      [net, "connect"],
      [net, "createConnection"],
      [tls, "connect"],
    ].map(([object, method]) =>
      vi.spyOn(object, method).mockImplementation(() => {
        throw new Error("prompt assembly must not use networking");
      })
    );
  });

  afterEach(() => {
    for (const spy of networkSpies) expect(spy).not.toHaveBeenCalled();
    vi.restoreAllMocks();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it.each(targets)("rejects non-local targets even when optional: %s", async target => {
    for (const optional of [false, true]) {
      await expect(processRuntimeImport(target, optional, directory)).rejects.toThrow(/Runtime imports require local files/);
    }
    expect(() => resolveRuntimeImportFilePath(target, directory)).toThrow(/Runtime imports require local files/);
    expect(() => resolveRuntimeImportKey(target, directory)).toThrow(/Runtime imports require local files/);
  });

  it.each(["runtime-import", "runtime-import?", "import", "import?"])("rejects URL macros including deprecated and optional forms: %s", async macro => {
    for (const scheme of ["http", "https"]) {
      const target = `${scheme}://user:password-secret@example.com/file.md?token=query-secret#fragment-secret`;
      const error = await processRuntimeImports(`{{#${macro} ${target}:1-3}}`, directory).catch(error => error);
      expect(error).toBeInstanceOf(Error);
      expect(error.message).toContain("Runtime imports require local files");
      expect(error.message).toContain("inside the AWF sandbox");
      expect(error.message).not.toMatch(/password-secret|query-secret|fragment-secret/);
      expect(JSON.stringify(global.core.info.mock.calls)).not.toMatch(/password-secret|query-secret|fragment-secret/);
    }
  });

  it.each(["runtime-import", "runtime-import?", "import", "import?:"])("rejects expressions in URLs before network access: %s", async macro => {
    const target = "https://example.com/${{ github.actor }}";
    await expect(processRuntimeImports(`{{#${macro} ${target}}}`, directory)).rejects.toThrow(/Runtime imports require local files/);
  });

  it("rejects remote targets even when content was cached", async () => {
    const target = "https://example.com/cached.md";
    await expect(processRuntimeImports(`{{#runtime-import ${target}}}`, directory, new Set(), new Map([[target, "static cached text"]]))).rejects.toThrow(/Runtime imports require local files/);
  });

  it("rejects URL targets before consulting the resolved-import or content cache", async () => {
    const target = "https://example.com/cached.md";
    await expect(processRuntimeImports(`{{#runtime-import ${target}}}`, directory, new Set(), new Map([[target, "cached content"]]), [], null, new Map(), new Set([target]))).rejects.toThrow(/Runtime imports require local files/);
  });

  it("rejects URLs nested in otherwise valid local imports", async () => {
    fs.writeFileSync(path.join(directory, ".github", "workflows", "outer.md"), "{{#runtime-import? https://example.com/nested.md}}");
    await expect(processRuntimeImports("{{#runtime-import outer.md}}", directory)).rejects.toThrow(/Runtime imports require local files/);
  });

  it("rejects remote imports inside a false author conditional", async () => {
    await expect(processRuntimeImports("{{#if false}}\n{{#runtime-import? https://example.com/hidden.md}}\n{{/if}}", directory)).rejects.toThrow(/Runtime imports require local files/);
  });

  it("leaves ordinary URL references for the agent to resolve", async () => {
    const text = "Fetch https://example.com/checklist.md using your tools inside the AWF sandbox.";
    fs.writeFileSync(path.join(directory, ".github", "workflows", "instructions.md"), text);
    expect(await processRuntimeImports("{{#runtime-import instructions.md}}", directory)).toBe(text);
  });

  it("preserves local line ranges, nested imports and missing optional files", async () => {
    fs.writeFileSync(path.join(directory, ".github", "workflows", "outer.md"), "Header\n{{#runtime-import inner.md:2-2}}\nFooter");
    fs.writeFileSync(path.join(directory, ".github", "workflows", "inner.md"), "First\nSelected\nLast");
    expect(await processRuntimeImports("{{#runtime-import outer.md}}\n{{#runtime-import? missing.md}}", directory)).toBe("Header\nSelected\nFooter\n");
  });
});
