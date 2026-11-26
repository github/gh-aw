# ADR-81371: Predict Blocked Prompt File Access with a Static `file-access` Lint Rule

**Date**: 2026-11-26
**Status**: Draft
**Deciders**: Not stated in PR evidence (PR author: pelikhan)
**Related**: [PR #81371](https://github.com/github/gh-aw/pull/81371), [specs/file-access-lint-spec.md](../../specs/file-access-lint-spec.md)

## Context

Workflow prompts often ask the agent to perform file operations that the workflow configuration does not allow: writing files with no `edit` tool and no write-capable `bash`, running Docker inside the agent sandbox, writing under read-only system paths, using memory directories that are not mounted, or modifying protected files when every pull-request safe output blocks them. As the PR body states, these failures appear only at run time.

Forces:

- The relevant facts are spread across the prompt and frontmatter: `sandbox.agent`, `tools.edit`, `tools.bash`, `tools.cache-memory`, `tools.repo-memory`, and `safe-outputs.*.protected-files`. The prompt alone does not show whether an instruction will be blocked.
- Imported workflows can change the result. Standalone lint (`gh aw lint`) sees only authored frontmatter, while `gh aw compile` sees compiler-resolved tools.
- Natural-language extraction of paths and write intent is heuristic, so a rule that guesses will produce false positives that erode trust.

## Decision

Add a built-in AW lint rule, `file-access`, at **Warning** severity. The rule:

1. Simulates the agent's effective file-access capabilities from frontmatter (`file_access.go`, FAL-M1 to FAL-M7). It follows the compiler's default tool resolution when the sandbox is on.
2. Extracts file-access instructions from Markdown prose and fenced shell blocks (`file_access_prompt.go`, FAL-X1 to FAL-X5). It skips negated and illustrative sentences, and it matches paths conservatively.
3. Reports five finding classes (FAL-R1 to FAL-R5). Each finding points to the frontmatter key that causes it.
4. **Defers rather than guesses.** When imports could change the result, the capability state stays unknown and the rule reports nothing.
5. Is specified in `specs/file-access-lint-spec.md`. `TestFileAccessSpecTraceability` fails if a requirement ID or conformance case has no named test.

## Alternatives Considered

1. **Leave these failures to run time (status quo).** Rejected. The PR body identifies the failures as surfacing only when a run is underway, which wastes turns and can fail silently.
2. **Guess when imports or sandbox state are unknown.** Rejected. The rule's detection is tuned to miss cases rather than raise false positives, and the model defers any result that imports could change (spec §3). A guessed result would be wrong in some workflows and would erode trust in every warning.
3. **Report findings as errors.** Rejected for this release. The spec states that each finding is a Warning because extraction from natural language is heuristic (spec §1).

## Consequences

Positive:

- Authors see a warning at lint or compile time that names the frontmatter key responsible, instead of a failed run.
- The spec and the traceability test link each requirement to a named test, so the rule's behaviour is reviewable and drift is caught.
- The rule is registered in `Default()` and documented in `pkg/workflow/lint/README.md` and `reference/workflow-linters.md`.

Negative:

- The rule misses cases by design, and a missed case produces no signal.
- Standalone lint cannot see compiled sandbox settings or imported tools, so some findings are deferred.
- Compile now warns on six existing workflows (`pr-nitpick-reviewer`, `grumpy-reviewer`, `security-review`, `typist`, `daily-file-diet`, `archie`) because their prompts use `/tmp/gh-aw/cache-memory/` without configuring `tools.cache-memory`. The PR leaves these for a separate fix.
- The change adds about 1,000 lines of Go plus a specification that must be maintained with the implementation.
