# ADR-72942: Diagnose assignment credentials through metadata and recent-run evidence

**Date**: 2026-10-29
**Status**: Proposed (pending PR acceptance)
**Deciders**: @pelikhan (requested credential diagnostics and recent-run inspection)

---

### Context

Workflows using `safe-outputs.assign-to-agent` can appear configured while falling back through `secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN` to an unsuitable credential. Neither `GITHUB_TOKEN` nor GitHub App installation tokens can assign Copilot. The CLI cannot retrieve stored Actions secret values, and a secret name alone does not establish token validity or permissions.

Workflow success is also insufficient evidence: `ignore-if-error` can suppress authentication failures and leave a successful conclusion with zero exported assignment errors. An observed `issue-monster` run, [40859170777](https://github.com/github/gh-aw/actions/runs/40859170777), exhibited that behavior despite HTTP 401 errors. `gh aw doctor` already checks authentication and repository setup, making it a suitable place to combine configuration diagnostics with bounded historical evidence without dispatching a new workflow.

Assignment configuration may come from frontmatter imports or Markdown includes. Shared fragments are not standalone workflows, and consumer overrides must be respected. Effective credentials can use repository, inherited organization, and safe-output environment secret scopes, whose metadata may require permissions unavailable to the caller.

### Decision

Add assignment diagnostics to `gh aw doctor`, keeping configuration presence separate from historical execution:

1. Discover standalone local workflows and merge their imported and Markdown-included safe-output configuration using compiler/parser semantics. Exclude import-safe shared fragments as independent workflows.
2. Resolve static secret references with the shared `actionslanguageservice` expression parser, preserving every fallback operand and its order. Check available secret metadata in the applicable scopes. Uninspectable scopes and dynamic expressions remain unknown; a known lower-priority secret must not hide an unknown higher-priority fallback.
3. Report configuration as `configured`, `missing`, or `unknown`. `configured` means the selected secret name is available, not that its value is a usable PAT. Per-handler App tokens and automatic tokens are unsuitable for assignment. Non-configured results return a nonzero exit status with host-aware PAT and repository-secret setup links.
4. Sample at most five completed runs created in the past seven days per assignment workflow. Inspect the latest run attempt's assignment-handler job logs, bounded to 40 API requests and 90 seconds across workflows. Recognize actual handler confirmations and failure markers rather than run conclusions or exported error counts.
5. Preserve successful assignments, already-assigned targets, credential failures, other assignment failures, and inconclusive evidence as distinct historical outcomes. Detected assignment failures return nonzero even when secret metadata is configured. Unavailable logs and absent evidence do not imply success or prove current credentials are invalid.

Expose configuration and run evidence in `agent_assignments` through `--json`, including the sample window and run links. Do not print or persist raw logs or secret values in diagnostic results. The command performs read-only inspection; it does not dispatch workflows, assign agents, create PATs, or change secrets.

### Alternatives Considered

#### Alternative 1: Validate the token itself (call the Copilot assignment API or introspect the PAT)

Direct validation could detect expired, under-scoped, or SSO-unapproved tokens, but the CLI cannot read stored Actions secret values. Asking users to paste those tokens would add credential-handling risk; attempting assignment would also mutate the repository. Metadata and historical outcomes therefore remain separate evidence, with explicit limitations.

#### Alternative 2: Rely only on runtime handling or workflow conclusions

Runtime handlers use the real credential, but users must locate their logs and successful conclusions can conceal suppressed failures. Retain runtime handling and mine its existing evidence in doctor instead of dispatching a diagnostic workflow or treating a green run as proof of assignment.

#### Alternative 3: Compile-time validation in `gh aw compile`

This would surface issues during authoring, but would add repository-secret metadata permissions and historical-run inspection to compilation. Keep these operational checks in doctor rather than adding them to every compile.

### Consequences

#### Positive
- Missing assignment credentials are found before a workflow runs, along with actionable PAT and secret setup links, including enterprise hosts.
- Secret values are not retrieved. Static secret names and ordered fallbacks are checked without evaluating arbitrary expressions.
- Historical authentication failures remain visible even when workflow success and error counters conceal them.
- Machine-readable `agent_assignments` output and a nonzero exit status let doctor be used as a CI or automation gate.

#### Negative
- Configuration presence and historical success cannot establish current validity, expiry, permissions, repository access, or Copilot availability. Historical failures can also outlive a credential repair.
- API permissions, dynamic environment names, unavailable logs, and bounded samples can leave coverage incomplete. Unknown configuration returns nonzero; unknown history is reported without declaring the current credential broken.
- Discovery, secret-scope precedence, the default token chain, and recognized handler log formats must remain aligned with compiler and runtime behavior.
- Recent-run inspection adds API traffic and latency, controlled by explicit sample, request, and time limits.

#### Neutral
- The checks are skipped outside a checkout, or when the requested repository has no attached local checkout. Authentication-only diagnostics stay available there.
- Personal-account repositories do not have inherited organization secrets. Organization lookup failures must not erase known repository credentials or be mistaken for proof that an inaccessible scope is empty.

---

*This record documents the proposed implementation and does not imply reviewer approval.*
