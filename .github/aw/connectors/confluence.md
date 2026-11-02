---
connectors:
  confluence:
    credentials:
      comment-writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - write:comment:confluence
        secret: ${{ github.aw.import-inputs.comment-writer-secret }}
      page-writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - write:page:confluence
        secret: ${{ github.aw.import-inputs.page-writer-secret }}
      reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - read:page:confluence
        secret: ${{ github.aw.import-inputs.reader-secret }}
    endpoints:
      api: https://api.atlassian.com
    "on":
      page-created:
        match:
          /content/status: current
          /content/type: page
          /eventType: avi:confluence:created:page
        nativeEvents:
          - avi:confluence:created:page
        normalize:
          space: /content/space/id
          subject: /content/id
          version: /content/version/number
        resources:
          /content/id: pages
          /content/space/id: spaces
        types:
          /content/id: string
          /content/space/id: integer
          /content/title: string
          /content/version/number: integer
        verificationProfile: atlassian.com/forge-invocation-token
      page-updated:
        match:
          /content/status: current
          /content/type: page
          /eventType: avi:confluence:updated:page
        nativeEvents:
          - avi:confluence:updated:page
        normalize:
          space: /content/space/id
          subject: /content/id
          version: /content/version/number
        resources:
          /content/id: pages
          /content/space/id: spaces
        types:
          /content/id: string
          /content/space/id: integer
          /content/title: string
          /content/version/number: integer
        verificationProfile: atlassian.com/forge-invocation-token
    resources:
      clouds:
        - 11111111-2222-4333-8444-555555555555
      pages:
        - "456"
      spaces:
        - "123"
    safe-outputs:
      add-review-comment:
        credential: comment-writer
        inputs:
          cloud-id:
            maxLength: 36
            required: true
            type: string
          page-id:
            maxLength: 20
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          body:
            body:
              literal:
                representation: storage
                value: <p>Review the workflow result before taking action.</p>
            pageId:
              input: page-id
          endpoint: api
          method: POST
          path: /ex/confluence/{cloud}/wiki/api/v2/footer-comments
          pathInputs:
            cloud: cloud-id
        resources:
          cloud-id: clouds
          page-id: pages
        responseInputs:
          /pageId: page-id
        responseRequired:
          - /id
          - /version/number
        responseResources:
          /pageId: pages
        responseTypes:
          /id: string
          /version/number: integer
      create-runbook:
        credential: page-writer
        inputs:
          cloud-id:
            maxLength: 36
            required: true
            type: string
          parent-id:
            maxLength: 20
            required: true
            type: string
          space-id:
            maxLength: 20
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          body:
            body:
              literal:
                representation: storage
                value: <p>Review the workflow result before taking action.</p>
            parentId:
              input: parent-id
            spaceId:
              input: space-id
            status:
              literal: current
            title:
              literal: Agentic workflow runbook
          endpoint: api
          method: POST
          path: /ex/confluence/{cloud}/wiki/api/v2/pages
          pathInputs:
            cloud: cloud-id
        resources:
          cloud-id: clouds
          parent-id: pages
          space-id: spaces
        responseInputs:
          /parentId: parent-id
          /spaceId: space-id
        responseRequired:
          - /id
          - /version/number
        responseResources:
          /spaceId: spaces
        responseTypes:
          /id: string
          /version/number: integer
    tools:
      get-page:
        credential: reader
        inputs:
          cloud-id:
            maxLength: 36
            required: true
            type: string
          page-id:
            maxLength: 20
            required: true
            type: string
          space-id:
            maxLength: 20
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          content: /results/0/body/storage/value
          page: /results/0/id
          representation: /results/0/body/storage/representation
          space: /results/0/spaceId
          title: /results/0/title
          version: /results/0/version/number
        request:
          endpoint: api
          method: GET
          path: /ex/confluence/{cloud}/wiki/api/v2/pages
          pathInputs:
            cloud: cloud-id
          query:
            body-format:
              literal: storage
            id:
              input: page-id
            limit:
              literal: 1
            space-id:
              input: space-id
            status:
              literal: current
        resources:
          cloud-id: clouds
          page-id: pages
          space-id: spaces
        responseChecks:
          /results/0/body/storage/representation: storage
          /results/0/status: current
        responseInputs:
          /results/0/id: page-id
          /results/0/spaceId: space-id
        responseRequired:
          - /results/0/version/number
          - /results/0/body/storage/value
        responseResources:
          /results/0/id: pages
          /results/0/spaceId: spaces
        responseTypes:
          /results/0/body/storage/value: string
          /results/0/id: string
          /results/0/spaceId: string
          /results/0/title: string
          /results/0/version/number: integer
        tool: get-confluence-page
import-schema:
  comment-writer-secret:
    default: CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN
    type: string
  page-writer-secret:
    default: CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN
    type: string
  reader-secret:
    default: CONFLUENCE_READ_ACCESS_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
