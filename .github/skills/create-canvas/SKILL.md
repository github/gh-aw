---
name: create-canvas
description: Author, validate, and debug canvas extensions that the agent can open in the GitHub Copilot app's side panel. Use when creating, reviewing, or troubleshooting a canvas extension — including its actions, inputs, rendered content, and the surrounding extension wiring (tools, hooks, lifecycle).
---

# Extension Canvas Authoring

Use this skill when creating or debugging an extension canvas that integrates with the runtime canvas model.

The host app no longer participates in extension canvas registration. Extensions declare canvases directly to the runtime via the Copilot SDK on `joinSession`; the runtime routes provider callbacks (`canvas.open`, `canvas.action.invoke`, etc.) straight to the declaring connection; the host app just renders whatever URL the provider returns.

## Authoring workflow (do this in order)

1. **Decide the scope.** Project (`.github/extensions/<name>/`) is committed to the repo and shared with the team. User (`$COPILOT_HOME/extensions/<name>/`) is local to the current user. Session (`$COPILOT_HOME/session-state/<sessionId>/extensions/<name>/`) is loaded only for the current session — ideal for a throwaway canvas for one conversation. Unless the request makes it obvious, ask before creating files — the choice changes who sees the extension, whether it's committed, and the resulting `extensionId`.

2. **Scaffold via tool, don't hand-write the skeleton.**
   ```
   extensions_manage({ operation: "scaffold", kind: "canvas", name: "<name>", location: "project" | "user" | "session" })
   ```
   The scaffold produces `extension.mjs` with a loopback server per instance, an example action, and `onClose` cleanup. Import `joinSession` and `createCanvas` from `@microsoft/copilot-sdk/extension`. In v1 scaffolds, move the bare `canvases` and `tools` arrays into `canvases.definitions` and `tools.definitions`, `hooks` into `hooks.handlers`, and `onPermissionRequest` into `permissions.onRequest`, as shown below.

3. **Edit the file.** Implement your `open`, `actions[]`, optional `onClose`, and (if needed) tools/hooks alongside.

4. **Reload.** Call `extensions_reload`. Stale canvas instances flip to `stale`, then back to `ready` once the provider reconnects.

5. **Verify.** Call `extensions_manage({ operation: "list" })` and `extensions_manage({ operation: "inspect", name: "<name>" })`. If the extension is marked failed, `inspect` surfaces the log file path and a tail — that log is the primary debugging surface.

6. **Drive it.** Run the validation checklist below via `list_canvas_capabilities`, `open_canvas`, and `invoke_canvas_action`.

## Extension shape

A canvas extension is a Copilot CLI extension running as a forked Node process that speaks JSON-RPC over stdio to the CLI.

- Entry file **must** be named `extension.mjs` (ES modules only; TypeScript is not supported).
- Discovery scans only immediate subdirectories of `.github/extensions/` (relative to git root), `$COPILOT_HOME/extensions/`, and `$COPILOT_HOME/session-state/<sessionId>/extensions/`.
- The runtime auto-derives a stable `extensionId` of `${source}:${name}` (e.g. `project:my-extension`); session-scoped extensions embed the session id as `session:<sessionId>:<name>`.
- Use a Copilot CLI that resolves `@microsoft/copilot-sdk/extension` automatically (such as 1.0.113). Do **not** add a `package.json` or `node_modules` for it.
- `stdout` is reserved for JSON-RPC. **Never `console.log`.** Use `session.log(message, { level, ephemeral })`.
- Tool names must be globally unique across all loaded extensions — collisions cause the second extension to fail.

## `joinSession` signature

```js
import { joinSession, createCanvas, CanvasError } from "@microsoft/copilot-sdk/extension";

const session = await joinSession({
    canvases: { definitions: [createCanvas({ /* ... */ })] },
    tools: { definitions: [/* optional */] },
    hooks: { handlers: {/* optional */} },
    permissions: { onRequest: async (request) => ({ kind: "approve-once" }) }, // optional
});
// session.sessionId, session.workspacePath (string | undefined),
// session.send, session.sendAndWait, session.log, session.on, session.rpc
```

## Canvas API (`canvas.d.ts`)

```js
import { createCanvas, CanvasError, joinSession } from "@microsoft/copilot-sdk/extension";

const canvas = createCanvas({
    id: "main",                  // unique within this extension
    displayName: "My canvas",    // human-readable label shown in host chrome
    description: "Short summary the agent sees in the system-prompt canvas catalog.",
    inputSchema: { /* JSON Schema for open input (optional) */ },
    actions: [
        {
            name: "do_thing",        // unique within this canvas; MUST NOT start with `canvas.`
            description: "...",
            inputSchema: { /* JSON Schema (optional) */ },
            handler: async (ctx) => {
                // ctx: { sessionId, extensionId, canvasId, instanceId, actionName, input, host }
                // Return raw result; throw CanvasError("code", "message") for errors.
                return { /* ... */ };
            },
        },
    ],
    open: async (ctx) => {
        // ctx: { sessionId, extensionId, canvasId, instanceId, input, host }
        // Idempotent: same instanceId may arrive again after provider reconnect.
        return {
            url: "http://127.0.0.1:<loopback-port>/", // optional for native canvases
            title: "...",   // optional, shown in host chrome
            status: "...",  // optional, shown in host chrome
        };
    },
    onClose: async (ctx) => {
        // Optional. Fire-and-forget; return value ignored.
    },
});
```

