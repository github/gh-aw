---
"gh-aw": patch
---

Include native Copilot tool calls in the automatic grader trace payload. `trace.toolCalls` now merges tool calls normalized from the staged `events.jsonl` session log (name, arguments, `toolCallId`, and `success` correlated from `tool.execution_complete`) with MCP gateway records, deduplicating gateway entries that duplicate a native call. Built-in tools such as `skill` are therefore visible to graders like `skill-constraint-coverage` without repository-supplied grading logic.
