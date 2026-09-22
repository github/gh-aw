---
"gh-aw": patch
---

Retry the primary `push_to_pull_request_branch` push when GitHub transiently fails its workflow-permission check ("Unable to determine if workflow can be created or updated due to timeout; `workflows` scope may be required."), matching the existing `create_pull_request` behavior. A workflow change without `allow-workflows` now fails before pushing with a typed `workflows_scope_required` error. When a transient timeout persists after retries, the handler falls back to a pull request so the prepared changes are not lost.
