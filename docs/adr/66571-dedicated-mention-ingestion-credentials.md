# ADR-66571: Resolve Mention Allowlists in the Trusted Safe-Output Job

**Date**: 2026-11-19
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

`safe-outputs.mentions.allowed-teams` lets a workflow preserve `@login` mentions for members of specific teams, but **Ingest agent output** runs in the untrusted `agent` job. Supplying a lookup token there would expose it to agent-controlled files and scripts, even if the token were read-only. The trusted `safe_outputs` job already re-sanitizes mention-aware content before publication, so mention candidates can remain intact through ingestion and be checked after the agent artifact is transferred (#50282).

### Decision

Mention/team allowlist resolution will be deferred to the trusted `safe_outputs` job. Ingestion will preserve mention candidates without performing directory, collaborator, or comment-author lookups and will not receive mention-specific credentials. The processor will use `safe-outputs.mentions.github-app`, then `safe-outputs.mentions.github-token`, then `github.token`; these credentials never inherit from global or per-handler safe-output credentials. A dedicated app token is minted in `safe_outputs` before **Process Safe Outputs**, with `issues: read` when `add-comment` is enabled and `members: read` when `allowed-teams` is configured. Permission overrides are restricted to `read` and `none`.

### Alternatives Considered

#### Alternative 1: Let ingestion inherit the existing safe-output write credentials

Reuse `safe-outputs.github-token` / `safe-outputs.github-app` (or `GH_AW_GITHUB_TOKEN`) for the membership lookup. This was the smallest change and required no new schema surface. It was rejected because it would place write-scoped safe-output credentials inside the `agent` job, violating the formal security property that agents execute without GitHub write permissions and that write credentials stay confined to downstream safe-output jobs (`security_architecture_formal_test.go`).

#### Alternative 2: Reuse the default Actions token for mention lookups in the agent job

Keep ingestion token-less but perform directory lookups with the job's default Actions token. Rejected because the default token cannot access organization team membership, and the lookup would still execute in the untrusted job boundary.

### Consequences

#### Positive
- Mentions of allowed team members survive ingestion, so downstream handlers can notify those users — the behaviour #50282 asks for.
- Membership read access is granted with a narrowly scoped, separately configured credential; the agent job gains no write capability and downstream write credentials are unchanged.
- Mention credentials and mention lookups stay out of the agent job; the trusted handler sanitizes mention-aware content before publication.

#### Negative
- Adds a third credential concept (`mentions.github-token` / `mentions.github-app`) with its own precedence and fallback chain (app → PAT → `GITHUB_TOKEN`), increasing the configuration surface users must understand and the compiler must keep consistent.
- Users who expect credentials to cascade from `safe-outputs.github-token` will see mentions silently escaped until they configure the mention-specific token; the non-inheritance rule is deliberate but is a latent surprise.
- Deferring mention filtering means mention-aware messages cross the artifact boundary before final mention sanitization; the safe-output handler must keep its sanitization step before every write.

#### Neutral
- `MentionsConfig` gains `GitHubToken` and `GitHubApp`; the workflow JSON schema, generated frontmatter reference, safe-outputs reference, and the normative safe-outputs specification were updated in the same change.
- Installation owner/repository selection, wildcard scoping, relay repository selection, and missing-key guards reuse existing compiler helpers in the trusted job.
- Same-job token references for mention credentials are validated against the `safe_outputs` job.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
