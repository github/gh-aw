---
"gh-aw": minor
---
Removed the deprecated top-level `sandbox: false` option and replaced it with `sandbox.agent: false`, so only the agent firewall can be disabled while the MCP gateway stays enabled. Add `gh aw fix` to migrate existing workflows.
