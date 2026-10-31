# ADR-73546: Separate Workflow Credentials from CLI Authentication

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: pelikhan; gh-aw maintainers [TODO: verify]

---

### Context

The CLI can report healthy authentication while a workflow credential is invalid: PR Sous Chef comment writes failed with HTTP 401, and Issue Monster assignments were skipped despite successful workflow conclusions. Workflow files can reference custom token secrets through AW/Actions expressions, imported configuration, environment bindings, fallback expressions, or default assignment credentials, so checking only built-in tokens does not identify the credentials a workflow actually consumes. Secret values must not be read or exposed, and the CLI OAuth credential must not be reused as a workflow credential. The implementation also needs to prevent an OAuth token from being persisted through any supported secret or Actions-variable storage path.

### Decision

We will treat workflow credentials as distinct from GitHub CLI authentication and make that distinction explicit in diagnostics and storage APIs. `doctor` will discover named workflow secret references with the AW/Actions expression AST, inspect only secret metadata and bounded existing-run evidence, and report token validations as unverified, unknown, or historical credential errors without retrieving values. All Actions secret and variable write paths will reject supplied values containing the GitHub OAuth prefix `gho_` before encryption or upload, while CLI authentication remains available for diagnostic metadata and log reads but is never copied into workflow credentials.

### Alternatives Considered

#### Alternative 1: Treat healthy CLI authentication or a successful workflow conclusion as proof of credential validity

This would preserve the existing simple diagnostic flow and avoid additional API requests. It was rejected because the PR evidence shows both signals can be healthy while workflow operations fail with authentication or permission errors, and a successful run does not identify which fallback token was used.

#### Alternative 2: Retrieve stored secret values or reuse the CLI OAuth token for workflow operations

This could provide direct value-based validation and would avoid discovering every configured reference. It was rejected because GitHub secret APIs do not provide a safe need for exposing stored values, requesting token values would create a disclosure risk, and reusing the `gh auth login` OAuth token violates the separation between CLI and workflow credentials.

#### Alternative 3: Validate only the known assignment token names and rely on secret-presence metadata

This would require less expression parsing and fewer run inspections. It was rejected because it would miss repository-specific custom tokens, imported configuration, named fallbacks, and credentials used by safe outputs or other workflow integrations; metadata alone cannot establish validity, permissions, expiry, or organization approval.

### Consequences

#### Positive
- `doctor` can surface custom workflow token references and historical authentication or permission failures even when a run concluded successfully, while preserving unknown and unverified states instead of claiming validity.
- OAuth credentials are rejected consistently before encryption or upload across direct secret setting, engine setup, interactive setup, bootstrap, and repository, organization, or enterprise variable writes.
- Diagnostics and replacement guidance avoid retrieving, logging, or asking users to provide secret values.

#### Negative
- AST-based discovery, import resolution, and bounded run inspection add implementation complexity, API usage, and up to the documented request and time budgets.
- Dynamic secret names and incomplete metadata or log coverage remain unknown; historical failures cannot prove that every listed token is invalid or identify the failing token.
- The storage guard is based on detecting the `gho_` prefix, so it cannot inspect credentials already stored on GitHub and may reject an unrelated supplied value that contains that marker.

#### Neutral
- JSON doctor and setup results gain a `token_validations` collection, and recognized credential failures or incomplete coverage can produce a nonzero diagnostic result.
- Users repair implicated credentials through GitHub's secure Actions secrets UI; normal CLI authentication continues to authorize metadata and log reads.
- The implementation adds regression tests for expression discovery, imported workflows, run evidence, JSON output, and all supported storage paths.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
