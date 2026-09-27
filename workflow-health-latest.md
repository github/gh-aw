# Workflow Health — 2026-09-27T04:45Z

## 4th re-confirmation: same 3 root causes still unfixed on live tracker #63556 (not expired)
`#63556` (filed 2026-09-26, expires ~2026-09-27T04:47Z) still accurately describes all 3 defects.
Re-verified all 3 live today via fresh job logs, no merged fix PR found:
1. **avenger.md — `/usr/local/bin/npm` symlink bind-mount crash.** Run 36284834929
   (2026-09-27T01:12Z) failed with `Refusing to use symlink as bind mountpoint:
   /usr/local/bin/npm`. Line 42 still mounts the npm symlink directly. Two prior fix PRs
   (#57946, #58722) remain closed unmerged since 2026-09-05.
2. **metrics-collector.md — missing `model-provider: github`.** `engine:` block (lines 14-16)
   still only has `id: codex`, no `model-provider: github`, while `model: copilot/gpt-5.3-codex`
   is set.
3. **gpclean.md — hardcoded retired `gpt-5-codex`.** Line 61 still `model: openai/gpt-5-codex`.
   Run 36291452078 (2026-09-27T03:27Z) failed all 4 codex-harness retries with `Model
   'gpt-5-codex' is retired... Did you mean 'gpt-5.3-codex'?`.
Posted a re-confirmation comment on the still-open `#63556` (no new tracker needed — it has not
yet auto-expired).

## Correction / new finding: metrics-collector's failure is actually a redaction-crash, not model_not_supported_error
Line-by-line re-read of run 36289314556's agent job log shows codex itself **exited cleanly
(exitCode=0)**; `model_not_supported_error` there is only a benign fallback-metadata warning. The
job is marked `failure` by a **separate post-agent secret-redaction crash**:
`##[error]ERR_VALIDATION: Secret redaction failed: ... Failed to scan directory
/tmp/gh-aw/aw-mcp: EACCES: permission denied, scandir '/tmp/gh-aw/aw-mcp'`. This is the **same
`/tmp/gh-aw/aw-mcp` redaction-crash class** previously tracked only against
`daily-firewall-report` (there: `Maximum call stack size exceeded`, reconfirmed again today in run
36288817598, 2026-09-27T02:32Z). Confirmed on a 2nd, unrelated workflow with a different
sub-error signature on the same directory scan — elevates this from single-workflow-flaky to a
genuine cross-workflow AWF-firewall redaction defect, independent of and in addition to the
`model-provider` config fix (still separately needed for metrics-collector.md's fallback-metadata
warning). Recommend flagging this to AWF firewall maintainers as a distinct systemic item.

## failing-workflows.json re-verified live (4 entries)
- **daily-firewall-report**: still crashing on redaction (root-caused above, now confirmed
  cross-workflow); agent output itself healthy (discussion created successfully both times).
- **daily-go-test-parallelizer**: no fresh failure evidence this run beyond the pre-loaded
  single-run snapshot; unchanged assessment from prior runs (healthy).
- **lint-monster**: no fresh failure evidence this run beyond the pre-loaded single-run snapshot;
  unchanged assessment from prior runs (healthy).
- **cjs**: plain GH Actions CI workflow, out of `gh aw` scope — recent runs show `action_required`
  (approval gate) alternating with `success`, not failures.

## Compilation Status
298/298 workflows have lock files (100%), compile-validate clean.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s safe-outputs still lack `update-issue: target: '*'`, so this
schedule-triggered workflow cannot update/close existing issues directly; `add_comment` used again
this run for the tracker update. Deep-report follow-up issues #63656 (extend expires for P0/P1
trackers) and #63657 (scoped create-pull-request safe-output) remain open and unaddressed —
recommend prioritizing these to close the diagnose-but-never-convert-to-PR gap that has now
produced 4 re-confirmation cycles of the same 3 defects.

## Actions Taken This Run (2026-09-27)
- Re-verified all 3 root causes from #63556 via live job logs and current file contents — all
  still unfixed, tracker still open (not yet expired) — posted re-confirmation comment with fresh
  run evidence rather than filing a duplicate tracker.
- Discovered and documented a correction: metrics-collector.md's recent failures are actually the
  same `/tmp/gh-aw/aw-mcp` redaction-crash class as daily-firewall-report's, not a genuine
  model-resolution failure — elevates the redaction crash to a cross-workflow systemic issue.
- No dashboard issue created this run — no compilation/health-category shifts warranting a full
  refresh; captured via the tracker comment instead.

> Last updated: 2026-09-27T04:45Z
