---
connectors:
  sentry:
    credentials:
      hosted-reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - event:read
        secret: ${{ github.aw.import-inputs.hosted-reader-secret }}
      reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - event:read
        secret: ${{ github.aw.import-inputs.reader-secret }}
      writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - event:write
        secret: ${{ github.aw.import-inputs.writer-secret }}
    endpoints:
      api: https://sentry.io
    mcp-servers:
      sentry:
        credential: hosted-reader
        maxResponseBytes: 65536
        readOnly: true
        tools:
          get_issue_details:
            inputs:
              issueId:
                maxLength: 32
                pattern: ^[0-9]+$
                required: true
                type: string
              organizationSlug:
                enum:
                  - example-org
                maxLength: 100
                required: true
                type: string
            resources:
              issueId: issues
              organizationSlug: organizations
        url: https://mcp.sentry.dev/mcp/example-org/example-project
    triggers:
      error-created:
        match:
          /action: created
          /data/issue/issueCategory: error
        nativeEvents:
          - issue.created
        normalize:
          installation: /installation/uuid
          project: /data/issue/project/id
          subject: /data/issue/id
        resources:
          /data/issue/id: issues
          /data/issue/project/id: projects
          /installation/uuid: installations
        verificationProfile: sentry.io/issue-hmac-sha256
      error-regressed:
        match:
          /action: unresolved
          /data/issue/issueCategory: error
          /data/issue/substatus: regressed
        nativeEvents:
          - issue.unresolved
        normalize:
          installation: /installation/uuid
          project: /data/issue/project/id
          subject: /data/issue/id
        resources:
          /data/issue/id: issues
          /data/issue/project/id: projects
          /installation/uuid: installations
        verificationProfile: sentry.io/issue-hmac-sha256
      issue-alert-triggered:
        match:
          /action: triggered
          /data/event/type: error
        nativeEvents:
          - event_alert.triggered
        normalize:
          event: /data/event/event_id
          installation: /installation/uuid
          project: /data/event/project
          subject: /data/event/issue_id
        resources:
          /data/event/event_id: events
          /data/event/issue_id: issues
          /data/event/project: projects
          /installation/uuid: installations
        verificationProfile: sentry.io/event-alert-hmac-sha256
    resources:
      events:
        - 0123456789abcdef0123456789abcdef
      installations:
        - 11111111-2222-4333-8444-555555555555
      issues:
        - "1234567890"
      organizations:
        - example-org
      projects:
        - "4500000000000001"
    safe-outputs:
      resolve-issue:
        credential: writer
        inputs:
          issue:
            maxLength: 32
            pattern: ^[0-9]+$
            required: true
            type: string
          organization:
            maxLength: 100
            pattern: ^[a-z0-9][a-z0-9-]*$
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          issue: /id
          project: /project/id
          status: /status
        request:
          body:
            status:
              literal: resolved
          endpoint: api
          method: PUT
          path: /api/0/organizations/{organization}/issues/{issue}/
          pathInputs:
            issue: issue
            organization: organization
        resources:
          issue: issues
          organization: organizations
        responseChecks:
          /issueCategory: error
          /status: resolved
        responseRequired:
          - /id
          - /project/id
        responseResources:
          /id: issues
          /project/id: projects
    tools:
      get-issue:
        credential: reader
        inputs:
          issue:
            maxLength: 32
            pattern: ^[0-9]+$
            required: true
            type: string
          organization:
            maxLength: 100
            pattern: ^[a-z0-9][a-z0-9-]*$
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          issue: /id
          project: /project/id
          status: /status
        request:
          endpoint: api
          method: GET
          path: /api/0/organizations/{organization}/issues/{issue}/
          pathInputs:
            issue: issue
            organization: organization
        resources:
          issue: issues
          organization: organizations
        responseChecks:
          /issueCategory: error
          /status: unresolved
        responseRequired:
          - /id
          - /status
          - /project/id
        responseResources:
          /id: issues
          /project/id: projects
        tool: get_issue_details
import-schema:
  hosted-reader-secret:
    default: SENTRY_MCP_OAUTH_TOKEN
    type: string
  reader-secret:
    default: SENTRY_READ_TOKEN
    type: string
  writer-secret:
    default: SENTRY_STATUS_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
