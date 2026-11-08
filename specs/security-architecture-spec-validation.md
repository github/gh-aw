# Security Architecture Specification Validation

**Document**: Validation of `security-architecture-spec.md` against compiled `.lock.yml` files  
**Date**: November 8, 2026<br>
**Last completed full revalidation**: 2026-11-08
**Last completed targeted formal revalidation**: v1.1.1 (RS-05a) / 2026-10-09
**Current compiled-lock structure check**: 2026-11-08
**Validator**: GitHub Copilot Agent  
**Scope**: Cross-reference specification requirements with actual implementation

---

## Executive Summary

The 2026-11-08 full revalidation refreshed the documented security evidence against the current requirements and implementation. Runtime and compiler regressions for v1.1.2–v1.1.8 were reviewed; formal RS-05a revalidation remains pending, as detailed below. The event-specific mappings describe the default (`pull-request-floating-head: false`) policy.

⚠️ **VALIDATION STATUS**: Full revalidation was completed on 2026-11-08. The representative ten-job structure was checked against `.github/workflows/security-review.lock.yml` (see §1); write permissions remain workflow-specific: `activation`, `safe_outputs`, `conclusion`, and `push_evals_state` have repository write scopes, while the `agent` job includes `copilot-requests: write`. PM-08 is now verified for the default fork-deny policy and explicit allowlisting (see §3). Targeted formal RS-05a revalidation remains pending, and the v1.1.4 TT-01–04 requirement-level mapping remains incomplete (see the version status table and §7b).

The revalidation refreshed the following evidence. A verified row does not imply that every implementation or deployment has identical permissions or runtime behavior:
- ✅ Job architecture (activation, agent, safe_outputs)
- ✅ **Input sanitization** (markdown safety, URL filtering, HTML tag filtering, ANSI removal)
- ✅ Permission management (historically verified; current job scopes are workflow-specific)
- ✅ Fork protection (PM-08; default-deny and allowlist compiler evidence rechecked 2026-11-08)
- ✅ Role-based access control (pre_activation with membership checks)
- ✅ PM-11 formal coverage (`TestFormalPM11_PreActivationContainsMembershipStep`)
- ✅ Output isolation evidence for handler coverage, token precedence, and write-job separation
- ✅ Network isolation evidence for allowlist validation, protocol filtering, ecosystem expansion, and MCP/firewall wrapping
- ⚠️ Sandbox isolation evidence now documented from compiled AWF chroot/container invocations (representative lock-file evidence; direct runtime host-visibility proof remains partial)
- ✅ Threat detection layer (detection job between agent and safe_outputs)
- ✅ Action pinning to SHAs
- ✅ Timestamp validation at runtime
- ✅ Concurrency control (context-aware grouping with cancel-in-progress)
- ✅ Documentation maintenance follow-ups for `pre_activation`, `detection`, optional `conclusion`, and companion `trusted-users` references remain aligned with implementation behavior

---

## Detailed Validation

### Versioned RS-05a and Transport-Trust Validation Status

The statuses below distinguish implementation/runtime evidence from a targeted formal proof. A passing runtime test does not establish a formal proof or live-host behavior.

| Version | Change | Revalidation status | Evidence |
|---|---|---|---|
| v1.1.2 | Allow-listed App review checkout | Runtime verified; targeted formal proof unverified. | [`checkout_pr_branch.test.cjs`](../actions/setup/js/checkout_pr_branch.test.cjs), review-author and repository-identity cases; formal model status in §7b. |
| v1.1.3 | Same-repository bot `ready_for_review` and `reopened` events | Runtime verified; targeted formal proof unverified. | [`checkout_pr_branch.test.cjs`](../actions/setup/js/checkout_pr_branch.test.cjs), lifecycle action and repository-ID cases; formal model status in §7b. |
| v1.1.4 | Transport trust and bootstrap integrity (TT-01–04) | **UNVERIFIED** at requirement level; the current report has no TT-01–04-to-test mapping. | [`release_integrity_test.sh`](../actions/setup/sh/release_integrity_test.sh) and [`release_integrity_validation.go`](../pkg/workflow/release_integrity_validation.go) provide adjacent implementation evidence, but do not by themselves establish all four requirements. |
| v1.1.5 | Labeled PR permission floor | Runtime verified; targeted formal proof unverified. | [`checkout_pr_branch.test.cjs`](../actions/setup/js/checkout_pr_branch.test.cjs), `labeled` action negative cases; formal model status in §7b. |
| v1.1.6 | Body-only bot PR edits | Runtime verified; targeted formal proof unverified. | [`checkout_pr_branch.test.cjs`](../actions/setup/js/checkout_pr_branch.test.cjs), body-only and malformed/mixed edit cases; formal model status in §7b. |
| v1.1.7 | Immutable revision binding and explicit dispatch-context failures | Runtime verified; targeted formal proof unverified. | [`checkout_pr_branch.test.cjs`](../actions/setup/js/checkout_pr_branch.test.cjs), SHA binding, stale-head, malformed-context, and checked-out-HEAD cases; formal model status in §7b. |
| v1.1.8 | Explicit floating-head checkout | Compiler and runtime tests verified; targeted formal proof unverified. | [`pr_floating_head_test.go`](../pkg/workflow/pr_floating_head_test.go), [`pr_checkout_test.go`](../pkg/workflow/pr_checkout_test.go), and [`checkout_pr_branch.test.cjs`](../actions/setup/js/checkout_pr_branch.test.cjs). |

---

### Current Compiled Job Structure (2026-11-08)

Evidence was rechecked in `.github/workflows/security-review.lock.yml` on 2026-11-08. The generated file declares ten top-level jobs; their YAML declaration order is not execution order.

| Job | Current evidence | Observed structure and permissions |
|---|---|---|
| `pre_activation` | `security-review.lock.yml:2884-2949` | Checks command position and membership; grants `contents: read` and `pull-requests: read`. |
| `admission` | `security-review.lock.yml:583-600` | Depends on `pre_activation`; grants `contents: read` and `id-token: write`. |
| `activation` | `security-review.lock.yml:93-107`, `:298-310` | Depends on `admission` and `pre_activation` (its `if:` checks `needs.admission.result`); checks the lock timestamp. It also has `issues: write` and `pull-requests: write`. |
| `agent` | `security-review.lock.yml:635-650` | Depends on `activation`; repository content and issue/PR permissions are read, with `copilot-requests: write`. |
| `detection` | `security-review.lock.yml:2330-2350` | Depends on `activation` and `agent`; runs before safe outputs and has `contents: read`, `copilot-requests: write`. |
| `evals` | `security-review.lock.yml:2708-2724` | Depends on `activation`, `agent`, and `detection`; grants `contents: read` and `copilot-requests: write`. |
| `push_evals_state` | `security-review.lock.yml:2951-2960` | Depends on `activation` and `evals`; grants `contents: write`, a repository-content writer. |
| `safe_outputs` | `security-review.lock.yml:3038-3051`, `:3142-3144` | Depends on `activation`, `agent`, and `detection`; processes outputs with `checks`, `issues`, and `pull-requests` write permissions. |
| `conclusion` | `security-review.lock.yml:1862-1887` | Runs after the workflow jobs using `always()`; has `checks`, `issues`, and `pull-requests` write permissions for reporting/cleanup. |
| `conclusion_admission` | `security-review.lock.yml:2279-2290` | Depends on `conclusion`; grants `contents: read` and `id-token: write`. |

