# Workflow Health — 2026-09-17T04:39Z

## NEW P0: cloud-hypervisor "--version exited with code undefined" (untracked until now)
29 open issues match this exact signature as of this run, first seen 2026-09-15T22:25Z (#61219),
sharply accelerating overnight 2026-09-16→17 (18 of 29 in the last ~8h). Engine-agnostic — confirmed
across copilot, codex, and claude engines. Root cause: `awf-config.json` embeds
`"cloudHypervisor":{"previewEnabled":true,...}` — the preview sandbox runtime's `cloud-hypervisor
--version` preflight is failing before the agent step starts, then a downstream
`model_not_supported_error` output flag gets set as a **misleading side effect** — NOT a genuine
model-resolution failure. Filed consolidated tracker (see dashboard issue for details) — no
prior tracker existed; 29 separate per-workflow `[aw] <name> failed` issues had been filed
independently. **DO NOT re-file per-workflow issues for this signature.**

## Codex gpt-5.3-codex model_not_supported_error — still unresolved but now entangled
Prior tracker #61030 self-expired again (`expires: 1d`, 5th cycle) on 2026-09-16T04:45Z, per
Dashboard #61270. `CodexDefaultModel = "gpt-5.4"` still doesn't match the 74 workflow files
hardcoding `gpt-5.3-codex`. **Important correction this run:** some recent occurrences attributed
to this signature (e.g. today's LintMonster failure) are actually the NEW cloud-hypervisor crash
above, not genuine model-resolution failures — re-triage the 74-file backlog only after the
hypervisor incident is resolved to avoid further misattribution.

## failing-workflows.json triage (all 4 entries)
- **daily-firewall-report**: latest failure (#61492, 2026-09-17T02:38Z) is the new hypervisor
  crash, not a workflow-specific bug.
- **lint-monster**: latest failure (#61500) is also the hypervisor crash (confirmed from job log).
- **daily-go-test-parallelizer**: mostly healthy — 4/5 most recent runs successful; has an active
  WIP tracker #61525. No new issue filed.
- **cjs**: plain `.github/workflows/cjs.yml` GH Actions workflow, out of `gh aw` scope (established
  in 2026-09-10 run). No action.

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Actions Taken This Run (2026-09-17)
- Created new consolidated P0 issue for the cloud-hypervisor "--version exited with code undefined"
  regression (29 affected open issues across 3 engines) — first ecosystem-wide tracker for this
  incident.
- Created "Workflow Health Dashboard - 2026-09-17" issue with full findings.
- Did not re-file any of the 29 individual per-workflow occurrences, nor the pre-existing codex
  model P0 (#61030, still closed/expired) — deferred to new tracker.

> Last updated: 2026-09-17T04:39Z
