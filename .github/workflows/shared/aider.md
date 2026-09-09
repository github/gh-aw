---
runtimes:
  python:
    version: "3.12"
pre-agent-steps:
  - name: Preinstall Aider CLI
    run: |
      # fastuuid only ships manylinux wheels for CPython; installing it explicitly first
      # ensures pip resolves the prebuilt wheel instead of falling back to a source build
      # that would require Cargo/crates.io network access.
      python3 -m pip install --quiet --user --disable-pip-version-check --only-binary=:all: fastuuid==0.14.0
      python3 -m pip install --quiet --user --disable-pip-version-check "aider-chat==$GH_AW_ENGINE_VERSION"
      "$HOME/.local/bin/aider" --version
    env:
      AIDER_ANALYTICS_DISABLE: "true"
      AIDER_CHECK_UPDATE: "false"
engine:
  id: aider
  detection-engine: copilot
  version: "0.86.2"
  display-name: Aider
  description: Aider AI pair programming CLI running in scripting (non-interactive) mode
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - .aider.conf.yml
        - CONVENTIONS.md
      path-prefixes:
        - .aider/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
        - pypi.org
        - files.pythonhosted.org
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    config-file:
      path: .aider.conf.yml
      step-name: Write Aider Config
      content: |-
        openai-api-base: http://172.30.0.30:10002
        openai-api-key: awf-copilot-proxy
    execution:
      command-name: aider
      args:
        - --yes-always
        - --edit-format
        - diff
        - --no-auto-commits
        - --no-check-update
        - --no-show-release-notes
        - --no-detect-urls
        - --no-pretty
        - --no-stream
        - --no-fancy-input
        - --analytics-disable
        - --openai-api-base
        - http://172.30.0.30:10002
        - --set-env
        - OPENAI_BASE_URL=http://172.30.0.30:10002
        - --openai-api-key
        - awf-copilot-proxy
      step-name: Execute Aider CLI
      model-env-var: AIDER_MODEL
      model-env-provider-prefix: openai
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        AIDER_GIT: "false"
        AIDER_CHECK_UPDATE: "false"
        AIDER_ANALYTICS_DISABLE: "true"
    harness-script: |
      const { spawnSync } = require("child_process");
      const { join } = require("path");
      const { homedir } = require("os");

      const [command, ...commandArgs] = process.argv.slice(2);

      const fail = (result, output, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const detail = result.signal ? `signal ${result.signal}` : `exit code ${result.status ?? "unknown"}`;
          throw new Error(`${action} failed with ${detail}`);
        }
        if (/\blitellm\.\w*Error:/.test(output)) {
          throw new Error(`${action} reported a LiteLLM error`);
        }
      };

      const localBin = join(homedir(), ".local", "bin");
      const env = { ...process.env, PATH: `${localBin}:${process.env.PATH || ""}` };
      delete env.GITHUB_COPILOT_TOKEN;
      env.AIDER_MODEL = env.AIDER_MODEL?.replace(
        /^(openai\/claude-(?:haiku|sonnet|opus)-\d+)-(\d+)$/,
        "$1.$2"
      );

      const promptFile = process.env.GH_AW_PROMPT;
      if (!promptFile) {
        throw new Error("GH_AW_PROMPT is not set");
      }

      const result = spawnSync(command, [...commandArgs, "--message-file", promptFile], {
        encoding: "utf8",
        env,
      });
      process.stdout.write(result.stdout || "");
      process.stderr.write(result.stderr || "");
      fail(result, `${result.stdout || ""}\n${result.stderr || ""}`, "Aider execution");
---

## Aider execution constraints

Aider runs one non-interactive turn: the prompt is delivered with `--message-file` and your
single reply is the whole run. Plan for that:

- **Edit files with *SEARCH/REPLACE* blocks.** Aider applies them for you, including for new
  files (empty `SEARCH` section). Do not write source files with `cat`/heredocs.
- **Put shell commands in ```bash blocks, one complete command per line.** Aider executes each
  line separately, so multi-line commands, backslash continuations and heredocs do not work.
  Chain steps with `&&` or `;` on a single line instead.
- **Suggest at most a few commands**; they all run from the repository root.
- **Emit safe outputs through the `safeoutputs` MCP CLI**, for example
  `safeoutputs noop --message "..."`.

<!--
# Aider CLI

Shared engine definition for [Aider](https://github.com/Aider-AI/aider), the
open-source AI pair programming CLI ([docs](https://aider.chat/docs/)).
Import this file and set `engine: id: aider` to use it:

```yaml
engine:
  id: aider
model: copilot/claude-sonnet-4-5
imports:
  - shared/aider.md
```

`model` must use `provider/model` format. Supported providers are `copilot`,
`anthropic`, and `openai`. Requests are routed through the AWF proxy, so the
model name is rewritten to Aider's `openai/<model>` LiteLLM form and the
generated `.aider.conf.yml` configures the OpenAI-compatible proxy endpoint.
Copilot Claude aliases such as `claude-sonnet-4-5` are normalized to the dotted
model IDs exposed by the proxy, such as `claude-sonnet-4.5`.

Aider runs in scripting mode: the generated prompt file is passed with
`--message-file` and all confirmations are auto-accepted (`--yes-always`).
The edit format is pinned to `diff` (the editblock coder) because the proxied
model names are unknown to Aider and would otherwise fall back to the `whole`
format, which rejects ```bash blocks and cannot run shell commands.
Aider reports some LiteLLM request failures with exit code 0, so the harness
also detects those errors in its output and fails the workflow.
Aider has no MCP client, so the compiler exposes MCP-backed tools through
`cli-proxy` and GitHub access through `gh-proxy`. Both proxies are enabled
automatically and cannot be disabled for this engine.
-->
