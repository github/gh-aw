---
"gh-aw": patch
---

Enable every Atomic tool in the experimental Atomic sample, including workflows, subagents, Intercom, web access, native MCP, and tools from the repository's own `.atomic/` extensions; only the interactive `ask_user_question` is excluded. Load skills and prompt templates, and the repository's `.atomic/` extensions, workflows, skills, and settings, which gh-aw restores from the base branch before the agent starts. Run the Atomic smoke workflow every two days as well as on manual dispatch, and give the Atomic smoke and conformance workflows a one-day npm release-age cooldown so a new pin can be validated the day after it ships.
