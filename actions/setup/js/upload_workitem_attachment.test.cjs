// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";

const req = createRequire(import.meta.url);

describe("upload_workitem_attachment", () => {
  let originalCore;
  let originalFetch;

  beforeEach(() => {
    originalCore = global.core;
    originalFetch = global.fetch;
    global.core = /** @type {any} */ { info: () => {}, warning: () => {}, error: () => {}, debug: () => {}, setFailed: () => {} };
    global.fetch = /** @type {any} */ () => {
      throw new Error("fetch should not be called in staged mode");
    };
  });

  afterEach(() => {
    global.core = originalCore;
    global.fetch = originalFetch;
  });

  it("exports main as a function", () => {
    const { main } = req("./upload_workitem_attachment.cjs");
    expect(typeof main).toBe("function");
  });

  it("returns a handler function", async () => {
    const { main } = req("./upload_workitem_attachment.cjs");
    const handler = await main({ staged: true });
    expect(typeof handler).toBe("function");
  });

  it("accepts being called without config", async () => {
    const { main } = req("./upload_workitem_attachment.cjs");
    const handler = await main();
    expect(typeof handler).toBe("function");
  });

  it("logs a staged preview without touching the network", async () => {
    const infos = [];
    global.core = /** @type {any} */ { ...global.core, info: msg => infos.push(msg) };
    const { main } = req("./upload_workitem_attachment.cjs");
    const handler = await main({ staged: true });
    await handler({ work_item_id: 7, file_path: "a/b.txt" }, {});
    expect(infos.join(" ")).toContain("Would attach a file to Azure DevOps work item 7");
  });

  it("does not leak the file path in staged output", async () => {
    const infos = [];
    global.core = /** @type {any} */ { ...global.core, info: msg => infos.push(msg) };
    const { main } = req("./upload_workitem_attachment.cjs");
    const handler = await main({ staged: true });
    await handler({ work_item_id: 7, file_path: "secret/file.pdf" }, {});
    expect(infos.join(" ")).not.toContain("secret/file.pdf");
  });
});
