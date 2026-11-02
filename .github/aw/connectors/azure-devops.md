---
connectors:
  azure-devops:
    credentials:
      agent-reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - vso.project
        secret: ${{ github.aw.import-inputs.agent-reader-secret }}
      writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - vso.work_write
        secret: ${{ github.aw.import-inputs.writer-secret }}
    endpoints:
      api: https://dev.azure.com
    mcp-servers:
      azure-devops:
        credential: agent-reader
        headers:
          X-MCP-Readonly: "true"
          X-MCP-Tools: core_list_projects
        maxResponseBytes: 65536
        readOnly: true
        tools:
          core_list_projects:
            inputs: {}
        url: https://mcp.dev.azure.com/fabrikam
    triggers:
      work-item-created:
        match:
          /eventType: workitem.created
          /resourceContainers/collection/id: 11111111-1111-4111-8111-111111111111
          /resourceContainers/project/id: 22222222-2222-4222-8222-222222222222
        nativeEvents:
          - workitem.created
        normalize:
          collection: /resourceContainers/collection/id
          project: /resourceContainers/project/id
          subject: /resource/id
        resources:
          /resource/id: work-items
          /resourceContainers/collection/id: collections
          /resourceContainers/project/id: projects
        verificationProfile: dev.azure.com/configured-https-auth
      work-item-deleted:
        match:
          /eventType: workitem.deleted
          /resourceContainers/collection/id: 11111111-1111-4111-8111-111111111111
          /resourceContainers/project/id: 22222222-2222-4222-8222-222222222222
        nativeEvents:
          - workitem.deleted
        normalize:
          collection: /resourceContainers/collection/id
          project: /resourceContainers/project/id
          subject: /resource/id
        resources:
          /resource/id: work-items
          /resourceContainers/collection/id: collections
          /resourceContainers/project/id: projects
        verificationProfile: dev.azure.com/configured-https-auth
      work-item-restored:
        match:
          /eventType: workitem.restored
          /resourceContainers/collection/id: 11111111-1111-4111-8111-111111111111
          /resourceContainers/project/id: 22222222-2222-4222-8222-222222222222
        nativeEvents:
          - workitem.restored
        normalize:
          collection: /resourceContainers/collection/id
          project: /resourceContainers/project/id
          subject: /resource/id
        resources:
          /resource/id: work-items
          /resourceContainers/collection/id: collections
          /resourceContainers/project/id: projects
        verificationProfile: dev.azure.com/configured-https-auth
      work-item-updated:
        match:
          /eventType: workitem.updated
          /resourceContainers/collection/id: 11111111-1111-4111-8111-111111111111
          /resourceContainers/project/id: 22222222-2222-4222-8222-222222222222
        nativeEvents:
          - workitem.updated
        normalize:
          collection: /resourceContainers/collection/id
          project: /resourceContainers/project/id
          subject: /resource/workItemId
        resources:
          /resource/workItemId: work-items
          /resourceContainers/collection/id: collections
          /resourceContainers/project/id: projects
        verificationProfile: dev.azure.com/configured-https-auth
    resources:
      assignees:
        - reviewer@example.invalid
      collections:
        - 11111111-1111-4111-8111-111111111111
      organizations:
        - fabrikam
      project-names:
        - Fabrikam
      projects:
        - 22222222-2222-4222-8222-222222222222
      work-items:
        - "123"
        - "124"
    safe-outputs:
      assign-work-item:
        credential: writer
        inputs:
          assignee:
            maxLength: 256
            required: true
            type: string
          organization:
            maxLength: 64
            pattern: ^[A-Za-z0-9][A-Za-z0-9-]*$
            required: true
            type: string
          project:
            maxLength: 36
            pattern: ^[0-9a-f-]{36}$
            required: true
            type: string
          work-item:
            maxLength: 10
            pattern: ^[1-9][0-9]*$
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          endpoint: api
          method: PATCH
          patch:
            - op: add
              path: /fields/System.AssignedTo
              value:
                input: assignee
          path: /{organization}/{project}/_apis/wit/workitems/{work-item}
          pathInputs:
            organization: organization
            project: project
            work-item: work-item
          query:
            api-version:
              literal: "7.1"
        resources:
          assignee: assignees
          organization: organizations
          project: projects
          work-item: work-items
        responseRequired:
          - /id
          - /fields/System.TeamProject
        responseResources:
          /fields/System.TeamProject: project-names
      comment-on-work-item:
        credential: writer
        inputs:
          organization:
            maxLength: 64
            pattern: ^[A-Za-z0-9][A-Za-z0-9-]*$
            required: true
            type: string
          project:
            maxLength: 36
            pattern: ^[0-9a-f-]{36}$
            required: true
            type: string
          text:
            maxLength: 4000
            required: true
            type: string
          work-item:
            maxLength: 10
            pattern: ^[1-9][0-9]*$
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          work-item: /workItemId
        request:
          body:
            text:
              input: text
          endpoint: api
          method: POST
          path: /{organization}/{project}/_apis/wit/workItems/{work-item}/comments
          pathInputs:
            organization: organization
            project: project
            work-item: work-item
          query:
            api-version:
              literal: 7.1-preview.4
            format:
              literal: Markdown
        resources:
          organization: organizations
          project: projects
          work-item: work-items
        responseRequired:
          - /workItemId
      create-work-item:
        credential: writer
        inputs:
          organization:
            maxLength: 64
            pattern: ^[A-Za-z0-9][A-Za-z0-9-]*$
            required: true
            type: string
          project:
            maxLength: 36
            pattern: ^[0-9a-f-]{36}$
            required: true
            type: string
          title:
            maxLength: 256
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          work-item: /id
        request:
          endpoint: api
          method: POST
          patch:
            - op: add
              path: /fields/System.Title
              value:
                input: title
          path: /{organization}/{project}/_apis/wit/workitems/$Task
          pathInputs:
            organization: organization
            project: project
          query:
            api-version:
              literal: "7.1"
        resources:
          organization: organizations
          project: projects
        responseRequired:
          - /id
          - /fields/System.TeamProject
        responseResources:
          /fields/System.TeamProject: project-names
      link-work-items:
        credential: writer
        inputs:
          organization:
            enum:
              - fabrikam
            maxLength: 64
            required: true
            type: string
          project:
            enum:
              - 22222222-2222-4222-8222-222222222222
            maxLength: 36
            required: true
            type: string
          work-item:
            enum:
              - "123"
            maxLength: 10
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          endpoint: api
          method: PATCH
          patch:
            - op: add
              path: /relations/-
              value:
                literal:
                  rel: System.LinkTypes.Related
                  url: https://dev.azure.com/fabrikam/22222222-2222-4222-8222-222222222222/_apis/wit/workItems/124
          path: /{organization}/{project}/_apis/wit/workitems/{work-item}
          pathInputs:
            organization: organization
            project: project
            work-item: work-item
          query:
            api-version:
              literal: "7.1"
        resources:
          organization: organizations
          project: projects
          work-item: work-items
        responseRequired:
          - /id
          - /fields/System.TeamProject
        responseResources:
          /fields/System.TeamProject: project-names
      update-work-item:
        credential: writer
        inputs:
          organization:
            maxLength: 64
            pattern: ^[A-Za-z0-9][A-Za-z0-9-]*$
            required: true
            type: string
          project:
            maxLength: 36
            pattern: ^[0-9a-f-]{36}$
            required: true
            type: string
          title:
            maxLength: 256
            required: true
            type: string
          work-item:
            maxLength: 10
            pattern: ^[1-9][0-9]*$
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          endpoint: api
          method: PATCH
          patch:
            - op: add
              path: /fields/System.Title
              value:
                input: title
          path: /{organization}/{project}/_apis/wit/workitems/{work-item}
          pathInputs:
            organization: organization
            project: project
            work-item: work-item
          query:
            api-version:
              literal: "7.1"
        resources:
          organization: organizations
          project: projects
          work-item: work-items
        responseRequired:
          - /id
          - /fields/System.TeamProject
        responseResources:
          /fields/System.TeamProject: project-names
import-schema:
  agent-reader-secret:
    default: AZURE_DEVOPS_MCP_ENTRA_TOKEN
    type: string
  writer-secret:
    default: AZURE_DEVOPS_WRITE_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
