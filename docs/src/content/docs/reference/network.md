---
title: Network Permissions
description: Control network access for AI engines using ecosystem identifiers and domain allowlists
sidebar:
  order: 1300
---

Control network access for AI engines using the top-level `network` field to specify which domains and services your agentic workflows can access during execution. Supported by all four engines (Copilot, Claude, Codex, Gemini) via the AWF firewall.

If no `network:` permission is specified, it defaults to `network: defaults`, which allows basic infrastructure domains (certificates, JSON schema, Ubuntu, common package mirrors, Microsoft sources).

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

## Protocol-Specific Domain Filtering

Restrict domains to HTTP-only or HTTPS-only access for legacy systems, strict HTTPS enforcement, or gradual migration. This is currently supported by Copilot and Claude when the AWF firewall is enabled. Domains without a protocol prefix allow both HTTP and HTTPS.

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

`network:` also controls which domains may appear in sanitized content. URLs from domains outside the allowlist are replaced with `(redacted)` to reduce exfiltration risk through untrusted links.

> [!TIP]
> If you see `(redacted)` in workflow outputs, add the domain to `network.allowed`. The same allowlist applies to both network egress (when the firewall is enabled) and content sanitization.

GitHub domains (`github.com`, `githubusercontent.com`, etc.) are always allowed by default.

## Ecosystem Identifiers

Mix ecosystem identifiers with specific domains for fine-grained control:

| Identifier | Includes |
|------------|----------|
| `defaults` | Basic infrastructure (certificates, JSON schema, Ubuntu, package mirrors) |
| `github` | GitHub domains (`github.com`, `docs.github.com`, `github.blog`, `*.githubusercontent.com`, and related) |
| `local` | Loopback addresses (`localhost`, `127.0.0.1`, `::1`) |
| `dev-tools` | Popular CI/CD and developer tool services (Codecov, Shields.io, Snyk, Renovate, CircleCI, etc.) |
| `default-safe-outputs` | Compound: `defaults` + `dev-tools` + `github` + `local` — recommended baseline for `safe-outputs.allowed-domains` |
| `containers` | Docker Hub, GitHub Container Registry, Quay, GCR, MCR |
| `linux-distros` | Debian, Ubuntu, Alpine, Fedora, and other Linux package repositories |
| `playwright` | Playwright browser testing (see [Playwright Reference](/gh-aw/reference/playwright/)) |
| `chrome` | Headless Chrome/Puppeteer (`*.google.com`, `*.googleapis.com`, `*.gvt1.com`) |
| `fonts` | Google Fonts (`fonts.googleapis.com`, `fonts.gstatic.com`) |
| `terraform` | HashiCorp registry, apt/yum releases |
| `bazel` | Bazel build system (`releases.bazel.build`, `bcr.bazel.build`) |
| `clojure` | Clojure packages (`clojars.org`) |
| `copilot-vendor` | Plan-specific Copilot API hosts (`api.business.githubcopilot.com`, `api.enterprise.githubcopilot.com`, `api.individual.githubcopilot.com`) and Copilot telemetry (`telemetry.enterprise.githubcopilot.com`) — not enabled by default, since agents route inference through the firewall gateway |
| `copilot` | Copilot engine transport (`api.githubcopilot.com`, GitHub API/web, `host.docker.internal`, `raw.githubusercontent.com`) |
| `claude` | Claude engine transport (Anthropic APIs, GitHub transport, certificate/OCSP services, Ubuntu package metadata, Playwright downloads, `host.docker.internal`) |
| `codex` | Codex engine transport (`api.openai.com`, `chatgpt.com`, GitHub API/web, `host.docker.internal`) |
| `gemini` | Gemini engine transport (`generativelanguage.googleapis.com`, `*.googleapis.com`, GitHub web, `host.docker.internal`, `raw.githubusercontent.com`) |
| `pi` | Pi engine transport (`api.githubcopilot.com`, GitHub web, `host.docker.internal`, `raw.githubusercontent.com`) |
| `pi-base` | Pi provider-independent baseline (`github.com`, `host.docker.internal`, `raw.githubusercontent.com`) |
| `threat-detection` | Compatibility alias for Copilot threat-detection network access (`api.githubcopilot.com`, Copilot plan APIs, telemetry, `api.github.com`, `github.com`, `host.docker.internal`, `registry.npmjs.org`) |
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

## Engine Domain Sets

Engine domain sets are named allowlist bundles for engine CLI authentication and direct provider transport. They are **not** added automatically. Add the matching identifier to `network.allowed` only when the agent needs direct egress to that engine's domains; agent inference normally runs through the AWF API proxy.

| Engine set | Included domains |
|---|---|
| `copilot` | `api.github.com`, `api.githubcopilot.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `claude` | Anthropic APIs, GitHub transport, certificate/OCSP services, Ubuntu package metadata, Playwright downloads, and `host.docker.internal` |
| `codex` | `172.30.0.1`, `api.github.com`, `api.openai.com`, `chatgpt.com`, `github.com`, `host.docker.internal`, `openai.com` |
| `gemini` | `*.googleapis.com`, `generativelanguage.googleapis.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `pi` | `api.githubcopilot.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; provider-scoped models replace the API host with the selected provider endpoint |
| `pi-base` | `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; applied as the provider-independent baseline before a provider prefix is resolved |
| `threat-detection` | Applied automatically only to Copilot threat-detection runs and available as a compatibility alias: Copilot API and telemetry hosts, `api.github.com`, `github.com`, `host.docker.internal`, and `registry.npmjs.org` for read-only lockfile validation. |

### Ecosystem Identifier Validation

Single-word entries in `network.allowed` that match the ecosystem identifier pattern (`[a-z][a-z0-9-]*`) are validated against the known ecosystem list at compile time. An unrecognized identifier produces a compilation error with the full list of valid options:

