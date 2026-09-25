# Workflow Health — 2026-09-25T04:38Z

## Re-confirmed: 3 root causes from #63098 still unfixed, still recurring daily
Live job-log inspection (not just metrics snapshot) confirms all 3 signatures reproduced verbatim
today, no fix PR landed despite Copilot-assignment attempt on 2026-09-24:
1. **avenger.md — `/usr/local/bin/npm` symlink bind-mount crash.** Run 36090952231
   (2026-09-25T03:35Z) failed with identical `Refusing to use symlink as bind mountpoint`. Line 42
   of avenger.md unchanged. 3 more auto-expiring failure issues today (#63337, #63260, #63242).
2. **metrics-collector.md — missing `model-provider: github`.** Run 36087037730
   (2026-09-25T02:42Z) set `model_not_supported_error` again. `engine:` block still only has
   `id: codex`, confirmed via direct file read. `metrics/latest.json` now stale at 23+ days.
3. **gpclean.md — hardcoded retired `gpt-5-codex`.** Run 36090234120 (2026-09-25T03:26Z) failed
   all 4 codex-harness retries with `Model 'gpt-5-codex' is retired... Did you mean
   'gpt-5.3-codex'?`. Line 61 still `model: openai/gpt-5-codex`.
Posted a re-confirmation comment on #63098 this run with exact run links and a recommendation to
prioritize a fix PR before a 3rd auto-expiry-without-fix cycle.

## NEW: daily-fact tracker (#62868) auto-expired without a fix — still 16/16 failing
`.github/workflows/shared/mcp/mempalace.md` unchanged (no wait-for-port/readiness logic added).
daily-fact failed its 16th consecutive scheduled run (2026-09-24T14:22Z, run 36012299234) with the
same `dial tcp 172.17.0.1:8765: connect: connection refused` mempalace-startup-race signature.
Filed as part of a new consolidated issue this run (see Actions Taken) since the prior tracker
self-expired via `expires: 1d` with `stateReason: NOT_PLANNED` (not resolved).

## NEW: daily-firewall-report secret-redaction stack-overflow crash (previously untracked)
Distinct from the "chart sub-agent model-access error" noted in prior runs (that's a soft,
non-fatal issue the agent already works around). This is a hard job-failure in the post-agent
redaction step, confirmed on 2 consecutive days:
- Run 36086384589 (2026-09-25T02:58Z) and run 35947503250 (2026-09-24T02:56Z): both show
  `##[error]ERR_VALIDATION: Secret redaction failed: ... Failed to scan directory
  /tmp/gh-aw/aw-mcp: Maximum call stack size exceeded` — even though the agent itself succeeded
  (discussion created, summary printed) both times. This is a **false-negative failure** masking a
  healthy run, likely a symlink loop/deep recursion under `/tmp/gh-aw/aw-mcp` that the AWF
  firewall's redaction directory-scanner doesn't guard against (infra-level, not this repo's
  `actions/setup/js/redact_secrets.cjs`, which doesn't reference `aw-mcp`). Filed in the same new
  consolidated issue this run.

## failing-workflows.json re-verified live (4 entries)
- **daily-firewall-report**: 2 consecutive failures — root-caused above as the redaction crash
  (not model-access as previously assumed); functionally healthy output both days.
- **daily-go-test-parallelizer**: fully healthy (5/5 recent runs successful).
- **lint-monster**: fully healthy (4/4 most recent runs successful).
- **cjs**: plain GH Actions CI workflow, out of `gh aw` scope — unchanged assessment.

## Compilation Status
298/298 workflows have lock files (100%), compile-validate clean.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s safe-outputs still lack `update-issue: target: '*'`, so this
schedule-triggered workflow cannot update/close existing issues directly; `add_comment` used as
the update mechanism for #63098 this run.

## Actions Taken This Run (2026-09-25)
- Re-verified all 3 root causes from yesterday's #63098 via live job logs — all still unfixed and
  recurring; posted a re-confirmation comment on #63098 with fresh run evidence.
- Root-caused daily-fact's #62868 tracker as auto-expired-without-fix (`stateReason: NOT_PLANNED`,
  not resolved) — mempalace.md unchanged, still 16/16 failing.
- Discovered and root-caused a new, previously-untracked daily-firewall-report crash signature
  (secret-redaction stack overflow) distinct from the previously-noted chart-generation issue.
- Filed one new consolidated P1 issue covering both new findings (daily-fact re-open +
  daily-firewall-report crash).
- No dashboard issue created this run — captured via the comment on #63098 and the new
  consolidated issue instead; no compilation/health-category shifts warranting a full refresh.

> Last updated: 2026-09-25T04:38Z
