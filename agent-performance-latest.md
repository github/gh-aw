# Agent Performance Analyzer - Latest Run (2026-11-12T12:45Z)

Data basis: complete 24h snapshot `metrics/daily/2026-11-08.json` (362 workflows, 511 executed, 444 ok = 86.9%; 354 safe outputs, 430 accepted / 0 rejected).
`metrics/latest.json` is partial (4.7h, null run/quality fields) and was not used for scoring.
Quality scores are null in every snapshot: success rate and safe-output acceptance are proxies. Output sampling was not performed this run.

## Bottom tier (executed >= 3, success < 100%)
- ai-moderator 1/16 (6.3%), avg 6513s, declared timeout 5 min. Cached logs show successes at 6.4-7.8 min (suspect: duration may include queue time).
- auditor 1/11 (9.1%), 0 tokens. Hourly `Collect failure evidence` failing (WHM P1 tracker). Logs blocked (403).
- code-scanning-fixer 2/3 (n too small).
- pr-specification-reviewer 31/41 (75.6%), avg 2642s vs 20 min timeout (suspect metric). No investigation budget or self-check found.
- design-decision-gate 31/35 (88.6%), 2.25M tokens. Trend 54% (10-25) -> 100% (11-03) -> 89% (11-08).
- mattpocock-skills-reviewer 29/31 (93.5%), 2.78M tokens, 60 comments. Inline comments 13 (10-25) -> 1 (11-03) -> 60 (11-08).
- pr-code-quality-reviewer 33/35 (94.3%). ponytail-reviewer 35/36 (97.2%), 24.4M tokens. test-quality-sentinel 40/41 (97.6%), 5.8M tokens.

## Verdicts
- Matt Pocock Skills Reviewer: REDESIGN (cost and output-volume swings). Not deprecate.
- Design Decision Gate: KEEP; trim token spend (64k/run).
- Impeccable Skills Reviewer: KEEP; path-gated to docs/** so 0-1 runs/day. Insufficient data.

## Other
- daily-go-test-parallelizer: 82.3M tokens over 9 runs (~9.1M/run). Largest resource outlier.
- metrics-collector.md:14-15 still `engine: id: codex` with no `model-provider` (re-verified).
- Open trackers reported by search for ai-moderator and metrics-collector failures (#77094, #77023). Integrity-filtered, so content/state unverified. No duplicate filed.

## Run log (condensed)
- 2026-09-30 to 2026-11-10: ranking deferred repeatedly (partial metrics snapshots).
- 2026-11-11: ranking from 11-08 complete snapshot.
- 2026-11-12: full ranking, bottom-tier audit, Pocock/Impeccable/DDG verdicts; weekly discussion created.
