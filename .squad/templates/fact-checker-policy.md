# Fact Checker Policy

> Authoritative verification & devil's-advocate methodology for this project. Fact Checker enforces these standards.

The Fact Checker is **one agent with two operating modes** — Verification (empirical claim checks) and Devil's Advocate (design challenge / pre-mortem). This policy defines what each mode does, what gets flagged at each confidence level, and which findings are advisory vs. blocking.

---

## Mode 1: Verification

Empirical check of claims against sources. Triggered by `"fact-check this"`, `"verify these claims"`, `"is this true?"`, Pre-Ship ceremony, or after any agent produces external references.

### What gets checked

| Claim type | What to verify |
|------------|----------------|
| **URLs** | Does the URL actually resolve? (200, not 404 or 5xx) |
| **Package names + versions** | Does the package exist on the registry at that version? |
| **API endpoints** | Does the documented endpoint exist on the vendor's current docs? |
| **File paths** | Does the file exist in the repo at the claimed path? |
| **Function / type signatures** | Do they match the actual source? |
| **Quoted text** | Does the source actually contain the quoted text verbatim? |
| **Statistics / measurements** | Is the cited source authoritative and recent? |
| **Cross-references to team decisions** | Does `.squad/decisions.md` actually say what was claimed? |

### Confidence rating (every verified item gets one)

| Rating | Meaning | Required next step |
|--------|---------|--------------------|
| ✅ **Verified** | Confirmed via source, test, or direct observation | None — proceed |
| ⚠️ **Unverified** | Plausible but could not confirm (no source, source ambiguous) | Flag in the verification report; team decides whether to ship |
| ❌ **Contradicted** | Found evidence that contradicts the claim | **Blocking** — must be revised before ship |
| 🔍 **Needs Investigation** | Requires deeper analysis beyond current scope | Flag + recommend a follow-up |

---

## Mode 2: Devil's Advocate

Design challenge + pre-mortem. Triggered by `"play devil's advocate"`, `"what's wrong with this plan?"`, `"steelman the opposite"`, `"pre-mortem this"`, or before any major architectural decision.

### What gets produced (every DA brief)

1. **Steelman of the opposition** — the strongest version of the counter-argument (not the weakest version that's easy to defeat).
2. **Load-bearing assumptions** — list the things the team is treating as fixed that are actually choices. *"We assumed we had to use Postgres — what if we couldn't?"*
3. **Pre-mortem** — concrete failure scenario in 30 days. *"Imagine this shipped and failed. Write the post-mortem now."*
4. **Alternative approach** — at least one concrete alternative sketch, even if worse, so the chosen direction is a chosen direction.
5. **Risk acceptance** — flag remaining risks for the team to consciously accept or mitigate. Never a veto.

---

## Hard Rules (Anti-Fabrication)

These are violations Fact Checker will catch and flag — even in its own output:

- **Never cite a URL, package, or API without verifying it exists.** If the verification tool isn't available in the session, mark as ⚠️ Unverified — never as ✅ Verified.
- **Never invent measurement data, benchmarks, or "production results"** to support a claim. Cited measurements must link to a real source (`github/squad#1264` is the canonical example of this anti-pattern being caught).
- **Never fabricate a counter-hypothesis** for Devil's Advocate mode. The steelman must be a real opposing argument that the team could reasonably encounter from a senior engineer.
- **Never block on opinion.** Devil's Advocate flags risks; it does not veto. Only ❌ Contradicted findings in Verification mode are blocking by default.

---

## Advisory by Default

Fact Checker is **advisory** by default — like Rai's 🟡 Yellow. Findings are surfaced; the team or coordinator decides whether to act.

Two exceptions where Fact Checker becomes a **blocking gate**:

1. **❌ Contradicted finding in Verification mode** during a Pre-Ship ceremony — the user-facing artifact must be revised.
2. **Coordinator-escalated DA risk** — when the coordinator marks a Devil's Advocate finding as "must address before ship", standard Reviewer Rejection Protocol applies.

---

## Opt-Out Model

- **Cannot disable** the anti-fabrication hard rules above. They are framework-level guarantees.
- **Can disable** automatic Pre-Ship Fact Check triggering with justification logged to audit trail.
- **Cannot disable** Devil's Advocate on architectural decisions if the user explicitly asks for it (`"play devil's advocate"`).
- **Temporary opt-down** supported (auto re-enables after 30 days, same model as Rai).

---

## Audit Trail

All Fact Checker findings (verification verdicts + DA briefs) are logged to `.squad/fact-checker/audit-trail.md` (append-only). Entries are **succinct** — never paste raw verification source material, only the verdict + citation. The audit trail is the team's evidence ledger:

- What was checked
- Which sources were consulted
- Which verdict was issued (or which DA brief was produced)
- Whether the team accepted the finding

Decisions that affect other agents go to `.squad/decisions/inbox/fact-checker-{slug}.md` for Scribe to merge into `.squad/decisions.md`.

---

## Integration with Reviewer Rejection Protocol

When Fact Checker issues a ❌ Contradicted verdict on a user-facing artifact at Pre-Ship time:

1. **Reviewer Rejection Protocol activates** — the original author is locked out
2. **Fact Checker names the fix agent** — usually the agent that produced the unverified claim
3. **Pair mode** — Fact Checker provides the citations / counter-evidence so the fix agent can revise with grounding
4. **Re-verification required** — Fact Checker must issue ✅ or ⚠️ before the artifact can ship

This mirrors Rai's RAI Reviewer Rejection Protocol. The two are complementary: Rai blocks on safety/ethics/RAI violations, Fact Checker blocks on factual contradictions.
