# ADR-74793: Harden Run-Step Expression Sanitization

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: [TODO: verify] gh-aw maintainers and security reviewers

---

### Context

GitHub Actions evaluates `${{ ... }}` expressions before Bash receives a `run` script, so shell comments and heredoc bodies are not safe merely because Bash would treat them as comments or literal input. The existing run-step sanitizer excluded comments and quoted heredoc bodies from expression validation, allowing attacker-controlled multiline values to remain in locations that GitHub still evaluates. The PR addresses the resulting template-injection risk identified in issue #74547 while preserving the literal-output semantics of quoted heredocs. Both the parsed-workflow validator and the raw-YAML validation path must make the same safety decision.

### Decision

We will scan GitHub Actions expressions in run-step comments and all heredoc bodies, in addition to executable shell content, because GitHub evaluates them before the shell. We will move unsafe expressions from comments and expanding heredocs into step environment variables, while leaving expressions in quoted heredocs unchanged and rejecting them during validation rather than rewriting their literal output. We will apply this behavior consistently in both parsed-workflow and raw-YAML validation paths and cover the behavior with focused regression tests.

### Alternatives Considered

#### Alternative 1: Continue treating comments and quoted heredocs as safe shell regions

We could preserve the previous shell-oriented filtering and ignore expressions in comments and quoted heredoc bodies because Bash does not execute those locations. We rejected this because GitHub's expression phase runs before Bash parsing, so shell semantics do not prevent expression evaluation or multiline injection.

#### Alternative 2: Rewrite every expression, including quoted heredoc bodies, to environment variables

We could replace all expressions with environment-variable references so no `${{ ... }}` marker remains in the generated script. We rejected this because variable expansion is intentionally disabled in quoted heredocs; rewriting those bodies would change the literal file content and could break workflows that depend on exact output.

#### Alternative 3: Update only the parsed-workflow validation path

We could add the new checks to the schema-backed path and leave the raw-YAML fast path unchanged. We rejected this because workflows would receive different security decisions depending on which validation path is available, recreating the inconsistency covered by the PR's both-path tests.

### Consequences

#### Positive
- Expressions in comments and heredoc bodies are no longer allowed to bypass compiler template-injection and expression validation.
- Expanding heredocs and comments retain their intended visible content through generated step environment variables while avoiding inline GitHub expressions.
- Parsed-workflow and raw-YAML validation paths share the same fail-closed behavior, reducing path-dependent security gaps.
- Regression tests document the sanitizer, heredoc, comment, and cross-path behavior for future changes.

#### Negative
- Existing workflows that intentionally place GitHub expressions in quoted heredoc output will now be rejected and must be redesigned or made explicitly literal.
- The sanitizer and heredoc-region parser become more complex because rewriting must distinguish regions where shell variable expansion is safe from regions where it would alter output.
- Some previously accepted workflows may require migration to step environment variables or other literal-generation techniques.

#### Neutral
- Step environment blocks gain generated variable names for expressions that are moved out of comments or expanding heredocs.
- Unsafe expressions in quoted heredocs remain visible in the source until validation reports them; they are not silently rewritten.
- The change affects compiler validation and sanitization, not the runtime shell or the semantics of already-safe expressions.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
