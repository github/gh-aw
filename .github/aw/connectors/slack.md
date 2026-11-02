---
connectors:
  slack:
    credentials:
      reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - channels:history
        secret: ${{ github.aw.import-inputs.reader-secret }}
      writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - chat:write
        secret: ${{ github.aw.import-inputs.writer-secret }}
    endpoints:
      api: https://slack.com
    triggers:
      threaded-app-mention:
        match:
          /api_app_id: A123ABC456
          /event/channel: C123ABC456
          /event/type: app_mention
          /team_id: T123ABC456
          /type: event_callback
        nativeEvents:
          - app_mention
        normalize:
          author-id: /event/user
          channel-id: /event/channel
          delivery-id: /event_id
          message-id: /event/ts
          thread-id: /event/thread_ts
          workspace-id: /team_id
        resources:
          /api_app_id: apps
          /event/channel: channels
          /event/thread_ts: threads
          /team_id: workspaces
        types:
          /event/thread_ts: string
          /event/ts: string
        verificationProfile: slack.com/events-v0
    resources:
      apps:
        - A123ABC456
      channels:
        - C123ABC456
      threads:
        - "1515449522.000016"
      workspaces:
        - T123ABC456
    safe-outputs:
      reply-in-thread:
        credential: writer
        inputs:
          channel:
            enum:
              - C123ABC456
            maxLength: 80
            required: true
            type: string
          text:
            maxLength: 4000
            required: true
            type: string
          thread:
            maxLength: 40
            pattern: ^[0-9]{10,20}\.[0-9]{6}$
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          channel: /channel
          message-id: /ts
          thread-id: /message/thread_ts
        request:
          body:
            channel:
              input: channel
            mrkdwn:
              literal: false
            parse:
              literal: none
            reply_broadcast:
              literal: false
            text:
              input: text
            thread_ts:
              input: thread
            unfurl_links:
              literal: false
            unfurl_media:
              literal: false
          endpoint: api
          method: POST
          path: /api/chat.postMessage
        resources:
          channel: channels
          thread: threads
        responseChecks:
          /ok: true
        responseInputs:
          /channel: channel
          /message/thread_ts: thread
        responseRequired:
          - /channel
          - /ts
          - /message/thread_ts
        responseResources:
          /channel: channels
          /message/thread_ts: threads
        responseTypes:
          /channel: string
          /message/thread_ts: string
          /ts: string
    tools:
      read-thread:
        credential: reader
        inputs:
          channel:
            enum:
              - C123ABC456
            maxLength: 80
            required: true
            type: string
          thread:
            maxLength: 40
            pattern: ^[0-9]{10,20}\.[0-9]{6}$
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          has-more: /has_more
          messages: /messages
        request:
          endpoint: api
          method: GET
          path: /api/conversations.replies
          query:
            channel:
              input: channel
            limit:
              literal: 15
            ts:
              input: thread
        resources:
          channel: channels
          thread: threads
        responseChecks:
          /has_more: false
          /ok: true
        responseInputs:
          /messages/0/ts: thread
        responseRequired:
          - /messages/0/ts
        responseResources:
          /messages/0/ts: threads
        responseTypes:
          /has_more: boolean
          /messages/0/ts: string
        tool: read-thread
import-schema:
  reader-secret:
    default: SLACK_READ_TOKEN
    type: string
  writer-secret:
    default: SLACK_WRITE_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
