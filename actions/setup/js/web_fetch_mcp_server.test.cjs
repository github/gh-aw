const { execFile } = require("node:child_process");
const { createServer } = require("node:http");
const { join } = require("node:path");
function callServer(args, proxy = "") {
  return new Promise((resolve, reject) => {
    const child = execFile(
      process.execPath,
      [join(__dirname, "web_fetch_mcp_server.cjs")],
      {
        env: { PATH: process.env.PATH, HTTP_PROXY: proxy, NO_PROXY: "*" },
        timeout: 5000,
      },
      (error, stdout) => {
        if (error) return reject(error);
        resolve(
          stdout
            .trim()
            .split("\n")
            .map(line => JSON.parse(line))
            .find(reply => reply.id === 2)?.result
        );
      }
    );
    child.stdin.end(
      [
        { jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-03-26" } },
        { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "web_fetch", arguments: args } },
      ]
        .map(request => JSON.stringify(request))
        .join("\n") + "\n"
    );
  });
}

describe("sandboxed web-fetch stdio MCP", () => {
  it("routes every hop through the proxy despite NO_PROXY and returns bounded text", async () => {
    const received = [];
    const proxy = createServer((req, res) => {
      received.push([req.method, req.url, req.headers.authorization, req.headers.cookie]);
      if (req.url === "http://first.example/") {
        res.writeHead(302, { location: "http://second.example/" }).end();
      } else {
        res.writeHead(200, { "content-type": "text/html" }).end("<p>Hello world</p><script>ignored</script>");
      }
    });
    await new Promise(resolve => proxy.listen(0, "127.0.0.1", resolve));
    try {
      const result = await callServer({ url: "http://first.example/", max_length: 5 }, `http://127.0.0.1:${proxy.address().port}`);
      expect(JSON.parse(result.content[0].text)).toMatchObject({ url: "http://second.example/", status: 200, content: "Hello", truncated: true });
      expect(received).toEqual([
        ["GET", "http://first.example/", undefined, undefined],
        ["GET", "http://second.example/", undefined, undefined],
      ]);
    } finally {
      await new Promise(resolve => proxy.close(resolve));
    }
  });

  it("fails closed without a proxy", async () => {
    const result = await callServer({ url: "http://example.com/" });
    expect(result).toMatchObject({ isError: true, content: [{ type: "text", text: expect.stringContaining("workflow network policy") }] });
  });
});
