import { describe, expect, it, afterEach } from "vitest";
import { createRequire } from "module";
import http from "node:http";
import net from "node:net";

const require = createRequire(import.meta.url);
const { parseWebFetchURL, readBoundedResponseBody, createWebFetchTransport, executeCopilotSDKWebFetch, createCopilotSDKWebFetchTool } = require("./copilot_sdk_web_fetch.cjs");

/** @type {http.Server[]} */
let servers = [];

afterEach(async () => {
  await Promise.all(
    servers.splice(0).map(
      server =>
        new Promise(resolve => {
          server.close(() => resolve(undefined));
        })
    )
  );
});

/**
 * @param {(req: http.IncomingMessage, res: http.ServerResponse) => void} handler
 * @returns {Promise<{server: http.Server, port: number}>}
 */
async function startServer(handler) {
  const server = http.createServer(handler);
  servers.push(server);
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  return { server, port };
}

/**
 * Minimal CONNECT-tunneling forward proxy, matching undici ProxyAgent's default
 * (proxyTunnel: true) behavior for both http and https destinations.
 *
 * @returns {Promise<{server: http.Server, port: number, connectHosts: string[]}>}
 */
async function startConnectProxy() {
  /** @type {string[]} */
  const connectHosts = [];
  const server = http.createServer((_req, res) => {
    res.writeHead(400);
    res.end("proxy only supports CONNECT");
  });
  server.on("connect", (req, clientSocket, head) => {
    connectHosts.push(req.url);
    const [host, portStr] = req.url.split(":");
    const targetSocket = net.connect(Number(portStr), host, () => {
      clientSocket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      targetSocket.write(head);
      targetSocket.pipe(clientSocket);
      clientSocket.pipe(targetSocket);
    });
    targetSocket.on("error", () => clientSocket.destroy());
    clientSocket.on("error", () => targetSocket.destroy());
  });
  servers.push(server);
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  return { server, port, connectHosts };
}

describe("parseWebFetchURL", () => {
  it("rejects non-http(s) schemes", () => {
    expect(() => parseWebFetchURL("ftp://example.com/file")).toThrow("http or https protocol");
  });

  it("rejects embedded credentials", () => {
    expect(() => parseWebFetchURL("https://" + "user:pass" + "@example.com")).toThrow("must not contain credentials");
  });

  it("rejects empty or non-string input", () => {
    expect(() => parseWebFetchURL("")).toThrow("non-empty string");
    expect(() => parseWebFetchURL(undefined)).toThrow("non-empty string");
  });
});

describe("createWebFetchTransport", () => {
  it("fails closed when no AWF proxy environment variable is configured", async () => {
    const transport = createWebFetchTransport({ env: {} });
    await expect(transport.fetch(new URL("http://example.com"), { method: "GET", redirect: "manual", signal: new AbortController().signal })).rejects.toThrow(/requires an AWF proxy/);
  });

  it("routes every request through the configured proxy instead of fetching directly", async () => {
    const { port: targetPort } = await startServer((_req, res) => {
      res.writeHead(200, { "content-type": "text/plain" });
      res.end("hello from origin");
    });
    const { port: proxyPort, connectHosts } = await startConnectProxy();

    const result = await executeCopilotSDKWebFetch({ url: `http://127.0.0.1:${targetPort}/` }, { env: { HTTP_PROXY: `http://127.0.0.1:${proxyPort}` } });

    expect(connectHosts).toContain(`127.0.0.1:${targetPort}`);
    const parsed = JSON.parse(result);
    expect(parsed.content).toBe("hello from origin");
    expect(parsed.status).toBe(200);
  });
});

describe("executeCopilotSDKWebFetch", () => {
  it("bounds redirects and fails once the limit is exceeded", async () => {
    let calls = 0;
    const fetchImpl = async () => {
      calls++;
      return new Response(null, { status: 302, headers: { location: "https://example.com/next" } });
    };
    await expect(executeCopilotSDKWebFetch({ url: "https://example.com/start" }, { fetchImpl, maxRedirects: 2 })).rejects.toThrow("exceeded 2 redirects");
    expect(calls).toBe(3);
  });

  it("aborts the request once the timeout elapses", async () => {
    const fetchImpl = (_url, init) =>
      new Promise((_resolve, reject) => {
        init.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
      });
    await expect(executeCopilotSDKWebFetch({ url: "https://example.com/slow" }, { fetchImpl, timeoutMs: 10 })).rejects.toThrow();
  });

  it("rejects URLs with embedded credentials before any request is made", async () => {
    let called = false;
    const fetchImpl = async () => {
      called = true;
      return new Response("ok");
    };
    await expect(executeCopilotSDKWebFetch({ url: "https://" + "user:pass" + "@example.com" }, { fetchImpl })).rejects.toThrow("must not contain credentials");
    expect(called).toBe(false);
  });
});

describe("readBoundedResponseBody", () => {
  it("throws once the response body exceeds the size limit", async () => {
    const chunkSize = 1024 * 1024;
    const chunkCount = 11; // 11 MiB > MAX_RESPONSE_BYTES (10 MiB)
    let produced = 0;
    const stream = new ReadableStream({
      pull(controller) {
        if (produced >= chunkCount) {
          controller.close();
          return;
        }
        produced++;
        controller.enqueue(new Uint8Array(chunkSize));
      },
    });
    await expect(readBoundedResponseBody(stream)).rejects.toThrow(/exceeds .* bytes/);
  });

  it("returns an empty string for a null body", async () => {
    await expect(readBoundedResponseBody(null)).resolves.toBe("");
  });
});

describe("createCopilotSDKWebFetchTool", () => {
  it("asserts the SDK preserves the web_fetch override contract", () => {
    const defineTool = () => ({ name: "web_fetch", overridesBuiltInTool: false });
    expect(() => createCopilotSDKWebFetchTool(defineTool)).toThrow("web_fetch override contract");
  });

  it("registers web_fetch with overridesBuiltInTool set", () => {
    const defineTool = (name, config) => ({ name, overridesBuiltInTool: config.overridesBuiltInTool });
    const tool = createCopilotSDKWebFetchTool(defineTool);
    expect(tool.name).toBe("web_fetch");
    expect(tool.overridesBuiltInTool).toBe(true);
  });
});
