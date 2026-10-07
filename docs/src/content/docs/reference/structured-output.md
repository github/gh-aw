---
title: Structured Output
description: Constrain an agent's primary response to a JSON Schema and consume validated JSON in downstream jobs.
sidebar:
  order: 315
---

The optional `structured-output` frontmatter field constrains the agent's **primary response**, independently of [safe outputs](/gh-aw/reference/safe-outputs/) used for side effects. The compiler accepts it only for engines whose actual runtime supports a native JSON Schema output mechanism. A JSON log format, a model provider's API capability, or instructions to "respond in JSON" do not establish engine support.

## Frontmatter

Specify exactly one of `schema` or `schema-file` in the main workflow. Shared workflow imports cannot declare this field.

```aw wrap
on: workflow_dispatch
engine: codex
structured-output:
  schema:
    type: object
    properties:
      decision:
        type: string
        enum: [APPROVE, ESCALATE]
      reasoning:
        type: string
    required: [decision, reasoning]
    additionalProperties: false
```

For a larger schema, use a repository-relative JSON file:

```aw wrap
structured-output:
  schema-file: .github/schemas/decision.schema.json
```

The file is resolved within the workflow's repository, including symlinks, and embedded into the compiled workflow. Paths escaping the repository are rejected. The runtime does not load the schema from a potentially untrusted PR checkout. Recompile after changing the schema file.

The schema must declare `type: object` at its root and fit within 64 KiB of compact JSON for native CLI invocation. JSON Schema draft-07 is the default; draft 2020-12 can be selected with `$schema: https://json-schema.org/draft/2020-12/schema`. Local `$ref` fragments such as `#/$defs/decision` are allowed. External schema references and GitHub Actions expressions in schemas are rejected. Engines and their model providers can impose additional restrictions on their native schema dialect.

## Native engine support

Support is opt-in for the verified execution paths below. Custom commands, harnesses, or conflicting output flags cannot bypass the native integration.

