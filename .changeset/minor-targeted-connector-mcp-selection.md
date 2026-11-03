---
"gh-aw": minor
---

Allow workflows to select exact native methods from an existing connector HTTP
MCP registration, such as `tools: {jira: {personal: [getJiraIssue]}}`. Narrowing
preserves declared input/resource checks and removes unselected methods and
unused grants before the agent snapshot is frozen. No provider tools are added.

Document minimal read-and-comment selections and reject unknown, wildcard,
duplicate, or empty MCP subsets.
