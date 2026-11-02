---
connectors:
  teams:
    credentials:
      bot-writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - https://api.botframework.com/.default
        secret: ${{ github.aw.import-inputs.bot-writer-secret }}
      reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - ChannelMessage.Read.Group
        secret: ${{ github.aw.import-inputs.reader-secret }}
    endpoints:
      bot: https://smba.trafficmanager.net
      graph: https://graph.microsoft.com
    "on":
      channel-message-created:
        match:
          /changeType: created
          /resourceData/@odata.type: '#Microsoft.Graph.chatMessage'
        nativeEvents:
          - created
        normalize:
          resource: /resource
          subject: /resourceData/id
          subscription: /subscriptionId
          tenant: /tenantId
        resources:
          /resource: notification-resources
          /resourceData/id: threads
          /subscriptionId: subscriptions
          /tenantId: tenants
        verificationProfile: microsoft.com/graph-basic-channel-notification
    resources:
      activities:
        - "1730000000000"
      channels:
        - 19:approved-channel@thread.tacv2
      conversations:
        - 19:approved-channel@thread.tacv2;messageid=1730000000000
      notification-resources:
        - teams('11111111-1111-4111-8111-111111111111')/channels('19:approved-channel@thread.tacv2')/messages('1730000000000')
      subscriptions:
        - 22222222-2222-4222-8222-222222222222
      teams:
        - 11111111-1111-4111-8111-111111111111
      tenants:
        - 33333333-3333-4333-8333-333333333333
      threads:
        - "1730000000000"
    safe-outputs:
      reply-to-activity:
        credential: bot-writer
        inputs:
          activity:
            maxLength: 128
            required: true
            type: string
          conversation:
            maxLength: 512
            required: true
            type: string
          text:
            maxLength: 4000
            required: true
            type: string
        maxResponseBytes: 16384
        project:
          id: /id
        request:
          body:
            replyToId:
              input: activity
            text:
              input: text
            type:
              literal: message
          endpoint: bot
          method: POST
          path: /teams/v3/conversations/{conversation}/activities/{activity}
          pathInputs:
            activity: activity
            conversation: conversation
        resources:
          activity: activities
          conversation: conversations
        responseRequired:
          - /id
      reply-with-review-card:
        credential: bot-writer
        inputs:
          activity:
            maxLength: 128
            required: true
            type: string
          conversation:
            maxLength: 512
            required: true
            type: string
          text:
            maxLength: 4000
            required: true
            type: string
        maxResponseBytes: 16384
        project:
          id: /id
        request:
          body:
            attachments:
              literal:
                - content:
                    $schema: http://adaptivecards.io/schemas/adaptive-card.json
                    body:
                      - text: Agentic workflow review
                        type: TextBlock
                        wrap: true
                    type: AdaptiveCard
                    version: "1.2"
                  contentType: application/vnd.microsoft.card.adaptive
            replyToId:
              input: activity
            text:
              input: text
            type:
              literal: message
          endpoint: bot
          method: POST
          path: /teams/v3/conversations/{conversation}/activities/{activity}
          pathInputs:
            activity: activity
            conversation: conversation
        resources:
          activity: activities
          conversation: conversations
        responseRequired:
          - /id
    tools:
      get-channel-message:
        credential: reader
        inputs:
          channel:
            maxLength: 256
            required: true
            type: string
          team:
            maxLength: 36
            required: true
            type: string
          thread:
            maxLength: 128
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          author: /from/user/id
          channel: /channelIdentity/channelId
          content-type: /body/contentType
          id: /id
          team: /channelIdentity/teamId
          text: /body/content
        request:
          endpoint: graph
          method: GET
          path: /v1.0/teams/{team}/channels/{channel}/messages/{thread}
          pathInputs:
            channel: channel
            team: team
            thread: thread
        resources:
          channel: channels
          team: teams
          thread: threads
        responseChecks:
          /messageType: message
          /replyToId: null
        responseRequired:
          - /id
          - /from/user/id
          - /body/content
        responseResources:
          /channelIdentity/channelId: channels
          /channelIdentity/teamId: teams
          /id: threads
        tool: get-channel-message
      list-channel-replies:
        credential: reader
        inputs:
          channel:
            maxLength: 256
            required: true
            type: string
          team:
            maxLength: 36
            required: true
            type: string
          thread:
            maxLength: 128
            required: true
            type: string
        maxResponseBytes: 65536
        project:
          replies: /value
        request:
          endpoint: graph
          method: GET
          path: /v1.0/teams/{team}/channels/{channel}/messages/{thread}/replies
          pathInputs:
            channel: channel
            team: team
            thread: thread
          query:
            $top:
              literal: 20
        resources:
          channel: channels
          team: teams
          thread: threads
        responseRequired:
          - /value
        tool: list-channel-replies
import-schema:
  bot-writer-secret:
    default: TEAMS_BOT_ACCESS_TOKEN
    type: string
  reader-secret:
    default: TEAMS_GRAPH_READ_ACCESS_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
