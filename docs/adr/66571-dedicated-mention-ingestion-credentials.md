# ADR-66571: Use Dedicated, Non-Inheriting Credentials for Mention Allowlist Resolution During Ingestion

**Date**: 2026-11-19
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

`safe-outputs.mentions.allowed-teams` lets a workflow preserve `@login` mentions for members of specific teams, but the **Ingest agent output** step — the step that sanitizes agent output before any safe-output handler runs — executed with the default GitHub Actions token, which cannot read organization team membership. Allowed team members therefore had their mentions escaped during ingestion, and downstream handlers could not restore the notification because the body was already sanitized (#50282). Mention resolution happens inside the `agent` job, which by the repository's security architecture must never hold safe-output *write* credentials, so the fix could not simply reuse `safe-outputs.github-token`, `safe-outputs.github-app`, per-handler tokens, or `GH_AW_GITHUB_TOKEN`. Any new credential also had to survive ingestion's `always()` semantics and remain invisible to the agent process itself.

### Decision

We will introduce dedicated ingestion credentials under `safe-outputs.mentions.github-token` and `safe-outputs.mentions.github-app` that are used **only** for mention allowlist resolution and that never inherit from any other safe-output credential. When a mention-specific app is configured, the compiler mints its installation token in the `agent` job after agent execution and MCP gateway shutdown, immediately before **Ingest agent output**; that token takes precedence over the mention PAT, requests comment-author read scopes plus `members: read` when `allowed-teams` is set, and honours explicit permission overrides. Without mention-specific credentials, ingestion keeps the default Actions token and no app token is minted. The primary driver is least privilege: read-only team-membership access must be grantable without widening what the agent job or the agent process can do.

### Alternatives Considered

#### Alternative 1: Let ingestion inherit the existing safe-output write credentials

Reuse `safe-outputs.github-token` / `safe-outputs.github-app` (or `GH_AW_GITHUB_TOKEN`) for the membership lookup. This was the smallest change and required no new schema surface. It was rejected because it would place write-scoped safe-output credentials inside the `agent` job, violating the formal security property that agents execute without GitHub write permissions and that write credentials stay confined to downstream safe-output jobs (`security_architecture_formal_test.go`).

#### Alternative 2: Defer mention restoration to the downstream safe-output handlers

Keep ingestion token-less and have each handler job — which already holds appropriate credentials — re-expand mentions for allowed teams. Rejected because ingestion escapes mentions before handlers see the payload, so the original `@login` is no longer distinguishable from author-supplied text; restoring it would require carrying unsanitized content past the sanitization boundary, re-introducing the injection risk ingestion exists to prevent.

#### Alternative 3: Mint the mention token at job start, alongside other tokens

Resolve the installation token at the beginning of the `agent` job together with existing token minting. Rejected because the credential would then be present in the environment for the entire agent execution and MCP gateway lifetime; minting it after gateway shutdown keeps the exposure window to the ingestion step only.

### Consequences

#### Positive
- Mentions of allowed team members survive ingestion, so downstream handlers can notify those users — the behaviour #50282 asks for.
- Membership read access is granted with a narrowly scoped, separately configured credential; the agent job gains no write capability and downstream write credentials are unchanged.
- Mention credentials are excluded from agent-visible validation and handler configuration, and app private keys stay out of the pre-ingestion portion of the agent job — properties now asserted by regression tests.

#### Negative
- Adds a third credential concept (`mentions.github-token` / `mentions.github-app`) with its own precedence and fallback chain (app → PAT → `GITHUB_TOKEN`), increasing the configuration surface users must understand and the compiler must keep consistent.
- Users who expect credentials to cascade from `safe-outputs.github-token` will see mentions silently escaped until they configure the mention-specific token; the non-inheritance rule is deliberate but is a latent surprise.
- Token minting inside the `agent` job adds compiler-emitted steps and ordering constraints (`always()`, same-job producer validation) to an already intricate step-lifecycle generator.

#### Neutral
- `MentionsConfig` gains `GitHubToken` and `GitHubApp`; the workflow JSON schema, generated frontmatter reference, safe-outputs reference, and the normative safe-outputs specification were updated in the same change.
- Installation owner/repository selection, wildcard scoping, relay repository selection, and missing-key guards reuse existing compiler helpers rather than introducing a parallel resolution path.
- Secret-safe diagnostic logging now reports which token class ingestion selected (mention app, mention PAT, or default Actions token).

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
