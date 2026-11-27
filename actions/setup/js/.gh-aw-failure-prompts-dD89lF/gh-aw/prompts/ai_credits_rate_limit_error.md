> [!WARNING]
> **AI Credits Budget Exceeded**
>
> The workflow hit the configured `max-ai-credits` guardrail.{metrics_summary}

<details>
<summary>Increase the limit</summary>

Update `max-ai-credits` in your workflow frontmatter:

```yaml
max-ai-credits: {suggested_credits}
```

</details>

<details>
<summary>Tips for reducing AI credit usage</summary>

- Review the [cost optimization guidance](https://github.github.com/gh-aw/reference/cost-management/).
- Reduce unnecessary model or tool calls in the prompt.
- Trim large inputs or excess context that does not change the outcome.
- Split large tasks across smaller runs when possible.

</details>
