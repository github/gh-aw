# Workflow Health — 2026-11-12T04:30Z
- Compile: 365/365 executable `.md` workflows have `.lock.yml` (compile-validate.txt holds only an install line, so no compile result was available).
- Metrics: `metrics/latest.json` is partial (4.7h, 100 log runs). `failing-workflows.json` is `[]`. Reliability scores not computable.
- Live run sample: 1000 most recent runs, 2026-11-11T22:38Z to 2026-11-12T04:29Z (about 6h, not 7 days). 89 workflows, 34 with >=1 failure, 62 action_required.
- Material delta: NEW P1 tracker for Auditor (`Collect failure evidence` fails hourly; 5/5 recent main runs). Annotation: health discovery `time_budget`, checkpoint unchanged. Logs blocked (HTTP 403 DIFC).
- Still open, already tracked: CI spec-pin/release-alignment #76787 (P1); CGO govulncheck #76789 (P2); Matt Pocock Skills Reviewer #77009, #76949, #76912.
- CodeQL - Code Quality: 10 failures 2026-11-11T23:35Z to 23:56Z; success since 04:28Z (recovered, no action).
- Actions: created Auditor P1 tracker and the 2026-11-12 dashboard issue.
