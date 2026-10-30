---
"gh-aw": major
---

Always include the resolved grader definition in `gh aw graders run` JSON output.

**Breaking change:** output is now `{"version":1,"definition":{...},"result":{...}}`
instead of the flat result object. No `--include-definition` flag is required.

**Migration:** read measurement fields under `.result`, such as `.result.value`
and `.result.status`, or pipe through `jq '.result'` to recover the former shape.
Resolved acceptance metadata is available under `.definition`; executable source
is excluded. Configuration is used unchanged for computation but emitted as
`config: {}` with `config_omitted: true`. Harbor reports retain only typed
reward-mapping fields and provenance/coverage metadata, explicitly omitting
free-form diagnostics and stderr. Existing exit codes and grading semantics
are unchanged.
