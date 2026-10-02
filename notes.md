# Copilot Session Insights — repo memory

## 2026-09-30 snapshot
- 50 sessions (4 branches); **54.0% completion** (27 success/11 action_required/6 failure/4 cancelled/2 in_progress), +44pts vs 09-29 (10%), **3rd-highest of 38 recorded days** (behind 09-02's 78% and 09-18's 58%), 37-day mean 31.9%.
- **true_agentic streak broke** after 2 consecutive 100% days (09-27/09-28): raw 4/6=66.7% ("Addressing comment on PR" x4 + "Running Copilot cloud agent" x2), but both non-successes were verified merge-triggered cancellations via `gh api pulls` (PR #64334 merged 05:03:21Z → own run cancelled 24s later; PR #64354 merged 05:03:44Z → own run cancelled 24s later) — adjusted rate 4/4=100%, so this is merge noise, not a genuine regression.
- **merge → Squad Implement Worker +~7s** sub-signature recurred x2 (05:03:28Z, 05:03:51Z), now seen on 09-26 (x3)/09-27 (x1)/09-30 (x2).
- **NEW CANDIDATE PATTERN push_supersede_cancellation**: on `copilot/add-replay-projections-to-ledger` (PR #64420, open all day), CJS and CGO were cancelled mid-run while the branch took 4 rapid pushes in a ~95min window (verified via `gh api commits`, no merge/close event) — looks like ordinary concurrency-group supersession by a newer push, not a PR-terminal-event cascade. Needs a 2nd observation to confirm.
- **provenance_inversion** 85.2% bot-driven (23/27), upper edge of the 72-86% band, still in-band.
- **burst_vs_isolated_success_gap** near parity for a 3rd time: isolated 54.5% (6/11) vs burst-fired 53.8% (21/39), ~1.01x — joins 09-13 (1.05x) and 09-18 (inverted), eroding confidence in the historical 3-8x law.
- 4 branches: `add-replay-projections-to-ledger` 34/50=68% (55.9% succ, heaviest but below 09-25's 88% record), `implement-new-ledger-design` 7/50=14% (71.4% succ, day's longest run: CGO 73.08min), `support-awf-routing` 7/50=14% (42.9% succ, only genuine non-cascade failure cluster: Agentic Commands x2 + Content Moderation + AI Moderator, 04:39-04:44Z), `fix-gh-proxy-token-issue` 2/50=4% (0% succ).
- Duration: raw mean 16.39m/median 8.95m (37 nonzero: mean 21.27m/median 18.0m) — highest average since 09-23, breaking a 4-day run of sub-5-min days; genuinely spread distribution (163s-4385s), not an artifact.
- Orphans 0/20 → 0% NORMAL, 38th healthy day. Conv logs empty 38th+ day (flagged via missing_data). Standard run (roll=36).

## 2026-09-28 snapshot
- 50 sessions (4 branches); **50.0% completion** (25 success/25 action_required, 0 failure/cancelled/in_progress — first perfectly binary day since at least 09-24), +2pts vs 09-27 (48%), **4th-highest of 33 recorded days**, 32-day mean 31.9%.
- **true_agentic streak day 2 of restart**: 9/9 "Addressing comment on PR" succeeded (100%), following 09-27's 7/7 restart after the 09-26 break.
- **provenance_inversion band shift reinforced**: 64.0% bot-driven (16/25), below the 72-86% band for the 4th time in 6 days (09-23 58.3%, 09-26 61.1%, 09-27 70.8%, today 64.0%) — now looks structural, not noise.
- **branch_level_stuck_gate partially broken**: Squad/Agentic Commands/Doc Build-Deploy still 0%, but CJS 3/9 (33.3%), CWI 3/6 (50.0%), CGO 2/5 (40.0%) posted genuine mixed success — first partial-recovery day for these three gates in recent memory.
- Branches: dynamic-checkouts-github-action 26/50=52% (23.1% succ, concentration leader again but down from 09-27's 40%), task-...-87cd5693 12/50=24% (91.7% succ — highest single-branch rate in recent days), preserve-review-provenance-marker 9/50=18% (66.7% succ), task-...-ca3f0885 3/50=6% (66.7% succ).
- **burst_vs_isolated_success_gap RESTORED** to historical band: isolated 100% (17/17) vs burst-fired 24.2% (8/33), 4.13x gap — back within 3-8x range after two sub-band days (09-26 1.75x, 09-27 1.47x).
- No merge_invalidation_cascade today (0 failure conclusions); 2 more isolated single-fire "Squad Implement Worker" events consistent with the recurring merge-adjacent sub-signature, but no multi-run cascade.
- Duration: raw mean 3.84m/median 1.48m (25 nonzero: mean 7.68m/median 5.72m); longest genuine run 23.92min; 2nd consecutive clean day without a status-resync artifact.
- Orphans 0/21 → 0% NORMAL, 36th healthy day. Conv logs empty 36th+ day (flagged via missing_data). Standard run (roll=84).

## 2026-09-27 snapshot (brief — full detail in cache-memory history.json)
- 50 sessions; **48.0% completion** (24 success/23 action_required/3 failure), +12pts vs 09-26 (36%), 2nd-highest reading in 2 weeks after 09-24 (44%).
- true_agentic_streak restarted after 09-26's break: 7/7 "Addressing comment on PR" succeeded (100%).
- provenance_inversion LOW for a 3rd time: 70.8% bot-driven, just below the 72-86% band (joins 09-23, 09-26).
- merge_invalidation_cascade x2 independent events, including a 2nd confirmed instance of "merge -> Squad Implement Worker +6-7s".
- 4 branches: dynamic-checkouts-github-action 20/50=40% (20.0% succ), task-...-87cd5693 15/50=30% (73.3% succ, merged as PR #63687), task-...-ca3f0885 9/50=18% (55.6% succ, merged as PR #63688), preserve-review-provenance-marker 6/50=12% (66.7% succ).
- Orphans 0/17 → 0% NORMAL, 35th healthy day. Conv logs empty 35th+ day.

## 2026-09-26 snapshot
- 50 sessions (8 branches, mostly CI/gate re-fires); **36.0% completion** (18 success/31 action_required/1 cancelled/0 failure), +16pts vs 09-25 (20%), slightly above ~31.5% mean.
- **true_agentic streak BROKEN**: 7/8=87.5% (5-day 100% streak 09-22..09-25 ends), but the 1 exception was a merge-triggered cancellation (PR #63474 merged 05:39:46Z, own run cancelled ~39s later), not a genuine failure.
- **provenance LOW again**: 61.1% bot-driven (11/18), 2nd-lowest of 30+ days after 09-23's 58.3% (called a one-off then) — 2 low readings in 4 days, may not be noise.
- **NEW RECORD merge_invalidation_cascade multiplicity**: 4 merges each disturbed an in-flight run within 6-39s (beats 09-14's record of 2/day). 3 of 4 share an identical new sub-signature (merge -> Squad Implement Worker action_required 6-7s later, PRs #63492/#63494/#63493) — previously only seen as isolated "smallest instance" events (09-19, 09-24), now recurring 3x in one 8min window.
- **branch_level_stuck_gate**, near-total: CJS/CWI/Doc Build-Deploy/Squad/Agentic Commands/Squad Implement Worker all 0% (25/25), CGO 1/7 (14.3%, single pass) vs Code scanning 10/10 + Addressing comment 7/8 (87.5%) — same bimodal split as prior weeks.
- Branches: fix-cache-memory-validation-failure 15/50=30% (33.3%succ), preserve-review-provenance-marker 14/50=28% (35.7%succ). Yesterday's record branch dynamic-checkouts-github-action (88%) dropped to 6/50=12% as PR #63241 stabilized.
- burst_vs_isolated: isolated 55.6% (5/9) vs burst 31.7% (13/41), 1.75x gap (narrower than usual 3-8x, same direction).
- Duration proxy: raw mean 3.06m/median 0m (18 nonzero: mean 8.32m/median 4.58m). 1 CGO success showed 437.7min resync artifact (same pattern as 09-16/18/20/23), excluded.
- Orphans 0/18 → 0% NORMAL, 34th healthy day. Conv logs empty 34th+ day — flagged in report as top system-improvement priority. Standard run (roll=74).

## 2026-09-25 snapshot
- 50 sessions (mostly CI/gate re-fires on 3 branches, not 50 distinct tasks); **20.0% completion** (10 success/38 action_required/2 in_progress), -24pts vs 09-24 (44%), below ~32% mean.
- **true_agentic 2/2=100%**, 5th consecutive full-recovery day (09-22 7/7, 09-23 5/5, 09-24 5/5), +1 in_progress.
- **provenance mid-band**: 80.0% bot-driven (8/10), inside 72-86% range.
- **branch_level_stuck_gate, no exceptions**: CGO/CWI/CJS 0/10 each, Doc Build-Deploy 0/8 vs Code scanning 8/8 + Addressing comment 2/2 (100%) — same bimodal split as 09-22.
- **NEW RECORD concentration**: dynamic-checkouts-github-action 44/50=88% (beats 86%), 18.2% succ on-branch.
- **burst_vs_isolated NEW RECORD**: isolated 66.7% (8/12) vs burst 5.3% (2/38), 12.7x gap (prior ~8x).
- PR #63241: 9 blocked gate cycles in ~40min (~5min apart) + passing Code scanning each time — dense but standing pattern. No merge/close cascade today.
- Other branches: add-diagnostic-message-for-failure 5/50=20%succ, fix-assert-trusted-checkout-runtime 1/50=100%succ.
- Duration proxy: mean 1.44m/median 0m. Orphans 0/18 → 0% NORMAL, 33rd healthy day. Conv logs empty 33rd+ day. roll=92 standard.

## 2026-09-24 snapshot
- 50 sessions; **44.0% completion** (22 success/8 failure/20 action_required), +20pts vs 09-23 (24%), above ~31% mean.
- **true_agentic 5/5=100%**, 4th consecutive full-recovery day (09-19 3/3, 09-22 7/7, 09-23 5/5).
- **provenance_inversion back in band**: 77.3% bot-driven (17/22), confirms 09-23's 58.3% low was a one-off.
- **NEW RECORD pr_terminal_event_cascade** (2nd instance): PR #63069 closed w/o merging 04:14:22Z → 8 runs/6 workflows failed 1s later + Squad Implement Worker action_required 7s later = 9 invalidated, beats prior peak 8 (09-23).
- Smaller `merge_invalidation_cascade`: PR #63048 merged → Squad Implement Worker action_required 7s later (1 run, tied smallest on record).
- Gates partly broke pattern: Doc Build-Deploy 0/4 unchanged, but CGO/CWI 1/5 (20%) posted first partial passes, both on merged branch improve-agent-detection.
- burst_vs_isolated: isolated 71.4% (5/7) vs burst-fired 39.5% (17/43), 1.8x gap (narrower than usual 3-8x).
- 4 branches: allow-add-labels-max-control 20/50=25.0%succ, ensure-empty-new-line-after-details 19/50=42.1%succ (cascade branch), fix-assert-trusted-checkout-runtime 6/50=83.3%succ, improve-agent-detection 5/50=80.0%succ (merged).
- Duration: raw mean 20.12m/median 4.06m (cascade-inflated).
- Orphans 0/18 → 0% NORMAL, 32nd healthy day. Conv logs empty 32nd+ day. Standard run (roll=53).

## 2026-09-23 snapshot
- 50 sessions; **24.0% raw completion** (12 success, 8 failure, 30 action_required), -7.7pts vs 30-day mean 31.7% (range 4-78%), down from 09-22 (40%). No run recorded for 09-21 (gap in daily cadence).
- **true_agentic_100pct_streak: 3rd consecutive full-recovery day** — 5/5 true-agentic runs succeeded (4x "Addressing comment on PR #62776" + 1x "Running Copilot cloud agent"), following 09-19 (3/3) and 09-22 (7/7).
- **provenance_inversion at a new low**: only 58.3% of successes (7/12) bot-driven (5x Code scanning AI findings PR#62776 + 2x PR#62815) — lowest bot-driven share of the last 30 recorded days, below the historical 72-86% band; true-agentic share (41.7%) roughly 2x typical.
- **NEW pattern: pr_terminal_event_cascade** (refines `merge_invalidation_cascade`) — PR #62815 ("Fix Cursor smoke workflow telemetry failure") was **closed without merging** at 04:01:42Z. One second later (04:01:43Z), 8 in-flight runs across 5 distinct workflows (Agentic Commands x5, Stale Lock Files, Content Moderation, AI Moderator) on `copilot/fix-github-actions-job-again` all flipped to `failure` simultaneously — the **largest cascade on record** (prior peak: 6 runs on 09-13), and the **first confirmed close-triggered (not merge-triggered) instance**. Confirms cascade triggers are "any PR terminal state," not merge-specific.
- Perpetually-blocked gates unchanged: CGO/CJS/CWI/Doc Build-Deploy/Squad = 0% success today (22/50 = 44% of all runs) — standing never-resolves pattern holds.
- New workflow observed: **`Stale Lock Files`** (4 firings: 1 cascade failure, 3 action_required) — not present in any of the 09-13 through 09-22 snapshots; watch for recurrence.
- Duration proxy: mean 31.51m / median 0m, entirely inflated by the 8-run cascade artifact (180-190.55min gaps); 20 nonzero entries average 78.8m. Not a genuine slowdown.
- 3 branches: `add-opt-in-shell-free-profile` 36/50 (72%, 25.0% success), `fix-github-actions-job-again` 12/50 (24%, 25.0% success, cascade branch), `fix-github-actions-job` 2/50 (4%, 0% success).
- Orphans 0/17 open PRs → 0% NORMAL, 31st consecutive healthy day (30-day baseline 0.0%). Only 2 in-progress runs repo-wide in trailing 6h (both on main: this workflow + Tidy). Conv logs empty (31st+ day) — now the single largest unresolved tooling gap. Standard run (roll=37, no experimental strategy).

## 2026-09-19 snapshot
- 50 sessions; **28.0% raw completion** (14 success, 2 failure, 34 action_required), -30pts vs 09-18 (58%), back near the 28-day mean 31.4% (range 4-78%) after yesterday's 2nd-highest reading.
- Standard run (roll=35 ≥ 30 threshold) — no experimental strategy this cycle.
- **true_agentic_100pct_streak RESTORED (small n)**: 3/3 true-agentic runs succeeded (2x "Addressing comment on PR" #61871/#61857 + 1x "Running Copilot cloud agent") — first 100% day since the streak broke 09-16 (09-16: 0/1, 09-17: 0/0 completed, 09-18: 8/10=80%). Sample is small; needs repeat days to confirm full recovery.
- **provenance_inversion** reverted to 78.6% bot-driven (11/14), back within the 75-86% historical band.
- **Perpetually-blocked gates**: `Squad` (10/10 action_required, 0%) and `Agentic Commands` (11/11 action_required, 0%) = 21/36 (58.3%) of today's non-completions — standing "never auto-resolves" pattern.
- **Genuine (non-cascade) failure-then-recovery**: `CGO`+`CWI` failed 01:17:52Z on `copilot/ensure-logs-command-caching`, both succeeded on retry 01:25:01Z (~7min later) — verified not merge-related (branch merged 2h later at 03:15:30Z).
- **merge_invalidation_cascade smallest instance yet**: PR #61871 human-merged (pelikhan) 03:15:30Z → `Squad Implement Worker` fired action_required 6s later (1 run invalidated) — clean single-run textbook case.
- **burst_vs_isolated_success_gap REVERTED** to historical norm: isolated 75.0% (3/4) vs burst-fired 23.9% (11/46), 3.1x gap — not a 2nd inversion, 09-18's flip looks like noise.
- 3 unique branches: `embed-threat-detect-digests` 22/50 (44%, 27.3% success), `ensure-logs-command-caching` 20/50 (40%, 25% success), `fix-update-job-failure` 8/50 (16%, 37.5% success, freshest branch).
- Duration: 2/50 entries show the same GitHub status-resync artifact as 09-16 (~117.6min, timestamp touched at the 03:15:30/31Z merge). Excluding those, real mean/median = 13.41min / 15.65min (max 26.33min).
- Orphans 0/12 open PRs → 0% NORMAL, 28th consecutive healthy day. Conv logs empty (28th+ day) — now the single largest tooling gap.

## 2026-09-18 snapshot
- 50 sessions; **58.0% raw completion** (29 success, 8 failure, 10 action_required, 3 cancelled), +22pts vs 09-17 (36%), **2nd-highest of 27 recorded days** (behind 09-02's 78%), 27-day mean 31.6%.
- **EXPERIMENTAL (roll=8): human_batch_merge_cascade_control** — 3 independent merge_invalidation_cascades (PRs #61599, #61600, #61602) clustered within a 67s window (15:48:15–15:49:22Z) across 3 branches, superficially resembling 09-16's cross_branch_scheduled_cascade. Verified via `gh api pulls`: all 3 PRs were `merged_by` the SAME human (pelikhan) back-to-back, and all 3 offsets were tight (1-2s) matching each branch's own merge time exactly. Clean positive control confirming the 09-16 diagnostic: same human + consistent tight offsets = ordinary `human_batch_merge_cascade` (new named sub-pattern), not a dispatcher. Effectiveness High; recommend promote.
- **true_agentic_streak still not restored**: 10 true-agentic runs (9x "Addressing comment on PR" + 1x "Running Copilot cloud agent"), 8 success / 2 cancelled (80%) — best since the 09-16 break but 3rd consecutive day without full 100% recovery.
- **provenance_inversion reverted**: 21/29 successes (72.4%) bot-driven, lower edge of the 72-86% historical band, ending the 2-day 100% streak (09-16/09-17).
- **burst_vs_isolated_success_gap INVERTED for the first time** (8/8 prior days had isolated ≥ burst-fired): today isolated 53.8% (7/13) vs burst-fired 59.5% (22/37) — burst-fired slightly ahead. Needs a 2nd inversion to confirm this isn't noise.
- 4 unique branches: `bump-mcpg-version-0425` 23/50 (46%, 82.6% success — highest single-branch success rate on record), `fix-install-awf-binary-failure` 14/50 (28.6% success), `fix-claude-engine-experiments-model` 7/50 (42.9% success), `fix-slash-command-crlf-activation` 6/50 (50% success).
- CGO dipped to 1/7 success (14.3%, lowest workflow today): 3 cascade-explained failures, 1 cancelled, 2 genuine non-cascade failures on bump-mcpg-version-0425 unrelated to its later 22:19:13Z merge.
- Duration proxy: mean 19.87m / median 10.14m (all), mean 24.84m / median 13.78m (40/50 nonzero) — widest window yet (~495min/8.25h).
- Orphans 0/14 open PRs → 0% NORMAL, 27th consecutive healthy day. Conv logs empty (27th+ day).

## 2026-09-17 snapshot
- 50 sessions; **36.0% raw completion** (18 success, 8 failure, 22 action_required, 1 cancelled, 1 in_progress), +18pts vs 09-16 (18%), mid-pack vs 26-day mean 30.6%.
- **provenance_inversion ties record**: 18/18 successes (100%) are CI-gate/review-bot workflows (CWI x4, CGO x2, CJS x2, Code scanning AI findings x4, + 6 single-instance reviewer/gate bots) — 2nd consecutive all-bot day after 09-16's first-ever 100% record.
- **true_agentic_streak still broken**: the only true-agentic candidate ("Addressing comment on PR #61430") was still `in_progress` at snapshot time — 2nd day without a completed true-agentic success.
- Non-merge failure cluster (open question): `copilot/allow-opt-out-detection-runs` fired two 3-workflow failure clusters (CGO+CWI+Doc Build-Deploy) at 06:05:30Z and 06:08:23Z, ~3min apart — its PR #61428 didn't merge until 06:44:14Z (36+min later), so this doesn't fit the standing merge_invalidation_cascade mechanic. Branch deleted post-merge, so a double-push couldn't be confirmed via commit history.
- 5 unique branches (up from 09-16's 3): `fix-copilot-sdk-issues` 26/50 (52%, 42.3% success), `allow-opt-out-detection-runs` 15/50 (30%, 13.3% success), `update-cli-version-checker` 7/50 (71.4% success), 2 singletons at 0%.
- Duration proxy: mean 6.81m / median 1.15m, tight 46-min window — reliable (no resync-artifact inflation unlike 09-16).
- Orphans 0/20 open PRs → 0% NORMAL, 26th consecutive healthy day. Conv logs empty (26th+ day). Standard run (roll=70).

## 2026-09-16 snapshot
- 50 sessions; **18.0% raw completion** (9 success, 33 failure, 7 action_required, 1 cancelled), -12.8pts vs 24-day mean 30.8%. **Cascade-adjusted completion 37.5%** (9/24) excluding 26 same-second review-bot failures.
- **EXPERIMENTAL (roll=5): cross_branch_cascade_synchronization** — 3 DIFFERENT branches (`fix-github-actions-job-failure-again` 8f, `daily-docs-healer-fix` 8f, `model-inventory-update-2026-09-16` 10f) each cascaded within a 23s window (03:26:16-39Z), no commit on main/any branch in that window. Only 2/3 offsets matched their own branch's merge (32-58s prior); the 3rd merged 15m45s later with no intervening push — implies a shared scheduled dispatcher, not a pure per-branch merge race. Refines `merge_invalidation_cascade`. Effectiveness High; recommend promote.
- CJS failed 6/6 (100%) across all 3 branches — first 100%-failure all-branch single-workflow day; only 2/6 inside the cascades.
- **true_agentic_100pct_streak BROKE** after 9 consecutive 100% days: "Addressing comment on PR #61232" was cancelled, not success. **provenance_inversion NEW RECORD 100% bot-driven** (9/9: 6 Agentic Commands + 3 Running Copilot Code Review).
- Orphans 0/20 open PRs → 0% NORMAL, 25th consecutive healthy day. Conv logs empty (25th+ day). Duration figures (mean 40.0m) inflated by a GitHub status-resync artifact, not real exec time — treat as unreliable this cycle.

## 2026-09-15 snapshot
- 50 sessions; **52.0% raw completion** (26 success, 2 failure, 21 action_required, 1 cancelled), +4pts vs 09-14 (48%), 3rd-highest of 24 recorded days (24-day mean 30.8%). Zero merge_invalidation_cascade today (no PR merges on either active branch in the 04:07–05:51Z window; verified via `gh api pulls`) — confirms cascades are merge-triggered, not a daily constant.
- 3 unique branches (vs 09-14/09-10's record-low 2): `copilot/update-logs-command-multi-target` 32/50 (64%, 53.1% success); `copilot/remove-effective-tokens` 17/50 (34%, 47.1% success); `copilot/bump-gh-aw-firewall-to-v02817` 1/50 (100%).
- True-agentic (Addressing comment on PR #61027 x2, #60997 x2, #60945 x1) = 5/5 = 100%, 9th consecutive 100% day. **provenance_inversion reverted** to 80.8% bot-driven (21/26), back inside the historical 75-86% band after 09-14's record 91.7%.
- burst_clustering inconclusive: only 1 run temporally isolated vs 49 burst-fired — smallest isolated sample on record, not statistically meaningful this cycle.
- Orphans 0/7 open PRs → 0% NORMAL, 24th consecutive healthy day. Conv logs empty (24th+ day). Standard run (roll=76).

## 2026-09-14 snapshot
- 50 sessions; **48.0% raw completion** (24 success, 6 failure, 20 action_required), +32pts vs 09-13 (16%), 2nd-highest recorded (23-day mean 29.1%). **Cascade-adjusted completion 52.2%** (24/46) once the 4 merge-invalidation artifacts below are excluded.
- **EXPERIMENTAL (roll=0): cascade_multiplicity_dual_merge** — for the first time, TWO independent merge_invalidation_cascade events fired in one day from two separate PR merges: PR #60701 (2m14s lifetime) invalidated 1 run (PR Data Prefetch); PR #60702 (~77min lifetime) invalidated 3 runs (CGO, CWI, Doc Build-Deploy), all at 00:28:19Z. Both verified via `gh api pulls`. Confirms cascade size scales with in-flight-workflow count at merge time and fires per-merge-event, not once/day. Effectiveness High; recommend promote (track cascade count/day + PR lifetime as predictor).
- 2 genuine (non-cascade) review-bot failures also hit `copilot/update-logs-command-support` while PR #60702 was open: Matt Pocock Skills Reviewer, PR Code Quality Reviewer.
- Only 2 unique branches fired all 50 sessions (ties 09-10 for lowest diversity): `update-logs-command-support` 43/50 (86%, 60.5% success excl. cascade+genuine failures); `update-release-notes-instructions` 7/50 (14%).
- True-agentic completions (Addressing comment on PR #60702 x2) = 2/2 = 100%, 8th consecutive 100% day. **provenance_inversion NEW RECORD HIGH**: 22/24 successes (91.7%) bot-driven — first time above the historical 75-86% range.
- burst_clustering rebounded: isolated 66.7% (2/3) vs burst-fired 46.8% (22/47), 1.42x gap, up from 09-13's cascade-contaminated 1.05x near-parity low.
- Orphans 0/4 open PRs → 0% NORMAL, 23rd consecutive healthy day. Conv logs empty (23rd+ day).

## 2026-09-13 snapshot
- 50 sessions; **16.0% raw completion** (8 success, 16 failure, 26 action_required); 21-day mean (cache-memory) 29.7%. **Cascade-adjusted completion 23.5%** (8/34) once the merge-invalidation artifacts below are excluded.
- **merge_invalidation_cascade — largest instance recorded (6th occurrence)**: PR #60561 merged at 05:18:27Z invalidated all 16 of today's failures on `copilot/fix-github-actions-job-failure` (incl. 8 reviewer-bot workflows firing/failing in a 4s window), 4x the prior peak of 4 runs. Verified via `gh pr list`. Excluding these, that branch converts 2 success/1 action_required = 66.7% (healthy).
- True-agentic completions (2x "Running Copilot cloud agent") = 2/2 = 100%, 7th consecutive 100% day. provenance_inversion holds (6/8 successes = bot-driven, 75%, upper edge of historical range).
- burst_clustering_temporal_density signal has collapsed to near-parity (isolated 16.7% vs burst 15.9%, ~1.05x) — cascade artifacts are almost all burst-classified and now dominate the bucket; needs cascade exclusion to stay useful.
- Orphans 0/3 open PRs (all Copilot-assigned + human reviewer requested) → 0% NORMAL, 22nd+ consecutive healthy day. Conv logs empty (22nd+ day). Standard run (roll=53).
- _Note: this repo-memory branch had not been updated since run 28925210910 (~07-08); cache-memory (`/tmp/gh-aw/cache-memory/session-analysis/history.json`) has been the continuously-updated source of truth in the interim (22 entries through today) and remains authoritative for the full daily series._

## 2026-07-04 snapshot
- 50 sessions; **54% completion** (27 success, 22 action_required, 1 failure) — **REGIME BREAK**: up sharply from 4%→8%→4% floor; highest in trailing window (prev max 40% on 06-10/06-27).
- **provenance_inversion FLIPPED**: 20/27 successes are core CI gates (Doc Build/Smoke CI/CWI/CGO) executing to success, not gate-blocked; action_required now dominated by agentic maintenance (PR Description Updater 5 + Label Closed PRs 5) + 12 partial CI. Strongest inversion break yet — echoes the smaller 06-23 gate-green episode (then 20%, 8/10 succ = green CI gates).
- 28/50 executed (non-zero); exec mean 6.72m / median 5.40m / max 15.83m (Addressing PR#43298). Overall mean 3.76m, median 1.7m. 47-min window (06:43–07:30Z).
- 1 failure: CGO on issue-update-maintenance-workflow (worst branch 3/12). fix-id-token-read-scope & aw-fix-missing-hippo-tool 5/5; add-dismiss-review-safe-output 7/9.
- Orphans 0/11 (2 unassigned but not gate-saturated: #43312, #43228; max gates/branch=2 on main) → 0% NORMAL, ~40th healthy day. Conv logs empty (37th day). Standard run (roll=70).

## 2026-07-03 snapshot
- 50 sessions; **4% completion** (2 success, 46 action_required, 2 in_progress) — floor regime continues (20%→8%→4% over 07-01..07-03); below 30d-mean ~13% & 15d-mean ~10%.
- provenance_inversion holds (5th+ obs): 2 successes = agentic (PR Sous Chef 8.62m + Skillet 0.48m, both on lint-monster-targeted-cleanup, 0/4 core CI gates); 46 action_required = CI gate sweeps (median 0m, avg 0.19m).
- Concentration: 8 `copilot/*` branches; top-2 duplicate-code-fix(16)+runtime-cloning(13)=58%. 22.5-min window (07:18–07:40Z).
- Orphans 0 (active runs all on `main`; max gates/copilot-branch=0); 12 open PRs (11 Copilot-assigned, 1 unassigned fresh codeql PR #43148 0-gate) → 0% NORMAL, ~39th healthy day. 0 escalations.
- Conv logs empty (36th day). **EXPERIMENTAL run (roll=8): Gate-Bundle Composition Divergence (GBCD)** — only 3/8 branches fire full 4/4 core CI gate set (Smoke CI+CGO+CWI+Doc-Deploy); lint/doc branches fire 0/4 (lightweight agentic wf); update-checkout fires 2/4 + moderation. Refines per_branch_gate_fanout: bundle is change-TYPE-adaptive, not uniform per PR-open. Effectiveness Medium; recommend Refine.

_(Prior peak 06-27: 40% (20 succ); superseded by 54% on 07-04. Per-day detail in session-trends.jsonl / session-insights-history.jsonl; older snapshots trimmed for size.)_

## Active patterns
- provenance_inversion (06-07): successes came from agentic runs, never gate sweeps. **BROKE 07-04**: 20/27 successes were core CI gates executing to success; watch whether this holds or reverts to the floor+inversion regime.
- inverse_gate_count_to_conclusiveness: Copilot-assigned ⇒ never orphaned (~38th healthy day).
- gate_sweep_zero_duration: snapshots routinely catch 45-50 action_required 0-duration runs.
- recovery_regression_oscillation: saw-tooth persists; spikes (38-40%) between troughs (0-10%).
- conversation_log_fetch_failure: 23rd+ recorded day (longest unresolved risk; behavioral/loop/context analysis unavailable — metrics are CI/infra metadata only).
- merge_invalidation_cascade: 7/23 recorded days; on 09-14 fired TWICE independently in one day (two separate PR merges) — cascade size scales with in-flight-workflow count at merge time, not a single-daily-incident.
- provenance_inversion: 09-14 set a new high (91.7% bot-driven), first time above the historical 75-86% band.
- gate_footprint_refire_signature (06-20): refire ratio = runs/distinct-workflows distinguishes broad CI from narrow re-fire.
- per_branch_gate_fanout (06-26): each PR-open fires a gate bundle; gate_count = f(PR-open), not branch health. **Refined 07-03 (GBCD):** bundle composition is change-TYPE-adaptive — code-change branches fire full 4/4 core CI gates, lint/doc branches fire 0/4 (lightweight agentic wf only), spec branches fire 2/4 + moderation. "~8-workflow uniform bundle" was an over-generalization from a code-heavy snapshot.
- gate_bundle_composition_divergence (07-03, experimental): fraction of open branches deviating from the full core CI gate set; 62.5% (5/8) diverged today. Distinguishes deterministic CI overhead from change-type-specific triggers.
- 09-15 update: provenance_inversion reverted to 80.8% (back in the 75-86% band); merge_invalidation_cascade had a zero-instance day (8/24 recorded days show >=1 cascade); conv logs empty 24th+ day.
- 09-16 update: provenance_inversion hit first-ever 100% bot-driven day (9/9); true_agentic_100pct_streak broke after 9 consecutive days; new cross_branch_scheduled_cascade sub-pattern identified (same-second failures across sibling branches not explained by own-branch merge timing).
- 09-17 update: provenance_inversion ties the 100% record for a 2nd consecutive day (18/18); true-agentic streak remains broken (candidate still in_progress); a same-branch double failure-cluster (3min apart, well before its own merge) doesn't fit merge_invalidation_cascade or cross_branch_scheduled_cascade — open question, watch for recurrence before naming a new pattern. Conv logs empty 26th+ day.
- 09-18 update: provenance_inversion reverted to 72.4% bot-driven, ending the 2-day 100% streak; true-agentic streak still not fully restored (8/10=80%, 3rd day since break); new **human_batch_merge_cascade** sub-pattern identified — same human merging multiple PRs back-to-back produces a same-window multi-branch cascade cluster that looks like but is NOT a cross_branch_scheduled_cascade (distinguish via merged_by identity + tight/consistent per-branch offsets); burst_vs_isolated_success_gap inverted for the first time (burst-fired > isolated). Conv logs empty 27th+ day.
- 09-19 update: true_agentic_100pct_streak RESTORED on a small sample (3/3) after the 09-16 break; provenance_inversion back to 78.6% bot-driven (in-band); burst_vs_isolated_success_gap reverted to the historical isolated>burst norm (09-18's inversion looks like noise, not a 2nd confirming instance); merge_invalidation_cascade's smallest-ever instance (1 run); genuine (non-cascade, non-merge) CGO/CWI failure-then-recovery pair verified. Conv logs empty 28th+ day — now flagged as the single largest unresolved tooling gap.
- 09-22 update (per cache-memory; no 09-20/09-21 repo-memory entries): true_agentic_100pct_streak held at 100% (7/7); provenance_inversion snapped back to 86.0% bot-driven (upper edge of band) after 09-20's record-low 53.3%; branch-level split perfectly bimodal (every CI-gate workflow 0% success, every true-agentic/code-scanning workflow 100% success); no cascade artifacts; conv logs empty 30th+ day.
- 09-23 update: **new pr_terminal_event_cascade pattern** — first confirmed cascade triggered by a PR **close** (not merge): PR #62815 closed unmerged at 04:01:42Z invalidated 8 in-flight runs across 5 workflows 1s later, the largest cascade yet (prior peak 6, 09-13). Refines merge_invalidation_cascade to "any PR terminal event." provenance_inversion hit a new LOW (58.3% bot-driven, below the 72-86% band) even as true_agentic_100pct_streak extended to a 3rd consecutive full-recovery day (5/5). New workflow name observed: `Stale Lock Files`. Conv logs empty 31st+ day.
- 09-24 update: pr_terminal_event_cascade NEW RECORD (9 runs, beats 8); provenance reverted to 77.3% bot-driven (in-band); true_agentic 4th 100% day (5/5); CGO/CWI posted first-ever partial passes (1/5) on a merged branch. Conv logs empty 32nd+ day.
- 09-25 update: true_agentic 5th 100% day (2/2, +1 in_progress); provenance mid-band (80.0%); branch_level_stuck_gate back to clean 09-22-style bimodal split; NEW RECORDS single-branch concentration (88%) and burst_vs_isolated gap (12.7x); no cascade today. Conv logs empty 33rd+ day.
- 09-26 update: true_agentic streak BROKE after 5 days (87.5%, 1 merge-triggered cancellation, not genuine failure); provenance LOW again (61.1%, 2nd-lowest of 30+ days); NEW RECORD merge_invalidation_cascade multiplicity (4/day, beats 09-14's 2); new "merge -> Squad Implement Worker +6-7s" sub-signature recurred 3x in one 8min window. Conv logs empty 34th+ day.
- 09-27 update: true_agentic streak restarted (7/7, day 1); provenance LOW for a 3rd time (70.8%, joining 09-23/09-26 — 3 of last 5 days below band); merge_invalidation_cascade x2 independent events, including a 2nd confirmed "merge -> Squad Implement Worker" instance. Conv logs empty 35th+ day.
- 09-28 update: true_agentic streak day 2 (9/9, 100%); **provenance_inversion band shift reinforced** — 64.0% bot-driven, below band for the 4th time in 6 days, now looks structural rather than one-off (watch for a 5th sub-band day before renaming the pattern); branch_level_stuck_gate **partially broken** for the first time in recent memory — CJS/CWI/CGO posted genuine mixed success (33-50%) instead of the usual clean 0%, while Squad/Agentic Commands/Doc Build-Deploy stayed at 0%; burst_vs_isolated_success_gap RESTORED to the historical 3-8x band (4.13x) after two sub-band days; first perfectly binary completion day (0 failure/cancelled/in_progress) since at least 09-24; no cascade today. Conv logs empty 36th+ day.
- 09-30 update (no 09-29 repo-memory entry; see cache-memory history.json for that day's `success_channel_isolation` experimental run): true_agentic streak broke after 2 days but only on merge-noise (adjusted 100%); provenance back to 85.2% bot-driven (upper band edge); burst_vs_isolated a 3rd near-parity reading (1.01x), weakening the 3-8x law; new candidate pattern **push_supersede_cancellation** (rapid same-branch pushes cancelling in-flight gates with no merge/close event) — first observed instance, unconfirmed; duration jumped to 16.4min raw mean, highest since 09-23. Conv logs empty 38th+ day.

## 2026-10-01 update (cache-memory only; no repo-memory snapshot that day)
- 14.0% raw completion (7/50), -40.0pts vs 09-30; 3rd-lowest of 39 recorded days. **provenance_inversion NEW RECORD LOW: 28.6% bot-driven** (2/7, both code-scanning) -- shatters the prior low of 53.3% (09-20); true_agentic_streak itself held 100% (5/5 resolved) -- the CI-gate/review-bot channels collapsed, not true-agentic. branch_level_stuck_gate on copilot/fix-agentic-conversation-session-state (62% of volume): 28 core-CI-gate firings at 0% vs its true-agentic workflow 3/3=100%. burst_vs_isolated 11.4x, 2nd-highest on record. Orphan 0/22, 39th healthy day. Conv logs empty 39th+ day.

## 2026-10-02 snapshot
- 50 sessions; **40.0% raw completion** (20 success, 18 action_required, 8 failure, 1 cancelled, 3 in_progress), +26.0pts vs 10-01 (14.0%); 39-day mean 32.1% (range 4-78%) -- today ranks 13th of 40 recorded days.
- **provenance_inversion** snapped back to 85.0% bot-driven (17/20), upper edge of the 72-86% band, matching 09-30's 85.2% -- confirms 10-01's 28.6% record low was a one-off collapse, not a regime shift.
- **true_agentic channel** 3/4 resolved = 75% (+1 in_progress). The 1 non-success -- "Addressing comment on PR #64884" cancelled 04:54:39Z -- is **not** merge-triggered (PR #64884 merged 4m21s later at 04:59:00Z; last branch push was 04:13:42Z, 40+min prior) and doesn't fit any known cascade/push-supersede pattern; flagged as an unexplained one-off.
- **merge_invalidation_cascade x2**: (1) PR #64940 merge (copilot/fix-permissions-issue, 06:52:46Z) invalidated 8 runs 2s pre-merge, all `failure` (Design Decision Gate, Impeccable/Matt Pocock/Ponytail Skills Reviewers, PR Code Quality Reviewer, PR Data Prefetch, Stale Lock Files, Test Quality Sentinel) -- 2nd-largest cascade on record behind 09-24's 9-run event. (2) That same merge AND PR #64884's merge (04:59:00Z) each independently reproduced the established "merge -> Squad Implement Worker +~7s" sub-signature exactly (06:52:53Z / 04:59:07Z) -- 2 instances in one day, 2nd-highest daily multiplicity after 09-26's 3x.
- **burst_vs_isolated_success_gap**: isolated 63.6% (7/11) vs burst-fired 36.1% (13/36), 1.76x gap -- direction holds, mid-range magnitude (between the recent near-parity days and the 8-12x extremes).
- **branch_level_stuck_gate NOT bimodal** today: copilot/update-gh-aw-mcpg-and-gh-aw-firewall-versions (open PR #64917) shows CGO/CWI flipping between success and action_required across two separate firings, unlike the clean 0%/100% splits of 09-22/09-25/10-01.
- Branch concentration: fix-permissions-issue 40.0% (also hosting the 8-run cascade + five 22-23min duration outliers), update-gh-aw-mcpg-and-gh-aw-firewall-versions 24.0%, implement-claims-ledger-type 18.0%, dispatch-work-coordinator-implementation 12.0%, update-lock-configuration 4.0%, create-tla-plus-proof-ledger-types 2.0%. 6 branches, ~159min window (04:17:48-06:56:56Z).
- Orphans 0/28 open PRs (max gate_count/branch=4, both candidates already Copilot-assigned) -> 0% NORMAL, 40th consecutive healthy day; baseline 0.0% over 13 recorded prior days. Conv logs empty 40th+ day. Standard run (roll=91).

## Active patterns (continued)
- 10-01 update: provenance_inversion NEW RECORD LOW (28.6% bot-driven), shattering the 53.3% low (09-20); true-agentic streak held 100% through the collapse -- confirms the inversion is channel-specific (CI-gate/review-bot), not a true-agentic regression. burst_vs_isolated 2nd-highest gap on record (11.4x).
- 10-02 update: provenance_inversion reverted to 85.0% bot-driven (upper band), confirming 10-01's record low was a one-off; true_agentic 75% (1 unexplained, non-cascade cancellation); merge_invalidation_cascade fired twice independently, including a 2nd-largest-on-record 8-run instance and 2 instances of the "+7s Squad Implement Worker" sub-signature in a single day; branch_level_stuck_gate broke its recent bimodal streak (CGO/CWI mixed outcomes on open PR #64917). Conv logs empty 40th+ day.
