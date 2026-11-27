> [!WARNING]
> **Model has no AI credits pricing**: The agent failed because model `{model_name}` is not in the built-in pricing table and no default fallback pricing is configured. The AWF API proxy rejected every inference request with HTTP 400.

This is a **configuration issue** — retrying will not help. The model must have pricing before the workflow can run.

<details>
<summary>How to fix this</summary>

**Option 1 — Add pricing in the workflow frontmatter:**

{pricing_snippet}

Use the provider key matching your engine: `github-copilot` (Copilot), `anthropic` (Claude), `openai` (Codex), or `google` (Gemini). Only `input` and `output` are required; the rest default to zero (or `output` for `reasoning`).

**Option 2 — Map the model to a known model using the `models` field:**

If `{model_name}` is an alias for a model already in the built-in pricing table, use the `models` frontmatter field to provide the mapping:

```yaml
models:
  {model_name_yaml_key}:
    - claude-sonnet-4-5
```

**Option 3 — Switch to a model already in the built-in pricing table:**

Replace `{model_name}` in the workflow frontmatter with a model name that the AWF pricing system recognizes (e.g. `claude-sonnet-4-5`, `gpt-4.1`, `gemini-2.0-flash`).

</details>
