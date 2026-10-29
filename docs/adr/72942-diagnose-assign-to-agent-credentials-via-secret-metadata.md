# ADR-72942: Diagnose assign-to-agent Credentials in `gh aw doctor` via Secret Metadata

**Date**: 2026-10-29
**Status**: Draft
**Deciders**: pelikhan [TODO: verify additional deciders]

---

### Context

Workflows that use `safe-outputs.assign-to-agent` can look correctly configured while, at runtime, the assignment step falls back through the default chain `secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN` to a credential that cannot assign Copilot (for example `GITHUB_TOKEN` or a GitHub App installation token). Users only discover the problem when a workflow run fails. `gh aw doctor` already checks authentication and repository setup, so it is the natural place for a pre-flight check. The CLI cannot read secret values. It can only list secret names through the repository `actions/secrets` and `actions/organization-secrets` endpoints. Assignment configuration can also come from imported workflow files.

### Decision

We will add an assign-to-agent credential check to `gh aw doctor`. It discovers local workflows, including imported safe-output configuration, that enable `assign-to-agent`. It then uses the `actionslanguageservice` expression parser to resolve the configured `github-token` (or the default fallback chain) into an ordered list of secret references. Finally, it checks those names against the repository and inherited organization Actions secret **metadata**. Each workflow gets a status of `configured`, `missing`, or `unknown`. `GITHUB_TOKEN` and `github-app` configurations are treated as `missing`. Dynamic expressions and failed secret lookups are reported as `unknown`, not `configured`. Any non-`configured` result makes doctor exit nonzero and print prefilled PAT-creation and repository-secret setup links. The results are also exposed as `agent_assignments` in `--json`. The main driver is to catch misconfiguration early without ever handling secret values.

### Alternatives Considered

#### Alternative 1: Validate the token itself (call the Copilot assignment API or introspect the PAT)

This would detect expired, under-scoped, or SSO-unapproved tokens, which a presence check misses. It was rejected because the CLI cannot read stored Actions secret values, and asking users to paste tokens locally would add credential-handling risk. Presence and validity are therefore reported as separate concerns, and the documentation explains the limits.

#### Alternative 2: Runtime-only detection (fail or warn inside the workflow's assignment step)

This is accurate because it uses the real token, but feedback comes only after a run and costs a workflow execution. It does not give the "fix before you run" experience or the direct setup links that `doctor` can provide.

#### Alternative 3: Compile-time validation in `gh aw compile`

This would surface issues during authoring, but it would make compilation depend on network access and API permissions for secret listing, which compilation intentionally avoids. Keeping the check in `doctor` leaves compile hermetic.

### Consequences

#### Positive
- Missing assignment credentials are found before a workflow runs, along with actionable PAT and secret setup links, including enterprise hosts.
- No secret values are handled. Only names from secret metadata are used, and fallback precedence (for example an unreachable fallback after `GITHUB_TOKEN`) is modelled correctly through expression parsing.
- Machine-readable `agent_assignments` output and a nonzero exit status let doctor be used as a CI or automation gate.

#### Negative
- A `configured` status only means the secret exists. Doctor cannot verify a wrong value, expiry, permissions, repository access, or Copilot availability, so false confidence is possible.
- `doctor` now depends on the secret-listing API. Callers without permission get `unknown` and a nonzero exit, which may be noisy for users without admin access.
- `doctor` now has new coupling to workflow compilation and import parsing (`workflow.Compiler`, `parser.ImportCache`) and to the default token fallback chain, which must be kept in sync if that chain changes.

#### Neutral
- The checks are skipped outside a checkout, or when the requested repository has no attached local checkout. Authentication-only diagnostics stay available there.
- `setup_repository.go` was refactored (`checkSetupRepositoryCheckout`, `renderSetupRepositoryCheckResult`) so these checks fit into the existing doctor flow.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
