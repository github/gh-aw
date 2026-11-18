# intent Package

> Intent attribution and resolution utilities for mapping pull requests and issues to labelled intent records.

## Overview

The `intent` package resolves the intent behind a pull request or issue by inspecting labels, closing issues, and explicit metadata. It produces `IntentRecord` values that classify attribution status and source, allowing downstream consumers to route work items or report on coverage.

Resolution is performed by a `Resolver`, which holds a label matcher function and a version string. The resolver applies a priority chain:

1. Explicit intent metadata (`PullRequestData.ExplicitIntent`) — used as-is.
2. A single closing issue — resolved from the issue's labels.
3. PR labels — used as an artifact fallback when no closing issues are present.
4. No supported sources — returns an `AttributionUnlinked` record.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `AttributionStatus` | string | Classifies the outcome of intent attribution |
| `AttributionSource` | string | Identifies the data source used for attribution |
| `Authorizer` | struct | Checks tool calls against a compiled execution policy; not yet enforced in the execution path, see `ExecutionPolicy` |
| `ExecutionPolicy` | struct | Compiled policy containing autonomy, tool restrictions, write scope, checks, approval, auto-merge, attempt, and rule-ID settings |
| `ErrToolDenied` | error | Returned by `AuthorizeTool` when the tool is in `DeniedTools` |
| `ErrToolNotAllowed` | error | Returned by `AuthorizeTool` when `AllowedTools` is non-nil and does not contain the tool |
| `IntentRecord` | struct | Holds the attribution result for a pull request or issue, including optional `Domains`, `Priority`, and `Risk` classification fields |
| `PolicyCompiler` | struct | Applies policy rules to mapped intent records |
| `RootReference` | struct | Represents a referenced issue or artifact root (node ID, type, URL, labels) |
| `PullRequestData` | struct | Input data for pull request resolution (node ID, URL, labels, explicit intent, closing issues) |
| `RepositoryContext` | struct | Repository owner, name, visibility, and organization used when matching policy rules |
| `Resolver` | struct | Stateless resolver that maps labels to intent records |
| `PolicyRule` | struct | Pairs a match condition with the execution policy fragment to apply |
| `PolicyCondition` | struct | Describes the intent labels and repository organization context that make a policy rule match |

### AttributionStatus constants

| Constant | Value | Description |
|----------|-------|-------------|
| `AttributionMapped` | `"mapped"` | Labels matched a known intent category |
| `AttributionUnmapped` | `"unmapped"` | Labels were present but matched no category |
| `AttributionUnlinked` | `"unlinked"` | No supported intent source was found |
| `AttributionAmbiguous` | `"ambiguous"` | Multiple competing sources were found |
| `AttributionSuggested` | `"suggested"` | Attribution was inferred by suggestion (not confirmed) |

### AttributionSource constants

| Constant | Value | Description |
|----------|-------|-------------|
| `SourceExplicitMetadata` | `"explicit_metadata"` | Explicitly provided intent metadata |
| `SourceClosingIssue` | `"closing_issue"` | Derived from a closing issue |
| `SourceParentIssue` | `"parent_issue"` | Derived from a parent issue |
| `SourceReferencedIssue` | `"referenced_issue"` | Derived from a referenced issue |
| `SourceProject` | `"project"` | Derived from a project assignment |
| `SourceMilestone` | `"milestone"` | Derived from a milestone assignment |
| `SourceIssueLabels` | `"issue_labels"` | Derived from labels on an issue |
| `SourceArtifactLabels` | `"artifact_labels"` | Derived from labels on the artifact (pull request) |
| `SourceSuggestion` | `"suggestion"` | Derived from a suggestion |
| `SourceNone` | `"none"` | No source was used |

### Resolver methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `ResolvePullRequest` | `func (r Resolver) ResolvePullRequest(pr PullRequestData) IntentRecord` | Resolves intent for a pull request using explicit intent, closing issues, or PR labels |
| `ResolveIssue` | `func (r Resolver) ResolveIssue(nodeID, url string, labels []string) IntentRecord` | Resolves intent for an issue using its labels |

### Policy compiler methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Compile` | `func (c PolicyCompiler) Compile(rec IntentRecord, repo RepositoryContext) ExecutionPolicy` | Compiles an `IntentRecord` and repository context into an execution policy |

### Policy rule types

`PolicyRule` configures a single policy fragment. Its `ID` identifies the matched rule in compiled policy output, `Scope` records the rule level (`"organization"`, `"repository"`, `"intent"`, or `"workflow"`), `When` holds the match criteria, and `Set` holds the `ExecutionPolicy` fields to merge when the rule applies.

`PolicyCondition` matches rule criteria against an `IntentRecord` and `RepositoryContext`. Empty condition fields act as wildcards. `Domain`, `Priority`, and `Risk` match against `IntentRecord.Labels` (not the dedicated `Domains`/`Priority`/`Risk` fields below, which are used only by `ResolveRisk`); `Org` matches either the repository organization or owner.

### Risk classification

| Function | Signature | Description |
|----------|-----------|-------------|
| `ResolveRisk` | `func ResolveRisk(rec IntentRecord) string` | Returns `rec.Risk` when set; otherwise derives a risk level from `rec.Domains`/`rec.Priority`: `security`+`critical` and `production` resolve to `"high"`, `infrastructure` to `"medium"`, `documentation` to `"low"`, and anything else to `"unknown"` |

### Tool authorization

| Type/Method | Signature | Description |
|-------------|-----------|-------------|
| `AuthorizeTool` | `func (a Authorizer) AuthorizeTool(policy ExecutionPolicy, tool string) error` | Checks tool calls against a compiled `ExecutionPolicy`; not yet enforced in the execution path, see `ExecutionPolicy`. Returns `ErrToolDenied` if `tool` is in `DeniedTools` (denial always wins), `ErrToolNotAllowed` if `AllowedTools` is non-nil and does not contain `tool`, or `nil` otherwise. A `nil` `AllowedTools` is unrestricted; a non-nil empty `AllowedTools` denies every tool. |

## Usage Examples

```go
resolver := intent.Resolver{
    ResolverVersion: "v1",
    MatchLabels: func(labels []string) []string {
        for _, l := range labels {
            if l == "security" {
                return []string{l}
            }
        }
        return nil
    },
}

record := resolver.ResolvePullRequest(intent.PullRequestData{
    NodeID: "PR_kwDOAAABCD4",
    URL:    "https://github.com/owner/repo/pull/42",
    Labels: []string{"security"},
})

fmt.Println(record.Status) // "mapped"
fmt.Println(record.Source) // "artifact_labels"
```

### Resolving an issue

```go
record := resolver.ResolveIssue(
    "I_kwDOAAABCQ4",
    "https://github.com/owner/repo/issues/1",
    []string{"security"},
)

fmt.Println(record.Status) // "mapped"
fmt.Println(record.Source) // "issue_labels"
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging for resolver and policy evaluation

**External**:
- None beyond the Go standard library (`slices`).

## Thread Safety

`Resolver` holds no mutable state and is safe for concurrent use. `IntentRecord` values are returned by value and do not share mutable state with the caller.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
