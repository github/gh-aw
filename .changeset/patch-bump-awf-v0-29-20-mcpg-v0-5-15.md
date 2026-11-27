---
"gh-aw": patch
---

Bump the default AWF firewall to v0.29.20 and MCP gateway to v0.5.15. The firewall binds cloud OIDC credentials to the configured upstream. The gateway hardens repository visibility guards, pull-request artifact coverage, request paths, and backend redirects. Gateway v0.5.15 honors parent OpenTelemetry sampling decisions, tightens audit-log redaction, adds explicit DIFC labels for code-quality findings, and makes Rust guard FFI input reads null-safe.
