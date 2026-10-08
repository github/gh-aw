---
title: Using Agy with GitHub Agentic Workflows
description: Configure the experimental Google Antigravity CLI engine with Gemini API-key authentication, native models, sandboxed tools and its verified Linux release.
---

> [!WARNING]
> Agy is an experimental built-in engine, not a Gemini alias or a feature-equivalent
> replacement. Initial support is Linux x64 with native CLI v1.3.1 and Gemini
> API-key authentication. ADC, Google WIF and Vertex AI are not supported.

## Selection and authentication

Set `engine: agy`; no shared-engine import is required. Both selection forms work:

```yaml
engine: agy
```

```yaml
engine:
  id: agy
  model: gemini-3.8-flash-medium
  version: "1.3.1"
```

Configure `GEMINI_API_KEY` as a repository secret, for example with
`gh aw secrets bootstrap --engine agy`. `GOOGLE_API_KEY` is not an alternative.
The harness explicitly selects `modelProvider: gemini` in a private per-run
`~/.gemini/antigravity-cli/settings.json`; an API key alone does not select this
native authentication profile.

The provider credential remains outside the agent sandbox. Agy receives a dummy
credential and the configured Gemini endpoint discovered from AWF. Endpoint
discovery failure is fatal; it does not fall back to direct inference or
interactive authentication.

## Models and installation

The default native model slug is `gemini-3.8-flash-medium`. Use native Agy slugs,
not `google/`, `gemini/` or Copilot provider prefixes. Availability depends on
the native release and the API key's access; an unknown or inaccessible model
fails the run.

The harness downloads the native Linux x64 v1.3.1 release, verifies its published
SHA-256 checksum, extracts only `antigravity`, and verifies the executable version.
Other releases and architectures are not supported. A custom `engine.command`
must report the same verified version. Installation and settings are temporary
and removed after execution; no inherited user authentication profile is used.

## Tools, configuration and limits

Agy consumes native streaming JSON input from stdin, keeping prompts out of
command arguments. Native streaming events are normalized into session artifacts;
repeated cumulative usage snapshots are not added together. Missing metrics
remain absent.

Configured HTTP MCP servers are translated from the gh-aw gateway into
owner-only `.agents/mcp_config.json`, using native `serverUrl` entries and
validated headers. CLI-mounted infrastructure tools, including `safeoutputs`
and `mcpscripts`, are omitted from native MCP configuration. Repository MCP
configuration is replaced by the explicitly configured gateway servers.
`AGENTS.md`, `GEMINI.md`, `.agents/` and `.gemini/` are protected instruction
and configuration surfaces.

Native permissions use blanket approvals **inside the outer gh-aw sandbox**,
not as the security boundary. Per-command bash restrictions, `bash: false`,
empty bash allowlists, and disabling native editing or web tools are rejected
rather than silently ignored. The fixed native timeout is five minutes, with
a wrapper watchdog and the Actions step timeout as additional bounds.
Permission denials, pending tool executions, interruption and non-success
native results fail execution even when the CLI exits zero. Successful execution
also requires a completed native turn with positive input and output token usage.

`engine.args`, `engine.bare`, `engine.api-target`, `engine.config`, `engine.cwd`,
provider overrides and `engine.auth` are rejected. Native `engine.permission-mode`,
`max-turns`, continuations, and harness or driver overrides are also rejected.
Use `max-turn-cache-misses`, `max-ai-credits` and `timeout-minutes` for outer
execution limits. Agent Plugins and native custom-agent selection are not
supported. Agy-specific settings, skills and hooks are not interchangeable with
Gemini settings.

## Troubleshooting and conformance

Missing credentials fail activation. A model-provider error usually means the
native profile was not selected; gh-aw creates that profile automatically.
AWF endpoint errors require a working configured Gemini proxy, not an additional
credential inside the agent.

The manual native authentication probe checks real inference and negative
authentication/model cases. `engine-conformance-agy.md` separately exercises the
production installer, AWF, native MCP, CLI-mounted MCP tools and staged safe
outputs. It remains dispatch-only; Credentials Check calls the feature-branch
`agy-conformance-reusable.lock.yml`, compiled from the same shared probes and
prompt. Release and Gemini deprecation are gated on that production path, not
on mocked tests or native authentication alone.

See the [engine reference](/gh-aw/reference/engines/) and
[authentication reference](/gh-aw/reference/auth/).

## Evaluating a Gemini API-key workflow

Do not migrate a WIF workflow or one requiring native bash restrictions.
For an API-key workflow within Agy's documented capabilities, changing the
engine is an explicit choice, not an automatic migration:

| Gemini configuration | Experimental Agy configuration |
|---|---|
| `engine: gemini` | `engine: agy` |
| Gemini CLI version pin | Verified native `version: "1.3.1"` only |
| Gemini model name | Native Agy slug, such as `gemini-3.8-flash-medium` |
| `GEMINI_API_KEY` | Same secret name; private `modelProvider: gemini` settings |
| Gemini settings, hooks or customizations | Not copied; review native configuration differences |
| Native bash allowlist or disabled shell | Unsupported; keep Gemini |

Recompile and evaluate the bounded production conformance workflow before
relying on the replacement. Existing Gemini binaries, pins and historical logs
retain their identity; no alias, codemod or removal date is introduced.
