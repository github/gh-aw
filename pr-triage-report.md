### Executive totals
- Open Copilot PRs: 26
- Eligible after 1h cooldown: 5
- Skipped by cooldown: 21
- Distribution by category: bug=4, chore=1
- Distribution by risk: medium=5
- Distribution by priority: medium=5
- Distribution by action: fast_track=1, batch_review=4, auto_merge=0, defer=0, close=0

### Top-priority PRs
- #77185 — Accept `-1` for runtime `GH_AW_DEFAULT_*MAX_AI_CREDITS`, and warn on values that fall back | risk=medium | priority=medium | score=70 | action=fast_track
- #77088 — Read AWF per-conversation steering deliveries from the unified session | risk=medium | priority=medium | score=64 | action=batch_review
- #77326 — Separate authored pricing overrides from reporting costs | risk=medium | priority=medium | score=62 | action=batch_review
- #77212 — Align model routing with AWF runtime, topology, and host support | risk=medium | priority=medium | score=61 | action=batch_review

### Auto-merge candidates
- None.

### Fast-track items
- #77185 — runtime budget validation fix is the highest-value user-visible correctness patch and has a clean validation signal.

### Batch opportunities
- pr-batch:model-routing-and-audit: #77088, #77212, #77326

### Close candidates
- None in the eligible set.

### Key trends and next actions
- The eligible queue is dominated by correctness fixes in billing/runtime behavior and model-routing compatibility.
- The highest-value change is the runtime credit fallback fix; it merits expedited human review.
- The model-routing/pricing/audit fixes are a coherent patch cluster and should be reviewed together to reduce regressions.
- Re-run triage after the 1h cooldown expires for remaining open Copilot PRs to capture newer work.

<details>
<summary>Skipped by cooldown</summary>

- #77427 — [spec-enforcer] Enforce specifications for workqueue, workflow/aw, workflow/cachepolicy (updated within 1h)
- #77414 — [docs] Update glossary - daily scan (updated within 1h)
- #77253 — Keep Atomic sessions on a workflow-configured drive (draft, within 1h)
- #77403 — Bump experimental Atomic sample to 0.9.84 (draft, within 1h)
- #77371 — Require scoped base branches for merge-pull-request (within 1h)
- #77362 — Add comment-memory storage for ledgers (within 1h)
- #77334 — Reduce generated model-routing decision steps (within 1h)
- #77319 — build(deps): bump the pip group across 1 directory with 2 updates
- #77262 — Opt Smoke Atomic into drive-memory sessions (draft)
- #76957 — Restrict legacy credential-backed proxy ingress
- #76615 — build(deps-dev): bump fast-xml-parser ...
- #76614 — build(deps): bump fast-xml-parser ...
- #76492 — build(deps): bump library/alpine ...
- #76489 — build(deps): bump @astrojs/markdown-remark ...
- #76488 — build(deps-dev): bump vitest ...
- #76487 — build(deps): bump pytest ...
- #76486 — build(deps): bump astro ...
- #76485 — build(deps): bump golang.org/x/sync ...
- #76483 — build(deps-dev): bump @github/copilot-sdk ...
- #76482 — build(deps): bump github.com/spf13/pflag ...
- #76481 — build(deps-dev): bump @playwright/test ...
- #76479 — build(deps): bump multidict ...
- #76477 — build(deps): bump @astrojs/starlight ...
</details>