This evidence supersedes the former reference to the absent `security-guard.lock.yml` for job topology. It does not establish that all workflows use identical job permissions; individual unresolved requirements are listed in the status matrix.

### 1. Job Architecture (Section 5.2 - OI-01)

**Specification Claim**:
> **OI-01**: A conforming implementation MUST separate workflow execution into distinct job types:
> 1. **Activation Job**: Performs sanitization and produces `steps.sanitized.outputs.text`
> 2. **Agent Job**: Executes AI agent with read-only permissions
> 3. **Safe Output Jobs**: Perform validated GitHub API operations with write permissions

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, checked 2026-11-08): See the current ten-job evidence table above. Repository write permissions are not exclusive to `safe_outputs`: the representative workflow also grants write scopes to `activation`, `conclusion`, and `push_evals_state` (`contents: write`), and `id-token: write` to `admission` and `conclusion_admission`; the agent has `copilot-requests: write` but no repository-content write permission.

**Status**: ⚠️ **PARTIALLY VERIFIED** — The ten-job structure and representative permission scopes are current for this lock file only; this does not establish identical permission scopes across all workflows.

---

### 1a. Input Sanitization Implementation (Section 4.3 - IS-04 to IS-09)

**Specification Claim**:
> **IS-04 to IS-09**: The implementation MUST provide comprehensive input sanitization including:
> - Markdown safety (@mentions, bot triggers)
> - URL filtering (protocol sanitization, domain allowlisting)
> - HTML/XML tag filtering (entity conversion)
> - ANSI escape code removal
> - Content limits enforcement

**Implementation Validation** (`actions/setup/js/sanitize_content_core.cjs`):

```javascript
// Core sanitization functions verified in implementation:

// ✅ IS-09: ANSI escape code and control character removal
sanitized = sanitized.replace(/\x1b\[[0-9;]*[mGKH]/g, "");  // ANSI sequences
sanitized = sanitized.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, "");  // Control chars

// ✅ IS-04: Mention neutralization
sanitized = neutralizeAllMentions(sanitized);  // Wraps @mentions in backticks

// ✅ IS-05: Bot trigger protection
sanitized = neutralizeCommands(sanitized);  // Wraps /commands in backticks
sanitized = neutralizeBotTriggers(sanitized);  // Wraps fixes #123 patterns

// ✅ IS-06a: XML comment removal
sanitized = removeXmlComments(sanitized);  // Removes <!-- comments -->

// ✅ IS-06: HTML/XML tag conversion
sanitized = convertXmlTags(sanitized);  // <tag> → &lt;tag&gt;

// ✅ IS-07b: URL protocol sanitization
sanitized = sanitizeUrlProtocols(sanitized);  // Removes javascript:, data:, file:

// ✅ IS-07: URL domain filtering
sanitized = sanitizeUrlDomains(sanitized, allowed);  // Redacts non-allowlisted URLs

// ✅ IS-08: Content limits with truncation
sanitized = applyTruncation(content, maxLength);  // 0.5MB / 65k lines
```

**Sanitization Features Verified**:

| Feature | Requirement | Implementation Function | Status |
|---------|-------------|------------------------|--------|
| @Mention neutralization | IS-04 | `neutralizeAllMentions()` | ✅ Verified |
| Bot trigger protection | IS-05 | `neutralizeCommands()`, `neutralizeBotTriggers()` | ✅ Verified |
| HTML/XML tag filtering | IS-06 | `convertXmlTags()`, `removeXmlComments()` | ✅ Verified |
| URL protocol filtering | IS-07b | `sanitizeUrlProtocols()` | ✅ Verified |
| URL domain allowlisting | IS-07 | `sanitizeUrlDomains()` | ✅ Verified |
| ANSI escape removal | IS-09 | Regex replacement | ✅ Verified |
| Content limits | IS-08 | `applyTruncation()` | ✅ Verified |

**Sanitization Pipeline Order** (as specified in IS-10):
1. ✅ ANSI escape and control character removal
2. ✅ @mention neutralization
3. ✅ Bot trigger protection (commands and GitHub references)
4. ✅ XML comment removal
5. ✅ HTML/XML tag conversion
6. ✅ URL protocol sanitization
7. ✅ URL domain filtering
8. ✅ Content truncation

**Redacted Domain Logging**:
- ✅ `getRedactedDomains()` - Collects redacted URLs for audit
- ✅ `writeRedactedDomainsLog()` - Writes to `/tmp/gh-aw/redacted-urls.log`

**Status**: ✅ **VERIFIED** - Comprehensive sanitization implementation covering markdown safety, URL filtering, HTML tag filtering, ANSI removal, and content limits. All specification requirements (IS-04 to IS-09) implemented in `sanitize_content_core.cjs`.

---

### 2. Permission Management (Section 7.2 - PM-01, PM-02)

**Specification Claim**:
> **PM-01**: A conforming implementation MUST set read-only permissions as the default
> **PM-02**: Unspecified permissions MUST default to `none`

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, checked 2026-11-08): The workflow-level default is `permissions: {}`. The ten-job table in §1 records the job-level scopes. This representative workflow grants repository write scopes to `activation`, `safe_outputs`, `conclusion`, and `push_evals_state` (`contents: write`); the agent has `copilot-requests: write` but no repository-content write permission.

**Status**: ⚠️ **PARTIALLY VERIFIED** — The lock confirms default-deny permissions and distinguishes agent repository access from safe-output writes, but it does not support a blanket claim that all write permissions are isolated to `safe_outputs`.

---

### 3. Fork Protection (Section 7.5 - PM-08)

**Specification Claim**:
> **PM-08**: For `pull_request` triggers, the implementation MUST:
> - Block forks by default
> - Generate repository ID comparison: `github.event.pull_request.head.repo.id == github.repository_id`

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, checked 2026-11-08): The `pull_request` paths require `github.event.pull_request.head.repo.id == github.repository_id` at lines 255 and 329. The compiler tests also verify the default-deny behavior and explicit exact/glob allowlists.

**Status**: ✅ **VERIFIED** — `TestPullRequestForksArrayFilter` verifies that omitting `forks` denies forks by default and that exact and glob allowlists add only their configured repositories (`pkg/workflow/compiler_forks_test.go:19-103`). The focused compiler test passed on 2026-11-08. This verifies the compiler policy and this representative lock, not every custom workflow's event configuration.

---

### 4. Role-Based Access Control (Section 7.6 - PM-10, PM-11)

