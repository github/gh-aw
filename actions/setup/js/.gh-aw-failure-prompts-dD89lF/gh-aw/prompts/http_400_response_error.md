> [!WARNING]
> **HTTP 400 Bad Request from agentic engine**: The agent failed after the engine returned `Response status code does not indicate success: 400 (Bad Request)`.

This is usually a **request validation failure** rather than a timeout or quota issue.

<details>
<summary>How to debug this</summary>

1. Inspect the run's `agent-stdio.log` and `safeoutputs.jsonl` artifacts around the first HTTP 400.
2. Check for malformed request data, invalid tool-call payloads, or stale conversation state being replayed.
3. If the failure happened on a resumed session, rerun fresh once to confirm whether persisted state is corrupt.

</details>
