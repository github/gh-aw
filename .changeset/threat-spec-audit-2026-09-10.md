---
"gh-aw": patch
---

Daily compiler threat spec audit (2026-09-10): reviewed open high/critical code-scanning alerts against `specs/compiler-threat-detection-spec.md` conformance scope. Alerts #675–#676 (`go/allocation-size-overflow` in `pkg/workflow/mcp_github_config.go`) are the same in-process, schema-validated capacity-hint pattern already dismissed for alert #677; alerts #667–#669 and #674 (`go/bad-redirect-check`) flag manifest path-traversal containment guards, not redirect handlers, and already carry rationale comments explaining the CodeQL heuristic mismatch. No `threat-detection-suppress` annotations exist in any workflow, and no compiler/parser source diff exists beyond the current repository state, so no new `CTR-*` rule or implementation change was required. Bumped spec to 1.0.32.
