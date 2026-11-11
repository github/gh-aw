# ADR-76711: Require an External Token for Security Advisory Safe Outputs

- **Date**: 2026-11-11
- **Status**: Proposed; maintainer approval pending
- **Proposed by**: @pelikhan ([PR #76711 author](https://github.com/github/gh-aw/pull/76711))
- **Deciders**: Pending maintainer review; no approval recorded here

---

### Context

Safe outputs let an agent request GitHub writes that a trusted, separately configured job performs. PR #76711 adds two outputs for repository security advisories: `create-security-advisory` (creates a draft advisory) and `add-security-advisory-comment` (comments on an existing advisory by GHSA ID). Advisory writes are sensitive. The default `GITHUB_TOKEN` cannot perform them, and a draft advisory can be published or given a private fork, so the agent must never control those actions. Comment access depends on advisory access and comment confidentiality, but workflow logs and artifacts can still contain their text.

The [current GitHub REST reference](https://docs.github.com/en/rest/security-advisories/repository-advisories?apiVersion=2026-03-10#create-a-repository-advisory-comment), checked on 2026-11-11, documents `POST /repos/{owner}/{repo}/security-advisories/{ghsa_id}/comments`, a required `body`, and a `201` response containing `id` and `html_url`. It supports GitHub App user and installation tokens and fine-grained personal access tokens with Repository security advisories write permission. The authenticated user must be an advisory collaborator, security manager, or repository administrator. This reference does not label the endpoint public preview or restrict it to public repositories; neither claim is retained. GHES availability depends on the deployed version and is not established by the GitHub.com reference.

### Decision

Add both safe outputs and require an explicitly configured external `github-token` or GitHub App with Repository security advisories write permission, set per output or globally. Compile-time validation rejects `GITHUB_TOKEN` expressions, and the runtime handlers reject a missing token. The create handler requests a draft with `start_private_fork: false`, never forwards agent-supplied state, repository, or visibility fields, and never falls back to a public issue comment. The primary driver is least privilege and confidentiality: the agent can only propose draft content, while publication stays with maintainers.

### Alternatives Considered

#### Alternative 1: Use the default `GITHUB_TOKEN`

This would match existing handlers for issues and pull requests, which need no extra configuration. It is rejected because the Actions token cannot perform advisory writes, so the output would fail after the agent run despite the simpler setup.

#### Alternative 2: Fall back to a public issue comment when advisory comments fail

Falling back to an issue comment could avoid a hard failure when the advisory endpoint is unavailable or access is denied. It is rejected because an issue comment can reach a wider audience than an unpublished advisory and disclose vulnerability details. The handler returns an error with only the HTTP status instead.

### Consequences

#### Positive

- Advisory writes run only with a token the workflow owner explicitly provisions, so the permission surface is visible in configuration.
- The agent can create drafts but cannot publish, create private forks, or choose another repository, so disclosure decisions stay with maintainers.
- Failed API calls expose only the HTTP status, so confidential request bodies are not echoed into logs.
- Statically staged handlers skip external-auth requirements and API calls, and show complete prepared requests with secret values redacted. Expression-valued staged settings still require external-auth configuration because they can resolve to a live write.

#### Negative

- Each workflow that uses these outputs needs a secret or GitHub App with advisory-write permission, which adds provisioning and rotation work.
- Endpoint availability and advisory access must be verified for the target installation. Unsupported versions and authorization failures surface as errors without an issue-comment fallback.
- Workflow logs and artifacts can contain advisory content, so a private draft does not by itself make a run confidential. Sensitive findings need a private workflow repository.
- The `GITHUB_TOKEN` check matches the configured expression text. It guards against common mistakes but does not prove a token's scope.
- There are no automatic retries, so transient API failures require a rerun.

#### Neutral

- The tool catalog, JSON schemas, handler registry, and TypeScript types gain both outputs. Generated lock files change when the catalog changes.
- App-token generation maps the advisory permission, so an App token can be scoped to advisory writes only. An explicit `repository-advisories: none` override suppresses the inferred advisory grant rather than being sent as an invalid installation-token permission; other scopes remain unchanged.
- The comment handler pins the `X-GitHub-Api-Version` header to `2026-03-10`, so a later API version change needs a code update.

---

Maintainer acceptance is pending; this record does not assert reviewer approval.
