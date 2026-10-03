---
engine:
  id: opencode
  detection-engine: copilot
  version: "1.2.14"
  display-name: OpenCode
  description: OpenCode CLI with headless mode and multi-provider LLM support
  runtime-id: opencode
  experimental: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    capabilities:
      max-turns: true
    manifest:
      files:
        - opencode.jsonc
        - AGENTS.md
      path-prefixes:
        - .opencode/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - opencode.ai
        - models.dev
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
        google: generativelanguage.googleapis.com
        groq: api.groq.com
        mistral: api.mistral.ai
        deepseek: api.deepseek.com
        xai: api.x.ai
    installation:
      package-manager: npm
      package-name: opencode-ai
      step-name: Install OpenCode CLI
      binary-name: opencode
      include-node-setup: true
      cooldown: true
      verify-command: opencode --version
      verify-step-name: Verify OpenCode CLI installation
      docs-url: https://opencode.ai/docs
    config-file:
      path: opencode.jsonc
      step-name: Write OpenCode Config
      content: |-
        {
          "agent": {
            "build": {
              "permission": {
                "bash": "allow",
                "edit": "allow",
                "read": "allow",
                "glob": "allow",
                "grep": "allow",
                "webfetch": "allow",
                "websearch": "allow",
                "external_directory": "allow"
              }
            }
          },
          "autoupdate": false,
          "disabled_providers": ["opencode", "openai"],
          "provider": {
            "awf-proxy": {
              "api": "http://172.30.0.30:10002",
              "options": {
                "apiKey": "awf-copilot-proxy"
              },
              "models": {
                "claude-sonnet-4.5": {}
              }
            }
          }
        }
      merge-strategy: json-merge
    execution:
      command-name: opencode
      args:
        - run
        - --format
        - json
        - --print-logs
        - --log-level
        - DEBUG
      step-name: Execute OpenCode CLI
      model-env-var: OPENCODE_MODEL
      model-env-provider-prefix: awf-proxy
      mcp-config-env-var: GH_AW_MCP_CONFIG
      write-timestamp: true
      provider-env-mode: universal-llm-consumer
      env:
        XDG_DATA_HOME: /tmp/opencode-data
    mcp:
      config-path: opencode.jsonc
    log-parser: |
      function parseLog(logContent) {
        return require("./parse_opencode_log.cjs").parseOpenCodeLog(logContent);
      }
---

<!--
# OpenCode CLI

Shared engine definition for the [OpenCode](https://opencode.ai) multi-provider AI
coding agent (BYOK). Import this file and set `engine: opencode` to use it:

```yaml
engine:
  id: opencode
model: copilot/auto
imports:
  - shared/opencode.md
```

`model` must use `provider/model` format. Supported providers are `copilot`,
`anthropic`, `openai`, and `codex`.

Execution uses `run --format json`. The shared OpenCode adapter maps emitted
text, reasoning, tool parts, step accounting, and provider errors to canonical
agent-session events. Debug/startup lines are not assistant messages. The CLI
does not emit session initialization or a session turn count; those values
remain unavailable rather than being inferred from text or tool counts.
-->
