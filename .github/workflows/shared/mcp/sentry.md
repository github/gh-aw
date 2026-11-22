---
mcp-servers:
  sentry:
    command: "npx"
    args: ["@sentry/mcp-server@0.49.0"]
    allowed:
      - whoami
      - find_organizations
      - find_teams
      - find_projects
      - find_releases
      - get_issue_details
      - get_trace_details
      - get_event_attachment
      - search_traces
      - search_errors
      - search_logs
      - search_issues
      - find_dsns
      - search_docs # requires SENTRY_OPENAI_API_KEY
      - get_doc
    env:
      SENTRY_AUTH_TOKEN: ${{ secrets.SENTRY_ACCESS_TOKEN }}
      SENTRY_HOST: ${{ env.SENTRY_HOST || 'sentry.io' }} # Optional - hostname only, not a full URL
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

Use this Sentry MCP import for read-only reliability triage with evidence-first reporting.

Preferred query loop:

1. Validate connectivity (`whoami`) and discover the target org/project.
2. Query spans first with `search_traces`. Without an embedded provider, pass a direct Sentry query, explicit fields, and sort.
3. Run companion checks with `search_errors` and `search_logs`, even when empty, and state that result explicitly.
4. For each major finding, verify one representative trace:
   - use `get_trace_details` when available
   - otherwise use `search_traces` with explicit span fields and a `trace:<id>` filter to confirm continuity
5. Every priority finding must cite exact evidence (query scope + metric/count + trace or run link).
6. If expected fields are missing, cross-check emit-side semantics in `actions/setup/js/send_otlp_span.cjs` before proposing fixes (notably `gh-aw.workflow.name`, OTLP `status.code`, `gh-aw.run.status`, `gen_ai.response.finish_reasons`, and resource `service.version`).

Grounding rules:

- Prefer recurring patterns over one-off outliers.
- Separate confirmed failures from observability gaps when core attributes are null/missing.
- Call out unsupported tools or backend limitations in the report notes instead of silently skipping checks.
- Treat repeated Sentry HTTP 400 responses for the same query shape as terminal, not transient. After the first field-type or query-syntax 400, change strategy to field discovery, `count()`, raw event rows with client-side aggregation, or another backend/fallback; if an equivalent 400 occurs again, stop retrying that query family and report the exact backend gap. The preferred fallback chain is: (1) use field discovery to confirm the schema type, (2) switch to `count()` aggregation, (3) fetch raw event rows and aggregate client-side, (4) fall back to local artifacts or an alternative backend.