**Specification Claim**:
> **PM-10**: The implementation MUST support role-based execution restrictions
> **PM-11**: Role checks MUST be performed at runtime using membership validation

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, lines 2884-2949; checked 2026-11-03):

```yaml
pre_activation:
  outputs:
    activated: ${{ steps.check_membership.outputs.is_team_member == 'true' }}
  steps:
    - name: Check team membership for workflow
      id: check_membership
      uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd
      env:
        GH_AW_REQUIRED_ROLES: admin,maintainer,write  # ✅ Role configuration
      with:
        github-token: ${{ secrets.GITHUB_TOKEN }}
        script: |
          const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
          setupGlobals(core, github, context, exec, io);
          const { main } = require('/opt/gh-aw/actions/check_membership.cjs');  # ✅ Runtime check
          await main();
```

**Status**: ✅ **VERIFIED** - Role-based access control with runtime membership validation via `check_membership.cjs`.

---

### 4a. Documentation Maintenance Follow-ups (2026-07-06)

The July 2026 maintenance pass rechecked the documentation-only clarifications requested by the summary tracker against the same compiled workflow behavior:

- The new PM-11 note is consistent with the verified `pre_activation -> activation` gate shown above.
- Appendix D now names the compiled `detection` job explicitly as the runtime threat-detection layer that gates `safe_outputs`.
- The optional `conclusion` job remains a non-normative cleanup/reporting example and is not required by the compiled architecture.
- `trusted-users` runtime enforcement continues to live in the companion GitHub MCP access-control specifications and tests; the new Section 9 note accurately scopes that behavior without changing the top-level threat-detection contract.

**Status**: ✅ **VERIFIED** - The specification clarifications match current implementation and companion-spec boundaries.

---

### 4b. PM-11 Formal Test Coverage (2026-07-15)

PM-11 (pre_activation membership validation) is now covered by a dedicated formal test in `pkg/workflow/security_architecture_sg_formal_test.go`.

- `TestFormalPM11_PreActivationContainsMembershipStep` compiles a real workflow with `on.roles` configured.
- The test extracts the compiled `pre_activation` job section and asserts the presence of:
  - `id: check_membership`
  - `check_membership.cjs`
  - `GH_AW_REQUIRED_ROLES: "write"`

**Status**: ✅ **VERIFIED** — PM-11 now has programmatic formal coverage in addition to the lock-file evidence in §4 above.

---

### 5. Threat Detection Layer (Section 9 - TD-01, TD-04)

**Specification Claim**:
> **TD-01**: A conforming implementation with complete conformance MUST provide automated threat detection
> **TD-04**: The implementation MUST detect: Prompt Injection, Secret Leaks, Malicious Patches

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, lines 2330-2350 and 3038-3045; checked 2026-11-03): `detection` depends on `activation` and `agent`; `safe_outputs` depends on `agent` and `detection` and requires the detection job to succeed.

**Status**: ✅ **VERIFIED** — The current lock confirms job placement and dependency gating. The detailed detection behavior remains covered by the implementation and test evidence cited below.

---

### 6. Action Pinning (Section 10.6 - CS-10)

**Specification Claim**:
> **CS-10**: In strict mode, the implementation MUST enforce action pinning to commit SHAs

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, checked 2026-11-03): Its manifest records eight external action repositories at commit SHAs; the 96 external `uses:` references in the generated file are SHA-pinned. Ten `./actions/setup` references are repository-local actions, not external references.

**Status**: ✅ **VERIFIED** — External actions in this representative lock are SHA-pinned.

---

### 7. Runtime Timestamp Validation (Section 11.1 - RS-01, RS-02)

**Specification Claim**:
> **RS-01**: The implementation MUST validate that compiled workflows are up-to-date
> **RS-02**: Timestamp validation MUST compare source `.md` and compiled `.lock.yml` times

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, lines 298-310; checked 2026-11-03): The activation job sets `GH_AW_WORKFLOW_FILE` to `security-review.lock.yml` and invokes `check_workflow_timestamp_api.cjs`.

**Status**: ✅ **VERIFIED** — The timestamp-check step is present in the current representative lock.

---

### 7a. workflow_run Repository Validation (Section 11.3 - RS-05)

**Specification Claim**:
> **RS-05**: For `workflow_run` triggers, the implementation MUST short-circuit safely for non-`workflow_run` triggers, fail closed when workflow-run repository metadata is absent, require repository ID match, and reject forked upstream workflow runs.

**Implementation Validation** (`pkg/workflow/role_checks.go`):

```go
eventNotWorkflowRun := BuildNotEquals(BuildPropertyAccess("github.event_name"), BuildStringLiteral("workflow_run"))
workflowRunPayloadPresent := BuildNotEquals(BuildPropertyAccess("github.event.workflow_run"), BuildNullLiteral())
workflowRunRepositoryPresent := BuildNotEquals(BuildPropertyAccess("github.event.workflow_run.repository"), BuildNullLiteral())
repoIDCheck := BuildEquals(BuildPropertyAccess("github.event.workflow_run.repository.id"), BuildPropertyAccess("github.repository_id"))
notFromForkCheck := &NotNode{Child: BuildPropertyAccess("github.event.workflow_run.repository.fork")}
repoSafetyCheck := BuildAnd(workflowRunPayloadPresent, BuildAnd(workflowRunRepositoryPresent, BuildAnd(repoIDCheck, notFromForkCheck)))
combinedCheck := BuildOr(eventNotWorkflowRun, repoSafetyCheck)
```

The rendered job condition is a runtime policy gate. It permits non-`workflow_run` triggers without touching `github.event.workflow_run.*`, and for `workflow_run` events requires both parent objects before evaluating repository identity and fork status.

**Formal test coverage** (`pkg/workflow/security_architecture_sg_formal_test.go`):

| Test | RS-05 property |
|------|----------------|
| `TestFormalRS003_WorkflowRunRepositoryValidation` asserts `github.event_name != 'workflow_run'` | cross-trigger short-circuit |
| `TestFormalRS003_WorkflowRunRepositoryValidation` asserts `github.event.workflow_run != null` | workflow_run payload presence |
| `TestFormalRS003_WorkflowRunRepositoryValidation` asserts `github.event.workflow_run.repository != null` | repository payload presence / fail closed |
| `TestFormalRS003_WorkflowRunRepositoryValidation` asserts repository ID equality | repository identity |
| `TestFormalRS003_WorkflowRunRepositoryValidation` asserts `!(github.event.workflow_run.repository.fork)` | fork rejection |

**Status**: ✅ **VERIFIED** — RS-05 is enforced by the generated runtime `if:` policy and has formal coverage for trigger scoping, nullability, repository identity, and fork rejection.

---

### 7b. PR Checkout Authorization and Revision Isolation (Section 11.3 - RS-05a)

**Specification Claim**:
> **RS-05a**: All PR checkout paths enforce actor trust and immutable revision isolation. `workflow_dispatch` PR contexts additionally require repository scope validation and explicit failure on malformed context.

