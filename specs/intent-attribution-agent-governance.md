---

title: Intent Attribution & Agent Governance Specification
version: 2.0.0
status: Partially Implemented
date: 2026-06-09
last_updated: 2026-11-03
replaces: objective-mapping-portfolio-reporting.md
---

# Intent Attribution & Agent Governance Specification

## Summary

This specification defines a deterministic intent layer for agentic GitHub workflows.

The system connects GitHub work to structured context such as:

* priority
* domain
* initiative
* risk
* root issue

That context can be used for two purposes:

1. **Attribution and reporting** — explain what work an artifact supported and how reliably that relationship is known
2. **Workflow governance** — determine what an agent may do, which checks are required, and whether human approval is necessary

The central principle is:

> **Intent determines authority. Execution produces evidence.**

The system does not claim that GitHub labels or merged pull requests prove business impact, ROI, or realized customer value.

## Conformance

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### RFC 2119 Norms

#### Attribution-Resolution Order

An implementation MUST resolve attribution in the following precedence order:

1. Explicit intent metadata attached to the artifact (`ExplicitIntent` on the pull request).
2. A single closing issue linked to the pull request.
3. Pull request labels used as an artifact-label fallback when no closing issue is present.

An implementation MUST NOT skip earlier sources in favor of later sources unless the earlier source is unavailable or explicitly absent.

An implementation MUST NOT mix sources across precedence levels for a single attribution record. Each record MUST be attributed to exactly one source.

#### Ambiguous-Root Handling

An implementation MUST produce an `ambiguous` attribution when two or more distinct closing issues are linked to the same pull request and no explicit intent override is present.

An implementation MUST NOT resolve ambiguity by arbitrary selection (e.g., first, last, or random issue).

An ambiguous attribution MUST be recorded with `status: "ambiguous"` and `source: "closing_issue"`.

An ambiguous attribution MUST NOT be treated as equivalent to a mapped attribution for reporting or authorization purposes.

#### Fail-Closed Behavior

An implementation MUST apply the safest available execution policy unless attribution status is authoritatively `mapped`. This includes `unlinked`, `ambiguous`, `unmapped`, `suggested`, empty, and unknown statuses.

The safest available policy MUST be: autonomy `propose_only`, write scope `none`, `human_approval_required: true`, `auto_merge_allowed: false`, `max_attempts: 1`.

An implementation MUST NOT grant elevated authority based on absent or unresolved attribution.

A policy decision MUST be deterministic: given identical attribution inputs, the same policy MUST always be produced.

#### Compliance Fixtures

The minimum conformance fixture set for these RFC 2119 norms is tracked in
`specs/intent-attribution-compliance/`:

- `specs/intent-attribution-compliance/explicit-intent-wins.yaml`
- `specs/intent-attribution-compliance/ambiguous-root-closing-issues.yaml`
- `specs/intent-attribution-compliance/unlinked-pr-fail-closed.yaml`


## Current implementation

