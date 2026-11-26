# ADR-80976: Reject Unprefixed `{{else}}` Delimiters in AW Prompts

**Date**: 2026-11-25
**Status**: Draft
**PR**: #80976
**Linked issue**: #80969

## Context

Issue Arborist reported an incomplete result in #80969 with the agent-declared reason "No task or objective was provided." PR #80976 traces this to the concise experiment variant of `.github/workflows/issue-arborist.md`. The prompt used `{{else}}`, which is not a supported AW prompt fallback delimiter. According to the PR body, the renderer discarded that branch, so the concise variant reached the agent with no workflow objective. This renderer behavior is taken from the PR description and was not independently re-run for this ADR.

Before this PR, 18 workflow prompt sources used the unprefixed form. The PR diff changes all 18 to `{{#else}}`. Nothing at compile time flagged the unsupported delimiter, so the defect surfaced only as an incomplete run. Experiment variants make this harder to notice, because a silently dropped branch affects only one variant.

## Decision

Make the supported delimiter a compile-time requirement and migrate the affected prompts:

- Add `PromptConditionSyntaxRule()` (ID `prompt-condition-syntax`, `Severity: Error`, targets `AWAST`) in `pkg/workflow/lint/prompt_condition_syntax.go`. It reports `{{else}}` or `{{ else }}` when it appears inside an open `{{#if}}` block. It skips inline code spans and fenced code blocks, and it tracks conditional depth.
- Register the rule in `Default()` in `pkg/workflow/lint/lint.go` and list it in `pkg/workflow/lint/README.md`.
- Change the 18 affected workflow sources from `{{else}}` to `{{#else}}`. The change to `ab-testing-advisor.md` only touches a documentation example inside an inline code span. The matching `.lock.yml` diffs update body-hash metadata.
- Add renderer regression tests for the `concise` and `detailed` Issue Arborist variants in `actions/setup/js/prompt_condition_regressions.test.cjs`, plus rule-level tests and a rule contract entry.

## Alternatives Considered

1. **Accept `{{else}}` as an alias for `{{#else}}` in the renderer.** Existing prompts would keep working without edits, and no new lint error would be needed. However, the repository would have two spellings for one construct, and the renderer would need semantics for each. This PR does not add tests for that behavior beyond the Issue Arborist cases.
2. **Fix only the 18 prompt sources, with no lint rule.** This is the smallest change and resolves #80969. It does not stop the same silent discard from recurring in new workflows or experiment variants. The diff shows that manual authoring already introduced the mistake across 18 files.

## Consequences

**Positive**

- Authoring mistakes fail at compile time with a file line number and a message that names `{{#else}}`, instead of producing a silently incomplete run.
- The Issue Arborist concise variant receives its objective again, and both variants are covered by a renderer regression test.
- The rule runs in the default lint set, so every workflow compile enforces the delimiter.

**Negative**

- This is a breaking change for authors. Any prompt that still uses an unprefixed `{{else}}` inside a conditional will fail compilation. The PR migrates the 18 known sources, but workflows outside this repository must make the same edit.
- The diff is noisy: 36 workflow files change (18 `.md` and 18 `.lock.yml`), and most lock-file changes are body-hash metadata.
- The rule is a hand-written scanner that mirrors part of the renderer's tokenization (`{{`/`}}`, inline code, fences). It can drift from renderer behavior, and its tests cover only the listed cases.
