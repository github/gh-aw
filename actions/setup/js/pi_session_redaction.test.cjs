import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { processFile } = await import("./redact_secrets.cjs");
let dir;
let originalCore;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-export-redaction-"));
  originalCore = global.core;
  global.core = { info: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
});
afterEach(() => {
  global.core = originalCore;
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Pi encoded HTML artifact redaction", () => {
  it("redacts recovered messages, rendered tools, known tokens, and runtime masks", () => {
    const token = "ghp_" + "a".repeat(36);
    const secret = 'custom-"secret-value';
    const data = { entries: [{ message: { content: [{ text: `${token} ${secret} runtime-mask-value` }] } }], renderedTools: { call: { callHtml: `secret ${secret}` } } };
    const file = path.join(dir, "session.html");
    fs.writeFileSync(file, `<html><script type="application/json" id="session-data">${Buffer.from(JSON.stringify(data)).toString("base64")}</script></html>`);
    expect(processFile(file, [secret], ["runtime-mask-value"])).toBeGreaterThan(0);
    const html = fs.readFileSync(file, "utf8");
    const encoded = /id="session-data">([^<]+)</.exec(html)[1];
    const decoded = Buffer.from(encoded, "base64").toString("utf8");
    expect(decoded).not.toContain(secret);
    expect(decoded).not.toContain(token);
    expect(decoded).not.toContain("runtime-mask-value");
    expect(JSON.parse(decoded).entries[0].message.content[0].text).toContain("***REDACTED***");
  });
  it.each(["not base64!", Buffer.from("invalid JSON").toString("base64")])("removes an unsanitizable export rather than leaving it uploadable", payload => {
    const file = path.join(dir, "session.html");
    fs.writeFileSync(file, `<script id="session-data">${payload}</script>`);
    processFile(file, []);
    expect(fs.existsSync(file)).toBe(false);
    expect(global.core.setFailed).toHaveBeenCalled();
  });
});
