---
"gh-aw": patch
---

Reconcile uncertain pull request merges through read-only lookups without retrying the mutation. Preserve indeterminate outcomes in processing results, summaries, decision audits, and session records rather than claiming the merge did not happen.

Keep raw merge errors out of manager logs and processing records, including deferred failures. Add versioned resolved-policy fingerprints and bounded check, approval, protected-file, and revalidation outcomes to merge decision audits.
