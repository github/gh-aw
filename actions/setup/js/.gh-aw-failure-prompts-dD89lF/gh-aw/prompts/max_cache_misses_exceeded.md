> [!WARNING]
> **Engine Cache Miss Limit Exceeded**: The {engine_label} engine hit the gh-aw API proxy consecutive cache miss guardrail and could not complete this run.

This signal was detected from engine runtime or AWF API proxy logs.

<details>
<summary>What caused this</summary>

The gh-aw API proxy enforces a per-run `apiProxy.maxCacheMisses` guardrail. When too many back-to-back requests bypass the prompt cache, the proxy returns a 403 `max_cache_misses_exceeded` error and the engine terminates.

Common causes:

- Dynamic or frequently changing content in the prompt (e.g. timestamps, run IDs, random values)
- Prompts that are too short for the provider's cache threshold
- High concurrency across workflow runs that share the same cache slot
- A burst of runs before the cache warms up after a deployment or key rotation

</details>

<details>
<summary>How to remediate</summary>

- **Wait and retry**: Cache misses are often transient. Re-running the workflow after a few minutes is usually sufficient.
- **Stabilize the prompt**: Move volatile values (current date, run ID, etc.) out of the system prompt or early turns and into later user turns so the cacheable prefix stays constant.
- **Reduce run frequency**: If the workflow fires too often, the cache slot may be evicted before reuse. Consider scheduling less aggressively or adding a concurrency group.
- **Review `max-daily-ai-credits`**: A very high run volume can exhaust per-key cache capacity. Check your usage and apply a daily cap if needed.

</details>
