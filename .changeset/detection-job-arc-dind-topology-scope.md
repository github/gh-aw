---
"gh-aw": patch
---

Apply `runner.topology: arc-dind` to the threat-detection job only when `safe-outputs.threat-detection.runs-on` selects a non GitHub-hosted runner. The detection job otherwise runs on a GitHub-hosted runner (`ubuntu-latest` by default), where the ARC/DinD codegen left `threat-detect` unstaged and wrote the detection result under a read-only mount that no later step read.
