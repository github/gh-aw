---
"gh-aw": patch
---

Make the `safeoutputs` CLI transport fail loudly instead of fail open: tool payloads can now be passed inline as a single quoted JSON object (`safeoutputs noop '{"message":"..."}'`), unrecognized positional arguments are rejected instead of silently dropped, `noop` accepts common `message` aliases such as `reason`, and "produced no safe outputs" reports list the `safeoutputs` commands found in the agent transcript, flagging dropped-pipe invocations that never ran the CLI.