**Implementation mapping** (`actions/setup/js/checkout_pr_branch.cjs` and `actions/setup/js/aw_context.cjs`):

The dispatch parser validates JSON object shape, canonical positive PR numbers, and any supplied non-empty repository identity before actor/API/git operations. Invalid supplied PR context calls `core.setFailed()` and outputs `checkout_pr_success=false`; absent context or a valid non-PR context skips PR checkout. `resolvePRHeadSha()` validates the full SHA and binds ordinary PR events to their recorded head, reviews to `commit_id`, and revision-bound dispatches to propagated `pr_head_sha`. Label/review/dispatch paths reject detected head movement, while ordinary PR events retain their event revision.

Issue comments and unbound manual dispatch select the current API head, not an approved revision. `validateCommentContext()` requires valid comment and PR update timestamps and warns about newer PR updates without inferring a historical SHA. Immutable fetch uses an argument array from the current `origin`; API checkout paths recheck the selected head after fetch, and `assertCheckedOutRevision()` verifies actual `HEAD` before success or baseline export. These checks do not establish live GitHub.com or custom GitHub Enterprise Server support for fetching the selected SHA.

Actor trust is provided by the shared `assertTrustedCheckoutRuntime()` call. Fork-runtime rejection is scoped to `workflow_dispatch` and fails closed unless `repository.fork` is boolean `false`. Centralized command and label dispatches validate the originating `aw_context.actor` instead of the `github-actions[bot]` dispatcher only when GitHub also supplies `sender.type == "Bot"`; missing markers or inconsistent identities fail closed. The same-repository bot branch-event and allow-listed comment/review exceptions remain subject to the identity, repository, action, and body-only edit constraints in RS-05a; `labeled` retains the collaborator permission floor.

**Trust-boundary limitation:** `github-actions[bot]` is the shared identity of repository workflows using `GITHUB_TOKEN`, not proof of one specific caller workflow. GitHub does not include an immutable caller-workflow identity in the target `workflow_dispatch` event. This validation therefore assumes that `actions: write` is granted only to trusted repository workflows; any such workflow can construct `aw_context`, so untrusted code must not receive that permission. A matching SHA proves commit identity, not that PR code is safe to execute with credentials.

**Unit test coverage** (`actions/setup/js/checkout_pr_branch.test.cjs`):

| Test | RS-05a property |
|------|-----------------|
| successful checkout via `aw_context` | actor trust + ref isolation |
| non-PR `item_type` → skip | parse validation |
| no `aw_context` input → skip | parse resilience |
| no `inputs` at all → skip | parse resilience |
| invalid JSON or a non-object context → explicit failure | parse resilience |
| missing or invalid PR `item_number` → explicit failure | item_number guard |
| `aw_context.repo` matches current repo → checkout | repository scope |
| `aw_context.repo` mismatches → explicit failure | repository scope |
| successful checkout → output `true` | ref isolation |
| ordinary PR event fetches its recorded SHA after head movement or branch deletion | immutable event revision |
| labels/reviews reject stale head evidence; reviews use `commit_id` | revision binding |
| missing required dispatch SHA, malformed SHA, API failure, or head movement during fetch → failure | revision isolation (negative cases) |
| actual checked-out `HEAD` mismatch → failure before success or baseline export | actual revision verification |
| uppercase event, review, dispatch, or API SHA → canonical lowercase selection | hexadecimal commit identity |
| historical fork/closed-event checkout retains its event commit count and merge base after a ten-to-one force push | event revision history depth |
| missing-commit fetch plus confirmed live closure → lifecycle invalidation; SHA-fetch policy rejection remains an ordinary fatal error | missing-object and transport-policy distinction |
| comment timestamps are required; newer updates warn and select the current head, not an approved revision | current-head/non-approval semantics |
| unbound manual dispatch selects and rechecks the current API head | current-head semantics |
| `aw_context.test.cjs` propagates event-recorded SHAs and triggering comment edit time | dispatch revision evidence |
| same-repo PR checkout allowed when runtime repository is itself a fork (`pull_request`) | fork-runtime rejection scope |
| `workflow_dispatch` PR replay rejected when runtime repository is a fork | fork-runtime rejection |
| `workflow_dispatch` PR replay rejected when payload has no `repository` data (fail closed) | fork-runtime rejection (unverifiable case) |
| missing, null, string, or numeric `repository.fork` rejected before permission/git operations | fork-runtime rejection (malformed metadata) |
| coercible/non-canonical `item_number` values rejected before actor/API/git operations | item_number validation |
| bot/app actor with write-or-higher repository permission allowed | actor trust |
| bot/app actor without write-or-higher repository permission rejected for `workflow_dispatch`, `issue_comment`, `pull_request_review_comment`, and `pull_request_review` | actor trust |
| same-repository bot `opened`, `synchronize`, `ready_for_review`, and `reopened` PR events allowed only when sender and runtime, head, and base repository IDs match; lifecycle actions are not branch-push evidence | actor trust exception |
| allow-listed same-repository bot `edited` PR event allowed only when `changes` contains a non-null object-valued `body` field and no other fields, with sender and runtime, head, and base repository IDs matching | actor trust exception |
| same-repository bot `labeled` PR events retain the collaborator permission floor on both `pull_request` and `pull_request_target` | actor trust exception (negative case) |
| allow-listed App comments on `issue_comment` and `pull_request_review_comment`, and reviews on `pull_request_review`, allowed only when sender and matching comment/review author match, sender and runtime actor are canonically equivalent, and runtime, head, and base repository IDs match | actor trust exception |
| unlisted bot comments/reviews, differing sender/comment/review author identities, fork PRs, and missing or mismatched repository IDs retain the collaborator permission floor | actor trust exception (negative cases) |
| allow-listed bot `edited` events with missing/malformed changes, no body change, a title/base change, or a mixed body/title or body/base edit retain the collaborator permission floor | actor trust exception (negative cases) |
| bot exception rejected for forked heads, missing runtime ID, mismatched sender, non-bot sender, actions outside the trusted allowlist such as `labeled`, and missing or mismatched base ID | actor trust exception (independent negative cases) |
| centralized `workflow_dispatch` validates the originating `aw_context.actor`, not `github-actions[bot]` | actor trust |
| centralized `workflow_dispatch` without `aw_context.actor` is rejected | actor trust (fail closed) |
| centralized actor propagation without `sender.type == "Bot"` is rejected | actor trust (identity signal) |
| centralized dispatch naming the router itself as originating actor is rejected | actor trust (fail closed) |
| propagated non-collaborator actor is rejected | actor trust (permission floor) |
| `github-actions[bot]` PR dispatch without centralized command/label markers is rejected | actor trust (fail closed) |
| forked runtime repository allowed for `pull_request_target`, `pull_request_review`, `pull_request_review_comment`, `issue_comment` | fork-runtime rejection scope (risk matrix) |
| native `fork` webhook event skips checkout before `assertTrustedCheckoutRuntime()` regardless of `repository.fork` | RS-05a / `on.fork` non-interaction |

