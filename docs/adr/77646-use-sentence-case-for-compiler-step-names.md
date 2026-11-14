# ADR-77646: Use Sentence Case for Compiler-Generated Step Names

**Date**: 2026-11-14
**Status**: Draft
**Deciders**: Unknown (AI-generated draft for PR #77646; pending maintainer review)

---

### Context

Compiled workflow jobs contain steps whose names are set in Go code under `pkg/workflow/` and `pkg/constants/`, not in the `.md` sources. A sweep of 365 lock files found 803 distinct step names in mixed casing: some use Title Case (`Process Safe Outputs`, `Start MCP Gateway`), some use sentence case (`Check workflow lock file`), and a few lowercase glossary terms (`Execute threat detection with AWF`, `Mount MCP servers as CLIs`). Step names appear in the Actions UI and logs, and tests and golden fixtures match them as exact strings, so any rename causes test and fixture churn. Issue #77051 asked for one casing rule and proposed sentence case with glossary terms capitalized.

### Decision

We will use sentence case for compiler-generated step names, with glossary terms capitalized (for example Threat Detection, MCP Servers, MCP Gateway, Safe Outputs). The primary driver is consistency: a reviewer can check a step name against one rule without a lookup table. The rule applies to new step names, and existing names change only when they are misleading or break the glossary; in this PR that is `Copy Safe Outputs`, `Execute threat detection with AWF` and `Mount MCP servers as CLIs`, with golden fixtures and test assertions regenerated in the same change.

### Alternatives Considered

#### Alternative 1: Title Case for all step names

Title Case would match more existing names, such as `Process Safe Outputs`, `Start MCP Gateway` and `Upload Safe Outputs Items`, so fewer renames would be needed. It was not chosen because Title Case needs rules for small words (`as`, `with`, `for`) that contributors would have to look up for every new step. This was a close call, and it remains a reasonable choice if maintainers prefer fewer renames. [TODO: verify the Title Case match count against the current lock files]

#### Alternative 2: Keep existing names and document no rule

This is the status quo. It needs no renames, no fixture regeneration and no test changes, and it carries no risk for any tool that matches old step names. It was not chosen because the mixed casing is visible in the Actions UI, and the sweep found a few names that are misleading or break the glossary. Maintainers may reasonably prefer this lower-churn option.

### Consequences

#### Positive
- New step names follow one casing rule, so they are easier to review.
- `Copy raw safe-output NDJSON to /tmp/gh-aw` says what the step copies, rather than implying it copies the outputs.
- Glossary terms such as Threat Detection and MCP Servers appear the same way in logs and the Actions UI, which makes them easier to search.

#### Negative
- Every affected compiled lock file and golden fixture changes, so the generated diff is large and hard to review by eye.
- Tests that match exact step names must be updated in the same change. Any external tool that matches the old names will break. [TODO: verify whether any external dashboard or script matches these step names]
- Each future rename repeats this fixture and test churn.

#### Neutral
- Established names such as `Process Safe Outputs` and `Setup Scripts` keep their current casing, because renaming them is not test-neutral. The inconsistency therefore remains for those names.
- `Print AWF reflect summary` writes to the job summary rather than the log. Issue #77051 suggests a more accurate name as an optional, low-priority follow-up.
- Reviewers should check the fixture diffs for unrelated changes.

---

*ADR created by adr-writer agent (Design Decision Gate). Review and finalize before changing status from Draft to Accepted.*
