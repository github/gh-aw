---
"gh-aw": patch
---

Configure Cursor's API as the AWF OpenAI route's custom upstream and resolve
both Cursor API and agent endpoints from `/reflect`, using the shared endpoint
helper and an ephemeral HTTP/1.1 configuration. Keep the real Cursor key in the
sidecar and give the agent a placeholder. Remove direct Cursor inference domains
and fail explicitly when AWF or reflection is unavailable.

Prefer reflected `base_url` values and add opt-in strict provider matching to the
shared resolver. Document gateway-only inference as a requirement for agentic
engine implementations. Document the native-protocol model and token-accounting
limitations separately from gateway routing.

Allow environment defaults in behavior-defined engine definitions so gateway
targets and runner-side credential bindings remain available when a consuming
workflow explicitly selects the engine by ID.
