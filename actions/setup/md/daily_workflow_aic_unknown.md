> [!WARNING]
> **Daily Workflow AI Credits Could Not Be Verified**: The daily guardrail could not prove complete AI Credits accounting for earlier workflow runs, so the daily AI Credits limit was not enforced for this run.

**Guardrail status:** `{status}`

**Reason:** {error}

The guardrail could not verify the accounting window either because a billable component's usage artifact is missing, empty, or unreadable, or because the guardrail could not reach the GitHub API to verify prior runs. The detailed reason above identifies the specific cause. The guardrail fails open: downstream work continued while accounting was unavailable.

{guidance}
