import { describe, it, expect, vi } from "vitest";
const { once } = require("node:events");
const { createBridge, main } = require("./gemini_copilot.cjs");

async function withBridge(fetchImpl, test) {
  const server = createBridge({ model: "gemini-3.8-flash", upstream: "http://copilot.test:10002", fetchImpl });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  try {
    await test(`http://127.0.0.1:${server.address().port}`);
  } finally {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
}

const requestBody = { contents: [{ role: "user", parts: [{ text: "Hello" }] }] };
const responseBody = { choices: [{ message: { content: "Hello back" }, finish_reason: "stop" }], usage: { prompt_tokens: 5, completion_tokens: 2, total_tokens: 7 } };

describe("Gemini Copilot bridge", () => {
  it("sets isolated CLI routing and propagates its exit status", async () => {
    const script = `
      const assert = require("node:assert/strict");
      assert.equal(process.env.GEMINI_MODEL, "gemini-3.8-flash");
      assert.equal(process.env.GEMINI_API_KEY, "gh-aw-copilot-placeholder");
      assert.match(process.env.GOOGLE_GEMINI_BASE_URL, /^http:\\/\\/127\\.0\\.0\\.1:\\d+$/);
      assert.equal(process.env.GOOGLE_GENAI_USE_VERTEXAI, "false");
      process.exitCode = 7;
    `;
    expect(await main([process.execPath, "-e", script], { GH_AW_GEMINI_COPILOT_MODEL: "copilot/gemini-3.8-flash" })).toBe(7);
  });

  it("closes the bridge on spawn failures", async () => {
    await expect(main(["/missing-gemini-cli"], { GH_AW_GEMINI_COPILOT_MODEL: "gemini-3.8-flash" })).rejects.toThrow("ENOENT");
  });
  it("routes utility requests to the selected model without forwarding auth headers", async () => {
    const upstream = vi.fn(async () => Response.json(responseBody));
    await withBridge(upstream, async base => {
      const result = await fetch(`${base}/v1beta/models/gemini-2.5-flash:generateContent`, {
        method: "POST",
        headers: { Authorization: "Bearer must-not-forward", "x-goog-api-key": "must-not-forward" },
        body: JSON.stringify(requestBody),
      });
      expect(result.status).toBe(200);
      expect((await result.json()).modelVersion).toBe("gemini-3.8-flash");
      const [url, options] = upstream.mock.calls[0];
      expect(url).toBe("http://copilot.test:10002/chat/completions");
      expect(options.headers).toEqual({ "Content-Type": "application/json" });
      expect(JSON.parse(options.body).model).toBe("gemini-3.8-flash");
      expect(options.redirect).toBe("error");
    });
  });

  it("returns valid Gemini SSE for streaming requests", async () => {
    await withBridge(
      async () => Response.json(responseBody),
      async base => {
        const result = await fetch(`${base}/v1beta/models/gemini-3.8-flash:streamGenerateContent?alt=sse`, { method: "POST", body: JSON.stringify(requestBody) });
        expect(result.headers.get("content-type")).toBe("text/event-stream");
        const text = await result.text();
        expect(text.endsWith("\n\n")).toBe(true);
        expect(JSON.parse(text.slice(6)).candidates[0].content.parts[0].text).toBe("Hello back");
      }
    );
  });

  it("preserves rate-limit status and retry timing", async () => {
    await withBridge(
      async () => new Response('{"error":{"code":"ai_credits_limit_exceeded"}}', { status: 429, headers: { "Retry-After": "30" } }),
      async base => {
        const result = await fetch(`${base}/v1beta/models/gemini:generateContent`, { method: "POST", body: JSON.stringify(requestBody) });
        expect(result.status).toBe(429);
        expect(result.headers.get("retry-after")).toBe("30");
        expect((await result.json()).error.message).toContain("ai_credits_limit_exceeded");
      }
    );
  });

  it("reports invalid input and unsupported endpoints without calling upstream", async () => {
    const upstream = vi.fn();
    await withBridge(upstream, async base => {
      expect((await fetch(`${base}/v1beta/models/gemini:generateContent`, { method: "POST", body: "invalid" })).status).toBe(400);
      for (const body of [null, [], {}, { contents: [null] }, { contents: [{ parts: [null] }] }]) {
        expect((await fetch(`${base}/v1beta/models/gemini:generateContent`, { method: "POST", body: JSON.stringify(body) })).status).toBe(400);
      }
      expect((await fetch(`${base}/v1beta/models/gemini:countTokens`, { method: "POST", body: "{}" })).status).toBe(501);
      expect(upstream).not.toHaveBeenCalled();
    });
  });

  it("reports upstream malformed completions and network errors as failures", async () => {
    for (const upstream of [
      async () => Response.json({ choices: [] }),
      async () => {
        throw new Error("connection refused");
      },
    ]) {
      await withBridge(upstream, async base => {
        const result = await fetch(`${base}/v1beta/models/gemini:generateContent`, { method: "POST", body: JSON.stringify(requestBody) });
        expect(result.status).toBe(502);
        expect((await result.json()).error.code).toBe(502);
      });
    }
  });
});
