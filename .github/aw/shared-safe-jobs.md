---
description: Shared pattern for custom safe-output jobs that consume structured agent output.
---

# Custom Safe-Output Job Pattern

Use this pattern when a workflow must perform a controlled write action after the agent finishes.

## Rules

- Define the tool schema under `safe-outputs.jobs.<job-name>.inputs`.
- Read the output file from `GH_AW_AGENT_OUTPUT`.
- Parse the JSON and iterate over `items`.
- Filter items by `type`, where the type is the job name with dashes converted to underscores.
- Validate required fields on every matching item.
- Respect staged mode by checking `GH_AW_SAFE_OUTPUTS_STAGED === 'true'`.
- Preview in staged mode instead of performing the real side effect.
- Include a generated-by footer and workflow provenance annotations on published content so tracking is preserved. For GitHub issues, use the [stable issue-creation API](https://github.github.com/gh-aw/reference/custom-safe-outputs/#issue-creation-api).
- Use warnings for skippable invalid items and fail the job only for fatal errors.
- If a step reads an agent-written file outside the compiler's built-in known paths, declare it under `artifacts:` (array of `/tmp/gh-aw/`-rooted paths — files, directories, or globs). Undeclared paths are silently dropped and never reach the job.

## Minimal Shape

```yaml
safe-outputs:
  jobs:
    custom-action:
      description: "Process structured agent output"
      runs-on: ubuntu-latest
      inputs:
        field1:
          type: string
          required: true
      steps:
        - name: Process items
          uses: actions/github-script@v8
          with:
            script: |
              const fs = require('fs');
              const staged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === 'true';
              const file = process.env.GH_AW_AGENT_OUTPUT;
              const data = JSON.parse(fs.readFileSync(file, 'utf8'));
              const items = (data.items || []).filter(item => item.type === 'custom_action');
```

## Use This Pattern For

- third-party API writes
- notifications
- controlled external-system updates
- custom post-processing that should remain outside the agent job

## Do Not Use This Pattern For

- direct agent-job writes
- broad shell-based mutation without a typed schema
- features already covered by built-in safe outputs
