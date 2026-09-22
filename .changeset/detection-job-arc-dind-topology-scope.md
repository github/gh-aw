---
"gh-aw": patch
---

Apply `runner.topology: arc-dind` to the threat-detection job only when the job declares its own runner through `safe-outputs.threat-detection.runs-on`. The detection job otherwise runs on the GitHub-hosted `ubuntu-latest` runner, where the ARC/DinD codegen left `threat-detect` unstaged and wrote the detection result under a read-only mount that no later step read.
