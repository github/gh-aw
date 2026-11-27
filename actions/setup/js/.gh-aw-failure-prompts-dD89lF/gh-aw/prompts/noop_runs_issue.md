This issue tracks runs from agentic workflows that emit only no-op outputs. Comments distinguish agent-reported no-action outcomes from incomplete runs using structured run status, without publishing agent messages.

<details>
<summary>What is a No-Op?</summary>

A no-op (no operation) occurs when an agentic workflow runs successfully but determines that no action is required. For example:
- A security scanner that finds no issues
- An update checker that finds nothing to update
- A monitoring workflow that finds everything is healthy

A successful agent run that emits only no-op outputs is categorized as `reported_no_action`. This is an agent report, not independent proof of task completion: missing data or blocked publication cannot be inferred from arbitrary agent prose. A failed agent run that emits only no-op outputs is categorized as `incomplete`, not a successful no-op or a presumed transient error. Where completion evidence is unavailable, the outcome remains unconfirmed.

</details>

<details>
<summary>How This Helps</summary>

This issue helps you:
- Track workflows that ran but determined no action was needed
- Distinguish between failures and intentional no-ops
- Monitor workflow health by seeing when workflows decide not to act

</details>

<details>
<summary>Resources</summary>

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)

</details>

<details>
<summary>How to disable no-op reporting for a workflow</summary>

To stop a workflow from posting here, set `report-as-issue: false` in its frontmatter:

```yaml
safe-outputs:
  noop:
    report-as-issue: false
```

</details>

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.
> 
> **No action to take** - Do not assign to an agent.

<!-- gh-aw-noop-runs -->
