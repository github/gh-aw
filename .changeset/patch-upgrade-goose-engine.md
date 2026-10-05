---
"gh-aw": patch
---

Upgrade the unsupported Goose sample to CLI v1.53.0 with a verified release
checksum, container-facing MCP routing, isolated noninteractive configuration,
file-based prompts, native turn limits, and structured tool/usage logs. Prevent
Goose's version from being inherited by the Copilot detection engine and make
the Goose smoke workflow fail when its operational checks do not pass.
The smoke profile uses native GitHub MCP instead of gh-proxy and a pinned
Copilot model so missing tools or fabricated PASS reports cannot hide failures.
Enable the smoke suite's token-telemetry assertions for Goose now that its
native structured logs and AWF proxy report token usage.
