# Copilot Session Insights — repo memory

## 2026-09-13 snapshot
- 50 sessions; **16.0% raw completion** (8 success, 16 failure, 26 action_required); 21-day mean (cache-memory) 29.7%. **Cascade-adjusted completion 23.5%** (8/34) once the merge-invalidation artifacts below are excluded.
- **merge_invalidation_cascade — largest instance recorded (6th occurrence)**: PR #60561 merged at 05:18:27Z invalidated all 16 of today's failures on `copilot/fix-github-actions-job-failure` (incl. 8 reviewer-bot workflows firing/failing in a 4s window), 4x the prior peak of 4 runs. Verified via `gh pr list`. Excluding these, that branch converts 2 success/1 action_required = 66.7% (healthy).
- True-agentic completions (2x "Running Copilot cloud agent") = 2/2 = 100%, 7th consecutive 100% day. provenance_inversion holds (6/8 successes = bot-driven, 75%, upper edge of historical range).
- burst_clustering_temporal_density signal has collapsed to near-parity (isolated 16.7% vs burst 15.9%, ~1.05x) — cascade artifacts are almost all burst-classified and now dominate the bucket; needs cascade exclusion to stay useful.
- Orphans 0/3 open PRs (all Copilot-assigned + human reviewer requested) → 0% NORMAL, 22nd+ consecutive healthy day. Conv logs empty (22nd+ day). Standard run (roll=53).
- _Note: this repo-memory branch had not been updated since run 28925210910 (~07-08); cache-memory (`/tmp/gh-aw/cache-memory/session-analysis/history.json`) has been the continuously-updated source of truth in the interim (22 entries through today) and remains authoritative for the full daily series._

## 2026-07-08 snapshot
- 50 sessions; **8% completion** (4 success, 41 action_required, 1 cancelled, 4 in_progress) — floor regime; saw-tooth pullback from 07-07 (18%). Trailing: 20→8→4→**54**→16→8→18→**8**. 30d-mean ~13%.
- **provenance_inversion holds**: all 4 successes = "Addressing comment on PR" agentic runs (13–23m, on cache-checkout-visibility #44224 ×2, specify-checkout #44225, fix-threat-detection #44202); 0/4 core CI gates. 45 gate stubs 0-dur. Reverts the 07-04 flip; consistent with the floor+inversion regime.
- 9/50 non-zero; exec mean 8.85m / median 6.45m / max 23.28m (Addressing PR#44225). Overall mean 1.59m, median 0m. 44-min window (06:37–07:21Z). 8 copilot/* branches; gate footprint top: allow-memory-deterministic-job 12, add-runtime-token-check 9, fix-threat-detection 9.
- Orphans 0/13 (all 13 open PRs Copilot-assigned; max 1 gate/copilot-branch, main=3) → 0% NORMAL, ~42nd healthy day. Conv logs empty (41st day).
- **EXPERIMENTAL (roll=26): Agentic Work-Time Concentration (AWTC)** — 99% of 79.7 wall-clock-min in 7 agentic comment-addressing runs; 45 gate stubs = 0.28m combined. CARM sub-metric: PR#44224 fired 3× (2 succ + 1 cancelled = retry churn), #44225 2×. Separates "compute spent" from "runs counted." Effectiveness High; recommend Refine.

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
- conversation_log_fetch_failure: 35th+ day (longest unresolved risk; behavioral/loop/context analysis unavailable — metrics are CI/infra metadata only).
- gate_footprint_refire_signature (06-20): refire ratio = runs/distinct-workflows distinguishes broad CI from narrow re-fire.
- per_branch_gate_fanout (06-26): each PR-open fires a gate bundle; gate_count = f(PR-open), not branch health. **Refined 07-03 (GBCD):** bundle composition is change-TYPE-adaptive — code-change branches fire full 4/4 core CI gates, lint/doc branches fire 0/4 (lightweight agentic wf only), spec branches fire 2/4 + moderation. "~8-workflow uniform bundle" was an over-generalization from a code-heavy snapshot.
- gate_bundle_composition_divergence (07-03, experimental): fraction of open branches deviating from the full core CI gate set; 62.5% (5/8) diverged today. Distinguishes deterministic CI overhead from change-type-specific triggers.
