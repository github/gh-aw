---
"gh-aw": patch
---

Fix static GitHub agent enclaves when primary GitHub tools are disabled (`tools.github: false` with `enclaves[].agent.tools.github`). The compiler now emits the enclave-scoped GitHub guard policy together with the matching safeoutputs `write-sink` policy, probes enclave-only GitHub with the enclave identity, refreshes deferred `awf-enclave` CLI tool schemas after late backend registration, and documents finite-disclosure response-schema bit budgets in the generated prompt.
