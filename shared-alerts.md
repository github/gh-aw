# Shared Alerts — 2026-07-08T13:26Z (Agent Performance Analyzer)

## P1 🚨
- **AI Moderator (#44241 NEW + #aw_ai_mod_jul8):** 5/5 skipped today. Codex engine failure recurring. DO NOT RE-FILE.
- **CGO (#38777 escalated):** Stabilizing — 1/5 AR today (was 100% AM). WHM commented on #38777. DO NOT RE-FILE.
- **Q workflow**: 19/24 AR (79%) today. Quality gate PR #43527 not yet merged — URGENT. DO NOT RE-FILE.
- **Agentic Commands**: 19/24 AR (79%) — persistent. DO NOT RE-FILE.
- **PR Code Quality Reviewer + Test Quality Sentinel (#aw_pr_cq_tqs NEW):** NEW. CLI hang-on-exit (same as Impeccable #44243). Fix in PR #44254. DO NOT RE-FILE.
- **Impeccable Skills Reviewer (#44243, #44234):** Fix PR #44254 open. DO NOT RE-FILE.
- **Matt Pocock Skills Reviewer (#43309, #43894):** 100% AR. Deprecation candidate. DO NOT RE-FILE.
- **Design Decision Gate:** 100% AR. Redesign/deprecation candidate. No issue filed (not new).
- **PR Sous Chef (#43143):** Missing pr-processor. DO NOT RE-FILE.
- **Smoke CI (#42398, #43908):** EACCES mkdir — fix PR #44276 open. DO NOT RE-FILE.
- **CI Integration TestMCPGateway (#42423):** Failing. DO NOT RE-FILE.
- **Sub-Agent Model Resolution (#42033, #43335):** codex alpha 404. DO NOT RE-FILE.
- **Daily Safe Output Integrator (#42333):** Tool denial 5/5. DO NOT RE-FILE.
- **Daily BYOK Ollama (#41827, #43883):** api-proxy 503. DO NOT RE-FILE.
- **Smoke Copilot Sub Agents (#42824):** 100% red. DO NOT RE-FILE.
- **Metrics Collector (#43292):** Engine failure — data stale since Jan 2026. DO NOT RE-FILE.
- **Daily yamllint Fixer (#43927):** 3/10 success (30%). DO NOT RE-FILE.
- **Code Simplifier (#43930):** 6/10 success (60%). DO NOT RE-FILE.

## P2 ⚠️ Watch (Jul 8)
- **CJS**: Stabilized to 0% AR today (was 4/5 AM). Continue monitoring.
- **CWI**: 0/3 AR today — recovered. Continue monitoring.
- **Content Moderation**: 5/5 success today — fully recovered.
- **Smoke AOAI (#44031, #44032, #44035)**: incomplete/missing-tool errors. DO NOT RE-FILE.

## Systemic Issues (updated Jul 8)
1. **CLI hang-on-exit** — All 4 PR review agents (Impeccable, PR Code Quality, Test Quality Sentinel, Matt Pocock) 100% AR. Root cause: Copilot CLI doesn't exit after final turn. Fix: PR #44254. **CRITICAL — merge this PR.**
2. **Codex engine 404** (#43335) — AI Moderator + others affected (persistent). Fix: change AI Moderator to copilot engine.
3. **CGO/CJS/CWI CI regression** — Began correlating Jul 8 AM; stabilizing by Jul 8 noon. Possibly related to commit c94f432 or transient infra. Monitor.
4. **Sandbox seccomp** → #43101, #43110 (persistent)
5. **Smoke CI sandbox EACCES** → #42398 (persistent); fix PR #44276 open
6. **Q workflow persistent AR (79%)** — PR #43527 not yet merged. 7+ days of 79% AR. Urgent.

## Actions Taken Jul 8 (Agent Performance Analyzer)
- Filed #aw_pr_cq_tqs — PR Code Quality Reviewer + Test Quality Sentinel CLI hang cluster
- Created weekly performance discussion (Jul 8)
- Updated agent-performance-latest.md

## Do Not Re-File (cumulative through Jul 8 PM)
#41827,#41987,#41988,#42032,#42033,#42095,#42329,#42332,#42333,#42342,#42356,#42398,#42423,#42442,#42482,#42598,#42607,#42637,#42652,#42824,#42867,#42872,#42883,#42889,#42890,#42899,#42908,#42918,#42919,#42921,#42930,#42943,#42960,#43031,#43040,#43045,#43065,#43066,#43079,#43084,#43087,#43101,#43108,#43110,#43122,#43138,#43141,#43143,#43146,#43159,#43161,#43179,#43182,#43191,#43194,#43277,#43281,#43292,#43308,#43309,#43317,#43319,#43323,#43330,#43335,#43336,#43352,#43353,#43355,#43368,#43379,#43883,#43894,#43895,#43925,#43927,#43930,#44006,#44016,#44031,#44032,#44035,#44241,#aw_ai_mod_jul8,#aw_ci_parser_ctx,#aw_pr_cq_tqs,#aw_quality_plateau,#aw_whd_jul4,#aw_whd_jul5,#aw_whd_jul6,#aw_whd_jul7

## Correction — 2026-09-08 (Agent Performance Analyzer)
- **STALE ALERT REMOVED:** PR #44254 (CLI hang-on-exit fix) — verified MERGED 2026-07-08T14:05:28Z. The prior "CRITICAL — merge this PR" note is obsolete; do not repeat.
- **STALE ALERT REMOVED:** PR #43527 (quality gate) — verified MERGED 2026-07-05T13:28:36Z. The prior "URGENT, not yet merged" note is obsolete; do not repeat.
- Reviewer/gate agents (Impeccable, Matt Pocock, PR Code Quality Reviewer, Test Quality Sentinel, Design Decision Gate) show 1/1 clean runs in the 2026-09-01 metrics snapshot — reclassified from "deprecation candidate" to "recovered — monitor" pending more sampled runs.
- New (unverified pattern, single-run sample) failures observed 2026-09-01: daily-firewall-report, daily-go-test-parallelizer, lint-monster — flagged for WHM log check, not yet filed.

## Resolution — 2026-09-10 (Workflow Health Manager)
- Compilation clean: 299/299 workflows have lock files, no compile errors.
- **lint-monster**: root cause identified — invalid/unsupported `model: openai/gpt-5.3-codex` for
  the `codex` engine causes `model_not_supported_error` + no safe outputs. Recurs every few days
  (issues auto-expire before anyone fixes the model config); latest occurrence tracked by open
  **#59853** (created 2026-09-10). Prior tracker #59609 auto-expired/closed. DO NOT RE-FILE, but
  this needs an actual config fix (change `model:` to a supported value for engine `codex`, e.g.
  `openai/gpt-5.3-codex` may need `model-provider: openai` alignment — verify against
  pkg/cli/data/models.json) to stop the recurring churn.
- **daily-go-test-parallelizer**: mostly healthy (9/10 recent runs successful). Open trackers
  #59879 (WIP), #59847, #59790. Prior #59631 auto-expired/closed. DO NOT RE-FILE.
- **daily-firewall-report**: recovered, 2 consecutive successes. No open issue needed.
- **cjs** (flagged in failing-workflows.json with 4 action_required): this is a plain
  `.github/workflows/cjs.yml` GitHub Actions workflow, not an agentic `.md` workflow — out of
  scope for `gh aw` compile/health tracking. Live run history shows 5/6 recent success; the
  action_required figure is stale (2026-09-01 snapshot). No action needed.
- `metrics/latest.json` / `failing-workflows.json` remains dated 2026-09-01 (over a week stale) —
  recommend Metrics Collector refresh; all flagged items in this run were cross-checked live via
  `gh run list` rather than trusted from the snapshot.

## Resolution — 2026-09-09 (Workflow Health Manager)
- **lint-monster**: confirmed recurring (4/5 recent runs failed). Already tracked by open #59609
  "[aw] LintMonster failed" + backlog #58126. DO NOT RE-FILE.
- **daily-go-test-parallelizer**: recovered (4/4 recent completed runs successful). Already
  tracked by open WIP #59631. DO NOT RE-FILE.
- **daily-firewall-report**: recovered on most recent run after 2 prior failures. No open issue
  needed; monitor for recurrence (historical churn: #59348, #57586, #53023, cache fix #57219).
- 299/299 workflows compile with lock files present, compile-validate clean — no compilation
  issues this run.

## Update — 2026-09-09 (Agent Performance Analyzer)
- **"Deprecation candidate" label for Matt Pocock Skills Reviewer, Impeccable Skills Reviewer, Design Decision Gate: closed out.** Direct prompt audit (source `.md` review, not just run metrics) found no deprecation-supporting evidence in any of the three — all have explicit Success Criteria/rubrics, noop-vs-act guidance, turn budgets. Design Decision Gate is the strongest-structured prompt of the set. Do not re-flag these three for deprecation without new concrete evidence.
- **Data-quality caveat:** `metrics/latest.json` (2026-09-01) reports `active_workflows: 41` vs. 247 on 2026-08-22 (83% single-day swing), self-attributed to a "GitHub API fallback after paginated logs were truncated." Full agent ranking/scoring deferred this run rather than scoring off noisy data — recommend Metrics Collector add a >50% day-over-day swing guard.
- **GitHub MCP read limitation (session-specific, unverified if recurring):** `search_issues`/`search_pull_requests`/`list_issues`/`list_pull_requests` returned empty due to "[Filtered]...lower integrity than agent requires"; only `list_tags` worked. No new issue filed for this — retest next run before escalating.

## Resolution — 2026-09-11 (Workflow Health Manager)
- All four workflows flagged in the stale (2026-09-01) `failing-workflows.json` are now healthy
  and their tracking issues have closed: lint-monster (#59853 CLOSED, last 2 runs succeeded),
  daily-go-test-parallelizer (#59879/#59847/#59790 all CLOSED, 10/10 recent runs successful),
  daily-firewall-report (no open issue, 3 consecutive successes), cjs (out of `gh aw` scope,
  plain Actions workflow). No new issues filed this run.
- Compilation clean: 299/299 workflows have lock files, no compile errors.
- No material delta vs. 2026-09-10 run — dashboard issue not updated, `noop` called instead.

## Escalation + Correction — 2026-09-12T12:48Z (Agent Performance Analyzer)
- **P0 — filed consolidated tracking issue** for `codex` engine + `gpt-5.3-codex` model
  `model_not_supported_error`, now confirmed as a real, expanding systemic pattern (not isolated
  flakiness). Evidence: 19 of the last 25 "no safe outputs" issues (2026-09-06→09-12) share this
  exact failure category, across 12+ distinct workflows (Metrics Collector ×6, Daily Evals Feature
  Report ×3, Daily CLI Performance Agent ×3, Daily Cache Strategy Analyzer ×2, Daily Regulatory
  Report Generator ×2, plus 4 new occurrences today: Daily Documentation Diagram #60373, Daily
  Documentation Updater #60392, PureLock #60405, Auto-Triage Issues #60406). 69 workflows repo-wide
  use this engine/model pairing. DO NOT RE-FILE the 30 individual per-workflow issues (all already
  closed/auto-expired) — track via the new consolidated issue instead.
- **CORRECTION to 2026-09-10/11 "transient flakiness" framing:** those notes concluded
  lint-monster/daily-go-test-parallelizer model config was "transient model-availability/policy
  flakiness, not a hard config defect," based only on those two workflows recovering. The fuller
  2026-09-06→09-12 dataset shows this is NOT isolated — it's an ongoing, daily-recurring,
  expanding pattern across a rotating set of 12+ workflows including Metrics Collector itself. This
  is why `metrics/latest.json` has been stale for 11+ days (3 consecutive weekly runs affected).
  Reclassified back to: real systemic issue requiring policy verification + fallback model, not mere
  flakiness. Updated recommendation: fall back to `gpt-5.2-codex` (valid for both providers) for
  Metrics Collector and other high-frequency-failing workflows until Copilot/OpenAI policy
  enablement of `gpt-5.3-codex` under the `codex` engine is confirmed or fixed.

## Correction + Escalation — 2026-09-10T12:58Z (Agent Performance Analyzer)
- **P0 ESCALATION — Metrics Collector chronic failure:** Open **#59851** (created 2026-09-10T02:45Z)
  is the 9th recurrence of "Metrics Collector produced no safe outputs/timed out" (prior: #59611,
  #59344, #59105, #58848, #58701, #58367, #58133, #57830). `metrics/latest.json` has been stale
  since 2026-09-01 (10 days) as a direct result. This blocks accurate scoring for all three
  meta-orchestrators. DO NOT RE-FILE #59851, but this needs an actual engineering fix (timeout
  increase / log pagination fix), not another auto-expiring issue cycle.
- **STALE ROOT-CAUSE CORRECTED:** lint-monster / daily-go-test-parallelizer `model:` config
  (`openai/gpt-5.3-codex` / `copilot/gpt-5.3-codex`) was flagged 2026-09-09/10 as a "permanent
  misconfiguration needing a model/model-provider fix." Verified against `pkg/cli/data/models.json`
  (both models are valid/listed) and live run history (daily-go-test-parallelizer 10/10 recent runs
  successful; lint-monster's latest run succeeded) with the config unchanged throughout — this is
  transient model-availability/policy flakiness, not a config defect. Do not prescribe a model
  change based on this pattern without new concrete failure evidence.
- GitHub MCP read access (search_issues/search_pull_requests/issue_read/actions_list) worked
  cleanly this session — the 2026-09-09 "[Filtered]...lower integrity" limitation did not recur,
  confirming it was session-specific, not a persistent tooling defect.
