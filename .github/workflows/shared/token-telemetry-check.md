---
jobs:
  check_token_telemetry:
    runs-on: ubuntu-latest
    needs: [agent, activation]
    if: needs.agent.result == 'success'
    permissions:
      contents: read
    steps:
      - name: Download agent artifact
        id: download-agent
        uses: actions/download-artifact@v8.0.1
        with:
          name: agent
          path: /tmp/gh-aw/

      - name: Assert token_usage.jsonl is non-empty
        if: steps.download-agent.outcome == 'success' && !contains(fromJSON('["kiro","goose","cursor"]'), needs.activation.outputs.engine_id)
        run: |
          # The AWF firewall proxy writes token_usage.jsonl for every LLM API call.
          # If all token_usage.jsonl files are missing or empty, the emitter is broken.
          TOKEN_FILES=(
            "/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"
            "/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl"
            "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"
          )

          FOUND_NONEMPTY=false
          for f in "${TOKEN_FILES[@]}"; do
            if [ -s "$f" ]; then
              COUNT=$(grep -c . "$f" 2>/dev/null || echo "0")
              echo "OK: $f — ${COUNT} record(s)"
              FOUND_NONEMPTY=true
            else
              [ -f "$f" ] && echo "EMPTY: $f" || echo "MISSING: $f"
            fi
          done

          if [ "${FOUND_NONEMPTY}" != "true" ]; then
            echo "::error::All token_usage.jsonl files are empty or missing after a successful agent run."
            echo "::error::The AWF firewall proxy token telemetry emitter may be broken."
            exit 1
          fi

      - name: Assert agent_usage.json has non-zero token counts
        if: steps.download-agent.outcome == 'success' && !contains(fromJSON('["kiro","goose","cursor"]'), needs.activation.outputs.engine_id)
        run: |
          USAGE_FILE="/tmp/gh-aw/agent_usage.json"
          if [ ! -f "${USAGE_FILE}" ]; then
            echo "::error::agent_usage.json not found in agent artifact — token summary was not written."
            exit 1
          fi
          INPUT_TOKENS=$(python3 -c "import json; d=json.load(open('${USAGE_FILE}')); print(d.get('input_tokens', 0))")
          if [ "${INPUT_TOKENS}" -le 0 ]; then
            echo "::error::agent_usage.json has zero input_tokens — token telemetry may be broken."
            cat "${USAGE_FILE}"
            exit 1
          fi
          echo "OK: agent_usage.json reports ${INPUT_TOKENS} input tokens"

      - name: Report engines without proxy token telemetry
        if: steps.download-agent.outcome == 'success' && contains(fromJSON('["kiro","goose","cursor"]'), needs.activation.outputs.engine_id)
        run: echo "::notice::This engine does not provide proxy token counts; token telemetry assertions are not applicable."
---
<!--
# Token Telemetry Check

This shared workflow adds a `check_token_telemetry` job that runs after the `agent` job
and asserts that AWF firewall proxy token telemetry is functioning correctly
for engines that provide proxy token counts. Kiro, Goose, and Cursor do not
currently provide these counts, so the job reports that the assertions are
not applicable instead of treating missing proxy records as a regression.

It guards against silent regressions in the `token_usage.jsonl` emitter, which feeds
downstream token-cost and API-consumption audits.

## What it checks

1. **`token_usage.jsonl` is non-empty** — scans the three known sandbox proxy log paths
   and asserts that at least one file has content (i.e., the firewall proxy recorded LLM calls).
2. **`agent_usage.json` has non-zero `input_tokens`** — asserts that the agent usage summary
   written to the artifact reports at least one input token.

## When it runs

The job only executes when `needs.agent.result == 'success'`, so it is skipped for noop,
skipped, or failed agent runs.

## Usage

```yaml
imports:
  - shared/token-telemetry-check.md
```
-->
