# ADR-71833: Safe-Output Integration Boundaries for Secret-Bearing curl Destinations

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify other reviewers]

---

### Context

The shell taint analyzer (`pkg/workflow/taint`) raises CTR-017 (secret leak) whenever a `${{ secrets.* }}` value flows into a network command. Webhook URLs such as Slack incoming webhooks are credentials that are *consumed* as the authenticated request destination, not disclosed in the payload, so these flows produce false positives. The analyzer cannot safely infer endpoint trust from the secret's opaque value, from a name like `WEBHOOK`/`SLACK`, or from argument position alone, and curl has many options (redirects, proxies, config files, tracing, URL rewriting, multiple URLs) that can send the destination somewhere else or record it. A related gap meant stdin uploads (`curl -T -`, `--upload-file=-`) were not tracked as request bodies.

### Decision

Safe-output jobs are declared integrations with third-party services and may consume credential-bearing destinations without an opt-in comment. The compiler supplies the normalized IDs of resolved `safe-outputs.jobs`, including imports and legacy `safe-jobs`; authored and companion AW analysis uses actual integration declarations. Ordinary jobs and native Actions workflows without AW integration provenance receive no exemption based on names or comments.

The exemption applies only to a direct, whole-value step `env` binding to a single `secrets.X` reference. The script must never mutate the variable (assignments, `read`, `eval`, `source`, `declare`, `printf -v`, arithmetic, and similar constructs revoke endpoint recognition). The variable must also be the entire, single, double-quoted curl destination accepted by a deliberately finite allow-list of curl options; any unknown option disables the exemption. Everywhere else, including payloads, stdin, uploads, non-authentication headers, logs, xtrace, and other URLs, the secret remains tainted. We also fix stdin tracking for `-T -`/`--upload-file=-`.

### Alternatives Considered

#### Alternative 1: Heuristic trust based on variable/secret name or argument position

Treat secrets named like `*WEBHOOK*`/`*SLACK*` or appearing in the URL position of curl as authenticated endpoints automatically. This would fix existing workflows without author changes. It was rejected because names and positions do not prove how the value is consumed, so a heuristic could silently hide real leaks.

#### Alternative 2: Disable or suppress CTR-017 per workflow/step

Rely on the existing broad lint disable or a step-level suppression. This is simpler, but it turns off all secret-leak checking for the scope, including payload, log, and header leaks. With integration provenance, all eight pinned affected workflows pass default lint unchanged, without broad suppression.

#### Alternative 3: Model and allow curl's full option surface

Parse every curl option and reason about redirects, proxies, and config files to decide trust. This was rejected because the option surface is large, changes between curl versions, and is hard to model soundly. A finite grammar that fails closed is easier to audit.

#### Alternative 4: Require an authenticated-endpoint comment

The initial implementation required `# gh-aw: authenticated-endpoint NAME`. This was replaced because declaring a safe-output integration already establishes intended credential consumption; another opt-in imposes unnecessary source edits. The linter no longer interprets this comment.

### Consequences

#### Positive
- Removes CTR-017 false positives for legitimate webhook-URL usage without weakening detection for payloads, headers, logs, or uploads.
- The existing safe-output integration declaration supplies the boundary without extra comments or workflow source edits.
- Fails closed: unknown curl options, mutation of the variable, or non-direct bindings revoke the exemption.
- Closes a stdin-upload tracking gap (`curl -T -`, `--upload-file=-`).

#### Negative
- The integration boundary asserts intent only. It does not verify the secret's real host, protocol, or recipient, or implicit curl configuration such as `~/.curlrc` (though `-q`/`--disable` is accepted).
- Standalone lint does not resolve imported declarations; compiler lint uses resolved imported integration jobs.
- Only `curl` is supported. `wget` and other HTTP clients get no equivalent exemption.

#### Neutral
- The curl option allow-list will need maintenance when authors need additional safe options.
- The contract is documented in `docs/src/content/docs/reference/workflow-linters.md`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
