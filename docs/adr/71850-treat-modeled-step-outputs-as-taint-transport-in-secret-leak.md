# ADR-71850: Treat Modeled Same-Job Step Outputs as Taint Transport in Secret-Leak Analysis

**Date**: 2026-07-01
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent (PR #71850) [TODO: verify]

---

### Context

The `secret-leak` taint rule (`pkg/workflow/taint`) treated every write to a step output (`$GITHUB_OUTPUT`, JavaScript `core.setOutput`, GitHub Script return values) as an `Output` sink, so it reported a disclosure as soon as a secret reached a step output, before it checked what the downstream consumers did with the value. A common authentication pattern hands a credential to a later step this way: a resolver step writes `credential=$TOKEN` to `$GITHUB_OUTPUT`, and `actions/checkout` consumes `${{ steps.credential.outputs.credential }}` as its `token`. That pattern was flagged as a leak. This one root cause blocked 62 pinned `gh-cao` workflows (issue #71849). Step outputs are only visible inside the job unless they are re-exported as job or workflow outputs, so disclosure actually happens at the consumers (logs, summaries, artifacts, network, publication, exported outputs), not at the handoff. The analyzer also has to stay conservative: any shortcut that weakens detection would hide real leaks.

### Decision

We will treat a step-output write as private same-job *transport*, not as a disclosure sink, for the `secret-leak` rule only, and only when all of the following hold: the analysis runs in workflow context, the output name is a literal identifier, the producing step id is resolved, and the downstream consumer scope is modeled. In that case taint moves into a `steps.<id>.outputs.<name>` binding (see `transportOutput` in `step_outputs.go`) and carries into downstream aliases, expressions, and env bindings. Disclosure checks stay in place at those downstream sinks. In every other case the analyzer falls back to the previous behavior: it emits the `Output` finding and/or records a coverage gap. Examples include dynamic or unknown names, multi-record writes (heredocs, compound `echo`/`printf`), ANSI-C quoted words, unresolved step ids, standalone JavaScript analysis, and fast-mode JavaScript entries that have no verified compiled match. Custom taint rules keep their own output-sink policy. To make this work for JavaScript in fast mode, we replay only the authored entry scripts inside the compiled workflow graph (`analyzeFastJavaScript`), so their output bindings reach the generated consumers.

### Alternatives Considered

#### Alternative 1: Keep step outputs as sinks and require suppressions

Keep reporting every step-output write and ask authors to suppress the intended credential handoffs. This needs no analyzer changes and is maximally conservative. We rejected it because the most common, correct authentication pattern would need suppressions in dozens of workflows (62 in the pinned corpus). Each suppression also hides later real disclosures of the same value, and the findings misstate the actual risk.

#### Alternative 2: Exempt step outputs from the secret-leak rule entirely

Drop `Output` as a sink for step outputs in `secret-leak`, without checking names, step ids, or consumer modeling. This is simpler, but secrets could then escape through outputs the analyzer cannot follow: dynamic names, multi-record writes, unresolved producers, and values re-exported as job or workflow outputs. Those leaks would no longer produce findings or gaps. That trade-off is not acceptable for a security linter.

#### Alternative 3: Name-based allowlist (e.g. outputs named `token`/`credential`)

Trust outputs whose names look like credentials. We rejected this because names grant no real trust, it is easy to get wrong in both directions, and the value would still need taint tracking downstream anyway.

### Consequences

#### Positive
- The standard credential-resolver → `actions/checkout` handoff no longer produces a false-positive disclosure, so the affected `gh-cao` workflows are unblocked without suppressions.
- Taint stays attached through step outputs, so real downstream disclosures (logging, artifacts, summaries, network, job/workflow outputs) are still reported at the actual leak point.
- Shell, `core.setOutput`, and GitHub Script returns use the same transport path (`transportOutput`), so behavior is the same across languages.

#### Negative
- The analyzer becomes more complex: output-name parsing for `echo`/`printf`/heredocs, step-id resolution, and a new fast-mode JavaScript replay with authored-to-compiled site matching (`fastSameSite`, `fastSiblingSite`). The site matching depends on step-index heuristics, which are hard to maintain.
- Correctness now depends on how complete consumer modeling is. A bug that marks a scope as "modeled" when it is not could hide a real leak.
- Rules now behave differently: `secret-leak` treats outputs as transport while custom rules treat them as sinks, which can confuse authors who write custom rules.
- The full pinned 74-workflow corpus could not be rerun (SAML 403), so the effect on the whole corpus has not been verified.

#### Neutral
- Cases that cannot be resolved still produce findings or coverage gaps, so some false positives remain for dynamic output names and multi-record writes.
- The linter reference docs (`workflow-linters.md`) now describe the transport-versus-disclosure distinction, and regression tests cover shared imports, cross-language handoffs, and fast-mode propagation.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
