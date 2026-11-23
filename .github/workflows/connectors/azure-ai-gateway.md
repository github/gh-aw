---
connectors:
  azure-ai-gateway:
    experimental: true
    docs:
      - https://ai.gateway.azure.com/docs/model-access
      - https://ai.gateway.azure.com/docs/security-identity
      - https://ai.gateway.azure.com/docs/models/api-formats-and-providers
    endpoints:
      models: "https://${{ github.aw.import-inputs.gateway-host }}"
    credentials:
      entra:
        phase: inference
        header: Authorization
        scheme: bearer
        scopes: [https://ai.gateway.azure.com/.default]
        azure-oidc:
          azure-tenant-id: "${{ github.aw.import-inputs.azure-tenant-id }}"
          azure-client-id: "${{ github.aw.import-inputs.azure-client-id }}"
          azure-scope: https://ai.gateway.azure.com/.default
      gateway-key:
        phase: inference
        header: Authorization
        scheme: bearer
        scopes: [Gateway model runtime access]
        token:
          secret: "${{ github.aw.import-inputs.key-secret }}"
    resources:
      models: ${{ github.aw.import-inputs.models }}
    inference:
      responses: &responses
        endpoint: models
        path: /default/models/openai/v1
        protocol: openai-responses
        credential: entra
        models: models
      completions:
        <<: *responses
        protocol: openai-chat-completions
      responses-key:
        <<: *responses
        credential: gateway-key
      completions-key:
        <<: *responses
        protocol: openai-chat-completions
        credential: gateway-key
import-schema:
  gateway-host:
    type: string
    default: example.ai.gateway.azure.com
    description: Exact gateway hostname from the Catalog SDK endpoint, without scheme or path; replace the specimen before execution.
  models:
    type: array
    items:
      type: string
    default: []
    description: Exact approved gateway wire model aliases; no wildcard or discovered models.
  azure-tenant-id:
    type: string
    default: "${{ vars.AZURE_TENANT_ID }}"
    description: Entra tenant ID or Actions variable expression for the selected Entra inference profile.
  azure-client-id:
    type: string
    default: "${{ vars.AZURE_CLIENT_ID }}"
    description: Entra application client ID or Actions variable expression for the selected Entra inference profile.
  key-secret:
    type: string
    default: AZURE_AI_GATEWAY_KEY
    description: Actions secret NAME containing a gateway key, used only by a selected key profile; never bind the key value.
---
<!--
<connector-documentation id="azure-ai-gateway" version="1">
  <purpose>Configure the workflow's Copilot inference through Azure AI Gateway. This is not a callable inference tool, MCP registration, model discovery service, or provider mutation.</purpose>
  <credentials>
    <credential name="entra" phase="inference">Configure an Entra application's federated identity to trust the GitHub Actions issuer, exact repository branch or environment subject, and api://AzureADTokenExchange audience. Grant the application's service principal runtime access to the approved gateway model assets. Bind azure-tenant-id and azure-client-id; the scope is https://ai.gateway.azure.com/.default. The workflow must grant id-token: write. The existing AWF inference proxy exchanges and refreshes tokens outside the agent; this requires the existing engine OIDC firewall/version safeguards. No stored gateway key is used by this profile.</credential>
    <credential name="gateway-key" phase="inference">Obtain a gateway API key through the gateway operator and store it as the Actions secret named by key-secret (default AZURE_AI_GATEWAY_KEY). Rotate it externally. Gateway keys use Authorization: Bearer, not the Azure OpenAI api-key header, and may grant broader access than the workflow's exact model selection. No Entra exchange or OIDC permission is needed by a key profile.</credential>
  </credentials>
  <inputs>gateway-host identifies the exact public-cloud SDK endpoint. models grants exact wire aliases. The remaining bindings configure credential routes, not secret values. Importing selects nothing. The consumer must select engine.inference with connector, profile and model and explicitly approve the hostname in network.allowed. engine.model supplies Copilot model metadata and can differ from the granted wire alias. The gateway has no /models route, so set engine.model-catalog: false to pass engine.model verbatim instead of resolving it as a gh-aw alias.</inputs>
  <limits>Only the Copilot engine and OpenAI-compatible Responses or Chat Completions are supported initially. Use responses or responses-key only when the alias supports Responses; completions or completions-key selects Chat Completions. This declaration does not cover custom endpoint paths, sovereign deployments, Anthropic Messages, embeddings, model discovery, routing or fallback. Configure MCP separately. Compile/setup do not verify token validity, model compatibility, pricing, context limits, RBAC or output quality. Do not retry with another credential after denial.</limits>
  <privacy>Gateway inference receives workflow prompts, selected context and tool results. Staging suppresses the GitHub effect, not inference, disclosure or Azure charges. Review that disclosure before any execution. The connector adds no network approval, readers, writer credentials or ingress.</privacy>
  <consumer>
    <frontmatter><![CDATA[
name: Azure AI Gateway deployment checklist
on: workflow_dispatch
engine:
  id: copilot
  model: gpt-5.2
  model-catalog: false
  inference:
    connector: azure-ai-gateway
    profile: responses
    model: deployment-checklist
permissions:
  contents: read
  id-token: write
network:
  allowed: [defaults, example.ai.gateway.azure.com, login.microsoftonline.com]
imports:
  - uses: ".github/workflows/connectors/azure-ai-gateway.md"
    with:
      gateway-host: example.ai.gateway.azure.com
      models: [deployment-checklist]
tools:
  github: false
  bash: false
  cli-proxy: false
  edit: false
safe-outputs:
  staged: true
  create-issue:
    max: 1
]]></frontmatter>
    <prompt><![CDATA[
Draft a short deployment checklist for a service that uses Azure AI Gateway.
Stage one GitHub issue with a factual title of at least 20 characters. Cover
model protocol compatibility, exact model access, token trust, disclosure and
cost review. Do not claim to have inspected a deployment or verified Azure
access. Do not request secrets, call other models, or perform live probes.
State that the issue is staged, not delivered.
]]></prompt>
  </consumer>
  <sources>
    <source href="https://ai.gateway.azure.com/docs/model-access" />
    <source href="https://ai.gateway.azure.com/docs/security-identity" />
    <source href="https://ai.gateway.azure.com/docs/models/api-formats-and-providers" />
  </sources>
</connector-documentation>
-->
