# Workflow Health — 2026-09-26T04:44Z

## Re-confirmed (3rd cycle): same 3 root causes from #63098 still unfixed, still recurring daily
`#63098` auto-expired again 2026-09-25T06:56Z (`stateReason: NOT_PLANNED`, not a fix). Live job-log
inspection today reproduces all 3 signatures verbatim, no fix PR landed:
1. **avenger.md — `/usr/local/bin/npm` symlink bind-mount crash.** Run 36197039805
   (2026-09-25T22:34Z) failed with identical `Refusing to use symlink as bind mountpoint`. Line ~42
   of avenger.md still mounts the npm symlink directly. New occurrence issue #63518. Two prior fix
   PRs (#57946, #58722) remain closed unmerged since 2026-09-05; diff still applicable.
2. **metrics-collector.md — missing `model-provider: github`.** Run 36212246220
   (2026-09-26T02:39Z) set `model_not_supported_error` again. `engine:` block still only has
   `id: codex`, no `model-provider: github`. `metrics/latest.json` now stale 25+ days. New
   occurrence issue #63324.
3. **gpclean.md — hardcoded retired `gpt-5-codex`.** Run 36214703354 (2026-09-26T03:26Z) failed
   all 4 codex-harness retries with `Model 'gpt-5-codex' is retired... Did you mean
   'gpt-5.3-codex'?`. Line 61 still `model: openai/gpt-5-codex`. New occurrence issue #63547.
Filed a new consolidated issue this run with exact fix diffs for all 3, since #63098 self-expired
before anyone acted on it twice now.

## Re-confirmed: daily-fact mempalace race + daily-firewall-report redaction crash (before #63348 expiry)
#63348 (filed yesterday) was about to auto-expire (`expires: 1d`) at run time with both findings
still live:
- **daily-fact**: 17th consecutive failure, run 36147041356 (2026-09-26T14:22Z), same
  `dial tcp 172.17.0.1:8765: connect: connection refused` mempalace-startup-race signature.
  `shared/mcp/mempalace.md` still has no readiness probe.
- **daily-firewall-report**: 3rd consecutive occurrence, run 36211775357 (2026-09-26T02:54Z),
  `ERR_VALIDATION: ... Failed to scan directory /tmp/gh-aw/aw-mcp: Maximum call stack size
  exceeded` — traced to `actions/setup/js/redact_secrets.cjs`'s recursive `findFiles` walking a
  circular/deep symlink structure under `/tmp/gh-aw/aw-mcp`. Agent itself succeeds each time; only
  the post-run redaction step crashes.
Posted a re-confirmation comment on #63348 with fresh run evidence before its expiry window closed
(2026-09-26T04:46Z), so the next run knows self-closure ≠ resolution.

## failing-workflows.json re-verified live (4 entries)
- **daily-firewall-report**: still crashing on redaction (root-caused above); agent output itself
  healthy both days.
- **daily-go-test-parallelizer**: fully healthy (4/4 recent runs successful).
- **lint-monster**: fully healthy (5/5 most recent runs successful).
- **cjs**: plain GH Actions CI workflow, out of `gh aw` scope — unchanged assessment.

## Compilation Status
298/298 workflows have lock files (100%), compile-validate clean.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s safe-outputs still lack `update-issue: target: '*'`, so this
schedule-triggered workflow cannot update/close existing issues directly; `add_comment` used as
the update mechanism for #63348 this run.

## Actions Taken This Run (2026-09-26)
- Re-verified daily-fact and daily-firewall-report findings via live job logs; posted a
  re-confirmation comment on #63348 with fresh run links before its `expires: 1d` window closed.
- Re-verified all 3 root causes from #63098 (which self-expired again 2026-09-25) via live job
  logs and current file contents — all still unfixed. Filed one new consolidated P1 issue with
  exact 3-line fix diffs for avenger.md, metrics-collector.md, and gpclean.md.
- No dashboard issue created this run — no compilation/health-category shifts warranting a full
  refresh; captured via the comment + new issue instead.

> Last updated: 2026-09-26T04:44Z
