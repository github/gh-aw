---
"gh-aw": minor
---

Make experimental connector declarations easier to author with complete import
descriptions, explicit Jira resource-array item types, consistent field order,
and shared identical primitive schemas. Preserve existing tool names, native
MCP methods, request shapes, grants, and response checks.

Linear GraphQL credentials now default to separate `LINEAR_READ_API_KEY` and
`LINEAR_WRITE_API_KEY` secret names. Existing consumers that intentionally share
`LINEAR_API_KEY` must explicitly bind both `reader-secret` and `writer-secret`
to that name through `with`; there is no automatic shared-key fallback.
