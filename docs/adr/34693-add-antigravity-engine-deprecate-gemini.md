# ADR-34693: Add Experimental Agy and Soft-Deprecate Gemini

**Date**: 2026-05-25
**Status**: Draft, revised from the original generated proposal; production conformance and maintainer approval remain pending
**Deciders**: Not recorded

## Context

Google Antigravity CLI exposes the native `agy` command. It is not the Gemini CLI
binary, does not share its version pins, and is not a feature-equivalent replacement.
The original draft proposed unverified authentication variables, settings paths,
provider aliases and a copied dedicated runtime. This revision replaces those
assumptions with the authentication-first experimental implementation.

Existing `engine: gemini` workflows use API keys or Google Workload Identity
Federation (WIF), and can depend on native command restrictions. They must keep
their actual Gemini binary, requested version, authentication and log identity.
Copilot remains the default engine.

## Proposed decision

Use `agy` as the canonical engine ID and `Google Antigravity CLI` as the display
name. Both `engine: agy` and `engine.id: agy` select a genuine embedded built-in,
without user imports. Its definition and runtime report experimental status;
interactive choices, CLI help and documentation label it experimental.

Keep installation, configuration, MCP conversion, execution and log parsing in
an embedded declarative engine definition using the existing behavior-defined
runtime. Extend shared registry, default-version and credential-isolation
mechanisms only where necessary. Do not copy Gemini's dedicated Go runtime.

Initial support is native Linux x64 v1.3.1, with a checksum-verified archive and
executable-version verification. A custom executable must report the same
verified version. Other static version pins fail rather than silently running
v1.3.1. The initial model is the native slug `gemini-3.8-flash-medium`.

Authentication requires `GEMINI_API_KEY` and private per-run
`~/.gemini/antigravity-cli/settings.json` containing `modelProvider: gemini`.
AWF retains the actual provider credential; the agent receives a dummy key and
the configured Gemini endpoint discovered through reflection. Discovery failure
must not fall back to direct Google inference. ADC/WIF and Vertex AI for Agy are
separately gated follow-ups, not initial-release requirements.

Engine identity and inference-provider identity stay distinct: Agy uses the
existing Gemini protocol and AWF target, not a new `agy` or `antigravity` target.
Existing Gemini target names, endpoints, ports and authentication remain intact.

The harness sends literal NDJSON input on stdin, invokes the native binary with
an argument array, isolates settings, and bounds execution with the native
timeout, a watchdog and signal escalation. Missing or unsuccessful results,
denials, interruption, pending tools and missing positive inference evidence
fail execution even if the native exit code is zero.

MCP uses gateway-backed HTTP servers converted to owner-only
`.agents/mcp_config.json` with `mcpServers` and native `serverUrl` entries.
Malformed headers, off-gateway endpoints, stdio entries and symlinks are
rejected. CLI-mounted infrastructure servers remain on gh-aw's existing transport.
Instruction and configuration surfaces include `AGENTS.md`, `GEMINI.md`,
`.agents/` and `.gemini/`.

Native blanket approvals operate inside the outer gh-aw sandbox, not as its
security boundary. Unsupported command restrictions, disabled native tools and
overrides of the verified headless profile must fail explicitly. Native events
are normalized by the same parser for Actions and embedded local CLI
reconstruction. Repeated cumulative result usage must not be added together,
and absent metrics must remain absent.

## Gemini compatibility and release gate

The intended soft deprecation is a single informational notice after effective
engine resolution, including imported configuration and CLI overrides. It must
not count as a warning or error, change strict-mode exit status, alias binaries,
automatically rewrite workflows, alter version pins or impose a removal date.
Interactive choices keep Gemini selectable.

API-key users may evaluate experimental Agy only within its documented
capabilities. WIF users must be told to retain `engine: gemini` until an equivalent
Agy profile is verified. Historical Gemini logs retain their original parser and
engine identity.

Native API-key authentication passed on a clean Actions runner in
[run 37732637929](https://github.com/github/gh-aw/actions/runs/37732637929),
including real fresh-challenge inference, missing/invalid keys, an unknown
model and controlled endpoint routing. That evidence does not prove AWF or MCP.

Release of Agy and Gemini soft deprecation together requires bounded production
conformance through the actual installer, Gemini AWF endpoint, native MCP,
CLI-mounted MCP and staged safe outputs. The manual
`engine-conformance-agy.md` workflow prepares that gate and remains dispatch-only.
Credentials Check calls the feature-branch `agy-conformance-reusable.lock.yml`.
Both entry points import `shared/agy-conformance.md`, preserving one copy of the
native/shared probes and prompt without importing a trigger-bearing workflow.
Production conformance
has not passed; no Gemini deprecation notice or release changeset is enabled by
this implementation checkpoint.

## Consequences

The experimental integration can be evaluated without silently changing existing
workflows. Its supported authentication, architecture and capabilities are
narrower than Gemini's. Supporting two independently pinned binaries is an
ongoing maintenance cost, but declarative reuse avoids duplicating runtime
infrastructure.

A rename-in-place, silent Gemini alias and dated removal are rejected because
they would change execution or break users who cannot migrate. A dedicated
copied runtime and new provider target are rejected because neither is necessary
for the verified native API-key contract.

Stable promotion, Agy WIF support and any eventual Gemini removal require
separate evidence-backed decisions. This draft does not record human approval
or claim that a local/mock test passed the production release gate.
