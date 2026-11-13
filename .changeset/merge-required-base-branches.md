---
"gh-aw": major
---

Require `safe-outputs.merge-pull-request.allowed-base-branches` as a non-empty list of owner-declared non-consequential integration destinations. Bare wildcard-only entries such as `"*"` and `"**"` are rejected. The live base branch must match during evaluation and final revalidation, including staged and cross-repository operations; all existing merge safeguards remain in force.

Existing workflows must add scoped base branch patterns and recompile. Null/default-only configuration is no longer accepted. `allowed-branches` continues to filter source branches and does not authorize destinations. Owners remain responsible for assessing downstream effects and protecting the policy from agent edits.
