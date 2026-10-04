---
"gh-aw": minor
---

Re-export the public `setupGlobals`, `createIssue`, and OpenTelemetry `logSpan` APIs from `actions/setup/js/index.cjs`. Issue creation uses the existing `create_issue.cjs` handler and adds the standard generated-by footer and provenance annotations for custom safe-output jobs. Pass workflow metadata to custom jobs and use the API for AW issue clustering.
