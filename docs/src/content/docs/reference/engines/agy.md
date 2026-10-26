---
title: Agy reference
description: Agy engine configuration, supported tools, runtime behavior, and limitations.
---

For first-time setup, see [Using Agy](/gh-aw/engines/agy/). Shared options are in [AI engines](/gh-aw/reference/engines/).

## Selection and authentication

Set `engine: agy`; no shared-engine import is required.

Both selection forms work:

```yaml
engine: agy
```

```yaml
engine:
  id: agy
  model: gemini-3.8-flash-medium
  version: "1.3.6"
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

The default native model slug is `gemini-3.8-flash-medium`. Exact ids must be
native Agy slugs, not `google/`, `gemini/` or Copilot provider prefixes, and are
passed through unchanged. Availability depends on the native release and the API
key's access; an unknown or inaccessible model fails the run.

Model aliases ([how they resolve](/gh-aw/reference/engines/#model-aliases))
resolve against the Gemini provider's live models from AWF `/reflect`, and Agy
receives the native slug.

The harness downloads the native Linux x64 v1.3.6 release, verifies its published
SHA-256 checksum, extracts only `antigravity`, and verifies the executable version.
Explicit `version: "1.3.3"` and `version: "1.3.1"` retain the previous verified releases.
Other releases and architectures are not supported. A custom `engine.command`
must report the same verified version. Installation and settings are temporary
and removed after execution; no inherited user authentication profile is used.

The archive checksum applies to the default installer only. `engine.command`
selects a separately trusted executable; its version is checked, but its bytes
are not attested by that archive checksum.

When Agy runs with the firewall enabled, add `agy` to `network.allowed` to
permit direct GitHub and release-download access. The bundle is not added
automatically; gateway connectivity remains available independently.

## Tools, configuration and limits

Configured HTTP MCP servers are translated into owner-only `.agents/mcp_config.json` with validated headers. Safe outputs, scripts, custom servers, and enclaves use native MCP unless explicitly mounted through `tools.cli-proxy`. Repository MCP configuration is replaced by the configured gateway servers. `AGENTS.md`, `GEMINI.md`, `.agents/`, and `.gemini/` are protected configuration surfaces.

Native tools receive broad approval **inside the outer sandbox**. The integration does not translate workflow restrictions into Agy's scoped native permissions.

| Setting | Behavior |
| --- | --- |
| Per-command bash restrictions, `bash: false`, empty bash allowlists | Rejected. |
| Disabled native editing or web tools | Rejected. |
| `engine.args`, `bare`, `api-target`, `config`, `cwd`, `auth`, provider overrides | Rejected. |
| Native `permission-mode`, `max-turns`, continuations, custom harness/driver | Rejected. |
| Agent Plugins and native custom-agent selection | Unsupported. |
| `max-turn-cache-misses`, `max-ai-credits`, `timeout-minutes` | Supported outer limits. |
| Native execution timeout | Fixed five minutes; wrapper watchdog and Actions timeout also apply. |

Permission denials, pending tools, interruption, and non-success native results fail execution even with exit code zero. Success requires a completed turn with positive input and output token usage. Cancellation signals the isolated process group, escalates to SIGKILL, and bounds output draining.

The built-in headless profile uses `--dangerously-skip-permissions`, not `/plan` mode. Dry-run compilation permits this internal flag but does not authorize live execution or replace sandbox/credential isolation.

## Troubleshooting and conformance

Missing credentials fail activation. Provider-selection errors indicate a problem with the generated native Gemini profile; AWF discovery errors require a working Gemini proxy, not credentials inside the agent.

The manual `engine-conformance-agy.md` workflow checks the production installer, AWF, native MCP, CLI-mounted tools, and staged safe outputs. Stable promotion depends on this path, not only native authentication probes or mocked tests. Native and unified-session parsing retain observed tools, errors, and usage; see the [session specification](/gh-aw/specs/unified-agent-session-specification/).

## Evaluating a Gemini API-key workflow

Do not migrate a WIF workflow or one requiring native bash restrictions.
For an API-key workflow within Agy's documented capabilities, changing the
engine is an explicit choice, not an automatic migration:

| Gemini configuration | Experimental Agy configuration |
|---|---|
| `engine: gemini` | `engine: agy` |
| Gemini CLI version pin | Verified native `version: "1.3.6"`; explicit `"1.3.3"` and `"1.3.1"` remain supported |
| Gemini model name | Native Agy slug, such as `gemini-3.8-flash-medium` |
| `GEMINI_API_KEY` | Same secret name; private `modelProvider: gemini` settings |
| Gemini settings, hooks or customizations | Not copied; review native configuration differences |
| Native bash allowlist or disabled shell | Unsupported; keep Gemini |

Recompile and evaluate the bounded production conformance workflow before
relying on the replacement. Existing Gemini binaries, pins and historical logs
retain their identity; no alias, codemod or removal date is introduced.
