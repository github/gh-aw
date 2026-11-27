> [!WARNING]
> **Engine Max Runs Exceeded**: The {engine_label} engine hit the workflow max runs guardrail and could not continue.

This signal was detected from engine runtime logs.

**What to do next**
- Increase workflow `max-runs` if the task legitimately needs more model invocations.
- Reduce per-run model calls by simplifying prompts, limiting retries, or breaking work into smaller steps.
- Review the run logs to identify repeated loops or retries that consumed invocation budget unexpectedly.