| Engine | Supported execution path | Native mechanism and evidence |
|--------|--------------------------|-------------------------------|
| Codex | Built-in harness, CLI >=0.132.0; verified at 0.159.3 | [`--output-schema` and `--output-last-message`](https://github.com/openai/codex/blob/rust-v0.159.3/codex-rs/exec/src/cli.rs); [schema-preserving resume](https://github.com/openai/codex/commit/af6ffb6ebb4c1aad5793bb1eff4ea1ac785cd22d) permits a constrained correction. |
| Claude Code | Built-in harness, CLI >=2.1.205; verified at 2.1.288 | [`--json-schema` and `structured_output`](https://code.claude.com/docs/en/headless#get-structured-output). [Draft-07 only](https://code.claude.com/docs/en/agent-sdk/structured-outputs); native retries are capped and the correction resumes the same session. |
| Copilot | Built-in SDK mode only, CLI >=1.0.90 and SDK 1.0.16 | SDK [`responseSchema`](https://github.com/github/copilot-sdk/blob/v1.0.16/nodejs/src/session.ts), not CLI event formatting. Custom drivers, ordinary CLI mode, and engine autopilot are unsupported. |
| Gemini | Built-in Gemini CLI 0.62.0 path, compatible Gemini-3 model | Native [generation settings](https://github.com/google-gemini/gemini-cli/blob/v0.62.0/docs/cli/generation-settings.md) pass the schema into [primary-chat generation](https://github.com/google-gemini/gemini-cli/blob/v0.62.0/packages/core/src/core/geminiChat.ts#L955-L962), using the pinned SDK's [`responseJsonSchema`](https://github.com/googleapis/js-genai/blob/v1.30.0/src/types.ts#L2178-L2193). |
| Goose | Imported Goose definition, verified at 1.53.0 | Recipe [`response.json_schema`](https://github.com/aaif-goose/goose/blob/v1.53.0/documentation/docs/guides/recipes/recipe-reference.md#response), native [`recipe__final_output` validation](https://github.com/aaif-goose/goose/blob/v1.53.0/crates/goose/src/agents/final_output_tool.rs), and [terminal enforcement](https://github.com/aaif-goose/goose/blob/v1.53.0/crates/goose/src/agents/state_machine/ops_recipe.rs). |

For Copilot, explicitly select SDK mode and a supported CLI version:

```aw wrap
engine:
  id: copilot
  version: 1.0.90
  copilot-sdk: true
```

Provider restrictions still apply. [OpenAI strict schemas](https://developers.openai.com/api/docs/guides/structured-outputs#supported-schemas) require every object property to be required and every object to set `additionalProperties: false`; use nullable types for optional values. Claude permits optional fields, but its [native schema dialect](https://platform.claude.com/docs/en/build-with-claude/structured-outputs#json-schema-limitations) is narrower than general draft-07. Gemini rejects unsupported keywords, recursive or anchor references, and conflicting model hooks/settings before execution. No integration rewrites an unsupported schema into a weaker prompt constraint.

The remaining configured runtimes stay disabled:

| Runtime inspected | Reason |
|-------------------|--------|
| Pi 1.0.0 | [`text`, `json`, and `rpc` modes](https://github.com/earendil-works/pi-mono/blob/v1.0.0/packages/coding-agent/src/cli/args.ts) serialize events; no primary-response schema entry point. |
| Aider 0.86.2 | [Editing protocols](https://github.com/Aider-AI/aider/blob/v0.86.2/aider/args.py) and [provider parameter passthrough](https://github.com/Aider-AI/aider/blob/v0.86.2/aider/models.py) do not establish native primary-response support in the configured runtime. |
| Crush 0.88.0 | [`run`](https://github.com/charmbracelet/crush/blob/v0.88.0/internal/cmd/run.go) has no response schema option; [`schema`](https://github.com/charmbracelet/crush/blob/v0.88.0/internal/cmd/schema.go) describes configuration. |
| Cursor 2026.07.20-8cc9c0b | Documented [JSON output](https://cursor.com/docs/cli/reference/output-format) is an envelope. No verified schema interface; proprietary pinned source is unavailable. |
| DeepSeek Harness 0.2.0-rc.2 | Pinned [headless arguments](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/apps/cli/src/args.ts) expose events and unrestricted text, not a response schema. |
| Kiro 2.27.1 | No schema interface in the [documented CLI](https://kiro.dev/docs/reference/cli-commands/); proprietary pinned source is unavailable. |
| OpenCode 1.18.33 | Configured [`run` CLI](https://github.com/anomalyco/opencode/blob/v1.18.33/packages/opencode/src/cli/cmd/run.ts) exposes event formatting only. The separate [native structured tool](https://github.com/anomalyco/opencode/blob/v1.18.33/packages/opencode/src/session/prompt.ts) omits a validator, so its [pinned SDK accepts unvalidated values](https://github.com/vercel/ai/blob/ai%406.0.168/packages/provider-utils/src/validate-types.ts). |
| Pydantic harness 0.54.0 / slim >=2.44.0 | The configured [CLI](https://github.com/pydantic/pydantic-ai/blob/v2.44.0/pydantic_ai_slim/pydantic_ai/_cli/__init__.py) defaults to text; [`StructuredDict`](https://github.com/pydantic/pydantic-ai/blob/v2.44.0/pydantic_ai_slim/pydantic_ai/output.py) does not validate the declared schema. Library `NativeOutput` support alone does not enable this runtime. |

Cursor and Kiro evidence describes their current public documentation, not a source snapshot of the proprietary pins. Pydantic's imported definition is upstream-managed and does not fully pin its dependency graph. `github-*` workflow/provider aliases inherit the underlying engine's support; they are not additional runtimes.

## Downstream access

GitHub Actions outputs are strings, not nested objects. `needs.agent.outputs.structured` contains compact, validated JSON. Access a field with `fromJSON(needs.agent.outputs.structured).decision`, or pass the complete value through an environment variable:

```aw wrap
jobs:
  route:
    needs: agent
    runs-on: ubuntu-latest
    steps:
      - name: Read decision
        env:
          STRUCTURED: ${{ needs.agent.outputs.structured }}
        run: |
          printf '%s' "$STRUCTURED" | jq -er '.decision'
```

Do not interpolate an agent-produced field directly into a shell command. Schema validation constrains structure; it does not make strings trusted shell code.

The compact response is limited to **256 KiB**, measured in both UTF-8 and UTF-16 to leave room for other GitHub Actions job outputs. Oversized responses fail rather than being truncated or silently replaced. After successful validation and redaction, the response and schema also appear as `structured-output.json` and `structured-output-schema.json` in the `agent` artifact. Failed responses are not published through that artifact.

Responses use JavaScript's finite JSON number range. Use strings for large integer identifiers that must retain exact precision; overflowing numbers fail rather than being serialized as `null`.

For a reusable workflow, explicitly expose the JSON string:

```aw wrap
on:
  workflow_call:
    outputs:
      structured:
        description: Validated primary response
        value: ${{ jobs.agent.outputs.structured }}
```

## Execution and failure contract

The compiler validates the schema before generating the workflow. Unsupported engines or incompatible execution modes, including samples replay, cause a compile-time error; no prompt-only fallback is generated.

The schema is passed through the engine's native schema mechanism. The engine extracts the primary JSON response, not its event-stream envelope, and validates it. A schema-invalid response receives at most one correction attempt with the same native constraint. Existing transport-error retry policies remain separate.

Missing responses, malformed JSON, refusals, exhausted attempts, schema violations, failed agent execution, and failed secret redaction prevent publication. The host validates the redacted response against the original compiler-provided schema before setting the job output. The agent-editable schema file is not trusted for this final check. Redaction can itself invalidate a schema, in which case the workflow fails rather than publishing changed data under the original contract.

Structured output does not replace tools or safe outputs: a workflow can perform normal tool calls and declare both response constraints and safe side effects.

## Engine integration contract

`WorkflowData.StructuredOutput` contains the resolved `StructuredOutputConfig.Schema`. `EngineCapabilities.StructuredOutput` defaults to `false` and must be explicitly enabled only for a verified native implementation. An engine can implement `StructuredOutputConfigValidator.ValidateStructuredOutputConfig(*EngineConfig)` to reject configurations that bypass its native schema handling.

The compiler prepares `/tmp/gh-aw/structured-output-schema.json` before engine execution. The engine writes the raw JSON response to `/tmp/gh-aw/structured-output.json`, validates before accepting a response, and preserves its existing behavior when structured output is absent. The compiler owns final post-redaction validation, the `structured` job output, output size limits, and diagnostic artifact collection.
