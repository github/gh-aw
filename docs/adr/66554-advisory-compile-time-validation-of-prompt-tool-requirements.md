# ADR-66554: Advisory Compile-Time Validation of Prompt Tool Requirements

**Date**: 2026-10-07
**Status**: Draft
**Deciders**: pelikhan (PR author), gh-aw compiler maintainers

---

### Context

Agentic workflow prompts routinely contain explicit tool instructions ("run `jq --version`", "use `mcp__github__list_issues`", "Read(path)"), but the effective tool configuration — merged `tools:` permissions, engine capabilities, and transport settings (native MCP vs. CLI, GitHub-backed Codex shell restrictions) — can deny exactly those tools. Nine workflows in this repository were silently blocked at runtime because their prompts required tools their compiled permissions did not grant, and the failure only surfaced as a degraded or aborted agent run. The compiler already parses prompt markdown (including imported prompts) and resolves engine capabilities, so the mismatch is detectable before the workflow ever runs. Any mechanism added here must not broaden permissions, since prompt text is untrusted relative to the security model.

### Decision

We will add a compile-time, **advisory-only** validator (`pkg/workflow/prompt_tool_validation.go`, invoked from the compiler pipeline) that extracts explicit tool requirements from prompt text — imperative invocations, qualified MCP tool names, shell commands in task fences, and native `Read(...)` calls — across the main markdown, imported markdown, and prompt imports, and emits a deduplicated compiler **warning** when a requirement cannot be satisfied by the merged permissions, engine capabilities, or transport. The primary driver is early, actionable feedback: warnings name the unsatisfied tool and the exact configuration change (e.g. "allow the specific command `jq` in tools.bash"). The validator is deliberately conservative — ambiguous prose, negative examples, and complex shell inference are left unchecked — and never grants or expands permissions.

### Alternatives Considered

#### Alternative 1: Hard compile error on unsatisfied prompt tool requirements

Failing compilation would guarantee prompts and permissions stay in sync. Rejected because natural-language extraction is heuristic: false positives (documentation examples, counter-examples, prose mentioning a command) would break compilation of valid workflows, and a hard gate would pressure authors to over-grant permissions to silence it. A warning preserves the "more restrictive wins" security posture while still surfacing the problem.

#### Alternative 2: Runtime detection only (post-run log mining / agent error detection)

The existing log/audit pipeline already classifies denied-tool failures after a run completes. Rejected as the primary mechanism because feedback arrives only after a workflow has been merged and executed, costs a full agent run per iteration, and cannot be enforced in CI on `gh aw compile`. It remains a complementary safety net rather than a replacement.

### Consequences

#### Positive
- Prompt/permission mismatches are caught at `gh aw compile` time instead of mid-run, with a concrete remediation hint per warning.
- Coverage spans imported and runtime-resolved prompt content, so shared imports are validated in the context of each consuming workflow.
- No security regression: the validator is read-only with respect to configuration and cannot widen any allowlist.

#### Negative
- Heuristic regex-based extraction will produce occasional false-positive warnings (and silent false negatives), adding noise authors must learn to interpret.
- Introduces ~460 lines of new compiler logic plus engine-capability and transport coupling that must be maintained as engines and transports evolve.
- Warning counts increase for existing workflows, which can mask other warnings until the backlog is aligned.

#### Neutral
- Nine existing workflows and their `.lock.yml` files were realigned in the same change (adding missing shell commands, correcting Claude prefix rules, switching GitHub-backed Codex to fixed-path MCP readers).
- Warning behavior is documented in `docs/src/content/docs/reference/compilation-process.md`; regression tests cover both the validator and the nine affected workflows.
- Deduplication is keyed on server/tool/command, so one warning is emitted per distinct unsatisfied requirement regardless of repetition.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
