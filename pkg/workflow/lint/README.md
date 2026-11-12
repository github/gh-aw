# lint Package

> Extensible, read-only linter for authored AW and native Actions ASTs.

## Overview

Rules inspect parsed workflow ASTs and return structured diagnostics. The runner does not mutate the input. Rules can target the authored `aw` document, the compiled native `actions` workflow, or both; each diagnostic identifies a source path and may include related locations.

## Public API

| Symbol | Description |
|--------|-------------|
| `Input` | Supplies an authored document, a compiled workflow, or both, with source and analysis context. |
| `Rule` | Rule identifier, description, severity, applicable AST targets, and check function. |
| `Diagnostic`, `RelatedLocation` | Structured, value-safe lint findings and associated source locations. |
| `Runner`, `New` | Validates and constructs a runner from custom rules. |
| `Runner.Rules` | Returns a copy of the runner's registered rules. |
| `Runner.Select` | Creates a runner containing selected rule IDs; unknown IDs are errors. |
| `Runner.Run` | Runs applicable rules against an input and returns diagnostics. |

Rule constructors in this package provide built-in checks for expression, permission, checkout, network, artifact, grader, and other workflow policies. Diagnostics are intended to identify source locations without including secret values.

### Built-in Rules

`Default()` returns the built-in runner. The following exported constructors
cover high-impact policy areas:

| Policy area | Rule constructors |
|-------------|-------------------|
| Expressions and secrets | `TemplateInjectionRule`, `PromptExpressionRule`, `SecretLeakRule`, `OTLPSecretLeakRule` |
| Actions and workflow integrity | `ActionPinRule`, `VersionCheckRule`, `JobTimeoutRule`, `NeedsConsistencyRule`, `StepReferenceRule` |
| Permissions and execution | `PermissionScopeRule`, `ExcessivePermissionsRule`, `PrivilegedCheckoutRule`, `CheckoutModeRule`, `ForkAssociationRule` |
| Network and outputs | `NetworkScopeRule`, `SafeOutputShellExpansionRule`, `PushTargetRule`, `AllowedLabelsRule`, `ArtifactExecutionRule` |
| Graders and custom jobs | `GraderFileIORule`, `GraderShellFileWriteRule`, `CustomJobDetectionRule` |
