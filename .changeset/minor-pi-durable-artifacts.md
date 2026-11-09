---
"gh-aw": minor
---

Add experimental artifact-backed session continuation with configurable
`engine.durable.retention-days`. Restore scoped continuation artifacts without memory tools and preserve OTel
and unified agentic session events. Extend the engine interface and declarative
engine behaviors with engine-declared persisted files, optional pinned runtime
dependencies, and continuation execution plans. Engines without a native durable
library can restore their session folders and resume through their own harness.
Enable session-file continuation for Copilot CLI and SDK, Claude, Codex, Pi CLI,
SDK and RPC, Gemini, and Agy. Resume exact native sessions for Copilot, Claude, Codex, and Pi; provide a
restored-file continuation prompt for Gemini and Agy. Start fresh when no history
exists, and keep historical Copilot activity out of current-run session metrics.
Use Pi's ordinary coding-agent session layer without a separate durable task runtime or dependencies.
Prior experimental Pi task-runtime artifacts are incompatible with native session
artifacts and are rejected rather than silently migrated.
Emit a compile-time experimental-feature warning whenever durable mode is enabled
for any engine; its configuration and behavior may change without deprecation.
