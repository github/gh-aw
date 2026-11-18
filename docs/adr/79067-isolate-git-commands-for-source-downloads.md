# ADR-79067: Isolate Git Commands Used for Disposable Source Downloads

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: Unknown [TODO: verify]

---

### Context

gh-aw downloads workflow sources and lists directories from remote repositories by shelling out to `git` (`archive`, `clone`, `fetch`, `checkout`, `ls-tree`) in `pkg/cli/download_workflow.go`, `pkg/parser/remote_download_file.go` and `pkg/parser/remote_list_files.go`. These commands previously inherited the user's Git configuration and environment: global/system config, hooks, templates, `core.fsmonitor`, filters, credential helpers, URL rewrites, `GIT_*` overrides and any transport (SSH, `file`, `ext::`). For a clone of untrusted remote content that is only meant to be read, inherited executable settings are an unwanted code-execution and content-alteration surface, and credentials could end up in arguments or on-disk configuration.

### Decision

We will add a shared `gitutil.SourceCommandContext` builder and route every Git operation on disposable source-download clones through it. It disables hooks, templates, fsmonitor, credential helpers, prompts and auto-maintenance, ignores inherited Git configuration and `GIT_*` environment overrides (except `GIT_EXEC_PATH`), runs outside the caller's checkout, and permits only HTTPS without redirects. Authentication uses host-scoped `gh` tokens injected via the child environment (`GIT_CONFIG_*` extraheader), never argv or config files. The primary driver is security: reading source must not execute or be influenced by user-controlled Git configuration.

### Alternatives Considered

#### Alternative 1: Keep inheriting user Git configuration

Leave `exec.CommandContext("git", ...)` calls as they were. This preserves SSH, custom credential helpers and `file` transport support and is the least code. It was not chosen because hooks, filters, templates and URL rewrites can run commands or alter downloaded content during a read-only operation.

#### Alternative 2: Per-call-site hardening flags

Add the needed `-c` flags and env settings individually at each call site. This avoids a new package API but is easy to miss on follow-up operations (`fetch`, `checkout`, `ls-tree`), and duplicates security-sensitive logic across packages. A single builder makes the policy auditable and testable.

#### Alternative 3: Replace shelling out with a pure-Go Git implementation

Using a library such as go-git would avoid inheriting Git configuration entirely. It was not chosen as visible in this PR [TODO: verify]: it would be a much larger change, add a dependency, and risk behavioral differences in sparse-checkout, shallow fetch and partial-clone usage.

### Consequences

#### Positive
- Hostile hooks, filters, templates, URL rewrites and env overrides cannot execute code or change downloaded content; covered by a regression test against a TLS test server.
- Credentials stay out of command arguments, clone URLs and on-disk configuration.
- One shared, documented helper (`pkg/gitutil/README.md`) enforces the policy consistently across the CLI and parser packages.

#### Negative
- SSH, `file` transports, custom remote helpers and custom/interactive credential helpers are no longer supported on these download paths, which may break users relying on them.
- Users depending on global Git config (proxies, custom CA settings, `url.insteadOf` mirrors) will lose that behavior since global/system config is ignored.
- Callers must remember to use the helper for every operation on a source clone; direct `exec.Command("git", ...)` use would bypass the protection.

#### Neutral
- Adds a runtime dependency on `github.com/cli/go-gh/v2/pkg/auth` in `pkg/gitutil`.
- Development checkouts and `ReadFileFromHEAD` are unchanged.
- The commands run with `cmd.Dir` set to the OS temp directory so they do not discover repository config from the caller's working directory.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
