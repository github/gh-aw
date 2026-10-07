# ADR-66039: Trust Compiler-Generated Wildcard GitHub App Token Steps in Strict Mode

**Date**: 2026-02-23
**Status**: Draft
**Deciders**: Unknown (PR #66039 author and reviewers)

---

### Context

Strict mode validation (`validateAppTokenPermissions`) requires every `actions/create-github-app-token` step to carry an explicit `repositories` input so that minted tokens are scoped to a known repository set. When a workflow declares `repositories: ["*"]` for a `github-app` (either under `tools.github` or under `safe-outputs`), the compiler intentionally omits the `repositories` input from the generated step, because the GitHub App token action interprets "no `repositories` input" as "all repositories the app is installed on". The validator could not distinguish this deliberate, user-declared wildcard from an accidentally unscoped hand-written step, so documented cross-repository access failed to compile under `strict: true` (see #65814). The fix must not weaken scoping checks for token steps the compiler did not generate, nor relax the separate requirement for explicit `permission-*` inputs.

### Decision

We will have the compiler record, at generation time, the identity of each token step it emits from an explicit `repositories: ["*"]` configuration, and have strict-mode validation consult that record before flagging a missing `repositories` input. The record is a `map[appTokenStepKey]bool` on the `Compiler` (keyed by step `id`, `client-id`, and `private-key`), populated in `buildGitHubAppTokenMintStepWithMeta` and reset at the start of each `CompileWorkflowData` run. The primary driver is correctness with minimal blast radius: provenance is known precisely at the point of generation, so no heuristic re-inference from the emitted YAML is needed.

### Alternatives Considered

#### Alternative 1: Emit `repositories: "*"` into the generated step

Keeping an explicit wildcard value in the generated YAML would satisfy the existing validator with no compiler state at all, and would make intent visible in the `.lock.yml`. It was rejected because `actions/create-github-app-token` does not treat `"*"` as a wildcard — it would be interpreted as a literal repository name — so this would change runtime behaviour and break the documented cross-repository access path.

#### Alternative 2: Thread wildcard intent through `WorkflowData` / validation inputs instead of compiler state

Rather than mutable `Compiler` state, the wildcard flag could be carried on the workflow data structures already passed into validation. This is arguably cleaner (no reset-per-compile hazard), but it requires touching more types and call sites across the generation and validation paths. It was a close call; the compiler-field approach was chosen for a smaller diff, with the `c.wildcardAppTokenSteps = nil` reset in `CompileWorkflowData` guarding batch-mode leakage.

#### Alternative 3: Skip the `repositories` check entirely for compiler-generated steps

The validator could exempt any step whose `id` matches a known generated prefix (e.g. `github-mcp-app-token`, `safe-outputs-app-token`). This is simpler but over-broad: it would also exempt generated steps whose configuration did *not* request a wildcard, silently dropping a real scoping check. Rejected as a loss of validation coverage.

### Consequences

#### Positive
- Workflows that legitimately declare `repositories: ["*"]` now compile under `strict: true` without warnings, unblocking documented cross-repository GitHub tools and safe outputs (#65814).
- Scoping enforcement is preserved for all hand-written and non-wildcard token steps; regression tests cover both the `github` tool and `safe-outputs` wildcard shapes, and assert that unrelated steps still fail.
- `permission-*` enforcement is untouched, so wildcard tokens still require explicitly enumerated permissions.

#### Negative
- Introduces mutable per-compilation state on `Compiler`, which must be reset correctly; a missed reset in a future code path could let one workflow's wildcard exemption leak into another during batch compilation.
- Validation is now coupled to generation: the step key (`id` + `client-id` + `private-key`) must stay in sync between `buildGitHubAppTokenMintStepWithMeta` and the emitted YAML, or the exemption silently stops applying and strict mode regresses.
- The generated `.lock.yml` gives no local signal that the omitted `repositories` input is intentional; readers must consult the source frontmatter.

#### Neutral
- The exemption is keyed on a 3-tuple rather than step `id` alone, which is stricter than necessary today but tolerant of future workflows that emit several app-token steps.
- The wildcard detection only triggers when `repositories` has exactly one entry equal to `"*"`; mixed lists such as `["*", "owner/repo"]` retain the existing behaviour.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
