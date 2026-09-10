---
"gh-aw": patch
---

Fix an invalid empty server-level GitHub guard policy when GitHub tools are enabled only for an enclave agent (`tools.github: false` with `enclaves[].agent.tools.github`). The generated MCP Gateway configuration referenced `determine-automatic-lockdown` step outputs even though that step is not generated, so both guard values expanded to empty strings and mcpg exited during startup with `min-integrity is required`. The GitHub server now gets a server-level guard policy mirroring the enclave identity policy, and the lockdown step outputs are only referenced when the step is generated.
