---
mcp-servers:
  sentry:
    command: "npx"
    args: ["@sentry/mcp-server@0.29.0"]
    allowed:
      - whoami
      - find_organizations
      - find_teams
      - find_projects
      - find_releases
      - get_issue_details
      - get_trace_details
      - get_event_attachment
      - search_events
      - search_issues
      - find_dsns
      - analyze_issue_with_seer
      - search_docs requires SENTRY_OPENAI_API_KEY
      - get_doc
    env:
      SENTRY_ACCESS_TOKEN: ${{ secrets.SENTRY_ACCESS_TOKEN }}
      SENTRY_HOST: ${{ env.SENTRY_HOST }} # Optional
      OPENAI_API_KEY: ${{ secrets.SENTRY_OPENAI_API_KEY }} # Optional
---

<!-- 

https://github.com/getsentry/sentry-mcp 

To utilize the stdio transport, you'll need to create an User Auth Token in Sentry with the necessary scopes. As of writing this is:

```
org:read
project:read
project:write
team:read
team:write
event:write
```
-->
