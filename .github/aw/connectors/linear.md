---
connectors:
  linear:
    credentials:
      mcp-reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - read
        secret: ${{ github.aw.import-inputs.mcp-reader-secret }}
      reader:
        header: Authorization
        phase: agent
        scheme: raw
        scopes:
          - read
        secret: ${{ github.aw.import-inputs.reader-secret }}
      writer:
        header: Authorization
        phase: safe-output
        scheme: raw
        scopes:
          - write
        secret: ${{ github.aw.import-inputs.writer-secret }}
    endpoints:
      api: https://api.linear.app
    mcp-servers:
      read-only:
        credential: mcp-reader
        maxResponseBytes: 131072
        readOnly: true
        tools:
          get_issue:
            inputs:
              id:
                maxLength: 36
                pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
                required: true
                type: string
            resources:
              id: issues
        url: https://mcp.linear.app/mcp/readonly
    "on":
      issue-created:
        match:
          /action: create
          /type: Issue
        nativeEvents:
          - create
        normalize:
          issue-id: /data/id
          organization-id: /organizationId
          team-id: /data/teamId
          url: /url
        resources:
          /data/teamId: teams
          /organizationId: organizations
        verificationProfile: linear.app/hmac-sha256-timestamp
      issue-removed:
        match:
          /action: remove
          /type: Issue
        nativeEvents:
          - remove
        normalize:
          issue-id: /data/id
          organization-id: /organizationId
          team-id: /data/teamId
          url: /url
        resources:
          /data/teamId: teams
          /organizationId: organizations
        verificationProfile: linear.app/hmac-sha256-timestamp
      issue-updated:
        match:
          /action: update
          /type: Issue
        nativeEvents:
          - update
        normalize:
          issue-id: /data/id
          organization-id: /organizationId
          team-id: /data/teamId
          url: /url
        resources:
          /data/teamId: teams
          /organizationId: organizations
        verificationProfile: linear.app/hmac-sha256-timestamp
    resources:
      issues:
        - 22222222-2222-4222-8222-222222222222
      organizations:
        - 11111111-1111-4111-8111-111111111111
      project-slugs:
        - abcdef123456
      projects:
        - 44444444-4444-4444-8444-444444444444
      team-keys:
        - ENG
      teams:
        - 33333333-3333-4333-8333-333333333333
    safe-outputs:
      add-comment:
        credential: writer
        inputs:
          body:
            maxLength: 65000
            pattern: \S
            required: true
            type: string
          issue-id:
            maxLength: 36
            pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
            required: true
            type: string
        maxResponseBytes: 131072
        request:
          endpoint: api
          graphql: |
            mutation ConnectorLinearComment($id: String!, $body: String!) {
              commentCreate(input: { issueId: $id, body: $body }) {
                success
                comment { id body issue { id } }
              }
            }
          method: POST
          path: /graphql
          query:
            body:
              input: body
            id:
              input: issue-id
        resources:
          issue-id: issues
        responseChecks:
          /data/commentCreate/success: true
        responseRequired:
          - /data/commentCreate/comment/id
        responseResources:
          /data/commentCreate/comment/issue/id: issues
      create-issue:
        credential: writer
        inputs:
          body:
            maxLength: 65000
            pattern: \S
            required: true
            type: string
          project-id:
            maxLength: 36
            pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
            required: true
            type: string
          team-id:
            maxLength: 36
            pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
            required: true
            type: string
          title:
            maxLength: 128
            pattern: \S
            required: true
            type: string
        maxResponseBytes: 16384
        request:
          endpoint: api
          graphql: |
            mutation ConnectorLinearCreate($team: String!, $project: String!, $title: String!, $body: String!) {
              issueCreate(input: { teamId: $team, projectId: $project, title: $title, description: $body }) {
                success
                issue { id identifier title team { id } project { id } }
              }
            }
          method: POST
          path: /graphql
          query:
            body:
              input: body
            project:
              input: project-id
            team:
              input: team-id
            title:
              input: title
        resources:
          project-id: projects
          team-id: teams
        responseChecks:
          /data/issueCreate/success: true
        responseRequired:
          - /data/issueCreate/issue/id
          - /data/issueCreate/issue/identifier
        responseResources:
          /data/issueCreate/issue/project/id: projects
          /data/issueCreate/issue/team/id: teams
      update-issue:
        credential: writer
        inputs:
          body:
            maxLength: 65000
            pattern: \S
            required: true
            type: string
          issue-id:
            maxLength: 36
            pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
            required: true
            type: string
          title:
            maxLength: 128
            pattern: \S
            required: true
            type: string
        maxResponseBytes: 16384
        request:
          endpoint: api
          graphql: |
            mutation ConnectorLinearUpdate($id: String!, $title: String!, $body: String!) {
              issueUpdate(id: $id, input: { title: $title, description: $body }) {
                success
                issue { id identifier title }
              }
            }
          method: POST
          path: /graphql
          query:
            body:
              input: body
            id:
              input: issue-id
            title:
              input: title
        resources:
          issue-id: issues
        responseChecks:
          /data/issueUpdate/success: true
        responseRequired:
          - /data/issueUpdate/issue/id
          - /data/issueUpdate/issue/identifier
        responseResources:
          /data/issueUpdate/issue/id: issues
    tools:
      get-issue:
        credential: reader
        inputs:
          issue-id:
            maxLength: 36
            pattern: ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
            required: true
            type: string
        maxResponseBytes: 131072
        project:
          issue: /data/issue
          issue-id: /data/issue/id
          organization-id: /data/organization/id
          team-id: /data/issue/team/id
        request:
          endpoint: api
          graphql: |
            query ConnectorLinearIssue($id: String!) {
              organization { id }
              issue(id: $id) {
                id identifier title description url
                team { id key }
                project { id }
              }
            }
          method: POST
          path: /graphql
          query:
            id:
              input: issue-id
        resources:
          issue-id: issues
        responseRequired:
          - /data/issue/id
          - /data/issue/title
        responseResources:
          /data/issue/id: issues
          /data/issue/team/id: teams
          /data/organization/id: organizations
        tool: get-issue
      resolve-project:
        credential: reader
        inputs:
          project-slug:
            maxLength: 12
            pattern: ^[0-9a-f]{12}$
            required: true
            type: string
        maxResponseBytes: 16384
        request:
          endpoint: api
          graphql: |
            query ConnectorLinearProject($slug: String!) {
              organization { id }
              projects(first: 1, filter: { slugId: { eq: $slug } }) {
                nodes { id slugId name }
                pageInfo { hasNextPage }
              }
            }
          method: POST
          path: /graphql
          query:
            slug:
              input: project-slug
        resources:
          project-slug: project-slugs
        responseChecks:
          /data/projects/pageInfo/hasNextPage: false
        responseRequired:
          - /data/projects/nodes/0/id
        responseResources:
          /data/organization/id: organizations
          /data/projects/nodes/0/id: projects
        tool: resolve-project
      resolve-team:
        credential: reader
        inputs:
          team-key:
            maxLength: 16
            pattern: ^[A-Z][A-Z0-9]{0,15}$
            required: true
            type: string
        maxResponseBytes: 16384
        request:
          endpoint: api
          graphql: |
            query ConnectorLinearTeam($key: String!) {
              organization { id }
              teams(first: 1, filter: { key: { eq: $key } }) {
                nodes { id key name }
                pageInfo { hasNextPage }
              }
            }
          method: POST
          path: /graphql
          query:
            key:
              input: team-key
        resources:
          team-key: team-keys
        responseChecks:
          /data/teams/pageInfo/hasNextPage: false
        responseRequired:
          - /data/teams/nodes/0/id
        responseResources:
          /data/organization/id: organizations
          /data/teams/nodes/0/id: teams
        tool: resolve-team
import-schema:
  mcp-reader-secret:
    default: LINEAR_READ_MCP_TOKEN
    type: string
  reader-secret:
    default: LINEAR_READ_API_KEY
    type: string
  writer-secret:
    default: LINEAR_WRITE_API_KEY
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
