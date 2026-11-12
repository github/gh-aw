---
"gh-aw": patch
---

Enable Atomic's default and bundled-extension tools in the experimental Atomic sample through an explicit allowlist, including workflows, subagents, Intercom, web access, and native MCP; the interactive `ask_user_question` is not listed. Run the Atomic smoke workflow every two days as well as on manual dispatch, and give the Atomic smoke and conformance workflows a one-day npm release-age cooldown so a new pin can be validated the day after it ships.
