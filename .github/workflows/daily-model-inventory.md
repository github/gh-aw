---
private: true
emoji: "📦"
name: Daily Model Inventory Checker
description: Queries model lists from OpenAI, Anthropic, and Google APIs daily, uses AWF /reflect for Copilot models, then analyzes the combined inventory to propose updates to the builtin model alias mapping
on:
  schedule:
    - cron: daily
  workflow_dispatch:

permissions:
  contents: read
  copilot-requests: write
  issues: read
  pull-requests: read

tracker-id: daily-model-inventory
engine:
  id: copilot
  copilot-sdk: true
  driver: .github/drivers/copilot_sdk_driver_sample_node.cjs
max-tool-denials: 3
strict: true
timeout-minutes: 30
network:
  allowed:
    - defaults
    - github
    - models.dev

# Supply pricing for a private/enterprise model not yet in the models.dev catalog.
# Cost values are per-token USD in scientific notation (e.g. 3e-06 = $3 per million tokens).
# Entries are merged with the built-in models.json at runtime and fill gaps for unknown models.
models:
  providers:
    openai:
      models:
        gpt-5-enterprise:
          cost:
            input: "3.75e-06"
            output: "1.5e-05"
            cache_read: "9.375e-07"
            cache_write: "3.75e-06"

jobs:
  collect_openai_models:
    runs-on: ubuntu-latest
    needs: [activation]
    permissions:
      contents: read
    steps:
      - name: Fetch OpenAI models
        id: fetch
        shell: bash
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
        run: |
          set -euo pipefail
          OUT="/tmp/gh-aw/agent/model-inventory/openai"
          mkdir -p "$OUT"
          if [ -z "${OPENAI_API_KEY:-}" ]; then
            echo '{"provider":"openai","error":"OPENAI_API_KEY not set","models":[]}' > "$OUT/models.json"
            echo '{"provider":"openai","error":"OPENAI_API_KEY not set"}' > "$OUT/raw.json"
            echo "status=skipped" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          # runner-guard:ignore RGS-012 -- the OpenAI API key is sent only to the official OpenAI models endpoint.
          HTTP_STATUS=$(curl -sf -o "$OUT/raw.json" -w "%{http_code}" \
            -H "Authorization: Bearer $OPENAI_API_KEY" \
            https://api.openai.com/v1/models) || true
          if [ "${HTTP_STATUS:-0}" = "200" ]; then
            jq '{
              provider: "openai",
              models: [
                .data[] | {
                  id,
                  owned_by,
                  created
                }
              ] | sort_by(.id)
            }' "$OUT/raw.json" > "$OUT/models.json"
            echo "status=ok" >> "$GITHUB_OUTPUT"
          else
            echo "{\"provider\":\"openai\",\"error\":\"HTTP $HTTP_STATUS\",\"models\":[]}" > "$OUT/models.json"
            echo "status=error" >> "$GITHUB_OUTPUT"
          fi

      - name: Log raw response
        if: always()
        shell: bash
        run: cat /tmp/gh-aw/agent/model-inventory/openai/raw.json 2>/dev/null || echo "(no raw.json)"

      - name: Upload OpenAI artifacts
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: openai-models
          path: |
            /tmp/gh-aw/agent/model-inventory/openai/models.json
            /tmp/gh-aw/agent/model-inventory/openai/raw.json
          if-no-files-found: error
          retention-days: 7

  collect_anthropic_models:
    runs-on: ubuntu-latest
    needs: [activation]
    permissions:
      contents: read
    steps:
      - name: Fetch Anthropic models
        id: fetch
        shell: bash
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          set -euo pipefail
          OUT="/tmp/gh-aw/agent/model-inventory/anthropic"
          mkdir -p "$OUT"
          if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
            echo '{"provider":"anthropic","error":"ANTHROPIC_API_KEY not set","models":[]}' > "$OUT/models.json"
            echo '{"provider":"anthropic","error":"ANTHROPIC_API_KEY not set"}' > "$OUT/raw.json"
            echo "status=skipped" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          # runner-guard:ignore RGS-012 -- the Anthropic API key is sent only to the official Anthropic models endpoint.
          HTTP_STATUS=$(curl -sf -o "$OUT/raw.json" -w "%{http_code}" \
            -H "x-api-key: $ANTHROPIC_API_KEY" \
            -H "anthropic-version: 2023-06-01" \
            https://api.anthropic.com/v1/models) || true
          if [ "${HTTP_STATUS:-0}" = "200" ]; then
            jq '{
              provider: "anthropic",
              models: [
                .data[] | {
                  id,
                  display_name,
                  created_at,
                  type
                }
              ] | sort_by(.id)
            }' "$OUT/raw.json" > "$OUT/models.json"
            echo "status=ok" >> "$GITHUB_OUTPUT"
          else
            echo "{\"provider\":\"anthropic\",\"error\":\"HTTP $HTTP_STATUS\",\"models\":[]}" > "$OUT/models.json"
            echo "status=error" >> "$GITHUB_OUTPUT"
          fi

      - name: Log raw response
        if: always()
        shell: bash
        run: cat /tmp/gh-aw/agent/model-inventory/anthropic/raw.json 2>/dev/null || echo "(no raw.json)"

      - name: Upload Anthropic artifacts
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: anthropic-models
          path: |
            /tmp/gh-aw/agent/model-inventory/anthropic/models.json
            /tmp/gh-aw/agent/model-inventory/anthropic/raw.json
          if-no-files-found: error
          retention-days: 7

  collect_gemini_models:
    runs-on: ubuntu-latest
    needs: [activation]
    permissions:
      contents: read
    steps:
      - name: Fetch Gemini models
        id: fetch
        shell: bash
        env:
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
        run: |
          set -euo pipefail
          OUT="/tmp/gh-aw/agent/model-inventory/gemini"
          mkdir -p "$OUT"
          if [ -z "${GEMINI_API_KEY:-}" ]; then
            echo '{"provider":"gemini","error":"GEMINI_API_KEY not set","models":[]}' > "$OUT/models.json"
            echo '{"provider":"gemini","error":"GEMINI_API_KEY not set"}' > "$OUT/raw.json"
            echo "status=skipped" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          # runner-guard:ignore RGS-012 -- the Gemini API key is sent only to the official Google models endpoint.
          HTTP_STATUS=$(curl -sf -o "$OUT/raw.json" -w "%{http_code}" \
            "https://generativelanguage.googleapis.com/v1beta/models?key=${GEMINI_API_KEY}") || true
          if [ "${HTTP_STATUS:-0}" = "200" ]; then
            jq '{
              provider: "gemini",
              models: [
                .models[] | {
                  id: (.name | ltrimstr("models/")),
                  display_name: .displayName,
                  description: .description,
                  input_token_limit: .inputTokenLimit,
                  output_token_limit: .outputTokenLimit,
                  supported_generation_methods: .supportedGenerationMethods,
                  version: .version
                }
              ] | sort_by(.id)
            }' "$OUT/raw.json" > "$OUT/models.json"
            echo "status=ok" >> "$GITHUB_OUTPUT"
          else
            echo "{\"provider\":\"gemini\",\"error\":\"HTTP $HTTP_STATUS\",\"models\":[]}" > "$OUT/models.json"
            echo "status=error" >> "$GITHUB_OUTPUT"
          fi

      - name: Log raw response
        if: always()
        shell: bash
        run: cat /tmp/gh-aw/agent/model-inventory/gemini/raw.json 2>/dev/null || echo "(no raw.json)"

      - name: Upload Gemini artifacts
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: gemini-models
          path: |
            /tmp/gh-aw/agent/model-inventory/gemini/models.json
            /tmp/gh-aw/agent/model-inventory/gemini/raw.json
          if-no-files-found: error
          retention-days: 7

  collect_copilot_billing_models:
    runs-on: ubuntu-latest
    needs: [activation]
    permissions:
      contents: read
    steps:
      - name: Create placeholder for GitHub Copilot billing models
        id: fetch
        shell: bash
        run: |
          set -euo pipefail
          OUT="/tmp/gh-aw/agent/model-inventory/copilot-billing"
          mkdir -p "$OUT"
          # NOTE: The GitHub Docs pricing page is JavaScript-rendered (Next.js), so static HTTP
          # fetching (urllib, curl) returns an empty page without the table content. The actual
          # table extraction is performed by the agent in Step 2.5 using playwright-cli, which
          # can execute JavaScript. This pre-job step only creates a placeholder so the artifact
          # upload succeeds and the agent knows to fetch the data itself.
          URL="https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing"
          echo "{\"source\":\"$URL\",\"note\":\"placeholder — agent fetches via playwright in Step 2.5\",\"models\":[]}" \
            > "$OUT/models.json"
          echo "status=placeholder" >> "$GITHUB_OUTPUT"

      - name: Log placeholder
        if: always()
        shell: bash
        run: cat /tmp/gh-aw/agent/model-inventory/copilot-billing/models.json 2>/dev/null || echo "(no models.json)"

      - name: Upload GitHub Copilot billing models artifact
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: copilot-billing-models
          path: /tmp/gh-aw/agent/model-inventory/copilot-billing/models.json
          if-no-files-found: error
          retention-days: 7

  collect_copilot_sdk_models:
    runs-on: ubuntu-latest
    needs: [activation]
    permissions:
      contents: read
    steps:
      - name: Install GitHub Copilot SDK with bundled GitHub Copilot CLI
        shell: bash
        run: |
          set -euo pipefail
          SDK_DIR="$RUNNER_TEMP/copilot-sdk"
          mkdir -p "$SDK_DIR"
          npm install --prefix "$SDK_DIR" --no-save @github/copilot-sdk@1.0.11

      - name: Fetch GitHub Copilot SDK model billing information
        shell: bash
        env:
          COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          OUT="/tmp/gh-aw/agent/model-inventory/copilot-sdk"
          mkdir -p "$OUT"
          cd "$RUNNER_TEMP/copilot-sdk"
          node --input-type=module > "$OUT/models.json" <<'EOF'
          import { CopilotClient } from "@github/copilot-sdk";

          const client = new CopilotClient({
            gitHubToken: process.env.COPILOT_GITHUB_TOKEN,
            logLevel: "error",
          });

          await client.start();
          try {
            const models = await client.listModels();
            process.stdout.write(`${JSON.stringify({ provider: "copilot-sdk", models }, null, 2)}\n`);
          } finally {
            await client.stop();
          }
          EOF

      - name: Log models
        if: always()
        shell: bash
        run: cat /tmp/gh-aw/agent/model-inventory/copilot-sdk/models.json 2>/dev/null || echo "(no models.json)"

      - name: Upload GitHub Copilot SDK models artifact
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: copilot-sdk-models
          path: /tmp/gh-aw/agent/model-inventory/copilot-sdk/models.json
          if-no-files-found: error
          retention-days: 7

