> [!WARNING]
> **Daily Workflow AI Credits Could Not Be Verified**: The agent was not started because the daily guardrail could not prove complete AI Credits accounting for earlier workflow runs.

**Guardrail status:** `{status}`  
**Reason:** {error}

The guardrail fails closed when a billable component ran but its usage artifact has no valid accounting record. This can happen when an inference request fails before the API proxy records usage, such as the detection request returning an HTTP error. The detailed reason identifies the affected run, component, job attempt, and whether each expected accounting file was missing or empty.

Inspect the affected prior run and its component logs. Once that run leaves the rolling 24-hour window, the guardrail can evaluate the remaining runs normally.
