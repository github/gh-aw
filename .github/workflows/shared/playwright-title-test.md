---
network:
  allowed:
    - playwright
    - local
    - host.docker.internal
tools:
  playwright:
  bash:
    - "cat /tmp/gh-aw/agent/playwright-title-test-expected.txt"
    - "playwright-cli *"
steps:
  - name: Start Playwright title test server
    env:
      PLAYWRIGHT_TITLE_TEST_PORT: "4173"
    run: |
      mkdir -p /tmp/gh-aw/agent
      cat > "$RUNNER_TEMP/playwright-title-test-server.cjs" <<'EOF'
      const crypto = require("node:crypto");
      const fs = require("node:fs");
      const http = require("node:http");

      const host = "0.0.0.0";
      const port = Number(process.env.PLAYWRIGHT_TITLE_TEST_PORT);
      const title = `playwright-${crypto.randomBytes(24).toString("base64url")}`;

      fs.writeFileSync(
        "/tmp/gh-aw/agent/playwright-title-test-expected.txt",
        `${title}\n`,
        { mode: 0o600 },
      );

      http.createServer((request, response) => {
        if (request.url === "/title.js") {
          response.writeHead(200, { "Content-Type": "text/javascript; charset=utf-8" });
          response.end(`document.title = ${JSON.stringify(title)};`);
          return;
        }

        response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
        response.end("<!doctype html><html><head><script src=\"/title.js\"></script></head><body>Playwright title test</body></html>");
      }).listen(port, host);
      EOF

      nohup node "$RUNNER_TEMP/playwright-title-test-server.cjs" \
        > "$RUNNER_TEMP/playwright-title-test-server.log" 2>&1 &
      echo "$!" > "$RUNNER_TEMP/playwright-title-test-server.pid"
      # runner-guard:ignore RGS-012 -- loopback-only readiness probe for the server started above; no secrets are sent.
      curl --fail --silent --show-error --retry 10 --retry-connrefused \
        --retry-all-errors --retry-max-time 30 \
        "http://127.0.0.1:${PLAYWRIGHT_TITLE_TEST_PORT}/" > /dev/null
post-steps:
  - name: Stop Playwright title test server
    if: always()
    run: |
      if [ -f "$RUNNER_TEMP/playwright-title-test-server.pid" ]; then
        kill "$(cat "$RUNNER_TEMP/playwright-title-test-server.pid")" 2>/dev/null || true
      fi
---

## Playwright title validation

Use Playwright CLI to open `http://host.docker.internal:4173/` and read the page title after
JavaScript runs. Read the expected value from
`/tmp/gh-aw/agent/playwright-title-test-expected.txt`, compare the two values for
exact equality, and report whether the test passed. Do not use curl or another
HTTP client to discover the title. Always close the browser when finished.