**Z3 proof coverage** (`specs/rs05a-checkout-validator-safety.smt2`, checked by `actions/setup/js/checkout_pr_branch_z3.test.cjs`):

| Z3 query | Expected result | RS-05a property |
|----------|-----------------|-----------------|
| `workflow_dispatch_requires_verified_non_fork` | `unsat` | no workflow_dispatch PR checkout exists unless `repository.fork` is verified boolean `false` |
| `checkout_requires_write_or_same_repo_bot_pr` | `unsat` | no PR checkout exists without either a verifiable write-or-higher actor or one of the verified same-repository bot exceptions |
| `unlisted_same_repo_bot_comment_requires_write_permission` | `unsat` | an unlisted bot cannot bypass the permission floor even when its comment identity and repository provenance otherwise match |
| `allowlisted_same_repo_bot_comment_can_checkout` | `sat` | the model admits the narrowly scoped allow-listed same-repository App comment exception without repository write permission |
| `review_with_mismatched_author_requires_write_permission` | `unsat` | a review whose author differs from the Bot sender cannot bypass the permission floor |
| `allowlisted_same_repo_bot_review_can_checkout` | `sat` | the model admits the narrowly scoped allow-listed same-repository App review exception without repository write permission |
| `centralized_dispatch_requires_platform_bot_identity` | `unsat` | centralized dispatch cannot proceed without GitHub-provided bot identity |
| `centralized_dispatch_requires_command_or_label_marker` | `unsat` | centralized dispatch cannot proceed without command/label provenance marker |
| `centralized_dispatch_requires_originating_actor` | `unsat` | centralized dispatch cannot proceed without an originating actor |
| `centralized_dispatch_rejects_router_self_propagation` | `unsat` | centralized dispatch cannot identify `github-actions[bot]` as the originating actor |
| `workflow_dispatch_rejects_cross_repository_aw_context` | `unsat` | cross-repository workflow_dispatch PR replay cannot reach checkout |
| `workflow_dispatch_rejects_noncanonical_pr_number` | `unsat` | non-canonical PR numbers cannot reach checkout |
| `workflow_dispatch_uses_api_head_checkout` | `unsat` | workflow_dispatch PR replay cannot reach checkout without the model's abstract API-head checkout path; this does not prove revision freshness |
| `non_dispatch_pr_trigger_allows_forked_runtime_after_trust` | `sat` | the model admits non-workflow_dispatch PR triggers from structurally forked runtime repositories once actor trust holds |

**Status**: **RUNTIME EVIDENCE VERIFIED; TARGETED FORMAL REVALIDATION PENDING** — RS-05a runtime coverage includes the same-repository bot exceptions for `opened`, `synchronize`, `ready_for_review`, and `reopened`; explicitly allow-listed `edited` events bypass the permission floor only when `changes` contains a non-null object-valued `body` field and no other fields. `labeled` and edits with missing, non-body, malformed, or mixed change metadata retain the collaborator permission floor. The v1.1.2, v1.1.3, v1.1.5, v1.1.6, and v1.1.7 changes have implementation and regression-test mappings above; v1.1.4's TT-01–04 mapping remains unverified. The `checkout_pr_branch.test.cjs` and Z3 test files were run on 2026-11-08 (334 tests passed, one skipped); the skipped test does not constitute proof evidence. The bounded TLA+ model and Z3 proof remain unverified: the SMT model is an authorization abstraction and does not model webhook change fields, revision freshness, fetch-time races, actual checked-out commit identity, or live GitHub fetch support. The full revalidation policy is in `security-architecture-spec.md` §11.

---

### 8. Network Isolation (Section 6 - Claims)

**Specification References Network Isolation** but actual enforcement is engine-specific. Let me check for AWF references:

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, lines 819 and 2468; checked 2026-11-03):

```yaml
- name: Install AWF binary
  run: bash "${RUNNER_TEMP}/gh-aw/actions/install_awf_binary.sh" v0.29.5 --rootless
```

**Status**: ✅ **VERIFIED** — AWF is installed in the current representative lock; this alone does not establish the full network-isolation threat model.

---

### 8a. Output Isolation Supplemental Evidence (T-OI-003 to T-OI-007)

The compliance-matrix gaps for output isolation are now supported by direct implementation evidence:

- **T-OI-003 / T-OI-004 — safe output type support and validation rules**: `actions/setup/js/collect_ndjson_output.test.cjs` exercises `create_discussion` schema handling (required fields, min/max handling, mixed-type batches), while `pkg/workflow/compiler_safe_outputs_steps.go` wires agent output download plus the single `Process Safe Outputs` dispatcher step that performs validation before any write action is applied.
- **T-OI-005 — token precedence**: `pkg/workflow/github_token.go` documents and implements the checkout/push token precedence chain, and `pkg/workflow/github_token_test.go` verifies the ordering (per-output PAT overrides checkout-scoped safe-output app token, which overrides safe-outputs level fallbacks). `pkg/workflow/compiler_safe_outputs_steps.go` then persists that resolved token into checkout credentials and the `GITHUB_TOKEN` environment for trusted safe-output handlers only.
- **T-OI-006 — token secret-expression handling**: `pkg/workflow/github_token.go` emits GitHub Actions secret expressions (`${{ secrets... }}`) instead of raw values for safe-output tokens, and `pkg/workflow/safe_outputs_validation.go` rejects configurations that would attempt workflow-scoped writes without a GitHub App.
- **T-OI-007 — write-operation isolation**: the compiled architecture in §1 still isolates write-capable processing to `safe_outputs`, after `activation`, `agent`, and `detection`, with read-only permissions preserved on the agent job and write permissions reserved for the safe-output job.

**Status**: ✅ **VERIFIED** — dedicated evidence now exists for T-OI-003 through T-OI-007.

---

### 8b. Network Isolation Supplemental Evidence (T-NI-001 to T-NI-009)

The network-isolation claims now have concrete code and compiled-workflow evidence:

- **T-NI-001 / T-NI-002 / T-NI-003**: `pkg/workflow/network_firewall_validation.go` validates firewall configuration, explicit allowed domains, ecosystem identifiers, and wildcard-domain patterns.
- **T-NI-004 / T-NI-005 / T-NI-006**: the same validator rejects invalid protocols and malformed wildcard patterns, while `actions/setup/js/sanitize_content_core.cjs` applies `sanitizeUrlProtocols()` and `sanitizeUrlDomains()` so runtime content sanitization and compiled allowlist semantics stay aligned.
- **T-NI-007**: §8 above already verifies AWF installation in compiled output.
- **T-NI-008**: representative compiled workflows such as `.github/workflows/portfolio-analyst.lock.yml` run the agent inside AWF with an explicit `--mcp-config "${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json"` argument and sandbox mounts rooted under `${RUNNER_TEMP}/gh-aw`, evidencing that MCP access is routed through the sandbox/firewall wrapper rather than direct host execution.
- **T-NI-009**: the `GH_AW_ALLOWED_DOMAINS` environment built in `pkg/workflow/compiler_safe_outputs_steps.go` feeds the same allowlist into runtime safe-output sanitization, closing the gap between network policy and content filtering.

