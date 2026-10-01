---
"gh-aw": minor
---

Add top-level `max-tool-calls` frontmatter field: an aggregate, run-wide budget for the total number of tool invocations the primary agent may dispatch. Every dispatched invocation counts once — shell/bash, file reads and writes, URL fetches, MCP tools, and custom tools — whether it succeeds, fails, or is denied, and tool calls from sub-agents spawned by the primary agent draw from the same budget. The budget is consumed before the tool executes, so no invocation beyond the cap runs and concurrent dispatches cannot overshoot it. Exhausting the budget emits a `guard.tool_calls_exceeded` event, stops the session, and fails the agent step. Omitting the field means unlimited. Supported only with `engine.id: copilot` and `engine.copilot-sdk: true`; the compiler rejects it on other engines, which expose no pre-execution tool interception point.
