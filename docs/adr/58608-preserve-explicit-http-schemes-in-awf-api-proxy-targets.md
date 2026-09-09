# ADR-58608: Preserve Explicit HTTP Schemes in AWF API Proxy Targets

**Date**: 2026-09-04
**Status**: Draft
**Deciders**: gh-aw maintainers

---

## Context

Custom engine base URLs are compiled into AWF API proxy targets. Previously,
the compiler removed the scheme from every target, causing an explicit
`http://` endpoint to be interpreted as HTTPS by AWF and contacted on port 443.
AWF v0.28.13 added support for preserving an explicit HTTP target scheme.

## Decision

When the effective AWF version is v0.28.13 or newer, preserve an explicit
`http://` scheme and authority, including an explicit port, for OpenAI,
Anthropic, and Gemini API proxy targets. Continue emitting bare hosts for
older AWF versions, HTTPS and scheme-less URLs, allowlists, and Copilot
targets. AWF currently normalizes target ports to the scheme default at
runtime, so custom-port endpoints are documented but not supported end to end.

## Alternatives Considered

### Always preserve the scheme

Rejected because older AWF versions do not accept scheme-qualified target
hosts and would fail to interpret the generated configuration correctly.

### Always emit a bare host

Rejected because AWF defaults a bare target to HTTPS, breaking explicit HTTP
gateways.

## Consequences

- Explicit HTTP gateways work with AWF v0.28.13 and newer.
- Older AWF versions retain the previous bare-host representation.
- The compiler preserves explicit authorities for accurate configuration
  output, while custom target ports remain subject to AWF runtime
  normalization.

