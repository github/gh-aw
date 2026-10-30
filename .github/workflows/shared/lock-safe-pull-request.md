---
tools:
  bash: true
  edit:
---

## Deliver a Tested Pull Request

Implement the selected, evidence-backed improvement rather than filing an implementation issue.
Search existing open issues and pull requests before starting; skip work already covered by
an active pull request. Preserve this workflow's analysis scope, prioritization, and output limits.
Use a separate local branch for each independent pull request; do not stack unrelated fixes.
Validate the actual change with the smallest relevant tests and include the evidence,
implementation, and validation results in the pull request body. Never claim checks ran when
they did not. Use `noop` when analysis finds no actionable change; use `report_incomplete`
when a concrete blocker prevents implementation or validation. Do not create an empty report-only PR.

Commit the intended changes locally, then call the `create_pull_request` safe output with
`title`, `body`, and the local `branch`. Never push or create a pull request directly with
git, gh, or GitHub write tools. `exclude-lock-yml: true` strips compiled `.lock.yml` files
under `.github/workflows/` from delivery. Keep workflow Markdown source changes and compile
them for validation; the runtime adds a warning when it drops generated YAML changes.
A maintainer must run `gh aw compile` again and commit regenerated locks with workflow-write
credentials before merging. Do not modify upstream-managed workflows declaring `source:`.