Runtime rules:
- Action names starting with `canvas.` are reserved and rejected at declaration time.
- Every `actions[]` entry must have a `handler`; missing handlers fall through to `CanvasError.noHandler()`.
- Canvas-level `inputSchema` is validated before `open`; action-level `inputSchema` is validated before dispatch. Failure returns `canvas_input_invalid` with Ajv details.
- Return raw values from action handlers; throw `CanvasError("code", "message")` for errors.
- `ctx.canvasId` (not `ctx.id`) is the canvas identifier in handler context.

Bind embedded servers to **loopback only** (`127.0.0.1`). The host only embeds loopback URLs. Push state updates to the iframe with Server-Sent Events (`/events`).

Use the [validation checklist](#validation-checklist) and [debugging steps](#debugging) after editing. See [state model](#state-model) for persistence rules.

<!-- instruction-prefix-end -->

## Loopback server pattern

```js
import { createServer } from "node:http";
import { createCanvas, joinSession } from "@microsoft/copilot-sdk/extension";

const servers = new Map(); // instanceId → { server, url }

async function startServer(instanceId) {
    const server = createServer((req, res) => {
        res.setHeader("Content-Type", "text/html; charset=utf-8");
        res.end(renderHtml(instanceId));
    });
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    const port = server.address().port;
    return { server, url: `http://127.0.0.1:${port}/` };
}

await joinSession({
    canvases: { definitions: [
        createCanvas({
            id: "my-canvas",
            displayName: "My canvas",
            description: "...",
            open: async (ctx) => {
                let entry = servers.get(ctx.instanceId);
                if (!entry) {
                    entry = await startServer(ctx.instanceId);
                    servers.set(ctx.instanceId, entry);
                }
                return { title: "My canvas", url: entry.url };
            },
            onClose: async (ctx) => {
                const entry = servers.get(ctx.instanceId);
                if (entry) {
                    servers.delete(ctx.instanceId);
                    await new Promise((r) => entry.server.close(() => r()));
                }
            },
        }),
    ] },
});
```

## ID model — three distinct identifiers

- **`canvasId`** — the canvas *type* declared by the extension. Accepted by `list_canvas_capabilities` and `open_canvas`; not by `invoke_canvas_action`.
- **`extensionId`** — auto-derived `${source}:${name}` (e.g. `project:triage-board`). Only pass it to disambiguate when two extensions declare the same `canvasId`.
- **`instanceId`** — caller-invented handle for one running panel (slug or UUID). Two panels of the same canvas need two `instanceId`s. Validated against `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` — max 128 chars, alphanumeric first character, then only `[A-Za-z0-9._-]`.

Example: `open_canvas({ canvasId: "triage-board", instanceId: "triage-1" })` then `invoke_canvas_action({ instanceId: "triage-1", actionName: "refresh" })`.

## Agent-side canvas tools

```
// Open a canvas panel
open_canvas({ canvasId: "my-canvas", instanceId: "my-canvas-1", input?: {...} })
// Returns { instanceId, url? } — no extensionId needed unless two providers share the canvasId.

// Discover a canvas type's actions and input schema
list_canvas_capabilities({ canvasId: "my-canvas" })

