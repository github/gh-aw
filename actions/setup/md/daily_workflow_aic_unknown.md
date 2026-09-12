> [!WARNING]
> **Daily Workflow AI Credits Could Not Be Verified**: The agent was not started because the daily guardrail could not prove complete AI Credits accounting for earlier workflow runs.

**Guardrail status:** `{status}`

**Reason:** {error}

The guardrail fails closed when a billable component ran but its usage artifact has no valid accounting record. The detailed reason identifies the affected run, component, job attempt, and whether each expected accounting file was missing, empty, or unreadable.

Inspect the affected prior run and its component logs. Once that run leaves the rolling 24-hour window, the guardrail can evaluate the remaining runs normally.
