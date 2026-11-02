---
connectors:
  tracker:
    credentials:
      reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - issues:read
        secret: ${{ github.aw.import-inputs.reader-secret }}
      writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - comments:write
        secret: ${{ github.aw.import-inputs.writer-secret }}
    endpoints:
      api: https://api.example.com
    triggers:
      issue-created:
        match:
          /event: issue.created
        nativeEvents:
          - issue.created
        normalize:
          subject: /issue/id
        verificationProfile: example.com/hmac-sha256
    resources:
      issues:
        - ${{ github.aw.import-inputs.issue }}
    safe-outputs:
      add-comment:
        credential: writer
        inputs:
          issue:
            maxLength: 80
            required: true
            type: string
          text:
            maxLength: 4000
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          body:
            body:
              input: text
          endpoint: api
          method: POST
          path: /issues/{issue}/comments
          pathInputs:
            issue: issue
        resources:
          issue: issues
    tools:
      get-issue:
        credential: reader
        inputs:
          issue:
            maxLength: 80
            required: true
            type: string
        maxResponseBytes: 65536
        request:
          endpoint: api
          method: GET
          path: /issues/{issue}
          pathInputs:
            issue: issue
        resources:
          issue: issues
        tool: get-issue
import-schema:
  issue:
    default: ISSUE-1
    type: string
  reader-secret:
    default: EXAMPLE_READ_TOKEN
    type: string
  writer-secret:
    default: EXAMPLE_WRITE_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
