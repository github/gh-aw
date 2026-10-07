---
name: Community Issue Status
description: Weekly status report for open community issues older than one week
intent: Give maintainers an evidence-backed, categorized view of older community issues without spamming contributors.
on:
  schedule:
    - cron: "17 9 * * 1"
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  copilot-requests: write
engine:
  id: copilot
  version: 1.0.92
  dynamic-workflows: true
  args: ["--experimental"]
steps:
  - name: Fetch a rotating batch of older community issues
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      RUN_NUMBER: ${{ github.run_number }}
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      cutoff=$(date -u -d '7 days ago' '+%Y-%m-%dT%H:%M:%SZ')
      gh issue list --repo "$GITHUB_REPOSITORY" \
        --search "is:issue label:community state:open created:<${cutoff}" \
        --limit 1000 --json number,title,body,url,createdAt,updatedAt,comments \
        > /tmp/gh-aw/agent/community-issues-raw.json
      jq --argjson cutoff "$(date -u -d '7 days ago' '+%s')" \
        --argjson run "$RUN_NUMBER" '
        [ .[] | select((.createdAt | fromdateiso8601) < $cutoff) ]
        | sort_by(.updatedAt) as $all
        | ($all | length) as $total
        | if $total == 0 then [] else
            [range(0; [60, $total] | min) |
              $all[((($run - 1) * 60) + .) % $total] |
              {number, title: .title[:180], url,
               body: (.body // "")[:1200], createdAt, updatedAt,
               comments: [.comments[-2:][]? |
                 {author: .author.login, createdAt, body: (.body // "")[:650], url}]}
            ]
          end
      ' /tmp/gh-aw/agent/community-issues-raw.json > /tmp/gh-aw/agent/community-issues.json
      echo "Selected $(jq length /tmp/gh-aw/agent/community-issues.json) older community issues"
safe-outputs:
  create-issue:
    max: 1
    title-prefix: "[Community Status] "
    close-older-issues: true
    close-older-key: community-issue-status
  noop:
timeout-minutes: 15
---

# Community Issue Status

Run the registered dynamic workflow `community-issue-status` exactly once with
`{"path":"/tmp/gh-aw/agent/community-issues.json"}`. Use the dynamic workflow
run tool, not a task agent or a shell-script replacement. Wait for the completed
run and inspect its actual result. If the run fails or its result is incomplete,
report the error rather than inventing statuses; do not publish a partial report.

When the result has `status: empty`, call `noop` because there are no eligible
issues. Otherwise create **one** status issue using `create-issue`, titled
"Open community issues older than one week". Include the UTC report date, the
fact that this is a rotating batch of up to 60 issues (not necessarily the
entire backlog if there are more than 60), counts for all four buckets, and a table grouped by bucket
with issue link, title, and the evidence-backed status sentence. Link the
workflow run with `${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.
Use the dynamic workflow's returned numbers, categories, and statuses as the
source of truth. Do not comment on individual community issues, promise
resolution dates, or infer implementation progress without evidence.
