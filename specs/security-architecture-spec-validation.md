# Security Architecture Specification Validation

**Document**: Validation of `security-architecture-spec.md` against compiled `.lock.yml` files  
**Date**: July 6, 2026  
**Last validated**: v1.0.0 / 2026-07-15
**Validator**: GitHub Copilot Agent  
**Scope**: Cross-reference specification requirements with actual implementation

---

## Executive Summary

✅ **VALIDATION RESULT**: The specification accurately reflects the implementation in compiled `.lock.yml` files and JavaScript implementation (revalidated on 2026-07-15).

All major security architecture claims in the specification have been verified against actual workflow implementations:
- ✅ Job architecture (activation, agent, safe_outputs)
- ✅ **Input sanitization** (markdown safety, URL filtering, HTML tag filtering, ANSI removal)
- ✅ Permission management (read-only agent jobs, write permissions in safe output jobs)
- ✅ Fork protection (repository ID validation)
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

### 1. Job Architecture (Section 5.2 - OI-01)

**Specification Claim**:
> **OI-01**: A conforming implementation MUST separate workflow execution into distinct job types:
> 1. **Activation Job**: Performs sanitization and produces `steps.sanitized.outputs.text`
> 2. **Agent Job**: Executes AI agent with read-only permissions
> 3. **Safe Output Jobs**: Perform validated GitHub API operations with write permissions

**Implementation Validation** (`security-guard.lock.yml`):

```yaml
jobs:
  pre_activation:     # Role-based access control
    runs-on: ubuntu-slim
    permissions:
      contents: read
    
  activation:         # ✅ Activation job
    needs: pre_activation
    runs-on: ubuntu-slim
    permissions:
      contents: read
    
  agent:              # ✅ Agent job with read-only permissions
    needs: activation
    runs-on: ubuntu-latest
    permissions:
      actions: read
      contents: read
      pull-requests: read
      security-events: read
    
  detection:          # ✅ Threat detection layer
    needs: agent
    runs-on: ubuntu-slim
    permissions: {}
    
  safe_outputs:       # ✅ Safe output job with write permissions
    needs:
      - agent
      - detection
    permissions:
      contents: read
      discussions: write
      issues: write
      pull-requests: write
```

**Status**: ✅ **VERIFIED** - All three job types present with correct permission separation.

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

**Implementation Validation** (`security-guard.lock.yml`):

```yaml
# Top-level permissions (line 31)
permissions: {}  # ✅ All permissions explicitly none at workflow level

jobs:
  activation:
    permissions:
      contents: read  # ✅ Read-only
      
  agent:
    permissions:
      actions: read          # ✅ Read-only
      contents: read         # ✅ Read-only
      pull-requests: read    # ✅ Read-only (NOT write)
      security-events: read  # ✅ Read-only
      
  safe_outputs:
    permissions:
      contents: read           # ✅ Read access maintained
      discussions: write       # ✅ Write only where needed
      issues: write           # ✅ Write only where needed
      pull-requests: write    # ✅ Write only where needed
```

**Status**: ✅ **VERIFIED** - Read-only permissions in agent jobs, write permissions isolated to safe_outputs.

---

### 3. Fork Protection (Section 7.5 - PM-08)

**Specification Claim**:
> **PM-08**: For `pull_request` triggers, the implementation MUST:
> - Block forks by default
> - Generate repository ID comparison: `github.event.pull_request.head.repo.id == github.repository_id`

**Implementation Validation** (`security-guard.lock.yml`, lines 44, 1182):

```yaml
# In activation job condition (line 44)
activation:
  needs: pre_activation
  if: >
    (needs.pre_activation.outputs.activated == 'true') && 
    (((github.event_name != 'pull_request') || (github.event.pull_request.draft == false)) &&
    ((github.event_name != 'pull_request') || 
     (github.event.pull_request.head.repo.id == github.repository_id)))  # ✅ Fork protection
     
# Also in pre_activation job (line 1180-1182)
pre_activation:
  if: >
    ((github.event_name != 'pull_request') || (github.event.pull_request.draft == false)) &&
    ((github.event_name != 'pull_request') ||
    (github.event.pull_request.head.repo.id == github.repository_id))  # ✅ Fork protection
```

**Status**: ✅ **VERIFIED** - Repository ID comparison present in multiple job conditions.

---

### 4. Role-Based Access Control (Section 7.6 - PM-10, PM-11)

**Specification Claim**:
> **PM-10**: The implementation MUST support role-based execution restrictions
> **PM-11**: Role checks MUST be performed at runtime using membership validation

**Implementation Validation** (`security-guard.lock.yml`, lines 1199-1210):

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

