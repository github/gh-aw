# ADR-73887: Isolate MCP Script Secrets per Tool with Allowlisted Child Environments

**Date**: 2026-11-01
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR author/reviewer) [TODO: verify]

---

### Context

MCP scripts (`mcp-scripts:`) let workflow authors declare custom tools whose handlers (JavaScript, Bash, Python, Go) run as child processes of an MCP scripts server on the runner. Each tool may declare an `env:` block that binds secrets. Previously the compiler merged every tool's `env:` entries into one shared map keyed only by variable name (`collectMCPScriptsSecrets`), injected those values into the server, the MCP gateway, and the agent engine environments (`applyMCPScriptsSecretEnv`, `collectCommonMCPSecrets`), and handlers were spawned with a copy of the full `process.env`. As a result a tool could read sibling tools' secrets, two tools declaring the same variable name silently overwrote each other, and credentials leaked into agent/gateway processes and dependency installers. This was flagged as a HIGH CWE-200 (information exposure) code scanning alert (#1058).

### Decision

We will give every `(tool name, variable name)` pair a distinct, collision-free transport variable (`GH_AW_MCP_SCRIPTS_ENV_<hex(tool)>_<hex(name)>`) that is shared by the generated tools manifest and the MCP scripts server's startup environment. At invocation time each handler receives a freshly built environment (`createToolEnvironment`) containing only a fixed runtime allowlist (`PATH`, `HOME`, temp dirs, locale, `GITHUB_WORKSPACE`, `RUNNER_TEMP`, Go toolchain paths) plus that tool's own declared bindings, resolved from the transport names; a missing binding is a hard error. Tool credentials are no longer added to agent or gateway environments, and dependency installers run with the allowlist only. The primary driver is least-privilege secret exposure between co-hosted tools.

### Alternatives Considered

#### Alternative 1: Keep the shared environment but strip sibling variables at spawn time

Continue passing the merged secrets map and `process.env` to each child, deleting variables declared by other tools before spawning. This is a smaller change, but it is a denylist: anything not explicitly removed (runner credentials, server auth tokens, ambient secrets) still reaches the handler, and it does not fix the overwrite collision when two tools declare the same variable name. Rejected because it cannot provide a reliable isolation boundary.

#### Alternative 2: Run one MCP scripts server (or process) per tool

Start a separate server process per tool, each with only that tool's secrets in its environment. This gives strong process-level separation without name mangling, but multiplies startup cost, ports/gateway entries, and configuration complexity for every workflow with several tools. Rejected as disproportionate to the problem, which can be solved at the child-process spawn boundary.

#### Alternative 3: Sanitized (non-hex) per-tool variable names

Prefix variables with a sanitized tool name (e.g. uppercased, non-alphanumerics replaced with `_`). More readable, but distinct tool/variable pairs can sanitize to the same name and reintroduce collisions. Hex encoding was chosen because it is injective.

### Consequences

#### Positive
- A tool handler can no longer observe another tool's secrets, the server's own auth token, or unrelated runner credentials.
- Tools that declare the same variable name (e.g. `API_KEY`) each get their own value instead of last-writer-wins.
- Agent engine and MCP gateway environments no longer carry MCP script credentials, shrinking the exposure surface.
- Misconfiguration fails loudly ("Missing MCP script environment binding") rather than silently running with an absent value.

#### Negative
- Breaking change: existing workflows must be recompiled (29 lock files regenerated in this PR) because manifest and startup env names changed.
- Handlers that relied on ambient runner variables (anything outside the allowlist) must now declare them explicitly in the tool's `env:` block; dependency installers no longer inherit credentials (e.g. private registry tokens).
- The runtime allowlist is a hard-coded list that must be maintained as new runtimes or toolchain variables are needed.
- Hex-encoded transport names are opaque when debugging lock files.

#### Neutral
- This is an environment-variable boundary only; tools still run trusted code on the runner host with no filesystem or network sandboxing.
- Transport variable names never reach the child process; only the tool-declared names do.
- Documentation (`mcp-scripts.md` reference and specification) is updated to describe the per-tool binding contract.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
