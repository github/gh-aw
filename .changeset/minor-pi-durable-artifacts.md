---
"gh-aw": minor
---

Add experimental Pi Durable execution with artifact-backed checkpoints and configurable
`engine.pi.durable.retention-days`. Install and verify pinned harness dependencies,
restore scoped continuation artifacts without memory tools, and preserve OTel
and unified agentic session events. Extend the engine interface and declarative
engine behaviors with engine-declared persisted files, optional pinned runtime
dependencies, and continuation execution plans. Engines without a native durable
library can restore their session folders and resume through their own harness.
Enable session-file continuation for Copilot CLI and SDK, Claude, Codex, Gemini,
and Agy. Resume exact native sessions for Copilot, Claude, and Codex; provide a
restored-file continuation prompt for Gemini and Agy. Start fresh when no history
exists, and keep historical Copilot activity out of current-run session metrics.
Retain Pi's specialized durable task runtime.
Emit a compile-time experimental-feature warning whenever durable mode is enabled
for any engine; its configuration and behavior may change without deprecation.
