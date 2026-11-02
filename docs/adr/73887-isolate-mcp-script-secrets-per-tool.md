# ADR-73887: Isolate MCP Script Secrets per Tool with Allowlisted Child Environments

**Date**: 2026-11-01
**Status**: Proposed (maintainer acceptance pending)
**Deciders**: Not recorded; no maintainer approval is asserted.
**Pull request**: [#73887](https://github.com/github/gh-aw/pull/73887)

---

### Context

MCP scripts (`mcp-scripts:`) let workflow authors declare custom tools whose handlers (JavaScript, Bash, Python, Go) run as child processes of an MCP scripts server on the runner. Each tool may declare an `env:` block containing secrets or non-secret configuration. Previously the compiler merged every tool's bindings into one shared map keyed only by variable name, forwarded those values to the server, MCP gateway, and agent engine environments, and spawned handlers and dependency installers with the full `process.env`. Consequently, a handler inherited sibling tools' secrets and unrelated server or runner credentials, while two tools declaring the same variable name silently overwrote each other's bindings.

The required boundary is least-privilege environment inheritance for co-hosted tools, not isolation of mutually untrusted programs. Repository context and dependency-install configuration must remain usable without restoring wholesale environment inheritance.

### Decision

The compiler assigns every `(tool name, variable name)` pair a distinct transport variable, `GH_AW_MCP_SCRIPTS_ENV_<hex(tool)>_<hex(name)>`. The generated tools manifest maps tool-local names to those references, and the MCP scripts server startup environment supplies the values. Encoding both components separately preserves distinct pairs, including names that would collide after sanitization.

At invocation, each language handler receives a freshly built environment containing an explicit runtime allowlist plus only that tool's declared bindings. Runtime entries preserve interpreter lookup, home and temporary directories, locale, workspace location, and supported toolchain paths. Explicit bindings take precedence over allowlisted defaults; missing references fail before handler execution. The child receives tool-local names, not compiler-generated transport names.

MCP script bindings are not automatically forwarded to agent or gateway environments. The gateway still receives the separate MCP scripts HTTP authentication key needed to call the server. Dependency installers receive an explicit non-credential configuration allowlist, not tool bindings or the server's full environment. This preserves named proxy, registry/index, and CA-file settings; URL settings reject unsupported schemes, user information, queries, and fragments with variable-name-only diagnostics. Endpoint paths must also be credential-free: URL validation cannot identify arbitrary secrets embedded in paths.

Repository-dependent tools must declare their repository context explicitly rather than depend on ambient inheritance. The shared issue, pull request, and discussion query tools bind `GITHUB_REPOSITORY` to `${{ github.repository }}`; the discussion tool also binds `GITHUB_REPOSITORY_OWNER` to `${{ github.repository_owner }}`. This retains repository defaults without expanding the runtime allowlist.

### Alternatives Considered

#### Alternative 1: Keep the shared environment but strip sibling variables at spawn time

Continue passing the merged secrets map and `process.env` to each child, deleting variables declared by other tools before spawning. This is a smaller change, but it is a denylist: anything not explicitly removed (runner credentials, server auth tokens, ambient secrets) still reaches the handler, and it does not fix the overwrite collision when two tools declare the same variable name. Rejected because a denylist cannot reliably constrain environment inheritance.

#### Alternative 2: Run one MCP scripts server (or process) per tool

Start a separate server process per tool, each with only that tool's secrets in its environment. This reduces secret aggregation in the server without name mangling, but multiplies startup cost, ports/gateway entries, and configuration complexity. Processes sharing a runner user and filesystem still do not provide a sandbox against hostile tools. Rejected because the selected child-process environment boundary addresses unintended inheritance without multiplying servers.

#### Alternative 3: Sanitized (non-hex) per-tool variable names

Prefix variables with a sanitized tool name (e.g. uppercased, non-alphanumerics replaced with `_`). More readable, but distinct tool/variable pairs can sanitize to the same name and reintroduce collisions. Hex encoding was chosen because it is injective.

### Consequences

#### Positive
- A generated tool handler no longer inherits sibling bindings, the server's HTTP authentication key, or unrelated runner credentials.
- Tools that declare the same variable name (e.g. `API_KEY`) each get their own value instead of last-writer-wins.
- Agent engine and MCP gateway environments no longer carry MCP script credentials, shrinking the exposure surface.
- Misconfiguration fails loudly ("Missing MCP script environment binding") rather than silently running with an absent value.

#### Negative
- Existing workflows should be recompiled and published with matching manifest, startup environment, and runtime revisions. The new transport contract changes generated lock files. The runtime still accepts explicit environment references, including older direct-name references when present in the server environment, but does not restore the former full-environment inheritance.
- Handlers relying on variables outside the runtime allowlist must declare them in the tool's `env:` block, including repository identity needed by GitHub CLI calls.
- Dependency installers retain allowlisted non-credential proxy, certificate, registry, and toolchain configuration, but do not inherit private-registry credentials from tool bindings. Installing authenticated dependencies may require a separate trusted setup step.
- Runtime and installer allowlists require maintenance as supported runtimes and configuration needs evolve.
- Hex-encoded transport names are opaque when debugging lock files.

#### Neutral
- The shared server still holds all tool transport values. This is an environment-inheritance boundary only: handlers and installers remain trusted code on the runner host, with shared filesystem and network access. It does not prevent reading credentials from accessible files, inspecting other processes where host permissions permit, or a tool returning its own secret in output.
- Transport variable names never reach the child process; only the tool-declared names do.
- The [MCP scripts reference](../src/content/docs/reference/mcp-scripts.md) and [specification](../src/content/docs/specs/mcp-scripts-specification.md) describe the per-tool binding contract.

---

Maintainers must record the actual deciders and acceptance before changing the status to Accepted.
