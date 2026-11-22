---
steps:
  - name: Pre-fetch ADR gate PR context
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      PR_NUMBER: ${{ github.event.pull_request.number || github.event.inputs.pr_number }}
      EXPR_GITHUB_EVENT_NAME: ${{ github.event_name }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
      EXPR_GITHUB_WORKSPACE: ${{ github.workspace }}
    run: |
      set -euo pipefail

      mkdir -p /tmp/gh-aw/agent
      SKIP_REASON=""
      if [ "$EXPR_GITHUB_EVENT_NAME" = "workflow_dispatch" ] && [ -z "${PR_NUMBER:-}" ]; then
        SKIP_REASON="workflow_dispatch did not include inputs.pr_number"
        printf '{"number":null,"title":"No pull request provided","body":"","labels":[],"baseRefName":"","headRefName":"","author":null,"url":""}\n' \
          > /tmp/gh-aw/agent/pr.json
      else
        gh pr view "$PR_NUMBER" \
          --repo "$EXPR_GITHUB_REPOSITORY" \
          --json number,title,body,labels,baseRefName,headRefName,author,url,state \
          > /tmp/gh-aw/agent/pr.json
        PR_STATE=$(jq -r '.state' /tmp/gh-aw/agent/pr.json)
        if [ "$PR_STATE" = "CLOSED" ] || [ "$PR_STATE" = "MERGED" ]; then
          SKIP_REASON="Pull request #$PR_NUMBER is $PR_STATE; ADR enforcement applies only to open pull requests"
        fi
      fi

      if [ -n "$SKIP_REASON" ]; then
        echo "::notice::$SKIP_REASON; skipping ADR gate."
        printf '[]\n' > /tmp/gh-aw/agent/pr-files.json
        printf '# ADR gate skipped: %s.\n' "$SKIP_REASON" \
          > /tmp/gh-aw/agent/pr.diff
        printf 'No .design-gate.yml read because ADR gate was skipped: %s.\n' "$SKIP_REASON" \
          > /tmp/gh-aw/agent/design-gate-config.yml
        jq -n \
          --arg skip_reason "$SKIP_REASON" \
          --arg pr_number "${PR_NUMBER:-}" \
          '{
            pr_number: (if $pr_number == "" then null else ($pr_number | tonumber) end),
            threshold: 100,
            has_custom_config: false,
            has_implementation_label: false,
            default_business_additions: 0,
            requires_adr_by_default_volume: false,
            file_count: 0,
            diff_available: false,
            skip_reason: $skip_reason
          }' > /tmp/gh-aw/agent/adr-prefetch-summary.json
        exit 0
      fi

      gh api --paginate "repos/$EXPR_GITHUB_REPOSITORY/pulls/$PR_NUMBER/files?per_page=100" \
        --jq '.[]' | jq -s '.' > /tmp/gh-aw/agent/pr-files.json

      FILE_COUNT=$(jq 'length' /tmp/gh-aw/agent/pr-files.json)

      if [ "$FILE_COUNT" -gt 300 ]; then
        echo "::warning::PR has $FILE_COUNT changed files (exceeds the 300-file GitHub diff API limit). Skipping full diff; file listing is available in pr-files.json."
        printf '# Diff unavailable: PR has %s changed files (exceeds the 300-file GitHub diff API limit).\n# Use pr-files.json for the full file listing instead.\n' "$FILE_COUNT" \
          > /tmp/gh-aw/agent/pr.diff
      else
        gh pr diff "$PR_NUMBER" \
          --repo "$EXPR_GITHUB_REPOSITORY" \
          > /tmp/gh-aw/agent/pr.diff
      fi

      if [ -f "$EXPR_GITHUB_WORKSPACE/.design-gate.yml" ]; then
        cp "$EXPR_GITHUB_WORKSPACE/.design-gate.yml" /tmp/gh-aw/agent/design-gate-config.yml
        HAS_CUSTOM_CONFIG=true
      else
        echo "No .design-gate.yml found — using defaults" > /tmp/gh-aw/agent/design-gate-config.yml
        HAS_CUSTOM_CONFIG=false
      fi

      BUSINESS_ADDITIONS_DEFAULT=$(jq '[.[] | select(.filename | test("^(src|lib|pkg|internal|app|core|domain|services|api)/")) | .additions] | add // 0' /tmp/gh-aw/agent/pr-files.json)
      HAS_IMPLEMENTATION_LABEL=$(jq '[.labels[]?.name] | index("implementation") != null' /tmp/gh-aw/agent/pr.json)

      jq -n \
        --argjson default_business_additions "$BUSINESS_ADDITIONS_DEFAULT" \
        --argjson has_implementation_label "$HAS_IMPLEMENTATION_LABEL" \
        --argjson has_custom_config "$HAS_CUSTOM_CONFIG" \
        --arg pr_number "$PR_NUMBER" \
        --arg threshold "100" \
        --argjson file_count "$FILE_COUNT" \
        --argjson diff_available "$(jq -n --argjson fc "$FILE_COUNT" 'if $fc <= 300 then true else false end')" \
        '{
          pr_number: ($pr_number | tonumber),
          threshold: ($threshold | tonumber),
          has_custom_config: $has_custom_config,
          has_implementation_label: $has_implementation_label,
          default_business_additions: $default_business_additions,
          requires_adr_by_default_volume: ($default_business_additions > ($threshold | tonumber)),
          file_count: $file_count,
          diff_available: $diff_available
        }' > /tmp/gh-aw/agent/adr-prefetch-summary.json
---
