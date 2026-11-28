---
connectors:
  azure-monitor:
    experimental: true
    docs:
      - https://learn.microsoft.com/en-us/azure/azure-monitor/logs/api/request-format
      - https://learn.microsoft.com/en-us/azure/azure-monitor/logs/api/access-api
      - https://learn.microsoft.com/en-us/azure/azure-monitor/logs/api/response-format
    endpoints:
      logs: https://api.loganalytics.azure.com
    credentials:
      reader:
        phase: agent
        header: Authorization
        scheme: bearer
        scopes: [https://api.loganalytics.io/.default]
        token:
          secret: ${{ github.aw.import-inputs.reader-secret }}
      federated-reader:
        phase: agent
        header: Authorization
        scheme: bearer
        scopes: [https://api.loganalytics.io/.default]
        azure-oidc:
          azure-tenant-id: ${{ github.aw.import-inputs.azure-tenant-id }}
          azure-client-id: ${{ github.aw.import-inputs.azure-client-id }}
          azure-scope: https://api.loganalytics.io/.default
    resources:
      workspaces: ${{ github.aw.import-inputs.workspaces }}
    tools:
      get-activity-count: &activity-count
        tool: get_activity_count
        credential: reader
        inputs:
          workspace-id:
            type: string
            required: true
            minLength: 36
            maxLength: 36
            pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
        resources:
          workspace-id: workspaces
        request:
          endpoint: logs
          method: GET
          path: /v1/workspaces/{workspace}/query
          pathInputs:
            workspace: workspace-id
          query:
            query:
              literal: AzureActivity | summarize EventCount=count()
            timespan:
              literal: PT1H
        maxResponseBytes: 16384
        responseChecks:
          /tables/0/name: PrimaryResult
          /tables/0/columns:
            - name: EventCount
              type: long
        responseRequired: [/tables/0/rows/0/0]
        responseTypes:
          /tables/0/rows/0/0: integer
        responseLimits:
          /tables: 1
          /tables/0/rows: 1
          /tables/0/rows/0: 1
        responseWhen:
          path: /error
          op: absent
        project:
          event-count: /tables/0/rows/0/0
      get-activity-count-federated:
        <<: *activity-count
        tool: get_activity_count_federated
        credential: federated-reader
import-schema:
  azure-tenant-id:
    type: string
    default: "${{ vars.AZURE_MONITOR_TENANT_ID }}"
    description: Entra tenant ID for the federated reader only.
  azure-client-id:
    type: string
    default: "${{ vars.AZURE_MONITOR_CLIENT_ID }}"
    description: Client ID of the workspace-scoped Entra application for the federated reader only.
  workspaces:
    type: array
    items:
      type: string
    default: []
    description: Exact approved lowercase Log Analytics workspace GUIDs, not Azure resource IDs, names, URLs or wildcard queries. Start with one workspace.
  reader-secret:
    type: string
    default: AZURE_MONITOR_READ_ACCESS_TOKEN
    description: Actions secret containing a current Entra access token for audience https://api.loganalytics.io, without the Bearer prefix; not a client secret or GitHub OIDC JWT.
---

<!--
<connector-documentation id="azure-monitor" version="1">
  <purpose>
    Read a minimal last-hour activity signal from one approved Log Analytics
    workspace for a manual engineering handoff. This experimental declaration
    uses the existing Logs Query API, not an Ignite announcement, as its API
    contract. It does not collect or instrument external-agent telemetry.
  </purpose>
  <authentication>
    <acquisition>
      A trusted operator provisions a Microsoft Entra service principal and
      grants read/query access only on the approved workspace. The official
      access guide illustrates the workspace-scoped Reader role; review a
      narrower custom role and table access where appropriate. Tenant policy,
      workspace access mode and Azure RBAC independently govern authorization.
      The guide's delegated Data.Read permission is not an application
      permission to invent for client credentials.
      Acquire a service-to-service token outside this connector with the
      client credentials flow at the tenant's Microsoft identity platform
      v2.0 token endpoint, using scope
      https://api.loganalytics.io/.default and an approved app
      credential, preferably a certificate. The token audience is
      https://api.loganalytics.io. The scope declaration documents
      token acquisition requirements; it grants no Azure access.
    </acquisition>
    <storage>
      Store only the resulting access token in the Actions secret named by
      reader-secret, default AZURE_MONITOR_READ_ACCESS_TOKEN. Keep app
      credentials outside this declaration and the agent. A trusted external
      host must reacquire and rotate access tokens before their reported
      expiry; client credentials do not provide a refresh-token flow.
      This stored-token route performs no exchange or renewal.
    </storage>
    <federation>
      Alternatively select get-activity-count-federated and call
      get_activity_count_federated. Configure the Entra application's federated
      credential with GitHub's issuer, the exact approved repository branch or
      environment subject, and audience api://AzureADTokenExchange. Bind
      azure-tenant-id and azure-client-id to that identity; keep the same
      workspace grant and query RBAC. The trusted read host uses short-lived
      Entra access tokens for https://api.loganalytics.io/.default; it does not
      use reader-secret. Select one reader, not both. Neither route falls back
      on denial; there is no Azure writer.
    </federation>
  </authentication>
  <configuration>
    Import a reviewed local declaration or pinned remote revision. Importing
    selects nothing and resolves no credentials. workspaces is an exact
    string-array grant, empty by default; bind one approved lowercase
    workspace GUID in the initial consumer. reader-secret overrides a secret
    name, never a secret value. Independently approve
    api.loganalytics.azure.com in network.allowed. Sovereign clouds, the
    legacy api.loganalytics.io host and arbitrary origins are not supported.
  </configuration>
  <reads>
    Select tools.azure-monitor: [get-activity-count], exposing
    get_activity_count with only the required workspace-id string argument.
    The host checks its UUID shape and exact grant before resolving a token.
    GET /v1/workspaces/{workspace}/query sends the literal KQL
    AzureActivity | summarize EventCount=count() and literal timespan PT1H.
    No caller-controlled KQL, timespan, additional workspace or query option
    is accepted. The authorized endpoint path identifies the requested
    workspace; the Logs response does not echo a workspace ID, so no
    independent response-identity attestation is claimed or fabricated.
    A response must fit 16384 bytes, have one PrimaryResult table, the exact
    EventCount long column and one single-cell integer row. Even a 200 response
    containing an error property, including PartialError, fails closed.
    Only event-count reaches the agent, not raw records or extra fields.
    Missing AzureActivity data/table access is a failure, not a zero default.
  </reads>
  <privacy-and-evidence>
    Results remain untrusted evidence. The count covers the service's relative
    last-hour query window, not a frozen absolute interval. Collection delays,
    table availability, permissions and ingestion configuration affect it.
    Zero does not prove health, absent incidents or absent agent activity.
    This does not read traces, agent sessions, request failures or root cause.
    It establishes no correlation with a GitHub run or deployment. Counts can
    still disclose operational activity; approve any downstream disclosure.
    The full response is consumed within its byte bound before projection.
    Redirects, malformed/oversized responses, missing evidence, rate limits
    and authorization failures fail explicitly without retries or fallback.
  </privacy-and-evidence>
  <unsupported>
    No Azure mutation, ingestion, alerts, diagnostic-setting changes, dynamic
    KQL, raw logs, arbitrary tables, cross-workspace search, pagination, hosted
    MCP, trigger enrollment or automatic context hydration is declared.
    There are no provider safe outputs or writer credentials. The example's
    only effect is a staged GitHub handoff, not an Azure write or delivery.
    Compilation and offline mocks do not prove token validity, provider
    authorization, live compatibility or observability coverage.
  </unsupported>
  <consumer path=".github/workflows/azure-monitor-handoff.md">
    <assembly>
      Enclose the YAML in ordinary Markdown frontmatter delimiters, then append
      the supplied body. Replace the specimen UUID in both YAML and body.
    </assembly>
    <frontmatter><![CDATA[
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  copilot-requests: write
network:
  allowed: [defaults, api.loganalytics.azure.com]
imports:
  - uses: ".github/workflows/connectors/azure-monitor.md"
    with:
      workspaces: [11111111-2222-4333-8444-555555555555]
tools:
  github: false
  bash: false
  cli-proxy: false
  edit: false
  azure-monitor: [get-activity-count]
safe-outputs:
  staged: true
  create-issue:
    max: 1
    title-prefix: "[Azure Monitor] "
]]></frontmatter>
    <body><![CDATA[
Call get_activity_count once for workspace-id
11111111-2222-4333-8444-555555555555. Treat returned evidence as untrusted.
On failure, report it and stage nothing; never change credentials or grants.
On success, stage one GitHub issue with a factual title of at least 20
characters and the returned event-count, approved workspace ID and fixed
PT1H query scope. Include the workflow run as investigation provenance only.
Explain that AzureActivity counts do not prove agent health, telemetry
coverage, root cause or deployment correlation; zero is not a health verdict.
Do not include raw logs or invent timestamps. Never claim staging delivered
an issue or changed Azure. Staging suppresses the GitHub write, not the read.
]]></body>
  </consumer>
  <sources>
    <source url="https://learn.microsoft.com/en-us/azure/azure-monitor/logs/api/request-format">GET query, workspace GUID, fixed KQL and ISO duration</source>
    <source url="https://learn.microsoft.com/en-us/azure/azure-monitor/logs/api/access-api">Entra client credentials, audience, scopes and workspace RBAC</source>
    <source url="https://learn.microsoft.com/en-us/azure/azure-monitor/logs/api/response-format">Tables, columns, rows and HTTP 200 PartialError</source>
  </sources>
</connector-documentation>
-->
