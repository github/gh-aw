---
# Documentation Server Lifecycle Management
# 
# This shared workflow provides instructions for starting, waiting for readiness,
# and cleaning up the Astro Starlight documentation dev server.
#
# Prerequisites:
# - npm install must have been run in docs/ directory
# - Bash permissions: npm *, npx *, curl *, kill *, echo *, sleep *
# - Working directory should be in repository root
# - Node.js >= 20.3.0 or >= 22 required (Astro 6.x requirement)
#   Workflows using this import should set: runtimes: node: version: "22"
---

## Starting the Documentation Preview Server

Navigate to the docs directory and start the development server in the background, binding to all network interfaces on a fixed port:

```bash
mkdir -p /tmp/gh-aw/agent
cd docs
nohup npm run dev -- --host 0.0.0.0 --port 4321 > /tmp/gh-aw/agent/preview.log 2>&1 &
PID=$!
echo $PID > /tmp/gh-aw/agent/server.pid
echo "Server PID: $PID"
```

This will:
- Start the Astro development server on port 4321, bound to all interfaces (`0.0.0.0`)
- Redirect output to `/tmp/gh-aw/agent/preview.log`
- Save the process ID to `/tmp/gh-aw/agent/server.pid` for later cleanup

**Note on the `nohup ... & PID=$!` pattern:** The `$!` variable (background PID) is captured into `PID` first, then written to file. Avoid `echo $! > file` in a single line — the AWF bash guard may flag `$!` as a dangerous expansion when it appears directly in a redirection context.

**Node.js version requirement:**
Astro 6.x requires Node.js >= 20.3.0 or >= 22.0.0. Workflows that use this shared lifecycle **must** configure a compatible runtime:
```yaml
runtimes:
  node:
    version: "22"
```
Without this, the dev server may fail with a Node.js version error and the agent will waste time debugging workarounds.

**Why `npm run dev` instead of `npm run preview`:**
The `npm run preview` command serves the pre-built static output. However, Astro's Starlight documentation site uses hybrid routing which requires the development server (`astro dev`) to correctly serve all pages at the `/gh-aw/` base URL. Using `npm run preview` returns 404 for `/gh-aw/` paths.

**Why `--host 0.0.0.0 --port 4321` is required:**
The agent runs inside a Docker container. Binding to `0.0.0.0` ensures the server is reachable from any interface. The `--port 4321` flag prevents port conflicts if a previous server instance is still shutting down.

## Waiting for Server Readiness

Poll the server with curl until the `/gh-aw/` path returns HTTP 200:

```bash
URL="http://localhost:4321/gh-aw/"
STATUS=""
echo "Readiness check target: $URL"
for i in {1..45}; do
  STATUS=$(curl -sS -o /dev/null -w "%{http_code}" --connect-timeout 5 --max-time 5 "$URL" || true)
  [ "$STATUS" = "200" ] && echo "Server ready at $URL" && break
  if [ -z "$STATUS" ]; then STATUS="curl_error"; fi
  echo "Waiting for server... ($i/45) (status: $STATUS)" && sleep 3
done
if [ "$STATUS" != "200" ]; then
  echo "Dev server failed to start after 135 seconds (final status: $STATUS)"
  cat /tmp/gh-aw/agent/preview.log || true
  exit 1
fi
```

This will:
- Attempt to connect up to 45 times (135 seconds total) to allow for Astro dev server startup
- Check that `/gh-aw/` specifically returns HTTP 200 (not just that the port is open)
- Avoid exiting early on transient curl connection failures (`|| true` keeps the loop running under `set -e`)
- Wait 3 seconds between attempts
- Exit successfully when the docs site is fully accessible

## Playwright Browser Access

With the built-in Playwright CLI integration, `playwright-cli` runs directly on the runner — not in a Docker container. Use `localhost` directly to reach the dev server:

```bash
playwright-cli browser_navigate --url "http://localhost:4321/gh-aw/"
playwright-cli browser_take_screenshot --filename /tmp/gh-aw/agent/screenshot.png --full-page true
```

No bridge IP detection is needed in CLI mode.

## Verifying Server Accessibility (Optional)

Optionally verify the server is serving content:

```bash
curl -s http://localhost:4321/gh-aw/ | head -20
```

## Stopping the Documentation Server

After you're done using the server, clean up the process:

```bash
kill $(cat /tmp/gh-aw/agent/server.pid) 2>/dev/null || true
rm -f /tmp/gh-aw/agent/server.pid /tmp/gh-aw/agent/preview.log
```

This will:
- Kill the server process using the saved PID
- Remove temporary files
- Ignore errors if the process already stopped

## Usage Notes

- The server runs on `http://localhost:4321` and is accessible at `http://localhost:4321/gh-aw/` for curl/bash and playwright-cli
- With the built-in Playwright CLI integration, use `localhost` directly for all playwright-cli commands — no bridge IP needed
- Always clean up the server when done to avoid orphan processes
- If the server fails to start, check `/tmp/gh-aw/agent/preview.log` for errors
- Node.js >= 22 is required; ensure `runtimes: node: version: "22"` is set in the workflow frontmatter
- No `npm run build` step is required before starting the dev server
