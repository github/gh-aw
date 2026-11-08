# Add an experimental mini-SWE-agent declarative engine

Date: 2026-11-08

## Status

Accepted

## Context

mini-SWE-agent provides a small, shell-based coding agent. gh-aw needs an
unsupported sample that can exercise its existing sandbox, provider routing,
CLI-proxy tools, and safe outputs without introducing a dedicated engine runtime.
The upstream interactive CLI and repository configuration are unsuitable for
unattended, reproducible workflow execution.

## Decision

Define `mini-swe-agent` in a shared Markdown import using the generic
behavior-defined runtime. Pin the Python package and dependency artifacts, run
the non-interactive Python API with isolated imports inside AWF, and discover the
selected provider's proxied endpoint through `/reflect`. Keep provider credentials
out of the agent environment, require native task submission for success, and
bound model calls with `max-turns`.

Use shell CLI proxies rather than claim native MCP support. Register a
dispatch-only conformance worker in the existing bounded portfolio and retain
the manual smoke workflow. Conformance needs only its fixture, script, safe-output,
and queue transports; native GitHub MCP access remains disabled. Non-MCP engines
retain the compiler-required `gh-proxy` transport with only the `repos` toolset.

Add reusable `behaviors.output-files` declarations for literal, secret-redactable
diagnostic files under `/tmp/gh-aw/agent/`. Retain the native trajectory through
the existing redaction- and recovery-validation-gated unified agent artifact,
including failed executions. It remains untrusted diagnostic data, not verified
structured session or token-accounting evidence.

## Consequences

The integration reuses generic compilation and execution paths instead of adding
mini-specific Go runtime code. A dedicated engine would duplicate those paths;
invoking the interactive CLI would introduce setup prompts and repository-controlled
configuration. Direct provider-key execution and direct raw artifact uploads are
rejected because they bypass the proxy or publication gates.

The sample remains experimental and unsupported. Native shell allowlists,
edit disabling, Agent Plugins, structured session parsing, and engine-native cost
accounting are unavailable. AWF enforces the workflow inference budget; package,
dependency, and Python API upgrades require renewed focused validation.