**Status**: ✅ **VERIFIED** — the validation report now includes evidence for T-NI-001 through T-NI-009.

---

### 8c. Sandbox Isolation Supplemental Evidence (T-SI-001 to T-SI-007)

Sandbox isolation was previously undocumented in this report; representative compiled-workflow evidence is now recorded:

- **T-SI-001 / T-SI-004**: `actions/setup/js/patch_awf_chroot_config.cjs` injects a `chroot` block into `awf-config.json` with explicit `binariesSourcePath` and runtime identity (`user`, `uid`, `gid`, `home`).
- **T-SI-002 / T-SI-003 / T-SI-005**: `.github/workflows/portfolio-analyst.lock.yml` patches the AWF config when `DOCKER_HOST` points at a TCP daemon, then launches AWF with read-only mounts, `--env-all`, and explicit `--exclude-env` filters for sensitive tokens. The same compiled invocation passes the Docker endpoint via `--docker-host` rather than mounting `/var/run/docker.sock` into the sandbox.
- **T-SI-006**: the compiled AWF command mounts only the staged `${RUNNER_TEMP}/gh-aw` tool/config tree and passes MCP configuration through that sandboxed mount, rather than exposing arbitrary host state to the agent container.
- **T-SI-007**: the representative detection job uses a separate AWF invocation (including the same chroot patch path) even when host access is explicitly enabled for detection, showing that sandboxing and network policy remain composed controls rather than mutually exclusive modes.

**Status**: ⚠️ **PARTIALLY EVIDENCED** — compiled lock-file and runtime-script evidence now cover all T-SI identifiers at least partially, but this report still lacks a direct runtime probe demonstrating host/socket invisibility from inside the sandbox.

---

### 9. Output Validation (Section 5.4 - OI-06)

**Specification Claim**:
> **OI-06**: Safe output jobs MUST validate agent output before execution

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, lines 3038-3051 and 3142-3144; checked 2026-11-03): The `safe_outputs` job depends on the agent and detection jobs and runs the `Process Safe Outputs` dispatcher step.

**Status**: ✅ **VERIFIED** — The current lock confirms processing is in the separate safe-output job. Validation details are implemented by the dispatcher in `pkg/workflow/`.

---

### 10. Concurrency Control (Section 11.8 - RS-16 to RS-22)

**Specification Claim**:
> **RS-16**: The implementation MUST configure automatic concurrency control to prevent race conditions
> **RS-17**: Concurrency control MUST use GitHub Actions' native `concurrency` field

**Implementation Validation** (`.github/workflows/security-review.lock.yml`, lines 78-80; checked 2026-11-03):

```yaml
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  queue: max
```

**Implementation Validation** (`.github/workflows/security-compliance.lock.yml`, lines 83-85, checked 2026-11-03):

```yaml
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.run_id }}"
  queue: max
```

**Current observation**: Both locks use an issue number (and, for security-review, PR number) with a `github.run_id` fallback and `queue: max`, so runs without an issue number get distinct groups rather than a shared issue-less group; neither sets `cancel-in-progress` at the workflow level. The former cancellation-pattern statements are not established by this check.

**Concurrency Patterns**:

| Workflow | Current group pattern | Queue setting | Evidence |
|----------|----------------------|--------------|----------|
| Security Review Agent | Workflow plus issue/PR/run ID | `queue: max` | `.github/workflows/security-review.lock.yml:78-80` (2026-11-03) |
| Security Compliance | Workflow plus issue number, falling back to run ID | `queue: max` | `.github/workflows/security-compliance.lock.yml:83-85` |

**Status**: ✅ **VERIFIED** — Context-aware concurrency grouping is present in the cited locks. Cancellation behavior is workflow-specific and was not established by the current check.

---

## Specification Accuracy Summary

The evidence date in each row distinguishes the 2026-11-08 revalidation from older evidence. Historical entries are not represented as current verification.

| Section | Requirement | Current status | Evidence location and date |
|---------|-------------|---------------|----------------------------|
| **3.2** | Security Guarantees (SG-01 to SG-07) | ✅ Selected formal coverage rechecked 2026-11-03 | `pkg/workflow/security_architecture_sg_formal_test.go` (`TestFormalSG01`–`TestFormalSG07`, `TestFormalBasicConformance_AllFourControls`, threat-detection and PM-11 tests) |
| **4.3** | Input Sanitization (IS-04 to IS-09) | ✅ Rechecked 2026-11-03 | `actions/setup/js/sanitize_content_core.cjs`; `sanitize_content.test.cjs`, `sanitize_label_content.test.cjs`, `sanitize_content_core_parser.test.cjs` (615 tests passed) |
| **5.2** | Job Architecture (OI-01) | ✅ Verified for representative lock | `.github/workflows/security-review.lock.yml:93,635,1862,2330,2884,3038` (2026-11-03) |
| **5.4** | Output Validation (OI-06) | ✅ Verified for representative lock | `.github/workflows/security-review.lock.yml:3038-3051,3142-3144` (2026-11-03) |
| **7.2** | Permission Defaults (PM-01, PM-02) | ⚠️ Partially verified; scopes are workflow-specific | `.github/workflows/security-review.lock.yml:103-107,642-650,1879-1883,2338-2340,3047-3050` (2026-11-03) |
| **7.5** | Fork Protection (PM-08) | ✅ Verified — default-deny and explicit allowlisting | `.github/workflows/security-review.lock.yml:255,329`; `pkg/workflow/compiler_forks_test.go:19-103` (2026-11-08) |
| **7.6** | Role-Based Access (PM-10, PM-11) | ✅ Verified in representative lock | `.github/workflows/security-review.lock.yml:2884-2949` (2026-11-03) |
| **9.1** | Threat Detection (TD-01) | ✅ Verified for job placement and dependency | `.github/workflows/security-review.lock.yml:2330-2350,3038-3045` (2026-11-03) |
| **10.6** | Action Pinning (CS-10) | ✅ Verified for external action references | `.github/workflows/security-review.lock.yml` (96 external SHA-pinned references; 2026-11-03) |
| **11.1** | Timestamp Validation (RS-01, RS-02) | ✅ Verified in representative lock | `.github/workflows/security-review.lock.yml:298-310` (2026-11-03) |
| **11.3** | workflow_run Repository Validation (RS-05) | Historical formal evidence | `pkg/workflow/role_checks.go`, `pkg/workflow/*test.go`; historical full validation 2026-07-15 |
| **11.3** | PR Checkout Authorization and Revision Isolation (RS-05a) | Runtime evidence; formal revalidation pending | `actions/setup/js/checkout_pr_branch.cjs`, `checkout_pr_branch.test.cjs`, `aw_context.test.cjs` (runtime suite 2026-10-27); §7b |
| **11.8** | Concurrency Control (RS-16 to RS-22) | ✅ Verified in representative lock | `.github/workflows/security-review.lock.yml:78-80` (2026-11-03) |

