# ADR-63493: Make safeoutputs CLI the only transport for pi

**Date**: 2026-09-25
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR evidence shows that gh-aw currently injects safe-output prompt fragments with wording that treats the `safeoutputs` CLI as an optional alternative to direct tool calls. The linked issue analysis and the PR changes show this is incorrect for `engine: pi`, because pi advertises `EngineCapabilities.MCP: false` and therefore has no native MCP tool-call interface. The implementation changes span prompt fragment templates, new engine-capability-aware prompt rendering logic, wiring into prompt composition, tests, and recompiled workflow lock files. The decision is how gh-aw should describe and generate safe-output transport instructions for engines that cannot call MCP tools directly.

### Decision

We will make safe-output transport wording depend on the engine's MCP capability and explicitly state that the `safeoutputs` CLI is the only transport for `engine: pi`. We will add compile-time prompt substitutions and capability-aware prompt rendering so MCP-capable engines keep the existing direct-tool framing while MCP-incapable engines receive unambiguous CLI-only instructions. We chose this because the PR and linked issue show that optional-language wording leads pi agents to search for nonexistent direct commands and finish without emitting required safe outputs.

### Alternatives Considered

#### Alternative 1: Keep uniform prompt wording for all engines

This was a realistic option because it avoids branching in prompt generation and keeps the safe-output instructions simpler to maintain. It was not chosen because the linked issue and PR description show the shared wording is materially false for pi, whose lack of MCP support makes the CLI mandatory rather than optional.

#### Alternative 2: Fix only pi prompt delivery or compaction behavior

This was a plausible option because the linked issue identifies compaction and prompt placement as contributing factors for long pi runs. It was not chosen for this PR because the immediate bug is the incorrect transport contract itself, and the PR description explicitly leaves compaction-exempt prompt delivery as follow-up work.

### Consequences

#### Positive
- pi workflows get transport instructions that match actual engine capabilities, reducing failed runs that never emit a safe output.
- MCP-capable engines retain their existing direct-tool guidance, so the fix is targeted rather than disruptive.
- Prompt-composition tests and lock-file assertions now verify that pi workflows contain CLI-only safe-output wording.

#### Negative
- Prompt generation becomes more complex because safe-output text now depends on per-engine capability lookup and placeholder substitution.
- Wording differences across engines increase maintenance burden for prompt fragments, tests, and compiled workflow outputs.

#### Neutral
- Recompiled lock files change across affected workflows even though the behavioral code path is limited to prompt wording.
- The PR does not resolve separate pi issues around compaction-exempt prompt delivery or model context-window defaults.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
