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
        url: https://mcp.dev.azure.com/${{ github.aw.import-inputs.organization }}
    triggers:
      work-item-created:
        match:
          /eventType: workitem.created
          /resourceContainers/collection/id: ${{ github.aw.import-inputs.collection }}
          /resourceContainers/project/id: ${{ github.aw.import-inputs.project }}
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
          /resourceContainers/collection/id: ${{ github.aw.import-inputs.collection }}
          /resourceContainers/project/id: ${{ github.aw.import-inputs.project }}
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
          /resourceContainers/collection/id: ${{ github.aw.import-inputs.collection }}
          /resourceContainers/project/id: ${{ github.aw.import-inputs.project }}
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
          /resourceContainers/collection/id: ${{ github.aw.import-inputs.collection }}
          /resourceContainers/project/id: ${{ github.aw.import-inputs.project }}
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
        - ${{ github.aw.import-inputs.assignee }}
      collections:
        - ${{ github.aw.import-inputs.collection }}
      organizations:
        - ${{ github.aw.import-inputs.organization }}
      project-names:
        - ${{ github.aw.import-inputs.project-name }}
      projects:
        - ${{ github.aw.import-inputs.project }}
      work-items:
        - "${{ github.aw.import-inputs.work-item }}"
        - "${{ github.aw.import-inputs.related-work-item }}"
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
              - ${{ github.aw.import-inputs.organization }}
            maxLength: 64
            required: true
            type: string
          project:
            enum:
              - ${{ github.aw.import-inputs.project }}
            maxLength: 36
            required: true
            type: string
          work-item:
            enum:
              - "${{ github.aw.import-inputs.work-item }}"
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
                  url: https://dev.azure.com/${{ github.aw.import-inputs.organization }}/${{ github.aw.import-inputs.project }}/_apis/wit/workItems/${{ github.aw.import-inputs.related-work-item }}
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
  organization:
    default: fabrikam
    type: string
    description: Approved Azure DevOps organization name, not a URL.
  collection:
    default: 11111111-1111-4111-8111-111111111111
    type: string
    description: Approved collection ID for nonoperational trigger declarations.
  project:
    default: 22222222-2222-4222-8222-222222222222
    type: string
    description: Approved project UUID for write targets.
  project-name:
    default: Fabrikam
    type: string
    description: Exact project name required in write responses.
  work-item:
    default: "123"
    type: string
    description: Exact approved existing work item ID; no wildcard or temporary ID.
  related-work-item:
    default: "124"
    type: string
    description: Exact approved target ID for the fixed Related link.
  assignee:
    default: reviewer@example.invalid
    type: string
    description: Exact approved assignee identity.
  agent-reader-secret:
    default: AZURE_DEVOPS_MCP_ENTRA_TOKEN
    type: string
  writer-secret:
    default: AZURE_DEVOPS_WRITE_TOKEN
    type: string
---

Select capabilities explicitly after importing "@builtin:connectors/azure-devops.md".
Review the specimen organization, collection, project, work item, and assignee
defaults; override them with trusted literal import inputs before use.
Read MCP exposes only core_list_projects with an Entra access token, not work item
bodies or a PAT fallback. Native ingress is not operational.
