// @ts-check
import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { createCompatServer } = require("./compat_mcp_server.cjs");
const { handleRequest } = require("./mcp_server_core.cjs");

describe("compat web_fetch", () => {
  it("advertises the Claude WebFetch url + prompt input shape", async () => {
    const server = createCompatServer();
    const { result } = await handleRequest(server, { id: 1, method: "tools/list", params: {} });
    expect(result.tools).toHaveLength(1);
    expect(result.tools[0]).toMatchObject({
      name: "web_fetch",
      inputSchema: { required: ["url", "prompt"], additionalProperties: false },
    });
    expect(Object.keys(result.tools[0].inputSchema.properties)).toEqual(["url", "prompt"]);
    expect(result.tools[0].description).toContain("does not run an LLM");
  });

  it("returns bounded simplified content without executing the prompt", async () => {
    const fetchImpl = vi.fn(async () => new Response("<h1>Example</h1><p>Hello &amp; welcome</p>", { headers: { "content-type": "text/html" } }));
    const server = createCompatServer({ fetchImpl });
    const { result } = await handleRequest(server, { id: 1, method: "tools/call", params: { name: "web_fetch", arguments: { url: "https://example.com", prompt: "Summarize this page" } } });
    expect(result.isError).not.toBe(true);
    expect(JSON.parse(result.content[0].text)).toMatchObject({ url: "https://example.com/", status: 200, content: "Example\n\nHello & welcome" });
    expect(fetchImpl).toHaveBeenCalledOnce();
    expect(fetchImpl.mock.calls[0][1].method).toBe("GET");
  });

  it.each([{ url: "https://example.com" }, { url: "https://example.com", prompt: "" }, { url: "https://example.com", prompt: 123 }, { url: "https://example.com", prompt: "read", unexpected: true }])(
    "rejects malformed inputs: %j",
    async args => {
      const fetchImpl = vi.fn();
      const { error } = await handleRequest(createCompatServer({ fetchImpl }), { id: 1, method: "tools/call", params: { name: "web_fetch", arguments: args } });
      expect(error.code).toBe(-32602);
      expect(fetchImpl).not.toHaveBeenCalled();
    }
  );

  it("fails closed without an AWF proxy", async () => {
    const { result } = await handleRequest(createCompatServer({ env: {} }), { id: 1, method: "tools/call", params: { name: "web_fetch", arguments: { url: "https://example.com", prompt: "read" } } });
    expect(result.isError).toBe(true);
    expect(result.content[0].text).toContain("requires an AWF proxy");
  });

  it("rejects non-HTTP URLs before fetching", async () => {
    const fetchImpl = vi.fn();
    const { result } = await handleRequest(createCompatServer({ fetchImpl }), { id: 1, method: "tools/call", params: { name: "web_fetch", arguments: { url: "file:///etc/passwd", prompt: "read" } } });
    expect(result.isError).toBe(true);
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});