Implementation status, operational sync guidance, and the runtime enforcement map are maintained in the [implementation companion](intent-attribution-agent-governance-implementation.md#current-implementation). This heading remains as a stable reference anchor; normative safeguards remain below.

### Safeguards

When governance policy resolution fails because `.github/objective-mapping.json`,
`.github/intent-policy.json`, or equivalent policy inputs are missing, malformed,
or produce no deterministic match, the implementation MUST fail closed to the
safest policy defined in **Fail-Closed Behavior** above.

An implementation MUST NOT silently reuse a stale cached policy decision when the
current repository policy inputs cannot be resolved. The failure reason and the
fallback to safe policy decision SHOULD be recorded in execution provenance.

### Sync Notes

See the [implementation companion's sync notes](intent-attribution-agent-governance-implementation.md#sync-notes). This heading remains as a stable reference anchor.

### Non-normative maintenance split proposal

This split was executed on 2026-11-09. Normative requirements remain in this specification; the companion document contains implementation and operational details, and the existing heading anchors are retained here.

## Product boundary

The system can establish:

* what GitHub artifact was produced
* whether the artifact was accepted, rejected, or remains pending
* which explicit GitHub relationship connected it to a root object
* which configured labels were found
* which deterministic rule was applied
* which workflow policy should govern the work
* which checks and approvals were required
* which execution evidence was recorded

The system does not independently establish:

* financial value
* ROI
* employee productivity
* customer value
* strategic success
* causal business impact

Those claims require separate evidence.

## Core model

```text
Intent
  ↓
Attribution
  ↓
Risk classification
  ↓
Policy compilation
  ↓
Agent execution
  ↓
GitHub artifact
  ↓
Outcome evaluation
  ↓
Evidence
```

The model distinguishes five concepts.

### Intent

Why the work exists and what context applies.

Examples:

* critical
* security
* authentication modernization
* documentation
* production incident

### Attribution

How the intent was connected to the work.

Examples:

* explicit workflow metadata
* closing issue
* parent issue
* direct issue labels
* pull request labels

### Policy

What the agent is permitted and required to do.

Examples:

* propose only
* supervised execution
* bounded autonomous execution
* required security tests
* mandatory human approval
* auto-merge prohibited

### Outcome

What observable GitHub state occurred.

Examples:

* pull request merged
* pull request closed without merge
* issue completed
* work still pending

### Evidence

What proves that the required process occurred.

Examples:

* test results
* review approval
* policy decision
* workflow trace
* GitHub artifact state

## Design principles

### Deterministic authority

Official attribution and workflow authorization must be derived from:

* explicit GitHub metadata
* repository or organization configuration
* deterministic precedence rules

LLMs may propose classifications or relationships, but suggestions must not affect official authorization or reporting until confirmed.

### Unknown is not zero

Missing attribution must not be represented as zero importance.

```json
{
  "status": "unlinked",
  "weight": null
}
```

A zero value means an explicit configured value of zero.

A null value means unknown or unavailable.

### Fail closed

Unknown, invalid, or ambiguous intent must result in the safest applicable workflow policy.

### Provenance is required

Every attribution and policy decision must explain:

* the source
* the rule
* the root object
* the configuration version
* any overrides

### Artifacts are not objectives

A pull request is an execution artifact.

An issue or explicitly declared objective represents intended work.

Multiple pull requests connected to one issue must not automatically multiply the number of completed objectives.

## Intent configuration

The initial implementation continues to support:

```text
.github/objective-mapping.json
```

A future migration may introduce:

```text
.github/intent-policy.json
```

### Compatibility configuration

```json
{
  "label_to_value": {
    "critical": 100,
    "p0": 100,
    "p1": 50,
    "security": 40
  },
  "multi_label_logic": "max",
  "priority_labels": [
    "critical",
    "p0",
    "p1"
  ]
}
```

Existing numeric values are interpreted as **relative weights**, not financial value or verified impact.

### Target configuration

```json
{
  "version": 1,
  "labels": {
    "critical": {
      "dimension": "priority",
      "value": "critical",
      "weight": 100
    },
    "p1": {
      "dimension": "priority",
      "value": "high",
      "weight": 50
    },
    "security": {
      "dimension": "domain",
      "value": "security"
    },
    "documentation": {
      "dimension": "domain",
      "value": "documentation"
    },
    "high-risk": {
      "dimension": "risk",
      "value": "high"
    },
    "auth-modernization": {
      "dimension": "initiative",
      "value": "auth-modernization"
    }
  },
  "scoring": {
    "dimension": "priority",
    "strategy": "max"
  },
  "attribution": {
    "multiple_roots": "ambiguous",
    "allow_artifact_label_fallback": true
  }
}
```

Separating dimensions prevents initiatives, priorities, domains, and risk labels from competing in one flat scoring calculation.

Only the configured scoring dimension contributes to weight.

### `.github/intent-policy.json` Schema

The future `.github/intent-policy.json` configuration file supersedes `.github/objective-mapping.json` for repositories that require policy governance in addition to attribution.

**Top-level fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | integer | Yes | Schema version. Current stable value: `1`. |
| `labels` | object | Yes | Map of GitHub label name → label descriptor (see below). |
| `scoring` | object | No | Scoring strategy for weighted attribution reporting. |
| `attribution` | object | No | Attribution-resolution behaviour overrides. |
| `rules` | array | No | Ordered policy rules compiled into an `ExecutionPolicy`. |

**`labels` descriptor fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `dimension` | string | Yes | One of `"priority"`, `"domain"`, `"risk"`, `"initiative"`. Controls which axis the label belongs to. |
| `value` | string | Yes | Canonical value for this label within its dimension (e.g., `"critical"`, `"security"`). |
| `weight` | integer | No | Numeric weight for scoring. Only meaningful on the active scoring dimension. |

**`scoring` fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `dimension` | string | Yes | The dimension used for weight computation (e.g., `"priority"`). |
| `strategy` | string | Yes | One of `"max"` (use the highest weight found) or `"sum"` (add all weights). |

**`attribution` fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `multiple_roots` | string | No | Behaviour when multiple closing issues are found. `"ambiguous"` (default) or `"first"`. MUST be `"ambiguous"` for governance use. |
| `allow_artifact_label_fallback` | boolean | No | Whether PR labels may be used when no closing issue is present. Default: `true`. |

**`rules` array element fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Stable unique identifier for the rule (e.g., `"security-critical"`). Used in policy decision provenance. |
| `scope` | string | No | Hint for evaluation ordering: `"organization"`, `"repository"`, `"intent"`, or `"workflow"`. Rules MUST be listed from highest to lowest precedence. |
| `when` | object | No | Match conditions. All specified fields must match. An empty `when` matches all intents. |
| `set` | object | Yes | `ExecutionPolicy` fragment to merge when the rule matches. |

**`when` match condition fields:**

| Field | Type | Description |
|-------|------|-------------|
| `domain` | string | Match if any label with `dimension: "domain"` has this value. |
| `priority` | string | Match if any label with `dimension: "priority"` has this value. |
| `risk` | string | Match if any label with `dimension: "risk"` has this value. |
| `org` | string | Match if the repository's owner or org equals this value. |

**`set` ExecutionPolicy fragment fields:**

| Field | Type | Description |
|-------|------|-------------|
| `autonomy` | string | `"propose_only"`, `"supervised"`, or `"bounded"`. |
| `write_scope` | string | `"none"`, `"feature_branch"`, or `"any_branch"`. |
| `allowed_tools` | array of string | Tool names the agent may call. Empty means unrestricted. |
| `denied_tools` | array of string | Tool names the agent must never call. Union with higher-precedence denials. |
| `required_checks` | array of string | Check names that must pass before completion. Union with higher-precedence checks. |
| `human_approval_required` | boolean | Whether a human must approve before the agent proceeds with write operations. |
| `auto_merge_allowed` | boolean | Whether the agent may auto-merge pull requests after checks pass. |
| `max_attempts` | integer | Maximum number of times the agent may retry the workflow. |

**Example `.github/intent-policy.json`:**

```json
{
  "version": 1,
  "labels": {
    "critical": { "dimension": "priority", "value": "critical", "weight": 100 },
    "p1":       { "dimension": "priority", "value": "high",     "weight": 50  },
    "security": { "dimension": "domain",   "value": "security"                },
    "documentation": { "dimension": "domain", "value": "documentation"        }
  },
  "scoring": { "dimension": "priority", "strategy": "max" },
  "attribution": { "multiple_roots": "ambiguous", "allow_artifact_label_fallback": true },
  "rules": [
    {
      "id": "security-critical",
      "scope": "intent",
      "when": { "domain": "security", "priority": "critical" },
      "set": {
        "autonomy": "supervised",
        "human_approval_required": true,
        "auto_merge_allowed": false,
        "required_checks": ["unit-tests", "security-tests"],
        "max_attempts": 2
      }
    },
    {
      "id": "documentation-safe",
      "scope": "intent",
      "when": { "domain": "documentation" },
      "set": {
        "autonomy": "bounded",
        "write_scope": "feature_branch",
        "human_approval_required": false,
        "auto_merge_allowed": true,
        "required_checks": ["docs-build"],
        "max_attempts": 3
      }
    }
  ]
}
```


```go
type IntentRecord struct {
    Status AttributionStatus `json:"status"`
    Source AttributionSource `json:"source"`

    Objective  string   `json:"objective,omitempty"`
    Initiative string   `json:"initiative,omitempty"`
    Priority   string   `json:"priority,omitempty"`
    Domains    []string `json:"domains,omitempty"`
    Risk       string   `json:"risk,omitempty"`

    RootNodeID string `json:"root_node_id,omitempty"`
    RootType   string `json:"root_type,omitempty"`
    RootURL    string `json:"root_url,omitempty"`

    Labels []string `json:"labels,omitempty"`
    Weight *int     `json:"weight"`

    Rule            string `json:"rule"`
    ConfigHash      string `json:"config_hash"`
    ResolverVersion string `json:"resolver_version"`
}
```

## Attribution states

```go
type AttributionStatus string

const (
    AttributionMapped    AttributionStatus = "mapped"
    AttributionUnmapped  AttributionStatus = "unmapped"
    AttributionUnlinked  AttributionStatus = "unlinked"
    AttributionAmbiguous AttributionStatus = "ambiguous"
    AttributionSuggested AttributionStatus = "suggested"
)
```

### `mapped`

A deterministic source was found and at least one configured intent label matched.

### `unmapped`

A root object was found, but its labels did not match the configuration.

### `unlinked`

No supported root or intent source was found.

### `ambiguous`

Multiple candidate roots were found and no deterministic policy selected one.

### `suggested`

A heuristic or AI-generated relationship exists but has not been confirmed.

Suggested attribution does not contribute to official metrics or policy decisions.

## Attribution sources

```go
type AttributionSource string

const (
    SourceExplicitMetadata AttributionSource = "explicit_metadata"
    SourceClosingIssue     AttributionSource = "closing_issue"
    SourceParentIssue      AttributionSource = "parent_issue"
    SourceReferencedIssue  AttributionSource = "referenced_issue"
    SourceProject          AttributionSource = "project"
    SourceMilestone        AttributionSource = "milestone"
    SourceIssueLabels      AttributionSource = "issue_labels"
    SourceArtifactLabels   AttributionSource = "artifact_labels"
    SourceSuggestion       AttributionSource = "suggestion"
    SourceNone             AttributionSource = "none"
)
```

## Deterministic resolution

Resolution order:

```text
1. Explicit workflow intent
2. Single closing issue
3. Parent or sub-issue relationship
4. Explicit referenced issue
5. Project or campaign context
6. Milestone
7. Direct artifact labels
8. Suggested attribution
9. Unlinked
```

Initial implementation:

```go
func (r *Resolver) Resolve(pr PullRequestData) IntentRecord {
    if pr.ExplicitIntent != nil {
        return r.fromExplicitIntent(pr.ExplicitIntent)
    }

    switch len(pr.ClosingIssues) {
    case 1:
        return r.fromRoot(
            pr.ClosingIssues[0],
            SourceClosingIssue,
            "single_closing_issue",
        )

    case 0:
        if len(pr.Labels) > 0 {
            return r.fromLabels(
                pr.NodeID,
                pr.URL,
                pr.Labels,
                SourceArtifactLabels,
                "pull_request_label_fallback",
            )
        }

        return r.unlinked("no_supported_intent_source")

    default:
        return r.ambiguous(
            SourceClosingIssue,
            "multiple_closing_issues",
        )
    }
}
```

The resolver must not silently select the first of multiple closing issues.

## Multiple-root policy

Default:

```text
0 candidates → continue resolution
1 candidate  → use candidate
2+ candidates → ambiguous
```

Future supported policies may include:

* explicit primary root
* highest-priority root
* all roots
* fractional attribution

The active policy must be recorded in decision provenance.

### Multiple-Root Open Questions and Proposed Resolutions

These proposals are non-normative until accepted by the governance policy owner.

1. **How are duplicate root references distinguished from multiple roots?** Proposed resolution: deduplicate by stable GitHub node ID when available; if identity is missing, keep the candidates distinct and fail closed as `ambiguous`.
2. **Can explicit intent metadata resolve multiple closing issues?** Proposed resolution: a schema-valid explicit intent may resolve them only when it names one canonical root and its provenance is retained; otherwise return `ambiguous`.
3. **Are the listed future policies active alternatives?** Proposed resolution: no. Explicit-primary, highest-priority, all-root, and fractional policies remain unsupported until separately specified with deterministic provenance and conformance fixtures.

## Risk classification

Risk should be explicit where possible.

When risk is absent, deterministic rules may derive it.

Example:

```text
security + critical → high
production          → high
infrastructure      → medium
dependency update   → medium
documentation       → low
unknown             → unknown
```

```go
func ResolveRisk(intent IntentRecord) string {
    if intent.Risk != "" {
        return intent.Risk
    }

    if contains(intent.Domains, "security") &&
       intent.Priority == "critical" {
        return "high"
    }

    if contains(intent.Domains, "production") {
        return "high"
    }

    if contains(intent.Domains, "infrastructure") {
        return "medium"
    }

    if contains(intent.Domains, "documentation") {
        return "low"
    }

    return "unknown"
}
```

## Execution policy

```go
type ExecutionPolicy struct {
    Autonomy string `json:"autonomy"`

    AllowedTools []string `json:"allowed_tools"`
    DeniedTools  []string `json:"denied_tools"`

    WriteScope string `json:"write_scope"`

    RequiredChecks []string `json:"required_checks"`

    HumanApprovalRequired bool `json:"human_approval_required"`
    AutoMergeAllowed      bool `json:"auto_merge_allowed"`

    MaxAttempts int `json:"max_attempts"`

    RuleIDs []string `json:"rule_ids"`
}
```

Supported initial autonomy levels:

### `propose_only`

The agent may inspect the repository and propose a plan or patch.

The agent may not modify the repository.

### `supervised`

The agent may create changes on a feature branch and open a pull request.

Human approval is required before merge.

### `bounded`

The agent may complete the workflow within explicitly configured limits.

Auto-merge may be permitted after required checks pass.

## Policy precedence

```text
organization constraints
> repository constraints
> intent-specific rules
> workflow defaults
> agent request
```

A lower-precedence rule may not weaken a higher-precedence constraint.

### Policy Precedence Open Questions and Proposed Resolutions

These proposals are non-normative until accepted by the governance policy owner.

4. **Are organization and repository policies constraints or replaceable defaults?** Proposed resolution: treat them as hard constraints; lower-precedence layers may narrow authority but cannot relax them.
5. **How are conflicting rules at the same precedence handled?** Proposed resolution: merge deterministically using the more restrictive value for authority-bearing fields; choose the lower autonomy and narrowest write scope, union required checks and denied tools, intersect allowed tools, require approval if any rule requires it, permit auto-merge only if all rules permit it, and use the lower attempt limit.
6. **What resolves ties between same-level rules?** Proposed resolution: reject conflicting policy compilation as ambiguous and apply the safest default rather than relying on declaration order or agent choice.

Example:

```json
{
  "rules": [
    {
      "id": "security-critical",
      "when": {
        "domain": "security",
        "priority": "critical"
      },
      "set": {
        "autonomy": "supervised",
        "write_scope": "feature_branch",
        "required_checks": [
          "unit-tests",
          "security-tests",
          "dependency-review"
        ],
        "human_approval_required": true,
        "auto_merge_allowed": false,
        "max_attempts": 2
      }
    },
    {
      "id": "documentation-low-risk",
      "when": {
        "domain": "documentation",
        "risk": "low"
      },
      "set": {
        "autonomy": "bounded",
        "write_scope": "feature_branch",
        "required_checks": [
          "documentation-build"
        ],
        "human_approval_required": false,
        "auto_merge_allowed": true,
        "max_attempts": 3
      }
    },
    {
      "id": "unknown-default",
      "when": {
        "risk": "unknown"
      },
      "set": {
        "autonomy": "propose_only",
        "write_scope": "none",
        "human_approval_required": true,
        "auto_merge_allowed": false,
        "max_attempts": 1
      }
    }
  ]
}
```

## Safe default

```go
func safestDefaultPolicy() ExecutionPolicy {
    return ExecutionPolicy{
        Autonomy:              "propose_only",
        WriteScope:            "none",
        HumanApprovalRequired: true,
        AutoMergeAllowed:      false,
        MaxAttempts:           1,
    }
}
```

Unknown or ambiguous intent must not grant elevated authority.

## Policy compilation

```go
type PolicyCompiler struct {
    Rules []PolicyRule
}

func (c *PolicyCompiler) Compile(
    intent IntentRecord,
    repository RepositoryContext,
) ExecutionPolicy {
    policy := safestDefaultPolicy()

    for _, rule := range c.Rules {
        if rule.Matches(intent, repository) {
            policy = mergePolicy(policy, rule.Set)
            policy.RuleIDs = append(
                policy.RuleIDs,
                rule.ID,
            )
        }
    }

    return policy
}
```

Policy merging must preserve stricter higher-precedence constraints.

## Decision provenance

```go
type PolicyDecision struct {
    Intent IntentRecord    `json:"intent"`
    Policy ExecutionPolicy `json:"policy"`

    AppliedRules []AppliedRule   `json:"applied_rules"`
    Overrides    []PolicyOverride `json:"overrides"`

    ConfigHash      string `json:"config_hash"`
    CompilerVersion string `json:"compiler_version"`
}
```

Example:

```json
{
  "policy": {
    "autonomy": "supervised",
    "human_approval_required": true,
    "auto_merge_allowed": false
  },
  "applied_rules": [
    {
      "id": "security-critical",
      "reason": "domain=security and priority=critical"
    }
  ],
  "overrides": [
    {
      "field": "auto_merge_allowed",
      "requested": true,
      "effective": false,
      "reason": "organization security policy"
    }
  ]
}
```

## Enforcement

The orchestrator must enforce the compiled policy at runtime.

Policy must not exist only in an agent prompt.

```go
func (o *Orchestrator) Execute(
    ctx context.Context,
    request WorkflowRequest,
) error {
    intent := o.intentResolver.Resolve(request.WorkItem)

    intent.Risk = ResolveRisk(intent)

    policy := o.policyCompiler.Compile(
        intent,
        request.Repository,
    )

    if err := o.authorizer.Validate(
        request,
        policy,
    ); err != nil {
        return err
    }

    runtime := NewRestrictedRuntime(policy)

    return runtime.Run(ctx, request.Workflow)
}
```

Individual tool calls must be authorized:

```go
func (a *Authorizer) AuthorizeTool(
    policy ExecutionPolicy,
    tool string,
) error {
    if slices.Contains(policy.DeniedTools, tool) {
        return ErrToolDenied
    }

    if !slices.Contains(policy.AllowedTools, tool) {
        return ErrToolNotAllowed
    }

    return nil
}
```

The agent must not be able to modify or expand its own policy.

### `Authorizer.AuthorizeTool` Implementation Audit

See the [implementation companion's runtime enforcement map](intent-attribution-agent-governance-implementation.md#authorizerauthorizetool-implementation-audit). This heading remains as a stable reference anchor.


Initial observable rules:

```text
merged pull request          → accepted
closed unmerged pull request → rejected
open pull request            → pending

completed issue              → accepted
closed as not planned        → rejected
open issue                   → pending
```

```go
type OutcomeRecord struct {
    ArtifactURL string `json:"artifact_url"`
    Status      string `json:"status"`

    Intent IntentRecord    `json:"intent"`
    Policy ExecutionPolicy `json:"policy"`

    EvaluatedAt time.Time `json:"evaluated_at"`
}
```

An accepted artifact is evidence of accepted execution.

It does not independently prove realized business impact.

## Evidence record

```go
type EvidenceRecord struct {
    WorkflowRunID string `json:"workflow_run_id"`
    ArtifactURL   string `json:"artifact_url"`

    RequiredChecks []string `json:"required_checks"`
    PassedChecks   []string `json:"passed_checks"`
    FailedChecks   []string `json:"failed_checks"`

    HumanApprovalRequired bool `json:"human_approval_required"`
    HumanApprovalReceived bool `json:"human_approval_received"`

    Outcome string `json:"outcome"`

    TraceID string `json:"trace_id,omitempty"`
}
```

## Attribution reporting

The existing weighted reporting remains useful when interpreted correctly.

### Attribution coverage

```text
mapped outcomes / all evaluated outcomes
```

### Acceptance rate

```text
accepted outcomes / attempted outcomes
```

### Weighted acceptance rate

```text
accepted mapped weight / attempted mapped weight
```

Weighted acceptance measures delivery performance across configured relative weights.

It is not ROI, planned-value completion, or verified business impact.

Every weighted result must be shown alongside attribution coverage.

```text
Weighted acceptance: 78%
Attribution coverage: 42%
```

## Unique root reporting

Strategic reporting must deduplicate root work items.

```text
Five merged pull requests
connected to one issue
=
one attributed root
```

Root completion must come from the root object's state or explicit completion evidence.

A merged pull request alone must not automatically mark the full root objective complete.

## OpenTelemetry

OpenTelemetry is the execution-observability layer.

It is not the authoritative intent store.

One trace should represent one workflow execution:

```text
agentic.workflow.run
├── intent.resolve
├── risk.resolve
├── policy.compile
├── authorization.validate
├── agent.execute
├── github.artifact.create
├── checks.evaluate
├── approval.evaluate
└── outcome.evaluate
```

Recommended span attributes:

```text
github.intent.status
github.intent.source
github.intent.priority
github.intent.risk
github.intent.domain

agent.policy.autonomy
agent.policy.write_scope
agent.policy.human_approval_required
agent.policy.auto_merge_allowed

github.artifact.type
github.artifact.outcome
```

High-cardinality identifiers belong on spans or persisted records, not metric dimensions.

Examples:

* pull request URL
* issue URL
* node ID
* trace ID
* workflow run ID

## Metrics

Initial operational metrics:

```text
agent.workflow.runs
agent.workflow.denied
agent.workflow.duration

github.intent.resolutions
github.intent.attribution.coverage

agent.policy.decisions
agent.policy.overrides

github.artifacts.created
github.artifacts.accepted
github.artifacts.rejected
```

Useful low-cardinality dimensions:

```text
intent.status
intent.source
priority
risk
domain
autonomy
outcome
```

## CLI

### Explain policy before execution

```bash
gh aw policy explain \
  --repo acme/platform \
  --issue 123
```

Example:

```text
Intent
  Priority: critical
  Domain: security
  Risk: high
  Source: issue labels

Execution policy
  Autonomy: supervised
  Write scope: feature branch
  Human approval: required
  Auto-merge: prohibited
  Maximum attempts: 2

Required checks
  unit-tests
  security-tests
  dependency-review

Applied rules
  security-critical
  organization-security-baseline
```

### Report outcomes

```bash
gh aw outcomes report \
  --repo acme/platform
```

Example:

```text
Outcomes
  Total:                  40
  Accepted:               28
  Rejected:                7
  Pending:                 5

Intent attribution
  Mapped:                 17
  Unmapped:                6
  Unlinked:               14
  Ambiguous:               3
  Coverage:              42.5%

Weighted delivery
  Attempted weight:     1150
  Accepted weight:       900
  Weighted acceptance: 78.3%

Unique attributed roots: 12
```

## Implementation phases

### Phase 1: current foundation

Already implemented or partially implemented:

1. Load label-to-weight mapping
2. Compute weights from labels
3. Trace pull requests to closing issues
4. Record root URL
5. Enrich outcome reports
6. Aggregate accepted and attempted weights
7. Produce per-label breakdowns

### Phase 2: honest attribution model

Implement:

1. Rename value semantics to relative weight
2. Add attribution states
3. Add attribution source
4. Make unknown weight nullable
5. Record root node ID
6. Mark multiple roots ambiguous
7. Add attribution coverage
8. Rename objective efficiency to weighted acceptance rate
9. Deduplicate root work items
10. Separate priority, domain, initiative, and risk dimensions

### Phase 3: prove attribution value

Before intent affects authority, the system must show that attribution is useful for analysis.

Validate:

1. Attribution coverage is high enough to be decision-relevant
2. Mapped categories reveal non-trivial work patterns
3. Unique-root reporting is more informative than raw artifact counts
4. Weighted acceptance is always shown alongside coverage
5. Manual samples show that mapped results are directionally correct

Deliver:

1. Coverage reporting by state: mapped, unmapped, unlinked, ambiguous
2. Unique-root reporting
3. Weighted acceptance over mapped items only
4. Representative manual-review samples for correctness checking

If this phase does not show clear analytical value, intent remains a reporting feature and does not drive policy.

### Phase 4: prove attribution trustworthiness

Before intent affects authority, the system must show that attribution is trustworthy enough for control.

Validate:

1. Identical GitHub state and configuration produce identical attribution
2. Ambiguous and unlinked cases fail closed
3. Source precedence is understandable and auditable
4. Fallback paths do not silently distort authority decisions
5. Manual validation shows high precision for cases that would change authority

Deliver:

1. Attribution provenance for every official decision
2. Determinism tests
3. Ambiguity and fallback reporting
4. Source-quality analysis by attribution source

If this phase does not show sufficient trustworthiness, intent remains analytics only.

### Phase 5: minimal intent-aware governance

Implement:

1. Safe default for ambiguous and unlinked intent
2. Minimal policy distinctions with clear operational value
3. A narrow policy explanation surface such as `gh aw policy explain`
4. Explicit fail-closed behavior when attribution is missing or disputed

Suggested initial trial:

1. Ambiguous or unlinked intent becomes `propose_only`
2. Explicitly high-risk or security-like intent becomes `supervised`
3. Low-risk documentation work may become `bounded`

This phase should remain intentionally small. It is a controlled trial, not a full policy framework.

### Phase 6: broader governance and execution evidence

Implement:

1. Risk resolution beyond simple deterministic categories
2. General policy compilation
3. Tool authorization
4. Write-scope enforcement
5. Required checks
6. Human approval requirement
7. Auto-merge restrictions
8. Decision provenance
9. OpenTelemetry workflow traces
10. Policy-decision spans
11. Authorization events
12. Evidence records
13. Low-cardinality operational metrics
14. Asynchronous trace links for later GitHub outcomes

### Phase 7: broader intent relationships

Potential extensions:

1. Parent and sub-issue resolution
2. Projects and milestones
3. Cross-repository initiatives
4. Organization-level policies
5. Suggested attribution requiring confirmation
6. External production or customer evidence

## Testing

### Attribution tests

* One mapped closing issue
* One unmapped closing issue
* No closing issue
* Pull request label fallback
* Multiple closing issues
* Null weight for unknown attribution
* Case-insensitive label normalization
* Root deduplication

### Attribution value tests

* Coverage is reported alongside weighted acceptance
* Unique-root counts differ from raw artifact counts when many artifacts map to one root
* Mapped-only weighted reporting excludes unlinked and ambiguous items
* Representative samples can be produced for manual correctness review

### Attribution trust tests

* Multiple closing issues fail closed as ambiguous
* Artifact-label fallback is reported as fallback provenance
* Identical input state produces identical attribution output
* Missing or ambiguous attribution does not grant elevated authority

### Policy tests

* Critical security work becomes supervised
* Low-risk documentation work becomes bounded
* Unknown risk becomes propose-only
* Organization policy overrides repository policy
* Required approval disables auto-merge
* Less restrictive rules cannot weaken stronger constraints

### Enforcement tests

* Denied tools cannot execute
* Write scope cannot be expanded
* Agent cannot modify its own policy
* Failed required checks prevent completion
* Missing approval prevents merge when required

### Determinism test

Given identical:

* GitHub state
* configuration
* resolver version
* compiler version

the normalized intent and policy outputs must be identical.

## Release gates

### Gate 1: attribution value proven

Proceed beyond attribution-only reporting only when:

1. Coverage is measured and visible beside weighted reporting
2. Root deduplication produces materially different and more honest reporting than artifact counts alone
3. Manual review shows that mapped results are directionally useful
4. The mapped categories reveal meaningful work patterns rather than label noise

### Gate 2: attribution trustworthy enough for control

Proceed to governance only when:

1. Deterministic resolution is demonstrated
2. Ambiguous and unlinked cases fail closed
3. Attribution provenance is available for official decisions
4. Manual validation shows sufficient precision for authority-changing cases
5. Fallback logic is auditable and not silently permissive

## Definition of done

### Attribution release done

The attribution release is complete when:

1. A GitHub issue or pull request resolves into a normalized intent record
2. Missing and ambiguous intent are represented explicitly
3. Coverage, ambiguity, and unique-root reporting are available
4. Weighted acceptance is reported only with attribution coverage beside it
5. Manual sampling can be used to assess mapping quality

### Initial governance release done

The initial governance release is complete when:

1. The attribution release is already complete
2. Intent deterministically compiles into at least one minimal execution-policy distinction
3. Ambiguous and unlinked intent receive the safest available policy
4. Runtime behavior is restricted by policy for the initial governed cases
5. Human approval and check requirements are enforced for the initial governed cases
6. Every policy decision records enough provenance to audit why authority was granted or denied
7. Every governed run emits execution telemetry
8. Every resulting artifact receives an outcome and evidence record
9. No LLM decision is required for official authorization

## Product position

This system is not a business-impact calculator.

It is a deterministic control layer for agentic GitHub work.

> **Intent determines authority. Execution produces evidence.**

## Change Log

### 2026-11-09

- Executed the implementation-document split and retained the legacy implementation and audit anchors.
- The three-consecutive-CI-run drift escalation norm remains in force; enforcement by a CI check and tracking issue was not verified in this pass.

### 2026-11-03

- Added numbered multiple-root and policy-precedence questions with non-normative proposed resolutions.
