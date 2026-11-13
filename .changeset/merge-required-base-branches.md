---
"gh-aw": major
---

Require `safe-outputs.merge-pull-request.allowed-base-branches` as a non-empty list of static owner-declared non-consequential integration destinations. Only `*` and `**` glob operators are supported; expressions, negation, character classes, braces, extglobs, `?`, `+`, and escapes are rejected. Entries made only of wildcard and separator characters, such as `"*"`, `"**"`, `"*/*"`, and `"**/**"`, are rejected. The live base branch must match during evaluation and final revalidation, including staged and cross-repository operations; all existing merge safeguards remain in force.

Existing workflows must add scoped base branch patterns and recompile. Null/default-only configuration is no longer accepted. `allowed-branches` continues to filter source branches and does not authorize destinations. Owners remain responsible for assessing downstream effects and protecting the policy from agent edits.
