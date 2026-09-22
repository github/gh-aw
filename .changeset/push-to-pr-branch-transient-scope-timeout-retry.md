---
"gh-aw": patch
---

Retry the primary `push_to_pull_request_branch` push when GitHub transiently fails its workflow-permission check ("Unable to determine if workflow can be created or updated due to timeout; `workflows` scope may be required."), matching the existing `create_pull_request` behavior. When the rejection persists after retries, the handler now classifies it: a real `workflows` scope problem (the agent's changeset touches `.github/workflows/**`) returns a typed `workflows_scope_required` error, and a persistent transient timeout falls back to a pull request so the prepared changes are not lost.
