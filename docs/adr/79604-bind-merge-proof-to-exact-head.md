# ADR-79604: Bind Opt-In Merge Proof Evidence to Exact-Head Authorization

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: PR author (pelikhan) for [PR #79604](https://github.com/github/gh-aw/pull/79604); maintainer review not yet recorded

---

### Context

`merge-pull-request` lets an AI agent merge a pull request into an eligible branch under workflow-configured gates. Existing gates (required checks, approvers, protected files, and an external authorization decision) do not establish that a trusted verifier evaluated the exact PR head and invariant configuration being merged. An authorization granted for an invocation can outlive a head change, and a green check conclusion alone does not show which invariants were evaluated. The verifier's credentials, implementation, and policy must stay outside the acting agent's control, and existing merge restrictions must remain intact.

### Decision

We will add an opt-in `merge-pull-request.proof-evidence` setting that requires a GitHub App-pinned verifier check, a static verifier version, and a configuration SHA-256 digest. The merge gate will accept proof only from the selected check run's strict `gh-aw.merge-proof.v1` output summary, bound to the repository, PR number, head SHA, verifier version, configuration digest, and a `passed` result, and will never consume agent-supplied evidence. The proof binding and exact merge inputs will go to the existing external authorization as one resolved-effect request, and final revalidation will re-check the proof and authorization freshness before the merge API is called with the evaluated head SHA. The primary driver is integrity: the independent verifier, not the acting agent, owns proof semantics.

### Alternatives Considered

#### Alternative 1: Accept agent-supplied proof in the safe-output message

The agent would attach a proof object to its merge request, and the handler would validate its shape. This was considered because it needs no check-run plumbing. It was rejected because the agent would then control both the requested effect and the evidence for it, so a forged or stale proof would pass as easily as a genuine one.

#### Alternative 2: Rely on required checks with a success conclusion

Keep the existing `required-checks` gate and treat a successful check run as sufficient. This was the lowest-effort option and the closest call. It was rejected as the sole mechanism because a green conclusion does not show which invariant configuration was evaluated or that it covered the exact head. The proof check still runs as a required check, so the conclusion gate remains in force alongside the proof binding.

#### Alternative 3: Embed a verifier or solver in gh-aw

The compiler or runtime could evaluate invariants directly. This was rejected because it would move verifier credentials, implementation, and policy into the same trust domain as the workflow tooling, and it would create a second authorization-like system. The chosen design introduces neither an embedded solver nor a second authorization system.

### Consequences

#### Positive
- A merge proceeds only when an independently pinned verifier has passed the exact head and configuration digest.
- Agent-authored proof, stale heads, and mismatched evidence are refused before the authority is asked, and again at final revalidation.
- The authorization covers the resolved effect, including check-run ID, App ID, verifier version, and digest, so replacement evidence cannot reuse an earlier decision.

#### Negative
- Authors must pin a check App ID, verifier version, and configuration digest, and keep them in sync with the verifier.
- Enabling proof requires an external `authorization` endpoint; a configuration without one is rejected.
- The `gh-aw.merge-proof.v1` summary format becomes a compatibility contract between gh-aw and the verifier, and changes need coordinated versioning.
- Each proof-enabled merge adds check evaluation and a revalidation comparison, which adds API calls and failure modes.

#### Neutral
- Staged previews skip the resolved-effect authorization call (the code gates it on `!isStaged`). [TODO: verify] that staged runs are intentionally covered only by the handler-level authorization boundary.
- Enabling proof does not permit default or protected-branch merges and does not bypass review, file-protection, or other merge gates.
- gh-aw treats the configuration digest as opaque; the verifier defines what it covers.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