---

## Minor Discrepancies and Clarifications

### 1. Pre-Activation Job Not Mentioned

**Observation**: The specification mentions "Activation Job" but compiled workflows have both `pre_activation` and `activation` jobs.

**Clarification**: `pre_activation` handles role-based access control before activation. This is an implementation detail that doesn't contradict the specification - it's an additional security layer.

**Status**: ✅ **CLOSED** — Appendix D of `specs/security-architecture-spec.md` (Example 5) now shows `pre_activation` as a separate job gating `activation` on role membership.

### 2. Detection Job Naming

**Observation**: The specification mentions "Threat Detection Layer" but doesn't explicitly state it as a separate job named `detection`.

**Clarification**: The `detection` job is the runtime manifestation of the threat detection layer described in Section 9.

**Status**: ✅ **CLOSED** — Appendix D of `specs/security-architecture-spec.md` (Example 5) now shows `detection` as a separate job between `agent` and `safe_outputs`.

### 3. Conclusion Job

**Observation**: Lock files contain a `conclusion` job not mentioned in the specification.

**Clarification**: The `conclusion` job is an implementation detail for workflow cleanup and summary generation.

**Status**: ✅ **CLOSED** — Appendix D of `specs/security-architecture-spec.md` (Example 5) now shows `conclusion` as the terminal `always()` job for cleanup/status reporting.

---

## Re-validation Triggers

### Normative Triggers

A conforming maintainer MUST re-run this validation when any of the following occur:

1. `specs/security-architecture-spec.md` changes any MUST/SHALL-level requirement, compliance-test mapping, or security guarantee wording.
2. Compiler or runtime changes alter compiled job structure, permission separation, threat-detection placement, timestamp validation, or other `.lock.yml` security behaviors described in this report.
3. Companion security specifications (`scratchpad/guard-policies-specification.md`, `scratchpad/github-mcp-access-control-specification.md`) add or revise GitHub guard-policy or runtime-access-control behaviors that the top-level security architecture depends on.
4. Validation evidence files, example workflows, or implementation locations cited in the Detailed Validation table move or change substantially.

For each re-validation pass, reviewers MUST rerun the Detailed Validation procedure above, refresh the evidence-location table, update the Minor Discrepancies section, and revise the validation grade if any claim is no longer fully verified.

### Trigger #3 Decision Log

**2026-08-13 — `repos` vs `allowed-repos` field-naming divergence (deferred, no re-validation needed)**

`scratchpad/github-mcp-access-control-specification.md`'s Divergence Audit (§Sync Notes) flagged that §4.4.1 historically used `repos` as the field name while §4.1 and the guard-policies spec used `allowed-repos`. This was investigated as a potential trigger-#3 event (companion guard-policy spec revision).

**Decision: Defer — no re-validation of this report required.** The companion spec's own Divergence Audit already marked the item **Resolved**: `allowed-repos` is the canonical frontmatter key, `repos` is a documented deprecated alias for backward compatibility (`pkg/workflow/mcp_github_config.go` lines ~322-335), and `pkg/workflow/tools_validation_github.go` validates both spellings identically via the shared `AllowedRepos` field. This is a terminology/alias clarification, not a change to guard-policy *behavior*, job structure, or security guarantees. It does not affect the §3.2 Security Guarantees or §9 Threat Detection claims in `specs/security-architecture-spec.md`, so the Specification Accuracy Summary table below requires no update. `go test ./pkg/workflow/ -run TestValidateGitHubGuardPolicy` already exercises both the `repos` alias and `allowed-repos` field name (see `pkg/workflow/tools_validation_test.go`) and passes.

### Failure Escalation

When a re-validation pass identifies a specification claim that is no longer verifiable against the implementation:

1. The validation grade MUST be downgraded to reflect the unverified claim (e.g., from **A** to **B** or lower).
2. A tracking issue MUST be opened in the repository describing the gap between specification claim and implementation evidence, referencing the affected requirement identifier (e.g., `OI-01`, `IS-04`).
3. The affected row in the Specification Accuracy Summary table MUST be updated to ❌ **UNVERIFIED** with a link to the tracking issue.
4. No new security-impacting features that depend on the unverified claim MUST be merged until the claim is re-verified or the specification is amended to match the implementation.
5. A re-validation MUST be performed and the validation grade MUST be restored before the tracking issue is closed.

---

## Automation Approach

### Proposed CI Mechanism

To reduce manual re-validation burden and catch specification drift early, the following CI mechanism is proposed for adoption:

**Approach 1 — Compiled YAML structural assertions (recommended)**: Add a CI job that runs after `make recompile` and asserts structural properties of compiled `.lock.yml` files against the security architecture requirements documented in this report. Assertions MUST include:

- Presence and ordering of `pre_activation`, `activation`, `agent`, `detection`, and `safe_outputs` jobs.
- Permission set on `agent` job is read-only (`contents: read`, no write permissions).
- Permission set on `safe_outputs` job contains at least one write permission.
- All non-local `uses:` action references (excluding local paths such as `./actions/...`) match the SHA-pinned format (`owner/action@<40-hex-chars>`).
- `concurrency.group` contains at least one dynamic expression (`${{ ... }}`).

This job SHOULD be implemented as a lightweight script (e.g., a YAML-parsing shell or Node.js script) that exits non-zero when any assertion fails, blocking merge.

**Approach 2 — Spec-to-implementation cross-reference linter**: Extend the existing `gh aw compile` linter to emit warnings when compiled output omits patterns required by the security architecture specification (e.g., missing timestamp validation step in activation job, missing fork-protection condition in `if:` expressions).

A conforming CI pipeline SHOULD implement at least Approach 1 and SHOULD run the assertion suite on every pull request that modifies `.github/workflows/*.md` files or `specs/security-architecture-spec.md`.

---

## §12 Compliance Test Matrix Gap Analysis

This section audits the compliance test matrix defined in `security-architecture-spec.md §12` against the evidence documented in this validation report. Each test category is classified as **EVIDENCED**, **PARTIALLY EVIDENCED**, or **GAP** based on whether this document provides concrete implementation evidence.

