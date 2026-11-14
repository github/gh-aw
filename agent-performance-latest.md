# Agent Performance Analyzer - Latest Run (2026-11-14)

Data basis: `metrics/latest.json` (collection_status complete, window 2026-11-13T02:31Z to 2026-11-14T02:31Z). 367 workflows, 164 active, 592 runs, 584 executed, 423 successful (72.4%). 406 safe outputs accepted, 0 rejected. 82.9M tokens.
Quality scores, engagement, PR merge rate, and cost are null in every snapshot. Success rate over executed runs and safe-output acceptance are the proxies. Output sampling not performed.

## Bottom tier (executed >= 3, by success rate)
- daily-go-test-parallelizer (codex) 0/9, 6.06M tokens. Blocked batch retried daily since 11-13. Filed prompt-improvement issue (this run).
- auditor (copilot) 2/21 (9.5%). Hourly "Collect failure evidence" failing. Logs HTTP 403, so the time_budget cause is unverified. Tracker #77641 is open; #77062 was closed not_planned.
- pr-specification-reviewer (copilot) 20/67 (29.9%). Seven open "Failed jobs" issues all trace to the non-agent `push_ledger_changes` job (missing artifact `gh-aw-ledger-transactions`). Existing tracker #77383. Ranking is largely downstream. No self-check or rubric in prompt; 20-min timeout vs 2120s average.
- ai-moderator (pi) 12/18 (66.7%). Timeout-vs-duration mismatch still unconfirmed.
- auto-triage-issues 3/4, code-scanning-fixer 3/4, aw-failure-investigator 4/5, pr-code-quality-reviewer 25/31 (80.6%), contribution-check 5/6. Sample sizes too small for conclusions.

## Top performers (executed >= 3)
- test-quality-sentinel 39/40 (97.5%), ponytail-reviewer 34/35 (97.1%), design-decision-gate 30/31 (96.8%), mattpocock-skills-reviewer 29/32 (90.6%), pr-sous-chef 105/116 (90.5%).

## Named reviewer verdicts
- Matt Pocock Skills Reviewer: REDESIGN (not deprecate). Cost and output volume swing (2.74M tokens and 59 outputs on 11-14; 1.5M and 48 on 11-13).
- Design Decision Gate: KEEP. Success 30/31. Tokens fell from 2.25M (11-08) to 418K (11-14). Trim check only.
- Impeccable Skills Reviewer: KEEP. Path-gated to docs/**, 1 run in window, 105K tokens. Insufficient data.

## Engine mix (executed-weighted)
- copilot 181/287 (63%), pi 157/179 (87.7%), codex 54/77 (70.1%, 65.6M tokens), claude 29/38 (76.3%).
- Copilot excluding auditor and pr-spec: 159/199 (79.8%).

## Corrections to prior record
- #77094 is a Tidy timeout tracker, closed not_planned 2026-11-12. Not an ai-moderator tracker.
- #77023 closed not_planned 2026-11-12. Metrics-collector model-provider gap is still present (metrics-collector.md:14-15).
- #77069 (DGTP parent) closed not_planned 2026-11-14 while DGTP is still 0/9.

## Open items
- metrics-collector.md still lacks `model-provider` under engine.
- Command-gated workflows (pr-spec, DDG, Pocock, Ponytail, PCQ, TQS, ai-moderator): activated-run success not separated in metrics. Scored on executed runs as a proxy.
- Auditor root cause unverified (logs 403).

## Run log (condensed)
- 2026-11-11: ranking from 11-08 snapshot.
- 2026-11-12: full ranking, Pocock/Impeccable/DDG verdicts.
- 2026-11-14: ranking from complete latest.json. Verified stale tracker claims. Filed DGTP prompt-improvement issue. pr-spec attributed to downstream ledger job (#77383).
