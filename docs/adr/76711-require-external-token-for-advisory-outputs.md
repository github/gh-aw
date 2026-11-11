# ADR-76711: Require an External Token for Security Advisory Safe Outputs

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: pelikhan (PR #76711 author); [TODO: verify — reviewers who accept this decision]

---

### Context

Safe outputs let an agent request GitHub writes that a trusted, separately configured job performs. PR #76711 adds two outputs for repository security advisories: `create-security-advisory` (creates a draft advisory) and `add-security-advisory-comment` (comments on an existing advisory by GHSA ID). Advisory writes are sensitive. The default `GITHUB_TOKEN` cannot perform them, and a draft advisory can be published or given a private fork, so the agent must never control those actions. Advisory comments use a public-preview REST endpoint, and their visibility follows the advisory, but workflow logs and artifacts can still contain their text.

### Decision

We will add both safe outputs and require an explicitly configured external `github-token` or GitHub App with Repository security advisories write permission, set per output or globally. Compile-time validation rejects `GITHUB_TOKEN` expressions, and the runtime handlers reject a missing token. The create handler always requests a draft with `start_private_fork: false`, never forwards agent-supplied state, repository, or visibility fields, and never falls back to a public issue comment. The primary driver is least privilege and confidentiality: the agent can only propose draft content, while publication stays with maintainers.

### Alternatives Considered

#### Alternative 1: Use the default `GITHUB_TOKEN`

This would match existing handlers for issues and pull requests, which need no extra configuration. We rejected it because the Actions token cannot perform advisory writes, so the output would fail after the agent run. A simpler setup was the main argument for it, and it is not a close call.

#### Alternative 2: Fall back to a public issue comment when advisory comments fail

The advisory comments endpoint is public preview and limited to public GitHub cloud repositories. A fallback to a normal issue comment would keep the feature working on private or GHES repositories. This was the most tempting option because it avoids a hard failure. We rejected it because an issue comment is public and could disclose an unpublished vulnerability. The handler returns an error with only the HTTP status instead.

### Consequences

#### Positive
- Advisory writes run only with a token the workflow owner explicitly provisions, so the permission surface is visible in configuration.
- The agent can create drafts but cannot publish, create private forks, or choose another repository, so disclosure decisions stay with maintainers.
- Failed API calls expose only the HTTP status, so confidential request bodies are not echoed into logs.
- Staged previews skip credential checks and API calls, so workflows can be reviewed before a token is configured.

#### Negative
- Each workflow that uses these outputs needs a secret or GitHub App with advisory-write permission, which adds provisioning and rotation work.
- The comments endpoint is public preview and documented for public GitHub cloud repositories. Private-repository and GHES use is not assumed, and failures there surface as errors. [TODO: verify — current endpoint availability before merge]
- Workflow logs and artifacts can contain advisory content, so a private draft does not by itself make a run confidential. Sensitive findings need a private workflow repository.
- The `GITHUB_TOKEN` check matches the configured expression text. It guards against common mistakes but does not prove a token's scope.
- There are no automatic retries, so transient API failures require a rerun.

#### Neutral
- The tool catalog, JSON schemas, handler registry, and TypeScript types gain both outputs. Generated lock files change when the catalog changes, and this PR regenerates `pr-sous-chef.lock.yml`.
- App-token generation maps the advisory permission, so an App token can be scoped to advisory writes only.
- The comment handler pins the `X-GitHub-Api-Version` header to `2026-03-10`, so a later API version change needs a code update.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