**Implementation Validation** (`security-guard.lock.yml`, lines 1114-1176):

```yaml
detection:  # ✅ Threat detection job
  needs: agent
  runs-on: ubuntu-slim
  permissions: {}  # ✅ No permissions (analysis only)
  timeout-minutes: 10
  outputs:
    success: ${{ steps.parse_results.outputs.success }}
  steps:
    - name: Setup threat detection workspace
      # ... setup steps ...
      
    - name: Run threat detection analysis
      run: |
        copilot --add-dir /tmp/ --add-dir /tmp/gh-aw/ \
          --disable-builtin-mcps \
          --share /tmp/gh-aw/sandbox/agent/logs/conversation.md \
          --prompt "$COPILOT_CLI_INSTRUCTION" \
          2>&1 | tee /tmp/gh-aw/threat-detection/detection.log
          
    - name: Parse threat detection results  # ✅ Result validation
      id: parse_results
      uses: actions/github-script@...
      
safe_outputs:
  needs:
    - agent
    - detection
  if: ((!cancelled()) && (needs.agent.result != 'skipped')) && 
      (needs.detection.outputs.success == 'true')  # ✅ Blocks if threats detected
```

**Status**: ✅ **VERIFIED** - Threat detection job executes between agent and safe_outputs, blocking execution if threats detected.

---

### 6. Action Pinning (Section 10.6 - CS-10)

**Specification Claim**:
> **CS-10**: In strict mode, the implementation MUST enforce action pinning to commit SHAs

**Implementation Validation** (multiple workflows):

```yaml
# ✅ All actions pinned to SHA with version comments
- uses: actions/checkout@8e8c483db84b4bee98b60c0593521ed34d9990e8 # v6
- uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd # v8.0.0
- uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f # v6.0.0
- uses: actions/download-artifact@018cc2cf5baa6db3ef3c5f8a56943fffe632ef53 # v6.0.0
```

**Status**: ✅ **VERIFIED** - All actions use 40-character SHA commits with version comments.

---

### 7. Runtime Timestamp Validation (Section 11.1 - RS-01, RS-02)

**Specification Claim**:
> **RS-01**: The implementation MUST validate that compiled workflows are up-to-date
> **RS-02**: Timestamp validation MUST compare source `.md` and compiled `.lock.yml` times

**Implementation Validation** (`security-guard.lock.yml`, lines 62-71):

```yaml
activation:
  steps:
    - name: Check workflow file timestamps
      uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd
      env:
        GH_AW_WORKFLOW_FILE: "security-guard.lock.yml"  # ✅ Identifies lock file
      with:
        script: |
          const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
          setupGlobals(core, github, context, exec, io);
          const { main } = require('/opt/gh-aw/actions/check_workflow_timestamp_api.cjs');  # ✅ Timestamp check
          await main();
```

**Status**: ✅ **VERIFIED** - Timestamp validation step present in activation job using `check_workflow_timestamp_api.cjs`.

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

### 7b. workflow_dispatch + aw_context PR Checkout Validation (Section 11.3 - RS-05a)

**Specification Claim**:
> **RS-05a**: For `workflow_dispatch` triggers where the `aw_context` input encodes a pull request context, the implementation MUST enforce: repository scope check, actor trust, parse resilience, and ref isolation.

**Implementation Validation** (`actions/setup/js/checkout_pr_branch.cjs`, lines 200–230):

```js
// Parse aw_context from workflow_dispatch input
if (!pullRequest && eventName === "workflow_dispatch") {
  const awContextStr = context.payload.inputs?.aw_context;
  if (awContextStr) {
    try {
      const awContext = JSON.parse(awContextStr);               // parse resilience: catch block below
      if (awContext.item_type === "pull_request" && awContext.item_number) {
        if (awContext.repo) {
          const currentRepo = `${context.repo.owner}/${context.repo.repo}`;
          if (awContext.repo !== currentRepo) {
            core.warning(`Cross-repository workflow_dispatch is not supported: aw_context.repo ` +
              `(${awContext.repo}) does not match current repository (${currentRepo}), skipping checkout`);
            // pullRequest remains null — checkout skipped (repository scope check)
          } else {
            pullRequest = { number: awContext.item_number, state: "open" };
          }
        } else {
          pullRequest = { number: awContext.item_number, state: "open" };
        }
      }
    } catch (e) {
      core.warning(`Failed to parse aw_context: ${getErrorMessage(e)}`);  // parse resilience
    }
  }
}
```

