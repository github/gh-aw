---
connectors:
  jira:
    credentials:
      mcp-personal:
        header: Authorization
        phase: agent
        scheme: basic
        scopes:
          - read:jira:agent-interface
          - search:jira:agent-interface
        secret: ${{ github.aw.import-inputs.mcp-personal-secret }}
      mcp-service-account:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - read:jira:agent-interface
          - search:jira:agent-interface
        secret: ${{ github.aw.import-inputs.mcp-service-account-secret }}
      rest-writer:
        header: Authorization
        phase: safe-output
        scheme: basic
        scopes:
          - write:jira-work
        secret: ${{ github.aw.import-inputs.rest-writer-secret }}
    endpoints:
      site: https://example.atlassian.net
    mcp-servers:
      personal:
        credential: mcp-personal
        maxResponseBytes: 65536
        readOnly: true
        tools:
          getJiraIssue:
            inputs:
              cloudId:
                maxLength: 80
                required: true
                type: string
              issueIdOrKey:
                maxLength: 80
                pattern: ^[A-Z][A-Z0-9_]*-[1-9][0-9]*$
                required: true
                type: string
            resources:
              cloudId: cloud-ids
              issueIdOrKey: issues
          searchJiraIssuesUsingJql:
            inputs:
              cloudId:
                maxLength: 80
                required: true
                type: string
              jql:
                maxLength: 1000
                required: true
                type: string
              maxResults:
                maximum: 25
                minimum: 1
                required: true
                type: integer
            resources:
              cloudId: cloud-ids
              jql: queries
        url: https://mcp.atlassian.com/v2/mcp
      service-account:
        credential: mcp-service-account
        maxResponseBytes: 65536
        readOnly: true
        tools:
          getJiraIssue:
            inputs:
              cloudId:
                maxLength: 80
                required: true
                type: string
              issueIdOrKey:
                maxLength: 80
                pattern: ^[A-Z][A-Z0-9_]*-[1-9][0-9]*$
                required: true
                type: string
            resources:
              cloudId: cloud-ids
              issueIdOrKey: issues
          searchJiraIssuesUsingJql:
            inputs:
              cloudId:
                maxLength: 80
                required: true
                type: string
              jql:
                maxLength: 1000
                required: true
                type: string
              maxResults:
                maximum: 25
                minimum: 1
                required: true
                type: integer
            resources:
              cloudId: cloud-ids
              jql: queries
        url: https://mcp.atlassian.com/v2/mcp
    triggers:
      issue-created:
        match:
          /issue/fields/project/key: APP
          /issue/self: https://example.atlassian.net/rest/api/3/issue/10001
          /webhookEvent: jira:issue_created
        nativeEvents:
          - jira:issue_created
        normalize:
          issue-id: /issue/id
          issue-key: /issue/key
          project: /issue/fields/project/key
          url: /issue/self
        resources:
          /issue/fields/project/key: projects
          /issue/id: issue-ids
        types:
          /issue/fields/project/key: string
          /issue/id: string
          /issue/key: string
          /issue/self: string
          /webhookEvent: string
        verificationProfile: atlassian.com/jira-admin-hmac-sha256
      issue-deleted:
        match:
          /issue/fields/project/key: APP
          /issue/self: https://example.atlassian.net/rest/api/3/issue/10001
          /webhookEvent: jira:issue_deleted
        nativeEvents:
          - jira:issue_deleted
        normalize:
          issue-id: /issue/id
          issue-key: /issue/key
          project: /issue/fields/project/key
          url: /issue/self
        resources:
          /issue/fields/project/key: projects
          /issue/id: issue-ids
        types:
          /issue/fields/project/key: string
          /issue/id: string
          /issue/key: string
          /issue/self: string
          /webhookEvent: string
        verificationProfile: atlassian.com/jira-admin-hmac-sha256
      issue-updated:
        match:
          /issue/fields/project/key: APP
          /issue/self: https://example.atlassian.net/rest/api/3/issue/10001
          /webhookEvent: jira:issue_updated
        nativeEvents:
          - jira:issue_updated
        normalize:
          issue-id: /issue/id
          issue-key: /issue/key
          project: /issue/fields/project/key
          url: /issue/self
        resources:
          /issue/fields/project/key: projects
          /issue/id: issue-ids
        types:
          /issue/fields/project/key: string
          /issue/id: string
          /issue/key: string
          /issue/self: string
          /webhookEvent: string
        verificationProfile: atlassian.com/jira-admin-hmac-sha256
    resources:
      cloud-ids:
        - 00000000-0000-4000-8000-000000000001
      issue-ids:
        - "10001"
      issue-urls:
        - https://example.atlassian.net/rest/api/3/issue/10001
      issues:
        - APP-1
      projects:
        - APP
      queries:
        - project = APP ORDER BY created DESC
    safe-outputs:
      add-comment:
        credential: rest-writer
        inputs:
          issue:
            maxLength: 80
            pattern: ^[A-Z][A-Z0-9_]*-[1-9][0-9]*$
            required: true
            type: string
          text:
            maxLength: 32767
            pattern: \S
            required: true
            type: string
        maxResponseBytes: 16384
        project:
          comment-id: /id
        request:
          body:
            body:
              encoding: jira-adf
              input: text
          endpoint: site
          method: POST
          path: /rest/api/3/issue/{issue}/comment
          pathInputs:
            issue: issue
        resources:
          issue: issues
        responseRequired:
          - /id
        responseTypes:
          /id: string
      add-label:
        credential: rest-writer
        inputs:
          issue:
            maxLength: 80
            pattern: ^[A-Z][A-Z0-9_]*-[1-9][0-9]*$
            required: true
            type: string
          label:
            maxLength: 255
            pattern: ^[A-Za-z0-9_.-]+$
            required: true
            type: string
        maxResponseBytes: 16384
        request:
          body:
            update:
              object:
                labels:
                  array:
                    - object:
                        add:
                          input: label
          endpoint: site
          method: PUT
          path: /rest/api/3/issue/{issue}
          pathInputs:
            issue: issue
        resources:
          issue: issues
      create-issue:
        credential: rest-writer
        inputs:
          description:
            maxLength: 32767
            pattern: \S
            required: true
            type: string
          issue-type:
            enum:
              - Task
            maxLength: 80
            required: true
            type: string
          project:
            maxLength: 80
            required: true
            type: string
          summary:
            maxLength: 255
            pattern: \S
            required: true
            type: string
        maxResponseBytes: 16384
        project:
          issue-id: /id
          issue-key: /key
          url: /self
        request:
          body:
            fields:
              object:
                description:
                  encoding: jira-adf
                  input: description
                issuetype:
                  object:
                    name:
                      input: issue-type
                project:
                  object:
                    key:
                      input: project
                summary:
                  input: summary
          endpoint: site
          method: POST
          path: /rest/api/3/issue
        resources:
          project: projects
        responseRequired:
          - /id
          - /key
          - /self
        responseTypes:
          /id: string
          /key: string
          /self: string
      update-issue:
        credential: rest-writer
        inputs:
          description:
            maxLength: 32767
            pattern: \S
            required: true
            type: string
          issue:
            maxLength: 80
            pattern: ^[A-Z][A-Z0-9_]*-[1-9][0-9]*$
            required: true
            type: string
          summary:
            maxLength: 255
            pattern: \S
            required: true
            type: string
        maxResponseBytes: 16384
        request:
          body:
            fields:
              object:
                description:
                  encoding: jira-adf
                  input: description
                summary:
                  input: summary
          endpoint: site
          method: PUT
          path: /rest/api/3/issue/{issue}
          pathInputs:
            issue: issue
        resources:
          issue: issues
import-schema:
  mcp-personal-secret:
    default: JIRA_MCP_PERSONAL_BASIC_AUTH
    type: string
  mcp-service-account-secret:
    default: JIRA_MCP_SERVICE_ACCOUNT_KEY
    type: string
  rest-writer-secret:
    default: JIRA_REST_BASIC_AUTH
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
