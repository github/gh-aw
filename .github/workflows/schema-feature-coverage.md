---
private: true
emoji: "📊"
description: Ensures 100% schema feature coverage across existing agentic workflows by creating PRs for any uncovered fields
on:
  schedule: weekly on monday around 07:00
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
  issues: read


engine: codex
model: copilot/gpt-5.3-codex
strict: true
network:
  allowed:
    - defaults
    - github
imports:
  - shared/otlp.md
  - shared/reporting.md
tools:
  cli-proxy: true
  bash: ["*"]
  edit:
  github:
    mode: remote
    toolsets: [default]
safe-outputs:
  create-pull-request:
    title-prefix: "[schema-coverage] "
    expires: 7d
    labels: [automation, schema-coverage]
    max: 10
timeout-minutes: 30
checkout:
  - fetch-depth: 1
    current: true
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Schema Feature Coverage Checker

You are responsible for ensuring **100% coverage** of schema features across the existing agentic workflows in this repository. Every top-level property defined in the main JSON schema should appear in at least one workflow file under `.github/workflows/` (including `shared/` subdirectories) or in the `schema-demos/` directory.

## Step 1: Extract All Schema Fields (Deterministic)

Run this exact command to obtain the complete sorted list of top-level properties:

```bash
jq -r '.properties | keys[]' pkg/parser/schemas/main_workflow_schema.json | sort
```

Save the output as your canonical field list.

## Step 2: Check Coverage Across All Workflows

For each field in the list, check whether it appears as a top-level YAML key in any `.md` workflow file under `.github/workflows/` or `schema-demos/`:

```bash
for field in $(jq -r '.properties | keys[]' pkg/parser/schemas/main_workflow_schema.json | sort); do
    count=$(grep -rl "^${field}:" .github/workflows/ schema-demos/ --include="*.md" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$count" = "0" ]; then
        echo "UNCOVERED: $field"
    else
        echo "COVERED ($count): $field"
    fi
done
```

Collect the full list of **UNCOVERED** fields.

## Step 3: Decide on Action

**If all fields are covered**: Call `noop` immediately with a brief summary and exit.

```json
{"noop": {"message": "All schema fields are covered across .github/workflows/**/*.md and schema-demos/**/*.md — no action needed."}}
```

**If there are uncovered fields**: Proceed to Step 4.

## Step 4: Create Pull Requests for Uncovered Fields

For each uncovered field (process up to 10 per run; subsequent weekly runs will handle any remaining ones):

1. **Create a new minimal demo workflow file** at `schema-demos/schema-demo-<field-name>.md`  
   (use the field name with any special characters replaced by hyphens, e.g., `disable-model-invocation` → `schema-demo-disable-model-invocation.md`)
2. **Include only the required minimum frontmatter** needed to compile (`description`, `on`, `permissions`, `engine`, `timeout-minutes`), plus the target field with a valid value
3. **Extract the field's description from the schema** using:
   ```bash
   jq -r '.properties["<FIELD>"].description // .properties["<FIELD>"] | if type == "string" then . else "See schema for details" end' pkg/parser/schemas/main_workflow_schema.json
   ```
4. **Write a brief markdown body** explaining what the field does (use the description from the schema)
5. **Call `create_pull_request`** with an informative title and body

### Template for Demo Workflow Files

```yaml
---
description: Demonstrates the `<FIELD>` schema field
on:
  workflow_dispatch:
permissions:
  contents: read
engine: codex
<FIELD>: <VALID_VALUE>
timeout-minutes: 5
---

# Schema Demo: `<FIELD>`

This workflow was auto-generated to demonstrate usage of the `<FIELD>` field in the
gh-aw frontmatter schema. It exists solely to achieve 100% schema feature coverage.

## What `<FIELD>` Does

<One-sentence description from the schema>

## Task

Call `noop` — this is a coverage-only demo workflow.

**Important**: Always call the `noop` safe-output tool.

```json
{"noop": {"message": "Coverage demo for `<FIELD>` — no action needed."}}
```
```

