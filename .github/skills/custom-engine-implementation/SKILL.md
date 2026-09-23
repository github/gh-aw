---
name: custom-engine-implementation
description: Add and test declarative behavior-defined agentic engines in gh-aw, extending Go infrastructure only when necessary.
---

# Custom Engine Implementation

Use this skill when adding a new agentic engine definition or extending the
behavior-defined engine infrastructure.

## Choose the smallest implementation

Prefer a shared Markdown engine definition. Installation, execution, MCP
configuration, provider routing, caching, manifests, plugins, network defaults,
harness scripts, and log parsing can already be declared through
`engine.behaviors`.

Only change Go code when the engine needs a reusable behavior that the
declarative model cannot express. Add a dedicated Go engine only when the
shared behavior-defined runtime cannot run the engine at all.

| Need | Implementation |
|---|---|
| Existing behaviors are sufficient | Add an imported shared engine definition |
| A reusable declarative behavior is missing | Extend the definition model and behavior-defined runtime |
| The shared runtime is fundamentally unsuitable | Add a dedicated Go engine and register it |

The relevant implementation is in:

- `pkg/workflow/engine_definition.go` for the definition model and catalog
- `pkg/workflow/behavior_defined_engine.go` for the shared runtime
- `pkg/workflow/engine_definition_loader.go` for embedded built-in definitions
- `pkg/workflow/agentic_engine.go` for dedicated Go engine registration
- `pkg/parser/schemas/main_workflow_schema.json` for the workflow schema

## Study representative engines

Read the closest examples before making changes:

- `.github/workflows/shared/opencode.md`: npm installation, merged
  configuration, MCP, provider routing, and log parsing
- `.github/workflows/shared/aider.md`: Python installation and a custom harness
  without native MCP
- `.github/workflows/shared/crush.md`: native MCP configuration adapter and
  harness
- `.github/workflows/shared/cursor.md`: plugin support
- `.github/workflows/shared/deepseek-harness.md`: provider endpoint discovery
  and a headless profile

Use the examples to identify a pattern, not as a reason to copy optional
behaviors.

## Add a shared engine definition

1. Create `.github/workflows/shared/<engine>.md`.
2. Declare `engine.id`, `display-name`, `description`, `experimental`, provider
   metadata, authentication, and only the required `behaviors`.
3. Pin a default CLI version. Make installation deterministic and provide a
   verification command.
4. Document setup, authentication, model syntax, MCP support, and limitations
   in the Markdown body. Keep in-repository integrations clearly identified as
   unsupported samples.
5. Add the engine ID and shared definition path to
   `.github/aw/engines.json`. Imported behavior-defined engines are registered
   dynamically; do not add them to `NewEngineRegistry()`.
6. Add `.github/workflows/smoke-<engine>.md`, following the closest existing
   smoke workflow. Import the shared definition and exercise the capabilities
   the engine claims to support.
7. Update the unsupported sample table in
   `docs/src/content/docs/reference/engines.md`.
8. Add a changeset when the engine is a user-visible repository sample. New
   experimental engines have precedent as a `minor` change.
9. Run `make recompile` and include the generated smoke workflow
   `.lock.yml`.

`TestKnownEngineImportsFile_MatchesSharedEngineFiles` in
`pkg/workflow/engine_definition_test.go` enforces catalog coverage for shared
external engines.

## Design the definition

Keep the definition declarative and minimal:

- Select the correct package manager, package name, binary name, version, and
  verification command under `behaviors.installation`.
- Put invocation arguments and non-secret environment variables under
  `behaviors.execution`.
- Use an existing secret strategy and provider environment mode where
  possible.
- Declare native MCP support only when the CLI can consume the generated
  configuration. Otherwise use a harness or the gh-aw CLI proxy pattern.
- Add only required default domains and map provider-specific domains under
  `behaviors.network.provider-domains`.
- Declare manifest files, cache paths, and plugin support only when the CLI
  consumes them.
- Add a log parser only when its emitted event format can be tested.

Treat inline JavaScript in harnesses, configuration adapters, and log parsers
as production code. Avoid shell interpolation, validate paths and child-process
arguments, preserve nonzero exit codes, and never print secret values. Enabling
package lifecycle scripts requires a pinned version and an explicit reason.

For log and error regular expressions, also load
`.github/skills/error-pattern-safety/SKILL.md`.

## Extend declarative infrastructure

When an engine exposes a generally useful capability that existing behaviors
cannot express:

1. Add the smallest field to the types in
   `pkg/workflow/engine_definition.go`.
2. Validate and render it in the focused behavior-defined engine files.
3. Update `pkg/parser/schemas/main_workflow_schema.json`.
4. Add focused tests for parsing, validation, and rendered execution behavior.
5. Run `make generate-schema-docs` when generated schema documentation changes.

Do not add a schema field for a behavior that can be represented by existing
installation, execution, environment, harness, or adapter fields.

For a dedicated Go runtime, implement the engine interfaces by composing the
existing helpers, keep engine-specific code in its own files, and register the
engine in `pkg/workflow/agentic_engine.go`. Cover configuration, commands,
environment, authentication, tools, logs, and failure handling with focused
tests.

## Test the right layers

Use tests that match the changed behavior:

| Area | Tests and examples |
|---|---|
| Harness, configuration, MCP, and environment | `behavior_defined_engine_harness_test.go` |
| Log parsing | `behavior_defined_engine_log_parser_test.go` |
| Cache behavior | `behavior_defined_engine_cache_test.go` |
| Definitions, catalog, and known imports | `engine_definition_test.go`, `engine_catalog_test.go` |
| Embedded definitions | `engine_definition_loader_test.go` |
| Real shared workflow harness | `aider_workflow_test.go` |
| Sandboxed CLI visibility | `cloud_hypervisor_test.go` |
| Generated smoke workflows | `compiled_lock_files_test.go` |

Test both definition parsing and the generated installation/execution steps.
Include negative cases for unsafe paths, invalid configuration, missing
credentials, or malformed logs when relevant.

## Validate

After Go changes:

```bash
make build
make fmt
```

After workflow Markdown changes:

```bash
make recompile
```

Run focused `go test` commands for the changed behavior while iterating. Before
an intermediate progress report, run:

```bash
make agent-report-progress-no-test
```

Before the final progress report, run:

```bash
make agent-report-progress
```

Use `make agent-finish` for the final repository validation when time allows.
Do not trigger a smoke workflow from a Copilot cloud agent run.

## Completion checklist

- The declarative path was preferred unless its limitations are documented.
- The engine definition uses a pinned, verified installation.
- Authentication, provider/model handling, MCP, and network access are covered.
- Harnesses and adapters do not expose secrets or interpolate untrusted input.
- `.github/aw/engines.json`, smoke workflow, generated lock file,
  documentation, and changeset are updated when applicable.
- Focused tests cover parsing and rendered runtime behavior.
- Repository validation passes.
