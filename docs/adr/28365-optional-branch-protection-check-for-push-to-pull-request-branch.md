# ADR-28365: Optional Branch Protection Pre-flight Check for `push-to-pull-request-branch`

**Date**: 2026-04-25 (revised 2026-11-09)
**Status**: Accepted
**Deciders**: pelikhan

---

## Part 1 — Narrative (Human-Friendly)

### Context

The `push-to-pull-request-branch` safe output handler performs a pre-flight check before pushing. Its classic branch-protection confirmation originally used `GET /repos/{owner}/{repo}/branches/{branch}/protection`, which requires `administration: read`, a GitHub App-only scope unavailable to standard `GITHUB_TOKEN`. The check should remain enabled by default without requiring that broader permission. The branch metadata endpoint and active-rules endpoint provide the ruleset information needed for the pre-flight check; GraphQL `Ref.refUpdateRule` provides the classic-protection confirmation.

### Decision

The branch-protection pre-flight remains opt-out via the `check-branch-protection` boolean frontmatter key (default `true`). When enabled, the handler reads branch metadata and active rules, then uses GraphQL `Ref.refUpdateRule` to confirm that classic protection is explicitly absent before allowing the narrow Copilot-code-review-only ruleset exception. An absent field, a classic rule, incomplete rules data, or an API failure does not establish safety and leaves the push blocked. The handler MUST NOT call the classic-protection REST endpoint or request `administration: read`. The default-branch guard runs independently of this setting.

### Alternatives Considered

#### Alternative 1: Always require `administration: read`

All workflows using `push-to-pull-request-branch` would need an elevated GitHub App permission for classic-protection inspection. This was rejected because `GITHUB_TOKEN` cannot receive that scope and the GraphQL field provides the required confirmation without it.

#### Alternative 2: Remove the branch protection pre-flight check entirely

Eliminating the check avoids the permission problem without any new configuration surface. It was rejected because the check provides genuine defense-in-depth: it prevents agentic pushes to branches under review policies, catching misconfigurations before GitHub's push-time enforcement triggers a harder failure. The value of the check outweighs the complexity of making it configurable.

#### Alternative 3: Silently ignore 403 and keep the existing behavior

Treating a failed classic-protection query as absence would make the check fail open. This is rejected: only an explicit GraphQL `null` confirms that no classic update rule applies.

### Consequences

#### Positive
- Classic protection is checked without requiring `administration: read`, including for standard `GITHUB_TOKEN` credentials.
- Users retain the explicit `check-branch-protection: false` opt-out.
- The default branch guard (using the repos API) continues to run unconditionally, preserving the most critical safety check.
- Extracting the combined guard logic into a `checkBranchPushable` helper improves testability and isolation.

#### Negative
- The configuration surface for the handler grows by one field, adding a new concept callers must understand.
- The exception requires complete active-rules data and an explicit GraphQL `null`; unavailable or incomplete evidence blocks the push.

#### Neutral
- The JSON schema for `push-to-pull-request-branch` gains a `check-branch-protection` boolean property, which may require documentation updates in tooling that consumes the schema.
- The permission computation in `ComputePermissionsForSafeOutputs` now branches on the effective `CheckBranchProtection` value, so permission calculations are no longer purely additive for this handler.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Branch Protection Check Configuration

1. The `push-to-pull-request-branch` handler **MUST** treat `check-branch-protection` as an optional boolean key in workflow frontmatter, defaulting to `true` when absent.
2. When `check-branch-protection` is `true` (the default), the handler **MUST** read branch metadata from `GET /repos/{owner}/{repo}/branches/{branch}` before executing the push. It **MUST NOT** call `GET /repos/{owner}/{repo}/branches/{branch}/protection`.
3. If branch metadata marks the branch protected, the handler **MUST** reject it unless all conditions for the exception in requirements 4–6 hold.
4. The exception **MAY** apply only to a non-default branch whose metadata explicitly disables classic protection and whose complete active-rules list contains only `copilot_code_review`.
5. To apply the exception, the handler **MUST** query GraphQL `Ref.refUpdateRule` for the fully qualified branch ref and accept only an explicit `null`. A missing field, a missing `Ref`, any non-null rule, or a GraphQL error **MUST NOT** be treated as absence of classic protection.
6. Missing or incomplete rules data, unknown or mixed rule types, and any failed active-rules or GraphQL query **MUST** leave the push blocked. Implementations **MUST** read the complete active-rules list before applying the exception.
7. An unprotected branch **MAY** proceed without active-rules or GraphQL checks. The handler **MUST** permit a missing branch (HTTP 404). If branch metadata returns HTTP 403, it **SHOULD** warn and permit the push because GitHub still enforces protection at push time. Unexpected errors **MUST** block the push.
8. When `check-branch-protection` is `false`, the handler **MUST NOT** query branch metadata, active rules, or GraphQL for the protection pre-flight. The default-branch guard remains mandatory.

### Default Branch Guard

1. The handler **MUST** check whether the target branch is the repository's default branch, regardless of the `check-branch-protection` setting.
2. The handler **MUST** block the push and return an error when the target branch equals the repository's default branch.
3. The handler **SHOULD** log a warning and continue when the repositories API call needed to resolve the default branch fails, rather than blocking the push due to an unrelated API error.

### Permission Computation

1. The permission compiler **MUST NOT** add `administration: read` for this pre-flight check, whether `check-branch-protection` is enabled or disabled.
2. Implementations **SHOULD** document that GraphQL `Ref.refUpdateRule` is used for classic-protection confirmation.

### Schema

1. The JSON schema for `push-to-pull-request-branch` **MUST** declare `check-branch-protection` as an optional boolean property with `default: true`.
2. The schema **MUST NOT** require `check-branch-protection` to be present.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

The implementation's conformance evidence is covered by `actions/setup/js/pr_helpers.test.cjs` and `actions/setup/js/push_to_pull_request_branch.test.cjs`, including explicit GraphQL `null` handling, rejection of incomplete or non-null responses, failed rules/query handling, and the Copilot-code-review-only ruleset exception.

---

*This ADR was revised on 2026-11-09 to replace the classic-protection REST requirement with GraphQL `Ref.refUpdateRule`, preserving the fail-closed pre-flight contract without requiring `administration: read`.*