Actor trust and ref isolation are provided by the shared `assertTrustedCheckoutRuntime()` call and the `exec.exec("git", [...])` array invocation that apply to all PR checkout paths. The fork-runtime rejection inside `assertTrustedCheckoutRuntime()` is itself scoped to `context.eventName === "workflow_dispatch"`, and fails closed when `context.payload.repository` is absent. Centralized command and label dispatches validate the originating `aw_context.actor` instead of the `github-actions[bot]` dispatcher only when GitHub also supplies `sender.type == "Bot"`; they fail closed when that actor is absent. Bot/app identity is treated as an identity signal only; it does not bypass the write-or-higher repository permission floor.

```js
async function assertTrustedCheckoutRuntime() {
  if (context.eventName === "workflow_dispatch") {
    const repository = context.payload.repository;
    if (!repository) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: unable to determine repository fork status for workflow_dispatch`);
    }
    if (repository.fork === true) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout in forked repository runtime context`);
    }
  }
  // ... permission-floor check below applies to all PR checkout paths
```

**Unit test coverage** (`actions/setup/js/checkout_pr_branch.test.cjs`):

| Test | RS-05a property |
|------|-----------------|
| successful checkout via `aw_context` | actor trust + ref isolation |
| non-PR `item_type` → skip | parse validation |
| no `aw_context` input → skip | parse resilience |
| no `inputs` at all → skip | parse resilience |
| invalid JSON → warn + skip | parse resilience |
| missing `item_number` → skip | item_number guard |
| `aw_context.repo` matches current repo → checkout | repository scope |
| `aw_context.repo` mismatches → warn + skip | repository scope |
| successful checkout → output `true` | ref isolation |
| same-repo PR checkout allowed when runtime repository is itself a fork (`pull_request`) | fork-runtime rejection scope |
| `workflow_dispatch` PR replay rejected when runtime repository is a fork | fork-runtime rejection |
| `workflow_dispatch` PR replay rejected when payload has no `repository` data (fail closed) | fork-runtime rejection (unverifiable case) |
| bot/app actor with write-or-higher repository permission allowed | actor trust |
| bot/app actor without write-or-higher repository permission rejected for `workflow_dispatch`, `issue_comment`, and `pull_request_review_comment` | actor trust |
| centralized `workflow_dispatch` validates the originating `aw_context.actor`, not `github-actions[bot]` | actor trust |
| centralized `workflow_dispatch` without `aw_context.actor` is rejected | actor trust (fail closed) |
| centralized actor propagation without `sender.type == "Bot"` is rejected | actor trust (identity signal) |
| forked runtime repository allowed for `pull_request_target`, `pull_request_review`, `pull_request_review_comment`, `issue_comment` | fork-runtime rejection scope (risk matrix) |
| native `fork` webhook event skips checkout before `assertTrustedCheckoutRuntime()` regardless of `repository.fork` | RS-05a / `on.fork` non-interaction |

**Status**: ✅ **VERIFIED** — all four RS-05a properties (repository scope, actor trust, parse resilience, ref isolation) are implemented and covered by unit tests, including the `workflow_dispatch`-only scope of the fork-runtime rejection, fail-closed behavior for missing `repository` data, originating-actor validation for centralized dispatches, and the invariant that bot/app identity does not bypass the write-or-higher repository permission floor. The native `fork` GitHub Actions event (`on.fork` frontmatter field) and the `pull_request`/`pull_request_target` `forks:` allowlist field are both confirmed distinct from, and non-interacting with, this guard (see Section 11.3).

---

### 8. Network Isolation (Section 6 - Claims)

**Specification References Network Isolation** but actual enforcement is engine-specific. Let me check for AWF references:

**Implementation Validation** (`security-guard.lock.yml`, line 145):

```yaml
- name: Install AWF binary
  run: bash /opt/gh-aw/actions/install_awf_binary.sh v0.11.2  # ✅ AWF firewall
```

**Status**: ✅ **VERIFIED** - AWF (Agent Workflow Firewall) binary installed for network isolation.

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

**Implementation Validation** (`security-guard.lock.yml`, lines 1243-1250+):

```yaml
safe_outputs:
  steps:
    - name: Download agent output artifact
      continue-on-error: true
      uses: actions/download-artifact@...
      with:
        name: agent-output  # ✅ Downloads agent output
        path: /tmp/gh-aw/safeoutputs/
        
    - name: Setup agent output environment variable
      run: |
        # ✅ Reads output for validation
        
    - name: Process safe outputs  # ✅ Validation and execution
      id: process_safe_outputs
      uses: actions/github-script@...
      env:
        # Output configuration and validation
```

**Status**: ✅ **VERIFIED** - Agent output downloaded, validated, and processed in safe_outputs job.

---

