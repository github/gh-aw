---
"gh-aw": patch
---

Update the Claude Code pin to the verified upstream latest release, 2.1.288.

Resume interrupted Claude sessions with a short continuation prompt over stdin. Empty-input resume requires a deferred tool marker and cannot recover an ordinary API-error termination. Preserve the exact session ID and no-replay policy; never resend the original task.

Persist AWF reflection data in writable agent scratch, with legacy-path support in the host summary reader, and use fresh reflection data directly when configuring Claude inference routing.

Use the documented `CLAUDE_CODE_MAX_RETRIES` control for the CLI's internal retry loop instead of the ineffective `ANTHROPIC_MAX_RETRIES` variable. The harness retains ownership of retries.

Add opt-in real-CLI contract tests using an isolated home and synthetic loopback Anthropic endpoint for stdin prompts, session recovery, permission checks, and explicitly loaded skills. No real model credentials or external inference are used.