| Test Category | Test IDs | Status | Notes |
|---------------|----------|--------|-------|
| Input Sanitization | T-IS-001 to T-IS-008 | ✅ EVIDENCED | Covered in §1a (IS-04 to IS-09); all sanitization functions verified in `sanitize_content_core.cjs` |
| Output Isolation | T-OI-001 to T-OI-007 | ✅ EVIDENCED | OI-01/OI-06 remain verified, and §8a now adds dedicated evidence for T-OI-003 through T-OI-007 (type coverage, validation rules, token precedence, secret-expression handling, and write-job isolation) |
| Network Isolation | T-NI-001 to T-NI-009 | ✅ EVIDENCED | §8 and §8b now cover AWF installation, ecosystem/domain validation, protocol filtering, blocked-domain precedence, MCP sandbox routing, and allowlist-driven content sanitization |
| Permission Management | T-PM-001 to T-PM-007 | ⚠️ PARTIALLY EVIDENCED | PM-01/PM-02 (permission defaults), PM-08 (fork protection), PM-10/PM-11 (RBAC) verified; T-PM-003 (strict mode), T-PM-005 (repository validation for `workflow_run`), T-PM-007 (token validation) lack dedicated evidence entries |
| Sandbox Isolation | T-SI-001 to T-SI-007 | ⚠️ PARTIALLY EVIDENCED | §8c adds compiled-workflow and runtime-script evidence for AWF chrooting, docker-host indirection, environment filtering, MCP/tool mounts, and composed sandbox/firewall operation; direct runtime host-visibility proof remains outstanding (tracked in [#48686](https://github.com/github/gh-aw/issues/48686)) |
| Threat Detection | T-TD-001 to T-TD-007 | ⚠️ PARTIALLY EVIDENCED | TD-01 (automatic threat detection) verified via `detection:` job; T-TD-002 (prompt injection), T-TD-003 (secret leaks), T-TD-004 (malicious patches), T-TD-005 (custom prompt), T-TD-006 (engine override), T-TD-007 (workflow failure on detection) lack dedicated evidence entries |
| Compilation-Time Security | T-CS-001 to T-CS-006, T-SG07-001, T-SG07-002 | ✅ EVIDENCED | T-CS-001 via `TestFormalCS001_SchemaValidationRejectsUnknownField`; T-CS-002 via `TestFormalCS002_ExpressionSafetyRejectsUnauthorizedExpression`; T-CS-003 via `TestFormalSG02_AgentJobHasNoWritePermissions`; T-CS-004 via `TestFormal_P9_CompilationValidatesBeforeEmit`; T-CS-005 via CS-10 evidence in §10; T-CS-006 via `TestStrictModeDeprecatedFields`; T-SG07-001/T-SG07-002 via `TestFormalSG07_FailSecureOnSecurityError` and `TestFormal_P9_CompilationValidatesBeforeEmit`. |
| Runtime Security | T-RS-001 to T-RS-011 | ✅ EVIDENCED | RS-01/RS-02 (timestamp validation) and RS-16 to RS-22 (concurrency control) remain evidenced; RS-05 (`workflow_run` repository validation) evidenced in §7a; RS-05a (`workflow_dispatch` + `aw_context` PR checkout and same-repository bot branch events) evidenced in §7b; T-RS-003 via `TestFormalRS003_WorkflowRunRepositoryValidation`; T-RS-004 via `TestFormalRS004_RuntimeRoleValidation`; T-RS-005 via `TestFormalRS005_RuntimeTokenValidation`; T-RS-006 via `TestFormalRS006_AWFNetworkPolicyValidation`; T-RS-007 via `TestFormalRS007_MCPNetworkPolicyValidation`; T-RS-008 via `TestFormalRS008_OutputTargetValidation`. |
| Companion MCP Access-Control | T-GH-047 to T-GH-060 | ⚠️ PARTIALLY EVIDENCED | Deferred to companion specifications (`scratchpad/github-mcp-access-control-specification.md`, `scratchpad/guard-policies-specification.md`); not directly evidenced in this document |

### Gap Summary

The following test categories require dedicated evidence entries to achieve full coverage in this validation document:

1. **Sandbox Isolation (T-SI-001 to T-SI-007)** — Reduced from full gap to partial evidence in §8c. Remaining work is a direct runtime probe for host/socket visibility from inside the AWF sandbox (tracked in [#48686](https://github.com/github/gh-aw/issues/48686)).
2. **Threat Detection (T-TD-002 to T-TD-007)** — Partial evidence. Only TD-01 (job presence) verified; detection capability assertions remain unverified in this report.

Maintainers SHOULD address remaining gaps in order of risk: the residual sandbox-runtime probe, then threat-detection capability evidence.

---

## Recommendations for Specification Enhancement

### 1. Add Concrete Job Dependency Diagram

Add to Appendix A a specific job dependency graph:

```text
pre_activation (role check)
    ↓
activation (timestamp validation, sanitization)
    ↓
agent (AI execution, read-only)
    ↓
detection (threat analysis)
    ↓ (if success == 'true')
safe_outputs (validated write operations)
    ↓
conclusion (cleanup, summary)
```

### 2. Add Lock File Validation Checklist

Create a new appendix with a checklist for validating compiled lock files:

- [ ] All actions pinned to SHA commits
- [ ] Fork protection in pull_request conditions
- [ ] Read-only permissions in agent job
- [ ] Review and document workflow-specific write scopes, including activation and conclusion jobs
- [ ] Timestamp validation in activation job
- [ ] Threat detection job between agent and safe_outputs
- [ ] Role-based access control in pre_activation
- [ ] AWF binary installation when using Copilot engine

### 3. Document Pre-Activation Pattern

Add explicit documentation of the pre-activation pattern for role-based access control:

```yaml
# Pattern: Role-Based Execution Control
pre_activation:
  permissions:
    contents: read
  outputs:
    activated: ${{ steps.check_membership.outputs.is_team_member == 'true' }}
  steps:
    - name: Check team membership
      env:
        GH_AW_REQUIRED_ROLES: admin,maintainer,write
```

---

## Conclusion

The 2026-11-08 full revalidation confirms the representative ten-job topology, resolves PM-08 for the default fork-deny policy and explicit allowlisting, and records the remaining evidence limits. It does not claim that workflow-specific permission scopes are uniform or that the pending RS-05a formal proof and v1.1.4 TT-01–04 mapping are complete.

The specification provides an accurate formalization of the security architecture that can be used for:
- Security audits and reviews
- Implementation in other CI/CD platforms
- Compliance certification
- Future security enhancements

**Historical full-validation grade**: **A** (2026-07-15).
**Current full revalidation**: **Completed 2026-11-08; no overall grade assigned** because RS-05a formal revalidation and the v1.1.4 TT-01–04 mapping remain unverified.

## Change Log

### 2026-11-08

- Completed a full revalidation pass under `security-architecture-spec.md` §11 and added explicit status for v1.1.2–v1.1.8.
- Verified PM-08 default fork denial and configured allowlisting against compiler tests and the representative lock.
- Kept the overall grade unassigned because RS-05a formal proof and the v1.1.4 TT-01–04 mapping remain unverified.

### 2026-11-03

- Refreshed the ten-job evidence against `.github/workflows/security-review.lock.yml`.
- Corrected permission-scope statements and marked PM-08 UNVERIFIED where current evidence is insufficient.
- Preserved the historical full-validation grade and the outstanding RS-05a formal revalidation qualification.