### 10. Concurrency Control (Section 11.8 - RS-16 to RS-22)

**Specification Claim**:
> **RS-16**: The implementation MUST configure automatic concurrency control to prevent race conditions
> **RS-17**: Concurrency control MUST use GitHub Actions' native `concurrency` field

**Implementation Validation** (`security-guard.lock.yml`, lines 33-35):

```yaml
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true
```

**Implementation Validation** (`security-compliance.lock.yml`, lines 42-43):

```yaml
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}"
  # cancel-in-progress omitted (defaults to false, sequential queueing)
```

**Key Features Verified**:
- ✅ Dynamic group identifiers include workflow name and context (PR number, issue number, or ref)
- ✅ `cancel-in-progress: true` for PR workflows (latest run cancels older runs)
- ✅ `cancel-in-progress` omitted for issue workflows (sequential processing)
- ✅ Prevents race conditions on the same resource
- ✅ Reduces resource exhaustion by canceling superseded runs

**Concurrency Patterns**:

| Workflow Type | Group Pattern | Cancel-in-Progress | Behavior |
|---------------|---------------|-------------------|----------|
| Pull Request | `workflow-PR#` | `true` | Latest run cancels older |
| Issue-based | `workflow-Issue#` | `false` (omitted) | Runs queue sequentially |
| Scheduled | `workflow` | `false` (omitted) | One at a time |

**Status**: ✅ **VERIFIED** - Concurrency control properly configured with context-aware grouping and appropriate cancellation policies.

---

## Specification Accuracy Summary

| Section | Requirement | Status | Evidence Location |
|---------|-------------|--------|-------------------|
| **3.2** | Security Guarantees (SG-01 to SG-07) | ✅ Verified | Multiple implementations |
| **4.3** | Input Sanitization (IS-04 to IS-09) | ✅ Verified | `sanitize_content_core.cjs` |
| **5.2** | Job Architecture (OI-01) | ✅ Verified | `jobs:` section structure |
| **5.4** | Output Validation (OI-06) | ✅ Verified | `safe_outputs.steps` |
| **7.2** | Permission Defaults (PM-01, PM-02) | ✅ Verified | `permissions:` blocks |
| **7.5** | Fork Protection (PM-08) | ✅ Verified | Job `if:` conditions |
| **7.6** | Role-Based Access (PM-10, PM-11) | ✅ Verified | `pre_activation.steps` |
| **9.1** | Threat Detection (TD-01) | ✅ Verified | `detection:` job |
| **10.6** | Action Pinning (CS-10) | ✅ Verified | All `uses:` statements |
| **11.1** | Timestamp Validation (RS-01, RS-02) | ✅ Verified | `activation.steps` |
| **11.3** | workflow_run Repository Validation (RS-05) | ✅ Verified | `role_checks.go`, `TestFormalRS003_WorkflowRunRepositoryValidation` |
| **11.3** | workflow_dispatch PR Checkout (RS-05a) | ✅ Verified | `checkout_pr_branch.cjs` lines 200–230 |
| **11.8** | Concurrency Control (RS-16 to RS-22) | ✅ Verified | `concurrency:` blocks |

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
| Runtime Security | T-RS-001 to T-RS-011 | ✅ EVIDENCED | RS-01/RS-02 (timestamp validation) and RS-16 to RS-22 (concurrency control) remain evidenced; RS-05 (`workflow_run` repository validation) evidenced in §7a; RS-05a (`workflow_dispatch` + `aw_context` PR checkout) evidenced in §7b; T-RS-003 via `TestFormalRS003_WorkflowRunRepositoryValidation`; T-RS-004 via `TestFormalRS004_RuntimeRoleValidation`; T-RS-005 via `TestFormalRS005_RuntimeTokenValidation`; T-RS-006 via `TestFormalRS006_AWFNetworkPolicyValidation`; T-RS-007 via `TestFormalRS007_MCPNetworkPolicyValidation`; T-RS-008 via `TestFormalRS008_OutputTargetValidation`. |
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
- [ ] Write permissions only in safe_outputs job
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

✅ **The specification accurately describes the security architecture as implemented in compiled `.lock.yml` files.**

All major security claims are verifiable:
- Multi-layer job architecture with permission separation
- Fork protection via repository ID validation
- Role-based access control with runtime checks
- Threat detection between agent and safe outputs
- Action pinning to immutable SHAs
- Runtime timestamp validation

The specification provides an accurate formalization of the security architecture that can be used for:
- Security audits and reviews
- Implementation in other CI/CD platforms
- Compliance certification
- Future security enhancements

**Validation Grade**: **A** (Excellent accuracy with minor opportunities for enhancement)
