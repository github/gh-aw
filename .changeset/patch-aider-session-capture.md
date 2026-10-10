---
"gh-aw": patch
---

Capture Aider provider replies without display formatting, preserve separate reasoning, structured refusals, partial replies and errors, and include observed exact token/cost accounting in a single terminal session result. Refusals do not produce duplicate assistant answers, and empty display warnings do not count as turns. Require Aider attribution for events interpreted by session, workflow, and guardrail consumers, including model snapshots and streamed content, while preserving opaque unknown extensions, and document historical and streaming evidence limits.