steps:
  - name: Download all model artifacts
    uses: actions/download-artifact@v8.0.1
    with:
      path: /tmp/gh-aw/agent/model-inventory/artifacts

  - name: Predownload models.dev API index
    shell: bash
    # runner-guard:ignore RGS-012 -- unauthenticated GET from a public read-only model index; no secrets are sent.
    run: |
      set -euo pipefail
      OUT="/tmp/gh-aw/agent/model-inventory/models-dev"
      mkdir -p "$OUT"
      # runner-guard:ignore RGS-012 -- unauthenticated GET from a public read-only model index; no secrets are sent.
      curl -fsS https://models.dev/api.json -o "$OUT/api.json"
      echo "Downloaded models.dev API index to $OUT/api.json"

  - name: Merge artifacts into combined inventory
    shell: bash
    run: |
      INVENTORY="/tmp/gh-aw/agent/model-inventory/inventory.json"
      jq -s '.' /tmp/gh-aw/agent/model-inventory/artifacts/*/models.json > "$INVENTORY"
      echo "Combined inventory written to $INVENTORY"
      cat "$INVENTORY"

sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
tools:
  cli-proxy: true
  playwright:
  bash:
    - "*"
  github:
    toolsets: [default]

safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[model-inventory] "
    labels: [automation, models]
    max: 1
    close-older-issues: true

imports:
  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: model_lists_queried
    question: Did the agent query model inventories from OpenAI, Anthropic, Google, and Copilot APIs?
  - id: updates_proposed_or_noop
    question: Were updates to the builtin model alias mapping proposed, or was noop used when no changes were needed?
---

# Daily Model Inventory Checker

You are an AI model catalog analyst for `${{ github.repository }}`.

Your task is to analyze the current model inventories from all configured AI providers and
determine whether the built-in model alias mapping in `pkg/workflow/data/model_aliases.json` needs
updating.

## Inputs

The pre-job steps have already fetched model lists from OpenAI, Anthropic, and Gemini, then merged
them into:

- Combined inventory: `/tmp/gh-aw/agent/model-inventory/inventory.json`
- Individual provider files: `/tmp/gh-aw/agent/model-inventory/artifacts/<provider>-models/models.json`
- Raw provider responses: `/tmp/gh-aw/agent/model-inventory/artifacts/<provider>-models/raw.json`
- Predownloaded models.dev API index: `/tmp/gh-aw/agent/model-inventory/models-dev/api.json`
- Copilot SDK models: `/tmp/gh-aw/agent/model-inventory/artifacts/copilot-sdk-models/models.json`
  (fetched through the Copilot CLI SDK; use each model's `billing` fields for pricing validation)
- Copilot live provider metadata: `/tmp/gh-aw/agent/model-inventory/reflect.json` (generated in
  Step 0 below; filter `.endpoints[] | select(.provider == "copilot") | .models`). If the
  file contains an `error` field, treat Copilot data as unavailable for this run and
  continue with the remaining providers.

Each enriched `models.json` entry has the form (fields vary by provider):
```json
{
  "provider": "copilot",
  "models": [
    {
      "id": "claude-sonnet-4-5",
      "name": "Claude Sonnet 4.5",
      "vendor": "anthropic",
      "capabilities": { "limits": { "max_context_window_tokens": 200000 } },
      "billing": { "multiplier": 1.0 }
    }
  ]
}
```
Note: Copilot model data is fetched during agent execution in Step 0 below because the AWF
`api-proxy` hostname is only reachable from within the agent Docker network. The fetch
enriches null models via `models_url` where possible (see `.github/aw/llms.md`). Copilot
serves models from multiple vendors (Anthropic, OpenAI, Google), and those models may include
`vendor` metadata.

If a provider's API key was not configured, the entry will have `"error": "... not set"` and an
empty `models` array. Skip providers with errors or empty model lists.

## Built-in Alias Reference

Read `pkg/workflow/data/model_aliases.json` to understand the current alias definitions. The current
built-in aliases are:

| Alias | Resolves to |
|-------|-------------|
| `sonnet` | Anthropic Sonnet family |
| `haiku` | Anthropic Haiku family |
| `opus` | Anthropic Opus family |
| `gpt-5` | OpenAI GPT-5 family |
| `gpt-5-mini` | OpenAI GPT-5 mini family |
| `gpt-5-codex` | OpenAI GPT-5 Codex family |
| `gemini-flash` | Google Gemini Flash family |
| `gemini-pro` | Google Gemini Pro family |
| `small` / `mini` | Lightweight/fast models |
| `large` | Full-capability models |
| `auto` | Convenience alias for `large` |

The alias pattern syntax is:
- `"vendor/model*id"` — wildcard glob (e.g. `"copilot/*sonnet*"`)
- `"alias"` — recursive reference to another alias

## Task

### Step 0: Fetch Copilot Models from API Proxy

Before loading the inventory, fetch Copilot model metadata from the AWF `api-proxy` `/reflect`
endpoint from within this agent execution context and write it to:
`/tmp/gh-aw/agent/model-inventory/reflect.json`.

Run:

```bash
set -euo pipefail
OUT="/tmp/gh-aw/agent/model-inventory/reflect.json"
mkdir -p "$(dirname "$OUT")"
if ! curl -fsS http://api-proxy:10000/reflect > "$OUT"; then
  printf '%s' '{"endpoints":[],"error":"reflect endpoint unavailable"}' > "$OUT"
fi
# For configured endpoints where /reflect returned null models, fetch directly from
# models_url (the api-proxy injects auth headers). Mirrors enrichReflectModels() in
# awf_reflect.cjs — see .github/aw/llms.md for endpoint port/URL reference.
while IFS= read -r entry; do
  provider=$(printf '%s' "$entry" | jq -r '.provider')
  models_url=$(printf '%s' "$entry" | jq -r '.models_url')
  echo "Fetching models for $provider from $models_url"
  if raw=$(curl -fsS "$models_url" 2>&1); then
    ids=$(printf '%s' "$raw" | jq -c '[.data[].id] // empty' 2>&1) || {
      echo "Warning: failed to parse models response for $provider: $ids"
      ids=""
    }
    if [ -n "$ids" ]; then
      jq --arg p "$provider" --argjson m "$ids" \
        '(.endpoints[] | select(.provider == $p) | .models) |= $m' \
        "$OUT" > "${OUT}.tmp" && mv "${OUT}.tmp" "$OUT"
      echo "Enriched $provider with $(printf '%s' "$ids" | jq 'length') model(s)"
    else
      echo "Warning: no model IDs extracted for $provider"
    fi
  else
    echo "Warning: failed to fetch models_url for $provider ($models_url): $raw"
  fi
done < <(jq -c '.endpoints[]? | select(.configured == true and .models == null and .models_url != null)' "$OUT" 2>/dev/null || true)
echo "Copilot reflect metadata written to $OUT"
```

### Step 1: Load and Validate the Inventory

Read the combined inventory from `/tmp/gh-aw/agent/model-inventory/inventory.json`. Then read
the `/tmp/gh-aw/agent/model-inventory/reflect.json` file from Step 0 and extract the configured
`copilot` endpoint (`.endpoints[] | select(.provider == "copilot" and .configured)`).
Also read `/tmp/gh-aw/agent/model-inventory/models-dev/api.json` as a secondary cross-provider
catalog snapshot.

List the providers that returned data and the count of models available from each, including
Copilot from the reflect file.

If the reflect file has an `error` field, or contains no `copilot` endpoint, note Copilot as
unavailable and continue.

### Step 2: Explore Raw API Fields

For each provider that returned data, examine the raw response to identify all available fields:

- OpenAI / Anthropic / Gemini: `/tmp/gh-aw/agent/model-inventory/artifacts/<provider>-models/raw.json`
- Copilot: `/tmp/gh-aw/agent/model-inventory/reflect.json` filtered to the `copilot` endpoint object

Specifically look for:

- **Context window metadata**: input/output token limits (e.g. `inputTokenLimit`, `outputTokenLimit`,
  `capabilities.limits.max_context_window_tokens`, `capabilities.limits.max_output_tokens`)
- **Capability flags**: supported generation methods, vision support, tool use, streaming
  (e.g. `supportedGenerationMethods`, `capabilities.supports.vision`, `capabilities.type`)
- **Billing/pricing fields**: any field that conveys relative cost, a multiplier, a tier name,
  or a premium indicator (e.g. `billing.multiplier`, `policy`, `tier`, `premium`, `cost_multiplier`)
- **Model metadata**: `display_name`, `vendor`, `version`, `created_at`/`created`

For `models.dev/api.json`, focus on normalized provider/model IDs and any capability or pricing-like
metadata that can improve alias coverage checks when provider APIs are partial.

Summarize which fields are present and which carry useful data worth including in future cached
inventories.

### Step 2.5: Fetch Copilot Pricing Table via Playwright

The GitHub Docs pricing page (`https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing`)
is a Next.js application that renders its content with JavaScript. The pre-job step creates only a
placeholder artifact. Use `playwright-cli` to load the fully-rendered page and extract the pricing table.

Run the following commands in sequence:

1. Navigate to the pricing page:

```bash
playwright-cli browser_navigate --url "https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing"
```

2. Wait for the page to load, then extract the pricing table using `browser_evaluate`:

```bash
playwright-cli browser_evaluate --function "() => {
  const EXCLUDED = new Set(['gpt-4o-mini', 'gpt-4.1', 'gpt-4o', 'gpt-5.4-nano']);
  const result = { headers: [], models: [] };
  const tables = document.querySelectorAll('table');
  for (const table of tables) {
    const headerCells = [...table.querySelectorAll('thead th, thead td')];
    if (!headerCells.length) continue;
    const headers = headerCells.map(th => th.textContent.trim());
    result.headers = headers;
    const rows = [...table.querySelectorAll('tbody tr')];
    for (const row of rows) {
      const cells = [...row.querySelectorAll('td, th')].map(td => td.textContent.trim());
      if (cells.length !== headers.length) continue;
      const entry = Object.fromEntries(headers.map((h, i) => [h, cells[i]]));
      const modelId = (entry['Model'] || '').trim();
      if (modelId && !EXCLUDED.has(modelId)) result.models.push(entry);
    }
    if (result.models.length) break;
  }
  return JSON.stringify(result);
}"
```

3. Parse the JSON output from `browser_evaluate` and write the result to override the placeholder artifact:

```bash
URL="https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing"
EXCLUDED='["gpt-4o-mini","gpt-4.1","gpt-4o","gpt-5.4-nano"]'
OUT="/tmp/gh-aw/agent/model-inventory/artifacts/copilot-billing-models/models.json"
mkdir -p "$(dirname "$OUT")"
# Replace <PLAYWRIGHT_OUTPUT> below with the JSON string captured from browser_evaluate:
# echo "{\"source\":\"$URL\",\"excluded_models\":$EXCLUDED,<PLAYWRIGHT_OUTPUT>}" | jq . > "$OUT"
```

If playwright extraction fails or returns an empty `models` array, note this in the issue report and
continue — treat billing multiplier validation as skipped for this run (same behaviour as before).

### Step 3: Validate models.json pricing data

Read the current built-in pricing payloads from:

- `pkg/cli/data/models.json`
- `actions/setup/js/models.json`

Treat these two files as a mirrored pair: proposed updates must keep them identical.

The Copilot pricing table is now in:

- `/tmp/gh-aw/agent/model-inventory/artifacts/copilot-billing-models/models.json`

This file was populated in Step 2.5 from:
`https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing`.

The Copilot SDK models are available at:

- `/tmp/gh-aw/agent/model-inventory/artifacts/copilot-sdk-models/models.json`

Use the Copilot SDK (`billing` fields), reflect endpoint (`billing.multiplier`), and docs pricing table as validation sources for `models.json` pricing fields.
Prefer Copilot SDK data when available; cross-check it against reflect data, and use docs table values as a secondary cross-check.

Also validate Copilot SDK routing metadata in `models.json` for `github-copilot` models:
- `provider_type` (for SDK provider selection)
- `wire_api` when present (`responses` or `completions`, for OpenAI/Azure-compatible transport selection)

When updating `models.json`, preserve or add `wire_api` for Copilot models where source data
provides it. Keep `wire_api` absent for models/providers where it is not applicable.

Treat `gpt-4o-mini`, `gpt-4.1`, `gpt-4o`, and `gpt-5.4-nano` as intentionally deprecated
Copilot-facing model IDs. Keep ignoring them even if they appear in the reflect data,
docs table, `models.dev`, or live provider inventories: do not propose adding or restoring them in
`pkg/cli/data/models.json` (or `actions/setup/js/models.json`), and exclude them from
missing/discrepancy tables.

Use `models.dev/api.json` as the refresh source for the baseline `models.json` payload.
When pricing updates are required, first run:

```bash
make refresh-models-json
```

Then validate/adjust pricing entries against Copilot SDK, reflect, and docs-derived data.

For each provider's enriched data, validate pricing/model coverage for each model:

1. **Copilot SDK data** — use the `models` list from
   `/tmp/gh-aw/agent/model-inventory/artifacts/copilot-sdk-models/models.json`. For each model, use
   its `billing` fields as the primary pricing source. Compare against the matching
   `github/<model-id>` entry in `models.json`, and list discrepancies or missing models.

2. **Copilot reflect data** — use the `copilot` endpoint's `models` list from
   `/tmp/gh-aw/agent/model-inventory/reflect.json`. For each model, use
   the `billing.multiplier` field to cross-check the SDK multiplier. Compare against the
   matching `github/<model-id>` entry in `models.json`, and list discrepancies or missing models.
   Cross-reference against the docs table as a secondary validation source.

3. **Gemini API** — use `inputTokenLimit` / `outputTokenLimit` as an approximate proxy for model
   complexity (this is an inference heuristic, not a definitive billing mapping).
   Large-context, high-output-limit models typically correspond to higher-priced tiers; smaller
   Flash models to lower-priced tiers. Flag any models whose limits suggest a pricing-tier change
   versus what is currently in `models.json`.

4. **OpenAI API** — use `owned_by` and model-ID naming conventions (e.g. `-mini`, `-nano`, `o1`,
   `o3`) to cross-check current pricing tiers. Flag missing models or likely mismatches.

5. **Anthropic API** — use `display_name` family grouping (haiku/sonnet/opus) to validate
   current pricing tiers. Flag any new model IDs not yet in `models.json`.

Produce a consolidated pricing gap table listing:
- Models present in the live inventory but **missing** from `models.json` — include
  the provider name for each model (e.g. "openai", "anthropic", "gemini", "copilot")
- Models in `models.json` that are **not currently returned** by live APIs; keep these
  in the payload as historical entries (do not propose automatic removals)
- Models where the **inferred pricing tier or multiplier signal** differs from the stored pricing

### Step 4: Identify New or Updated Model Families

Compare the live model list against the current aliases in `pkg/workflow/data/model_aliases.json`.
Look for:

1. **New model generations** — e.g. a new `claude-sonnet-5` or `gpt-6` that is not covered by
   any existing alias glob pattern.
2. **New model families** — entirely new families (e.g. a new reasoning or multimodal line)
   that have no corresponding alias.
3. **Stale aliases** — patterns that no longer match any live model.
4. **Task-oriented alias gaps** — useful semantic aliases that are missing, such as:
   - `summarization-model` → a fast, cost-effective model good at summarization
   - `coding-model` → a model optimized for code generation
   - `reasoning-model` → a model with extended reasoning/thinking capability
   - `vision-model` → a model that supports image input

### Step 5: Propose Alias Mapping Updates

For each finding from Step 4, produce a concrete JSON snippet showing the proposed new or updated
alias entry in the `aliases` object in `pkg/workflow/data/model_aliases.json`. Use the alias pattern syntax:

```json
{
  "new-alias": [
    "copilot/vendor-model-id*",
    "vendor/vendor-model-id*"
  ]
}
```

Focus on aliases that provide genuine value to workflow authors. Prioritize:
- Adding patterns to existing aliases to cover new model generations
- Adding new semantic task-oriented aliases
- Updating patterns that are stale

### Step 6: Create Issue

If you found any meaningful updates to propose, create a GitHub issue using `create_issue`.

**Issue title format**: `Model alias inventory update - YYYY-MM-DD`

**Issue body structure** (use h3 `###` or lower — never h1/h2):

```markdown
### Summary

Brief description of what was found.

- Providers queried: OpenAI, Anthropic, Gemini, Copilot SDK, Copilot (reflect)
- Total models found: <count>
- Proposed alias changes: <count>
- Pricing gaps found: <count>

### Provider Model Counts

| Provider | Models Available | Status |
|----------|-----------------|--------|
| openai   | 42              | ✅ ok  |
| anthropic | 15             | ✅ ok  |
| gemini   | 28              | ✅ ok  |
| copilot-sdk | 35          | ✅ ok  |
| copilot (reflect) | 35    | ✅ ok  |

### Raw API Fields Discovered

For each provider, list noteworthy fields found in the raw response that are now captured
in the enriched `models.json` artifact (context limits, capabilities, billing fields, etc.).

### models.json Pricing Analysis

#### Missing from models.json

| Model ID | Provider | Inferred Pricing | Basis |
|----------|----------|-----------------:|-------|
| ...      | ...      | ...              | ...   |

#### Historical entries not currently returned

List model IDs that appear in `models.json` but are absent from all live inventories.
Treat these as historical records that should remain in the payload unless a human explicitly
decides to delete them.

#### Inferred vs stored pricing discrepancies

| Model ID | Stored Pricing | Inferred Pricing | Inferred From |
|----------|---------------:|-----------------:|---------------|
| ...      | ...            | ...              | ...           |

### Proposed Alias Updates

For each change, explain:
1. **What**: The alias name and new/updated patterns
2. **Why**: Which live model(s) prompted this change
3. **Syntax**: JSON snippet showing the new or updated entry for the `aliases` object in `pkg/workflow/data/model_aliases.json`

<details>
<summary><b>Full Model Lists by Provider</b></summary>

List the complete sorted model IDs for each provider.

</details>

### Notes

Any caveats, historical entries retained, or aliases that are already well-covered.
```

If no updates are needed (all live models are already covered by existing aliases, all
`models.json` pricing entries are up to date, and no new task-oriented aliases are warranted), create an issue with
title `Model alias inventory - no changes needed - YYYY-MM-DD` and a brief summary confirming
coverage is up to date.


### Output Format

**Report Structure Guidelines**

- Use `###` (or lower) headers only.
- Keep summary and critical actions visible; move long detail into `<details>` blocks.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

```markdown
### Summary

**X items found** — [brief description]

<details>
<summary><b>View Full Details</b></summary>

... detailed content here ...

</details>
```