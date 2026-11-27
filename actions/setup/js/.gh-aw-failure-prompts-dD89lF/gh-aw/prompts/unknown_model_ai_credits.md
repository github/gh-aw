> [!WARNING]
> **Unknown Model for AI Credits Pricing**: The agent failed because the requested model is not in the built-in AI credits pricing table and `max-ai-credits` is active. The AWF API proxy rejected the request with an HTTP 400 error.

This is a **configuration issue**, not a transient error — retrying will not help.

<details>
<summary>How to fix this</summary>

Choose one of the following options:

**Option 1 — Map the model to a known model using the `models` field:**

Use the `models` frontmatter field to provide an alias from your custom model name to a model that exists in the built-in pricing table:

```yaml
---
model: my-custom-model
max-ai-credits: 500
models:
  my-custom-model:
    model: gpt-4.1
---
```

**Option 2 — Add pricing for your model in the frontmatter:**

Use the `models.providers` field to supply per-token pricing for your custom model. Use the provider key that matches your engine (`github-copilot`, `anthropic`, `openai`, `google`):

```yaml
---
model: my-custom-model
max-ai-credits: 500
models:
  providers:
    openai:            # github-copilot | anthropic | openai | google
      models:
        my-custom-model:
          cost:
            input: "3.75e-06"      # $3.75 per million input tokens (required)
            output: "1.5e-05"      # $15.00 per million output tokens (required)
            cache_read: "3.75e-07" # $0.375 per million cached-read tokens (optional)
            cache_write: "4.5e-06" # $4.50 per million cache-write tokens (optional)
            reasoning: "1.5e-05"   # $15.00 per million reasoning tokens (optional, defaults to output price)
---
```

Use the provider key matching your engine: `github-copilot` (Copilot), `anthropic` (Claude), `openai` (Codex), or `google` (Gemini). Only `input` and `output` are required; the rest default to zero (or `output` for `reasoning`).

**Option 3 — Use a model already in the built-in pricing table:**

Switch to a model name that the AWF pricing system recognizes directly (e.g. `gpt-4.1`, `claude-sonnet-4-5`, `gemini-2.0-flash`).

</details>
