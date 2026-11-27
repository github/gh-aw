
> [!WARNING]
> **Excessive Tool Denials**: The Copilot SDK hit the max tool denial guardrail and stopped the session early (`{denial_count}/{threshold}`).

<details>
<summary><strong>Last denied request</strong></summary>

```text
{reason}
```

</details>

<details>
<summary><strong>Last 5 tool calls</strong></summary>

{recent_tool_calls_list}

</details>

This is a structured guardrail event (`guard.tool_denials_exceeded`) captured in `events.jsonl`.

<details>
<summary>How to fix this</summary>

The prompt attempted actions outside the workflow's allowed tools.

Update the workflow prompt and/or permissions so required actions are permitted:

```
The workflow {workflow_id} stopped because the Copilot SDK exceeded its tool denial threshold ({denial_count}/{threshold}).
Last denied request:
{reason}

Please update the workflow so the prompt only uses tools permitted by the workflow tool policy.
```

</details>