// Invoke an action on an open panel
invoke_canvas_action({ instanceId: "my-canvas-1", actionName: "do_thing", input?: {...} })
```

Re-opening the same `instanceId` focuses the existing panel (the host emits `session.canvas.opened` with `reopen: true` and reloads the iframe without invoking the provider again).

## Injecting instructions

- **`additionalContext` from hooks** (everyday path). `onSessionStart` → once-per-session guidance; `onUserPromptSubmitted` → per-turn nudges. The runtime appends it as a `developer`-role message.
- **`systemMessage` on `joinSession`** (system-prompt path):
  - `{ mode: "append", content }` — append to the SDK foundation.
  - `{ mode: "customize", sections: {...} }` — override individual sections (`identity`, `tone`, `safety`, `custom_instructions`, `runtime_instructions`, etc.) with `replace` / `remove` / `append` / `prepend` / transform-callback actions.
  - **Do NOT use** `{ mode: "replace", content }` from an extension — it discards the entire SDK-managed prompt including safety guardrails.

## State model

**`instanceId` identifies the panel, not the data.** Never key persistent state by `instanceId` alone — iframes reload, extensions restart, and a fresh `instanceId` may be opened for the same logical content.

Pick the right storage scope:
- **Per session / workspace** — write artifact files into `session.workspacePath` (returned by `joinSession`). Lives alongside session artifacts, cleaned up with the session.
- **Per user / global** — write under `$COPILOT_HOME/extensions/<extension-name>/artifacts/`. Follows the user across sessions.
- **Per artifact / document** — key under a stable `documentId` or file path passed via `input`.
- **In-repo, committed artifacts** — architecture diagrams, ADRs, design docs that belong in the repo. Write to a sensible repo path.
- **Per panel (`instanceId`)** — only ephemeral UI state that genuinely should not survive a reload.

Resolve the owning ID in `open()` from `input`, load persisted state for that ID, and render. Two `instanceId`s for the same `documentId` should show the same content.

## App theme tokens

The runtime mirrors the canvas theme contract onto your canvas document:
- Root/body attributes: `data-color-mode`, `data-dark-theme`, `data-light-theme`, `data-theme-source`, `data-theme-tone`, `data-visual-mode`, `pointer-on-hover` class.
- Semantic tokens: `--background-color-default`, `--border-color-default`, `--text-color-default`, `--text-color-muted`, `--color-focus-outline`.
- Type ramp: `--font-sans`, `--font-mono`, `--font-weight-semibold`, `--text-body-medium`, `--leading-body-medium`.
- True-color: `--true-color-red`, `--true-color-red-muted`, `--true-color-blue`, `--true-color-blue-muted`.

```css
body {
    margin: 0;
    background: var(--background-color-default, #ffffff);
    color: var(--text-color-default, #1f2328);
    font-family: var(--font-sans, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
    font-size: var(--text-body-medium, 14px);
    line-height: var(--leading-body-medium, 20px);
}
```

Only depend on the attributes, classes, and variables documented above — others are app-internal and can change.

## Validation checklist

1. **Discovery** — confirm your canvas appears in the `<canvases>` section of the system prompt, then verify `list_canvas_capabilities({ canvasId })` returns your `actions[]`.
2. **Open** — call `open_canvas({ canvasId, input? })`. Confirm you receive `{ instanceId, url? }` and no error.
3. **Action** — call `invoke_canvas_action({ instanceId, actionName, input? })` and confirm correct response payload.
4. **Input validation** — call `open_canvas` with input that fails `inputSchema`. Expect `canvas_input_invalid`.
5. **Reserved verb rejection** — call `invoke_canvas_action(instanceId, "canvas.open", ...)`. Expect `canvas_reserved_action_name`.

## Debugging

1. `extensions_manage({ operation: "list" })` — is the extension loaded? Marked `failed`?
2. `extensions_manage({ operation: "inspect", name: "<name>" })` — surfaces log file path and tail. Primary debugging surface.
3. After editing, **always** `extensions_reload` before re-running RPC calls.
4. To force iframe reload, call `open_canvas` again with the same `instanceId`.
5. If not discovered at all, verify the file is named exactly `extension.mjs` and lives in an immediate subdirectory of `.github/extensions/`, `$COPILOT_HOME/extensions/`, or the session's extensions dir.

## Sharing extensions via Gist

Use `share_extension` / `install_extension` tools. Gist format:
- Flat file structure (subdirs encoded as `\` in gist keys, decoded on install).
- A `copilot-extension.json` manifest: `{ "name": "<extension-name>", "version": 1 }`.
- `node_modules/`, `dist/`, `build/`, `.git/`, hidden dirs (except `.github/`) skipped during share.
- Per-file cap: ~1 MB; total cap: ~5 MB.

## Common pitfalls

- **`console.log` corrupts JSON-RPC.** Use `session.log()` for user-visible messaging.
- Do not declare action names starting with `canvas.`.
- Do not forget to wire a `handler` on every `actions[]` entry.
- Bind servers to **loopback only**.
- `open` may be re-invoked on provider reconnect — treat it as idempotent and rehydrate from durable storage.
- Return raw values from action handlers; throw `CanvasError("code", "message")` for errors.
- The canvas context field is `ctx.canvasId`, not `ctx.id`.
- Tool names must be globally unique across all loaded extensions.
- `extension.mjs` only — `.ts` is not supported. The CLI resolves `@microsoft/copilot-sdk/extension`; do not add a `package.json` for it.

## Session object quick reference

```js
await session.log("message", { level?: "info"|"warning"|"error", ephemeral?: boolean });
await session.send({ prompt: "...", attachments?: [{ type: "file", path: "..." }] });
const response = await session.sendAndWait({ prompt: "..." });
const unsub = session.on("tool.execution_complete", (event) => { /* ... */ });
// session.workspacePath — path to session workspace (string | undefined)
// session.sessionId — current session ID
```

Key event types: `assistant.message`, `tool.execution_start`, `tool.execution_complete`, `user.message`, `session.idle`, `session.error`, `permission.requested`, `session.shutdown`.
