> [!WARNING]
> **Model Not Supported**: The requested model was rejected by the provider for this request.

This may be an invalid model name, a policy or access restriction for this token/repository, or a change in provider availability. Retrying the same session will not resolve a model that remains unavailable.

<details>
<summary>How to fix this</summary>

Specify a valid model for the selected engine in the workflow frontmatter:

```yaml
---
engine: copilot
model: gpt-5-mini
---
```

To find valid models, check your engine/provider documentation (for Copilot see [supported models](https://docs.github.com/en/copilot/using-github-copilot/using-github-copilot-in-the-command-line#supported-models)) and the model catalog returned for the failing token/context.

If the model previously worked with the same workflow and token type, compare the successful and failing runs' model catalogs and request context. If the catalog changed, share sanitized timestamps, repository/organization context, and Copilot service request IDs with the Copilot model-access support team. Do not share tokens.

If the error text is `No model available. Check policy enablement under GitHub Settings > Copilot`, the model is not disabled in the workflow but by Copilot policy. Enable the model under **GitHub Settings > Copilot > Policies** for the org/repo, or pick a model that is already enabled. This can also be triggered by a subagent (`task` tool) dispatch requesting a model that the policy does not allow, even when the main agent's model is enabled.

</details>