### Field-Specific Guidance

Use the following valid values for each field that may be uncovered. Only create a
demo file for fields that are **actually uncovered** (count = 0 in Step 2).

| Field | Valid Demo Value | Notes |
|---|---|---|
| `bots` | `["dependabot[bot]"]` | Array of bot identifiers |
| `command` | `"schema-demo"` | Command name string |
| `container` | `{image: "ubuntu:latest"}` | Standard GitHub Actions container |
| `disable-model-invocation` | `false` | Boolean — keep false so the workflow can still run |
| `environment` | `"production"` | GitHub Actions environment name |
| `github-app` | See schema docs for object structure | Requires an App to be configured |
| `infer` | `false` | Deprecated boolean (kept for compatibility) |
| `labels` | `["automation", "demo"]` | Array of workflow label strings |
| `metadata` | `{author: "schema-coverage", version: "1.0.0"}` | Custom key-value pairs |
| `plugins` | `["github/example-plugin"]` | Experimental — note in PR body |
| `post-steps` | `[{name: "Post-step", run: "echo done"}]` | Array of GitHub Actions steps |
| `private` | `true` | Boolean — marks workflow as non-shareable |
| `run-name` | `"Schema Demo #${{ github.run_number }}"` | Custom run name string |
| `secret-masking` | `{steps: [{name: "Mask", run: "echo masked"}]}` | Secret masking config |
| `secrets` | `{MY_SECRET: {required: false}}` | Secrets configuration |
| `services` | `{redis: {image: "redis:7"}}` | Docker services |
| `resources` | `[{url: "https://example.com/resource.md"}]` | Additional resource URLs |
| `mcp-servers` | `{my-server: {command: "npx", args: ["-y", "@modelcontextprotocol/server-memory"]}}` | MCP server config object |
| `mcp-scripts` | `{my-script: {steps: [{name: "Run", run: "echo hello"}]}}` | MCP script definitions |

### PR Creation Call

After creating (or editing) the demo file for a field, call:

```json
{
  "create_pull_request": {
    "title": "feat: Add schema coverage demo for `<FIELD>` field",
    "body": "## Schema Coverage Demo\n\nThis PR adds a minimal demo workflow that demonstrates usage of the `<FIELD>` field in the gh-aw frontmatter schema.\n\n**Why**: The schema feature coverage checker found that `<FIELD>` was not used in any existing workflow.\n\n**What**: Adds `schema-demos/schema-demo-<FIELD>.md` with a valid, minimal demonstration of this field.\n\n### Field Description\n\n<description extracted from schema using jq>"
  }
}
```

## Important Notes

- **One PR per uncovered field** — make each PR distinct and focused
- **Keep demo workflows minimal and valid** — only include the required `on`, `permissions`, `engine`, `timeout-minutes` fields plus the target field
- **Do not modify existing workflow files** — only create new `schema-demos/schema-demo-*.md` files
- **Validate field values** against the schema description before creating the file
- **If a field requires complex external setup** (e.g., `github-app`, `environment` with a specific name), note this clearly in the PR body and use a placeholder value
- **Up to 10 PRs per run** — if more than 10 fields are uncovered, handle the first 10 alphabetically; subsequent weekly runs will pick up the rest

## Success Criteria

A successful run will:
- ✅ Extract all schema fields using `jq` deterministically
- ✅ Check all `.github/workflows/**/*.md` and `schema-demos/**/*.md` files for field usage using `grep`
- ✅ Either confirm full coverage (call `noop`) OR create PRs for uncovered fields
- ✅ Each PR adds exactly one new demo workflow demonstrating one uncovered field
- ✅ All created demo workflows have valid frontmatter

**Important**: You MUST always call either `noop` or `create_pull_request` — failing to call any safe-output tool is the most common cause of safe-output workflow failures.

```json
{"noop": {"message": "No action needed: [brief explanation]"}}
```