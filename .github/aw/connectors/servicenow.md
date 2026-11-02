---
connectors:
  servicenow:
    credentials:
      reader:
        header: Authorization
        phase: agent
        scheme: bearer
        scopes:
          - x_gh_aw.records.read
        secret: ${{ github.aw.import-inputs.reader-secret }}
      writer:
        header: Authorization
        phase: safe-output
        scheme: bearer
        scopes:
          - x_gh_aw.records.work-notes
        secret: ${{ github.aw.import-inputs.writer-secret }}
    endpoints:
      instance: https://example.service-now.com
    triggers:
      incident-updated:
        match:
          /event: record.updated
          /instance: https://example.service-now.com
          /table: incident
        nativeEvents:
          - record.updated
        normalize:
          delivery-id: /delivery_id
          subject: /sys_id
        resources:
          /sys_id: incidents
        types:
          /delivery_id: string
          /sys_id: string
        verificationProfile: gh-aw.local/servicenow-admin-ingress
      requested-item-updated:
        match:
          /event: record.updated
          /instance: https://example.service-now.com
          /table: sc_req_item
        nativeEvents:
          - record.updated
        normalize:
          delivery-id: /delivery_id
          subject: /sys_id
        resources:
          /sys_id: requested-items
        types:
          /delivery_id: string
          /sys_id: string
        verificationProfile: gh-aw.local/servicenow-admin-ingress
    resources:
      incidents:
        - 0123456789abcdef0123456789abcdef
      requested-items:
        - fedcba9876543210fedcba9876543210
    safe-outputs:
      append-incident-work-note:
        credential: writer
        inputs:
          note:
            maxLength: 4000
            pattern: (?s)^.*\S.*$
            required: true
            type: string
          record:
            maxLength: 32
            pattern: ^[0-9a-f]{32}$
            required: true
            type: string
        maxResponseBytes: 1024
        project:
          sys-id: /result/sys_id
        request:
          body:
            work_notes:
              input: note
          endpoint: instance
          method: PATCH
          path: /api/now/table/incident/{record}
          pathInputs:
            record: record
          query:
            sysparm_display_value:
              literal: "false"
            sysparm_exclude_reference_link:
              literal: "true"
            sysparm_fields:
              literal: sys_id
        resources:
          record: incidents
        responseInputs:
          /result/sys_id: record
        responseRequired:
          - /result/sys_id
        responseResources:
          /result/sys_id: incidents
        responseTypes:
          /result/sys_id: string
      append-requested-item-work-note:
        credential: writer
        inputs:
          note:
            maxLength: 4000
            pattern: (?s)^.*\S.*$
            required: true
            type: string
          record:
            maxLength: 32
            pattern: ^[0-9a-f]{32}$
            required: true
            type: string
        maxResponseBytes: 1024
        project:
          sys-id: /result/sys_id
        request:
          body:
            work_notes:
              input: note
          endpoint: instance
          method: PATCH
          path: /api/now/table/sc_req_item/{record}
          pathInputs:
            record: record
          query:
            sysparm_display_value:
              literal: "false"
            sysparm_exclude_reference_link:
              literal: "true"
            sysparm_fields:
              literal: sys_id
        resources:
          record: requested-items
        responseInputs:
          /result/sys_id: record
        responseRequired:
          - /result/sys_id
        responseResources:
          /result/sys_id: requested-items
        responseTypes:
          /result/sys_id: string
    tools:
      get-incident:
        credential: reader
        inputs:
          record:
            maxLength: 32
            pattern: ^[0-9a-f]{32}$
            required: true
            type: string
        maxResponseBytes: 8192
        project:
          number: /result/number
          revision: /result/sys_mod_count
          state: /result/state
          sys-id: /result/sys_id
        request:
          endpoint: instance
          method: GET
          path: /api/now/table/incident/{record}
          pathInputs:
            record: record
          query:
            sysparm_display_value:
              literal: "false"
            sysparm_exclude_reference_link:
              literal: "true"
            sysparm_fields:
              literal: sys_id,number,state,sys_mod_count
        resources:
          record: incidents
        responseInputs:
          /result/sys_id: record
        responseRequired:
          - /result/sys_id
          - /result/number
          - /result/state
          - /result/sys_mod_count
        responseResources:
          /result/sys_id: incidents
        responseTypes:
          /result/number: string
          /result/state: string
          /result/sys_id: string
          /result/sys_mod_count: string
        tool: get-incident
      get-requested-item:
        credential: reader
        inputs:
          record:
            maxLength: 32
            pattern: ^[0-9a-f]{32}$
            required: true
            type: string
        maxResponseBytes: 8192
        project:
          number: /result/number
          revision: /result/sys_mod_count
          state: /result/state
          sys-id: /result/sys_id
        request:
          endpoint: instance
          method: GET
          path: /api/now/table/sc_req_item/{record}
          pathInputs:
            record: record
          query:
            sysparm_display_value:
              literal: "false"
            sysparm_exclude_reference_link:
              literal: "true"
            sysparm_fields:
              literal: sys_id,number,state,sys_mod_count
        resources:
          record: requested-items
        responseInputs:
          /result/sys_id: record
        responseRequired:
          - /result/sys_id
          - /result/number
          - /result/state
          - /result/sys_mod_count
        responseResources:
          /result/sys_id: requested-items
        responseTypes:
          /result/number: string
          /result/state: string
          /result/sys_id: string
          /result/sys_mod_count: string
        tool: get-requested-item
import-schema:
  reader-secret:
    default: SERVICENOW_READ_TOKEN
    type: string
  writer-secret:
    default: SERVICENOW_WRITE_TOKEN
    type: string
---

Use ordinary AW imports and explicitly selected read tools and hyphen-prefixed safe outputs. Event envelopes contain references, not resource content; retrieve approved content through scoped read APIs. Native ingress is not operational.
