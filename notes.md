# Copilot Session Insights — repo memory

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