```yaml wrap
# ❌ Compilation error: 'rustxxxx' is not a valid ecosystem identifier
network:
  allowed:
    - defaults
    - rustxxxx

# ✅ Use the correct identifier
network:
  allowed:
    - defaults
    - rust

# ✅ Dotted domain names are validated as domains, not ecosystem identifiers
network:
  allowed:
    - defaults
    - crates.io
```

## Strict Mode Validation

When [strict mode](/gh-aw/reference/frontmatter/#strict-mode-strict) is enabled (default), strict mode warns when you list individual ecosystem member domains (e.g., `pypi.org`, `npmjs.org`) and recommends the ecosystem identifier instead (e.g., `python`, `node`). Custom domains outside known ecosystems are allowed without warnings.

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

All engines (Copilot, Claude, Codex, Gemini) enforce network permissions through AWF (Agent Workflow Firewall), a wrapper from [github.com/github/gh-aw-firewall](https://github.com/github/gh-aw-firewall) that applies domain-based access controls via `--allow-domains`. AWF automatically includes subdomains (for example, `github.com` allows `api.github.com`), supports wildcard patterns, and logs network activity for audit.

```yaml wrap
engine: copilot          # or claude, codex, gemini
network:
  allowed:
    - defaults             # Basic infrastructure
    - python              # Python ecosystem
    - "api.example.com"   # Custom domain
```

Each engine has a named domain set for CLI authentication and model API transport. These sets expand only when explicitly listed in `network.allowed` and **never include the `node` or `python` ecosystem registries**. Selecting an engine does not grant access to `registry.npmjs.org`, `pypi.org`, or `files.pythonhosted.org`.

That is safe because engine CLIs and SDKs are installed by workflow steps on the runner before the sandboxed agent starts, and containerized `npx`/`uvx` MCP servers are launched by the MCP gateway outside the agent firewall. Registry access inside the sandbox is therefore unnecessary for those components.

This guarantee is scoped to the `node` and `python` ecosystems. Some engine domain sets still include unrelated infrastructure domains used by the CLI itself, such as `ghcr.io`, `packagecloud.io`, and `packages.microsoft.com` for OS-level package or container installation.

To let the agent reach engine domains or the `node`/`python` registries, opt in explicitly with the matching domain set or ecosystem identifier (`copilot`, `node`, `python`, …) in `network.allowed`, or declare the corresponding entry under `runtimes:` for language ecosystems. With `network: {}` or `network: { allowed: [defaults, github] }`, those domains remain blocked.

See [`domains.go`](https://github.com/github/gh-aw/blob/main/pkg/workflow/domains.go) for the full lists. This invariant is enforced by `TestEngineDefaultDomainsDoNotOverlapEcosystems` in [`domains_package_registry_test.go`](https://github.com/github/gh-aw/blob/main/pkg/workflow/domains_package_registry_test.go), which fails the build if any engine's static default domain list overlaps with the full `node` or `python` ecosystem domain sets, including registry entries added later.

### Firewall Log Level

Control the verbosity of AWF firewall logs using `log-level`. Options: `debug` (verbose), `info` (default), `warn`, `error`.

```yaml wrap
network:
  firewall:
    log-level: info
  allowed:
    - defaults
    - python
```

### SSL Bump for HTTPS Inspection

Enable SSL bump to filter HTTPS traffic by URL path patterns instead of only domain names. Use it when you must allow specific API endpoints while blocking others on the same domain. Specify permitted HTTPS patterns with `allow-urls`.

```yaml wrap
network:
  firewall:
    ssl-bump: true
    allow-urls:
      - "https://github.com/githubnext/*"
      - "https://api.github.com/repos/*/issues"
  allowed:
    - defaults
```

**Security**: SSL bump intercepts and decrypts HTTPS as a man-in-the-middle. Enable it only when URL-level filtering is necessary, and craft `allow-urls` patterns carefully to avoid breaking legitimate connections. It requires AWF v0.9.0+ and does not apply to Sandbox Runtime (SRT). See [Sandbox Configuration](/gh-aw/reference/sandbox/) for full AWF options.

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

Wildcard patterns (`*.example.com`) match the base domain and all subdomains at any depth. Only a single leading wildcard is allowed (`*.*.example.com` is invalid), and it must be followed by a dot and domain. Both `example.com` and `*.example.com` match all subdomains.

```yaml wrap
network:
  allowed:
    - defaults
    - "*.cdn.example.com"     # Matches img.cdn.example.com, static.cdn.example.com
    - "*.storage.example.com" # Matches files.storage.example.com
```

## Troubleshooting

If network access is blocked, confirm that the required domains or ecosystems are in `allowed`. Start with `network: defaults`, then add only what the workflow needs. Violations appear in workflow logs.

Use `gh aw logs --run-id <run-id>` to identify blocked domains. For deeper analysis, run `gh aw audit <run-id>`; the **Firewall Analysis** section shows each domain request with its allow/deny status, request volume, and policy attribution. Pass two run IDs to compare runs:

```bash
gh aw audit 12345678              # Single run
gh aw audit 12345678 12345679     # Compare two runs
```

See the [Network Configuration Guide](/gh-aw/guides/network-configuration/#troubleshooting-firewall-blocking) and [Audit Commands](/gh-aw/reference/audit/) for more.

## Learn More

See also the [Network Configuration Guide](/gh-aw/guides/network-configuration/), [Frontmatter](/gh-aw/reference/frontmatter/), [Tools](/gh-aw/reference/tools/), [Playwright](/gh-aw/reference/playwright/), [Audit Commands](/gh-aw/reference/audit/), and the [Security Guide](/gh-aw/introduction/architecture/).
