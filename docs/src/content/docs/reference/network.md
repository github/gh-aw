---
title: Network Permissions
description: Control network access for AI engines using ecosystem identifiers and domain allowlists
sidebar:
  order: 1300
---

Control which domains and services AI engines can reach during workflow execution with the top-level `network` field. All built-in engines support it through the AWF firewall; see the [AI Engines reference](/gh-aw/reference/engines/) for the complete engine list.

If no `network:` permission is specified, it defaults to `network: defaults`, which allows only certificate validation, JSON schema, and stock Ubuntu archive/keyserver hosts. The [complete baseline](#defaults-baseline) lists every allowed host.

For custom CA trust when AWF's API proxy verifies upstream TLS, see [Custom CA certificate for the API proxy](/gh-aw/reference/sandbox/#custom-ca-certificate-for-the-api-proxy).

> [!TIP]
> New to network configuration? See the [Network Configuration Guide](/gh-aw/guides/network-configuration/) for practical examples and troubleshooting tips.

## Configuration

Network permissions follow least privilege:

- **Default** (`network: defaults`) allows basic infrastructure only.
- **Selective** (`network: { allowed: [...] }`) allows only listed domains or ecosystems.
- **No access** (`network: {}`) blocks all network access.

Listed domains automatically match subdomains. Wildcards such as `*.example.com` are also supported; see [Wildcard Domain Patterns](#wildcard-domain-patterns).

```yaml wrap
network:
  allowed:
    - defaults              # Basic infrastructure
    - python               # Python/PyPI ecosystem
    - node                 # Node.js/NPM ecosystem
    - "api.example.com"    # Custom domain
```

## Blocking Domains

Use `blocked` to exclude specific domains or ecosystems from the allowed set. Blocked entries take precedence and include subdomains, which is useful for privacy, security, or compliance:

```yaml wrap
network:
  allowed:
    - defaults
    - github
    - node
  blocked:
    - python               # Block Python ecosystem
    - "cdn.example.com"    # Block specific CDN
```

## Hosted web tools

Claude and Codex provider-hosted web search and fetch tools run on provider infrastructure, so `network.allowed` does not constrain their destinations. Configure a separate `hosted-web` policy when they are needed:

```aw wrap
engine: claude
network:
  allowed:
    - defaults
  hosted-web:
    allowed:
      - docs.github.com
    max-uses: 5
```

`allowed` and `blocked` are mutually exclusive, contain lowercase DNS hostnames only, and are never derived from `network.allowed`. The presence of `hosted-web` enables it and requires one non-empty list. Use `hosted-web: false` to reject hosted web tool calls. When `network` is configured for Claude or Codex without `hosted-web`, gh-aw emits `enabled: false` for that engine.

## Protocol-Specific Domain Filtering

Restrict a domain to HTTP-only or HTTPS-only access, for legacy systems, strict HTTPS enforcement, or gradual migration. Supported by Copilot and Claude when the AWF firewall is enabled. Domains without a protocol prefix allow both.

```yaml wrap
engine: copilot
network:
  allowed:
    - "https://secure.api.example.com"   # HTTPS-only access
    - "http://legacy.example.com"        # HTTP-only access  
    - "example.org"                      # Both protocols (default)
    - "https://*.api.example.com"        # HTTPS wildcard
```

## Content Sanitization

`network:` also controls sanitized content: URLs from domains outside the allowlist are replaced with `(redacted)` to reduce exfiltration risk through untrusted links. GitHub domains (`github.com`, `githubusercontent.com`, etc.) are always allowed.

> [!TIP]
> If you see `(redacted)`, add the domain to `network.allowed`. The same allowlist governs egress and sanitization.

## Ecosystem Identifiers

Mix ecosystem identifiers with specific domains for fine-grained control:

| Identifier | Includes |
|------------|----------|
| `defaults` | Certificate validation, JSON schema, and stock Ubuntu archive/keyserver hosts ([complete list](#defaults-baseline)) |
| `github` | GitHub domains (`github.com`, `docs.github.com`, `github.blog`, `*.githubusercontent.com`, and related) |
| `local` | Loopback addresses (`localhost`, `127.0.0.1`, `::1`) |
| `dev-tools` | Popular CI/CD and developer tool services (Codecov, Shields.io, Snyk, Renovate, CircleCI, etc.) |
| `default-safe-outputs` | Compound: `defaults` + `dev-tools` + `github` + `local` — recommended baseline for `safe-outputs.allowed-domains` |
| `containers` | Docker Hub, GitHub Container Registry, Quay, GCR, MCR |
| `linux-distros` | Linux distribution and third-party package sources, including Launchpad PPAs, packagecloud, Snap, Google, and Microsoft repositories — explicit opt-in |
| `playwright` | Playwright browser testing (see [Playwright Reference](/gh-aw/reference/playwright/)) |
| `chrome` | Headless Chrome/Puppeteer (`*.google.com`, `*.googleapis.com`, `*.gvt1.com`) |
| `fonts` | Google Fonts (`fonts.googleapis.com`, `fonts.gstatic.com`) |
| `terraform` | HashiCorp registry, apt/yum releases |
| `bazel` | Bazel build system (`releases.bazel.build`, `bcr.bazel.build`) |
| `clojure` | Clojure packages (`clojars.org`) |
| `copilot`, `claude`, `codex`, `gemini`, `pi`, `pi-base`, `threat-detection` | Engine gateway/GitHub transport sets, not added automatically ([details](#engine-domain-sets)) |
| `copilot-vendor`, `codex-vendor`, `gemini-vendor` | Direct provider API sets, explicit opt-in ([details](#engine-domain-sets)) |
| `dart` | Dart/Flutter packages (`pub.dev`, `storage.googleapis.com`) |
| `deno` | Deno runtime (`deno.land`, `jsr.io`, `googleapis.deno.dev`) |
| `dotnet` | NuGet packages and .NET SDK |
| `elixir` | Elixir packages (`hex.pm`) |
| `go` | Go modules and toolchain downloads (`proxy.golang.org`, `sum.golang.org`, `go.dev`) |
| `haskell` | Haskell packages (`hackage.haskell.org`, GHCup) |
| `java` | Maven Central, Gradle, Adoptium |
| `julia` | Julia packages (`pkg.julialang.org`, `storage.julialang.net`) |
| `kotlin` | Kotlin/JetBrains packages (`download.jetbrains.com`) |
| `latex` | CTAN, TUG, MiKTeX packages — **note**: TeX Live's `tlmgr` uses redirected CTAN mirrors not reachable through the firewall; prefer `apt-get install texlive-full` (covered by `defaults`) or MiKTeX (`packages.miktex.org`) |
| `lean` | Lean packages (`lean-lang.org`, `reservoir.lean-lang.org`) |
| `lua` | Lua packages (`luarocks.org`) |
| `node` | npm, yarn, pnpm, Node.js, Bun |
| `node-cdns` | Node.js CDN assets (jsDelivr, jQuery CDN) |
| `ocaml` | OCaml packages (`opam.ocaml.org`) |
| `perl` | Perl/CPAN packages (`cpan.org`, `metacpan.org`) |
| `php` | PHP Composer packages (`packagist.org`, `getcomposer.org`) |
| `powershell` | PowerShell Gallery (`powershellgallery.com`) |
| `python` | PyPI, conda, pythonhosted.org |
| `python-native` | PyPI + crates.io — for Python packages with native extensions (pyo3/maturin) |
| `r` | R/CRAN packages (`cran.r-project.org`) |
| `ruby` | RubyGems, Bundler |
| `rust` | Rust crates (`crates.io`, `static.rust-lang.org`, `sh.rustup.rs`) |
| `scala` | Scala packages (`repo.scala-sbt.org`, `jitpack.io`) |
| `swift` | Swift packages (`swift.org`, `cocoapods.org`) |
| `zig` | Zig packages (`ziglang.org`) |

### Defaults baseline

`defaults` contains exactly these entries. Each also allows its subdomains:

| Purpose | Hosts |
|---------|-------|
| Certificate validation | `crl3.digicert.com`, `crl4.digicert.com`, `ocsp.digicert.com`, `ts-crl.ws.symantec.com`, `ts-ocsp.ws.symantec.com`, `crl.geotrust.com`, `ocsp.geotrust.com`, `crl.thawte.com`, `ocsp.thawte.com`, `crl.verisign.com`, `ocsp.verisign.com`, `crl.globalsign.com`, `ocsp.globalsign.com`, `crls.ssl.com`, `ocsp.ssl.com`, `crl.identrust.com`, `ocsp.identrust.com`, `crl.sectigo.com`, `ocsp.sectigo.com`, `crl.usertrust.com`, `ocsp.usertrust.com`, `s.symcb.com`, `s.symcd.com` |
| JSON schema | `json-schema.org`, `json.schemastore.org` |
| Stock Ubuntu archives and keyserver | `archive.ubuntu.com`, `security.ubuntu.com`, `keyserver.ubuntu.com`, `azure.archive.ubuntu.com` |

Third-party package sources (`ppa.launchpad.net`, `packagecloud.io`, `api.snapcraft.io`, `packages.cloud.google.com`, `packages.microsoft.com`) require the explicit `linux-distros` opt-in (`allowed: [defaults, linux-distros]`) or individual entries. `www.googleapis.com` is not in `defaults`; allow it explicitly or opt into an ecosystem that includes it, such as `chrome`.

## Engine Domain Sets

Engine domain sets are named bundles for gateway/GitHub transport. They are **not** added automatically. Agent inference normally runs through the AWF API proxy, so direct provider API hosts are granted separately. Workflows that relied on an engine set for direct API egress must add the matching vendor set or literal domain to `network.allowed`.

| Engine set | Included domains |
|---|---|
| `copilot` | `api.github.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `copilot-vendor` | Direct Copilot API hosts and optional telemetry, including `api.githubcopilot.com` |
| `claude` | `api.github.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `codex` | `172.30.0.1`, `api.github.com`, `github.com`, `host.docker.internal` |
| `codex-vendor` | Direct OpenAI API and optional authentication/telemetry: `api.openai.com`, `auth.openai.com`, `ab.chatgpt.com` |
| `gemini` | `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `gemini-vendor` | Direct Gemini API: `generativelanguage.googleapis.com` |
| `pi` | `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; provider endpoint metadata remains model-aware, but the selectable bundle does not grant the provider API host |
| `pi-base` | `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; applied before a provider prefix selects a gateway route |
| `threat-detection` | Applied automatically only to inline Copilot threat-detection runs and available as a compatibility alias: Copilot API hosts, `api.github.com`, `github.com`, and `host.docker.internal`. Telemetry requires an explicit `copilot-vendor` opt-in. |

Inline Copilot detection no longer automatically allows `registry.npmjs.org`, including through the `threat-detection` alias. To keep npm lockfile metadata validation, set `features.gh-aw-detection: false`, add `node` or `registry.npmjs.org` to `network.allowed`, and recompile the workflow. Inline detection inherits only the registry host from `node`, and runtime configuration alone does not opt it in. The grant is direct host access, not read-only, and explicit `network.blocked` rules still take precedence. Without it, registry verification is unavailable; the bundled detector does not provide this metadata verification.

Gemini's default upstream remains available to the AWF API proxy without granting the agent direct access to `generativelanguage.googleapis.com`. Add `gemini-vendor` or that hostname to `network.allowed` for direct access. An explicitly configured `GEMINI_API_BASE_URL` retains custom-target egress; it does not automatically allow the default hostname.

The `claude` set excludes telemetry, certificate/OCSP hosts, package mirrors, container registries, and Playwright downloads. Add them via explicit domains or ecosystem opt-ins such as `defaults`, `containers`, or `playwright` (configuring the Playwright tool also enables its download domains). The default Anthropic target goes through the AWF API proxy and does not grant agent-side access to `api.anthropic.com`; add that literal domain for intentional direct access. `engine.api-target` configures only the Copilot target. A custom Anthropic target set with `ANTHROPIC_BASE_URL` is handled independently of the direct API grant. The broad `anthropic.com` suffix is never included because it would also permit `statsig.anthropic.com`. CLI installation runs outside the agent firewall and needs no download hosts in this set.

The `codex` set excludes direct OpenAI API egress and the broad `openai.com` and `chatgpt.com` suffixes. Add `codex-vendor` for `api.openai.com` or optional agent-side authentication/telemetry. [OpenAI WIF](/gh-aw/reference/auth/#codex-openai-workload-identity-federation) differs: the API proxy exchanges tokens with `auth.openai.com` on its own egress path, so that host and `codex-vendor` are not needed in the agent or detection allowlist. External Codex detection keeps its API transport, authenticates through the API proxy, and keeps any explicit vendor opt-in; GitHub-hosted detection uses Copilot transport without `api.openai.com`.

The `gemini` set excludes `*.googleapis.com`; package downloads such as `storage.googleapis.com` require their own ecosystem opt-in.

`github.com` is retained for direct repository access in the GitHub and engine bundles. AWF has no exact-host-only domain spelling: this suffix also permits GitHub Packages (`npm.pkg.github.com`, `nuget.pkg.github.com`, `maven.pkg.github.com`, and `rubygems.pkg.github.com`) without a language ecosystem opt-in. To exclude that capability while preserving repository access, set `network.blocked: [pkg.github.com]`; blocked domains take precedence over allowed domains.

### Ecosystem Identifier Validation

Single-word entries in `network.allowed` that match the ecosystem identifier pattern (`[a-z][a-z0-9-]*`) are validated against the known ecosystem list at compile time. An unrecognized identifier produces a compilation error with the full list of valid options:

```yaml wrap
network:
  allowed:
    - defaults
    - rustxxxx    # ❌ Compilation error: use `rust` instead
    - crates.io   # ✅ Dotted domain names are validated as domains
```

## Strict Mode Validation

With [strict mode](/gh-aw/reference/frontmatter/#strict-mode-strict) (default), listing individual ecosystem member domains (e.g., `pypi.org`, `npmjs.org`) warns and recommends the ecosystem identifier (e.g., `python`, `node`). Custom domains outside known ecosystems produce no warnings.

````yaml wrap
# ⚠ Warns: recommend 'python' instead of 'pypi.org'
strict: true
network:
  allowed:
    - defaults
    - "pypi.org"
    - "npmjs.org"

# ✅ No warnings
strict: true
network:
  allowed:
    - defaults
    - python
    - node
    - "api.example.com"  # Custom domain
````

The emitted message takes the form:

````text
recommend using ecosystem identifiers instead of individual domain names for better maintainability: 'pypi.org' → 'python', 'npmjs.org' → 'node'
````

## Implementation

All built-in engines listed in the [AI Engines reference](/gh-aw/reference/engines/) enforce network permissions through AWF (Agent Workflow Firewall), a wrapper from [github.com/github/gh-aw-firewall](https://github.com/github/gh-aw-firewall) that applies domain-based access controls via `--allow-domains`. AWF automatically includes subdomains (for example, `github.com` allows `api.github.com`), supports wildcard patterns, and logs network activity for audit.

Each engine's named domain set covers CLI authentication and model API transport. Sets expand only when listed in `network.allowed`, and none include public npm or PyPI registries (`registry.npmjs.org`, `pypi.org`, `files.pythonhosted.org`). Selecting an engine therefore does not grant registry access. GitHub Packages is the deliberate GitHub transport exception described above.

This is safe because engine CLIs and SDKs are installed on the runner before the sandboxed agent starts, and containerized `npx`/`uvx` MCP servers run in the MCP gateway outside the agent firewall. This guarantee covers only the `node` and `python` ecosystems. Engine sets also exclude container registries and OS/package mirrors such as `ghcr.io`, `packagecloud.io`, and `packages.microsoft.com`; add the ecosystem or explicit domain when the agent needs them.

To let the agent reach engine domains or `node`/`python` registries, list the matching set or ecosystem identifier (`copilot`, `node`, `python`, …) in `network.allowed`, or declare the entry under `runtimes:` for language ecosystems. With `network: {}` or `network: { allowed: [defaults, github] }`, those domains stay blocked.

Runtime selection also skips unrelated telemetry hosts: `node` excludes `telemetry.vercel.com`, and `dotnet` excludes `dc.services.visualstudio.com`. This applies whether the ecosystem comes from `runtimes:` or `network.allowed`. Recompile existing workflows to apply these updated domain sets to their lock files.

See [`domains.go`](https://github.com/github/gh-aw/blob/main/pkg/workflow/domains.go) for the full lists. `TestEngineDefaultDomainsDoNotOverlapEcosystems` in [`domains_package_registry_test.go`](https://github.com/github/gh-aw/blob/main/pkg/workflow/domains_package_registry_test.go) checks for overlap with package ecosystems (Node, Python, Go, Dart, Java, .NET, Ruby), apart from the documented GitHub Packages exception.

### Firewall Log Level

Control the verbosity of AWF firewall logs using `--log-level`. Options: `debug` (verbose), `info` (default), `warn`, `error`. Passing custom AWF arguments is an internal override and requires non-strict mode.

```aw wrap
strict: false
sandbox:
  agent:
    args: [--log-level, info]
```

### SSL Bump for HTTPS Inspection

SSL bump enables HTTPS inspection for URL path filtering. Advanced non-strict configurations pass `--ssl-bump` and `--allow-urls` through the internal `sandbox.agent.args` override. URL patterns form one comma-separated argument. The compiler rejects the legacy `network.firewall` block, including `network.firewall.ssl-bump`.

```aw wrap
strict: false
sandbox:
  agent:
    args:
      - --ssl-bump
      - --allow-urls
      - "https://github.com/githubnext/*,https://api.github.com/repos/*/issues"
```

**Security**: SSL bump intercepts and decrypts HTTPS as a man-in-the-middle. Enable it only when URL-level filtering is necessary, and craft `--allow-urls` patterns carefully to avoid breaking legitimate connections. `gh aw lint` checks the authored SSL prerequisite but does not prove runtime AWF enforcement. See [Sandbox Configuration](/gh-aw/reference/sandbox/) for sandbox options.

### Disabling the Firewall

The firewall is enabled by default via `sandbox.agent: awf`. When disabled, network permissions still apply to content sanitization, but the agent can make unrestricted network requests. Disable it only during development or when AWF is incompatible with your workflow; keep it enabled in production.

## Caller-Extensible Allowlist (`network.allowed-input`)

Reusable workflows compiled to `.lock.yml` bake `network.allowed` into the lock file, so consumers normally cannot extend it without forking. Set `network.allowed-input: true` to expose a `workflow_call` input named `network_allowed` so callers can add domains or ecosystems at runtime.

The compiled `network.allowed` remains the baseline. The caller's value is unioned in before AWF starts, ecosystem shorthands are expanded to concrete domain sets, and the result is deduplicated.

```yaml wrap
# source workflow (compiled to a reusable .lock.yml)
on:
  workflow_call:
network:
  allowed:
    - defaults
  allowed-input: true
```

```yaml wrap
# consumer workflow
jobs:
  run:
    uses: owner/repo/.github/workflows/worker.lock.yml@v1
    with:
      network_allowed: rust,github.com
```

The `network_allowed` input is a string accepting comma-separated ecosystem identifiers and/or domains. The behavior of the source workflow is unchanged when `allowed-input` is omitted or `false`.

## Wildcard Domain Patterns

Wildcards (`*.example.com`) match the base domain and all subdomains at any depth. Only one leading wildcard is allowed (`*.*.example.com` is invalid), followed by a dot and domain.

```yaml wrap
network:
  allowed:
    - defaults
    - "*.cdn.example.com"     # Matches img.cdn.example.com, static.cdn.example.com
    - "*.storage.example.com" # Matches files.storage.example.com
```

## Troubleshooting

If network access is blocked, confirm the required domains or ecosystems are in `allowed`, starting from `network: defaults` and adding only what the workflow needs. Violations appear in workflow logs.

Use `gh aw logs --run-id <run-id>` to identify blocked domains. For deeper analysis, run `gh aw audit <run-id>`; the **Firewall Analysis** section shows each domain request with its allow/deny status, request volume, and policy attribution. Pass two run IDs to compare runs:

```bash
gh aw audit 12345678              # Single run
gh aw audit 12345678 12345679     # Compare two runs
```

See the [Network Configuration Guide](/gh-aw/guides/network-configuration/#troubleshooting-firewall-blocking) and [Audit Commands](/gh-aw/reference/audit/) for more.

## Learn More

See also the [Network Configuration Guide](/gh-aw/guides/network-configuration/), [Frontmatter](/gh-aw/reference/frontmatter/), [Tools](/gh-aw/reference/tools/), [Playwright](/gh-aw/reference/playwright/), [Audit Commands](/gh-aw/reference/audit/), and the [Security Guide](/gh-aw/introduction/architecture/).
