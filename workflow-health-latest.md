# Workflow Health — 2026-09-28T04:50Z

## 4th self-expiry confirmed: #63556 closed not_planned, same 3 root causes still live
`#63556` (filed 2026-09-26) closed `not_planned` at 2026-09-27T06:54:57Z without a fix landing —
the 4th consecutive auto-expiry of the same finding (`#63098` → `#63348` → `#63556` → now).
Re-verified all 3 on current `main`:
1. **avenger.md:42 — npm symlink bind-mount.** Currently dormant: `check_ci_status` gate hasn't
   re-invoked the agent job since the last failure (run 36284834929, 2026-09-27T01:12Z); ~27
   consecutive runs since have `agent: skipped`. Still a live landmine when the gate reopens.
2. **metrics-collector.md:14-16 — missing `model-provider: github`.** Daily failures continue
   (run 36371253821, 2026-09-28T02:48Z) but root-caused **2x in a row** as the separate
   `/tmp/gh-aw/aw-mcp` secret-redaction crash (`EACCES: permission denied, scandir
   '/tmp/gh-aw/aw-mcp'`) — a genuine cross-workflow AWF-firewall defect, independent of the
   still-needed model-provider config fix.
3. **gpclean.md:58,61 — hardcoded retired `gpt-5-codex`.** New failure today: run 36374058667
   (2026-09-28T03:31Z), auto-filed as #63913. All 4 codex-harness retries failed with
   `Model 'gpt-5-codex' is retired... Did you mean 'gpt-5.3-codex'?`.

**Decision this run:** did not file a 5th duplicate findings tracker (it would self-expire under
the current `expires: 1d` policy). Instead posted a reinforcement comment on the still-open
structural-fix issue `#63656` (extend expires for P0/P1 trackers) — the real unblocker for this
loop — plus noted `#63657` (scoped create-pull-request safe-output) as the complementary fix.
Both remain open, unmerged.

## failing-workflows.json re-verified live (4 entries)
- **daily-firewall-report**: same `/tmp/gh-aw/aw-mcp` redaction-crash class as metrics-collector;
  agent output itself healthy (discussion created).
- **daily-go-test-parallelizer**: no fresh failure evidence beyond pre-loaded snapshot; unchanged.
- **lint-monster**: no fresh failure evidence beyond pre-loaded snapshot; unchanged.
- **cjs**: plain GH Actions CI, out of `gh aw` scope — `action_required` (approval gate)
  alternating with `success`, not failures.

## Compilation Status
298/298 workflows have lock files (100%), compile-validate clean.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md` still lacks `update-issue: target: '*'` for closing/updating issues
it didn't create this run, and `expires: 1d` on P0/P1 trackers keeps causing this exact
self-expiry loop. Issues #63656 and #63657 track both fixes — still open, unmerged, 2nd run in a
row recommending they be prioritized over filing more duplicate findings.

## Actions Taken This Run (2026-09-28)
- Re-verified all 3 root causes via live job logs (metrics-collector run 36371253821, gpclean run
  36374058667, avenger last-triggered run 36284834929) and current file contents on `main` — all
  still unfixed.
- No new findings tracker filed (would duplicate/self-expire); posted reinforcement comment on
  `#63656` instead, consolidating evidence and pointing at the structural fix.
- No dashboard issue created this run — no compilation/health-category shift; captured via the
  tracker comment.

> Last updated: 2026-09-28T04:50Z
