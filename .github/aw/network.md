---
description: Network access configuration reference for gh-aw workflows — valid ecosystem identifiers, domain patterns, and common mistakes to avoid.
---

# Network Access Configuration

The `network` frontmatter controls which domains an AI engine can reach. Enforced by the Agent Workflow Firewall (AWF).

## Quick Reference

```yaml
# Shorthand — use default infrastructure domains only
network: defaults

# Custom — allow defaults plus package registries for a Node.js project
network:
  allowed:
    - defaults
    - node

# Custom — allow specific external APIs
network:
  allowed:
    - defaults
    - api.example.com
    - "*.trusted-partner.com"

# No network access
network:
  allowed: []
```

## Valid Values for `network.allowed`

| Type | Examples | Notes |
|---|---|---|
| **Ecosystem identifier** | `defaults`, `node`, `python` | Expands to a curated list of domains |
| **Exact domain** | `api.example.com`, `registry.npmjs.org` | Must be a fully-qualified domain (FQDN) |
| **Wildcard subdomain** | `*.example.com` | Matches `sub.example.com`, `deep.nested.example.com`, and `example.com` itself |

> ⚠️ **Bare shorthands like `npm`, `pypi`, `localhost` are NOT valid** unless listed below. Unrecognised single-word entries cause a **compile-time error**. Use ecosystem identifiers (`node`, `python`) or explicit FQDNs (`registry.npmjs.org`, `pypi.org`) instead.

## Ecosystem Identifiers

Keywords expanding to curated domain lists:

| Identifier | Runtime / Tool | Key Domains Enabled |
|---|---|---|
| `defaults` | Basic infrastructure | Certificate authorities, Ubuntu package verification, JSON schema |
| `github` | GitHub domains | `*.githubusercontent.com`, `codeload.github.com`, `docs.github.com` |
| `github-actions` | GitHub Actions artifacts | Azure Blob storage for action caches and artifacts |
| `node` | npm / yarn / pnpm | `registry.npmjs.org`, `npmjs.com`, `yarnpkg.com` |
| `python` | pip / PyPI / conda | `pypi.org`, `files.pythonhosted.org`, `pip.pypa.io` |
| `go` | Go modules | `proxy.golang.org`, `sum.golang.org`, `go.dev` |
| `dotnet` | NuGet / .NET | `api.nuget.org`, `nuget.org`, `dotnet.microsoft.com` |
| `java` | Maven / Gradle | `repo1.maven.org`, `plugins.gradle.org`, `jdk.java.net` |
| `ruby` | Bundler / RubyGems | `rubygems.org`, `api.rubygems.org` |
| `rust` | Cargo | `crates.io`, `index.crates.io`, `static.crates.io`, `sh.rustup.rs` |
| `swift` | Swift Package Manager | `swift.org`, `cocoapods.org` |
| `php` | Composer / Packagist | `packagist.org`, `repo.packagist.org`, `getcomposer.org` |
| `dart` | pub.dev | `pub.dev`, `pub.dartlang.org` |
| `haskell` | Hackage / GHCup | `*.hackage.haskell.org`, `get-ghcup.haskell.org` |
| `perl` | CPAN | `cpan.org`, `metacpan.org` |
| `containers` | Docker / GHCR | `ghcr.io`, `registry.hub.docker.com`, `*.docker.io` |
| `playwright` | Playwright browsers | `playwright.download.prss.microsoft.com`, `cdn.playwright.dev` |
| `linux-distros` | apt / yum / apk | `deb.debian.org`, `security.debian.org`, Ubuntu/Alpine mirrors |
| `terraform` | HashiCorp | `releases.hashicorp.com`, `registry.terraform.io` |
| `local` | Loopback addresses | `127.0.0.1`, `::1`, `localhost` |
| `bazel` | Bazel build | `releases.bazel.build`, `bcr.bazel.build` |
| `clojure` | Clojure / Clojars | `clojars.org`, `repo.clojars.org` |
| `deno` | Deno / JSR | `deno.land`, `jsr.io` |
| `elixir` | Hex.pm | `hex.pm`, `repo.hex.pm` |
| `fonts` | Google Fonts | `fonts.googleapis.com`, `fonts.gstatic.com` |
| `julia` | Julia packages | `pkg.julialang.org`, `julialang.org` |
| `kotlin` | Kotlin / JetBrains | `packages.jetbrains.team` |
| `lua` | LuaRocks | `luarocks.org` |
| `node-cdns` | JS CDNs | `cdn.jsdelivr.net`, `code.jquery.com`, `unpkg.com` |
| `ocaml` | OPAM | `opam.ocaml.org`, `ocaml.org` |
| `powershell` | PowerShell Gallery | `powershellgallery.com` |
| `r` | CRAN | `cran.r-project.org`, `cloud.r-project.org` |
| `scala` | sbt / Scala | `repo.scala-sbt.org`, `repo1.maven.org` |
| `zig` | Zig packages | `ziglang.org` |
| `dev-tools` | CI/CD tools | Renovate, Codecov, shields.io, and other dev tooling |
| `chrome` | Chrome / Chromium browser services | `safebrowsing.googleapis.com`, `optimizationguide-pa.googleapis.com`, `update.googleapis.com`, `*.gvt1.com` |
| `latex` | LaTeX / TeX | `ctan.org`, `mirror.ctan.org`, `miktex.org`, `tug.org` |
| `lean` | Lean theorem prover | `lean-lang.org`, `elan.lean-lang.org`, `reservoir.lean-lang.org` |
| `python-native` | Python native build deps | Native toolchain mirrors for building Python packages from source |
| `copilot-vendor` | Copilot plan-specific APIs / telemetry | `api.business.githubcopilot.com`, `api.enterprise.githubcopilot.com`, `api.individual.githubcopilot.com`, `telemetry.enterprise.githubcopilot.com` |
| `codex-vendor` | Optional Codex authentication / telemetry (explicit opt-in) | `auth.openai.com`, `ab.chatgpt.com` |
| `copilot` | Copilot engine transport | `api.githubcopilot.com`, GitHub API/web, `host.docker.internal`, `raw.githubusercontent.com` |
| `claude` | Claude engine transport | `api.github.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; direct Anthropic API is added only when the effective target is Anthropic |
| `codex` | Codex engine transport | `172.30.0.1`, `api.openai.com`, GitHub API/web, `host.docker.internal` |
| `gemini` | Gemini engine transport (deprecated, prefer `agy`) | `generativelanguage.googleapis.com`, GitHub web, `host.docker.internal`, `raw.githubusercontent.com` |
| `agy` | Agy (Antigravity CLI) engine transport (experimental) | `github.com`, `api.github.com`, `objects.githubusercontent.com`, `release-assets.githubusercontent.com`, `host.docker.internal` |
| `pi` | Pi engine transport | `api.githubcopilot.com`, GitHub web, `host.docker.internal`, `raw.githubusercontent.com` |
| `pi-base` | Pi provider-independent baseline | `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `threat-detection` | Compatibility alias for Copilot threat detection | Copilot API hosts, GitHub API/web, `host.docker.internal`, `registry.npmjs.org` |

The `chrome` set excludes general Google web/API access, including accounts, fonts,
package hosts, and the Gemini API. Add concrete hosts such as `accounts.google.com`
and `www.google.com`, or the corresponding ecosystem, to `network.allowed` when
needed. Playwright browser installation happens before the agent starts.
The retained `*.gvt1.com` download-CDN rule also permits non-browser artifacts on
that shared CDN; the firewall cannot restrict it by URL path.

## Engine Domain Sets

Engine domain sets are named allow-list bundles for engine CLI authentication
and direct provider transport. They are **not** added automatically. Add the
matching identifier to `network.allowed` only when the agent needs direct
egress to that engine's domains; agent inference normally runs through the AWF
API proxy.

| Engine set | Included domains |
|---|---|
| `copilot` | `api.github.com`, `api.githubcopilot.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `claude` | `api.github.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; `api.anthropic.com` is added only for the default Anthropic target |
| `codex` | `172.30.0.1`, `api.github.com`, `api.openai.com`, `github.com`, `host.docker.internal` |
| `codex-vendor` | Optional authentication/telemetry: `auth.openai.com`, `ab.chatgpt.com` |
| `gemini` | `generativelanguage.googleapis.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com` |
| `pi` | `api.githubcopilot.com`, `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; provider-scoped models replace the API host with the selected provider endpoint |
| `pi-base` | `github.com`, `host.docker.internal`, `raw.githubusercontent.com`; applied as the provider-independent baseline before a provider prefix is resolved |
| `threat-detection` | Applied automatically only to Copilot threat-detection runs and available as a compatibility alias: Copilot API hosts, `api.github.com`, `github.com`, `host.docker.internal`, and `registry.npmjs.org` for read-only lockfile validation. Vendor telemetry is available only when `copilot-vendor` is explicitly added to `network.allowed`. |

The `codex` set excludes the broad `openai.com` and `chatgpt.com` suffixes. Add `codex-vendor` explicitly for the listed authentication/telemetry hosts; any other required vendor host needs its own explicit domain. GitHub-hosted Codex detection uses Copilot transport without direct `api.openai.com` access. The `gemini` set excludes `*.googleapis.com`; package downloads such as `storage.googleapis.com` require an ecosystem opt-in such as `dart`, or an explicit domain.

The `claude` set excludes certificate/OCSP services, package metadata, telemetry, and Playwright downloads. Add the required domain or ecosystem explicitly; configuring the Playwright tool also enables its download domains.

AWF treats bare domains as suffix rules, not exact-host rules. Retaining `github.com` for repository access also permits GitHub Packages subdomains. Set `network.blocked: [pkg.github.com]` to exclude them while preserving repository access; blocked domains take precedence.

## Invalid Shorthands

Services started inside the agent's AWF sandbox are reachable at `localhost` and
`127.0.0.1` without `local`: those addresses are on the default proxy bypass
list. Add `local` only when a workflow needs the firewall allowlist to represent
loopback access explicitly, such as a different runtime topology.

These look like ecosystem identifiers but are **not recognised** — using them causes a **compile-time error**:

| Invalid value | What you probably meant | Correct value |
|---|---|---|
| `npm` | npm registry | `node` |
| `pypi` | Python Package Index | `python` |
| `pip` | pip package manager | `python` |
| `cargo` | Rust crate registry | `rust` |
| `gem` or `gems` | RubyGems | `ruby` |
| `nuget` | NuGet package registry | `dotnet` |
| `maven` | Maven Central | `java` |
| `gradle` | Gradle plugins | `java` |
| `composer` | PHP Composer | `php` |
| `docker` | Docker Hub / GHCR | `containers` |
| `localhost` | Loopback interface | `local` |

## Domain Pattern Rules

- **Wildcard `*` requires a dot prefix**: `*.example.com` valid; bare `*` blocked (rejected outright in strict mode).
- **No protocol prefix**: `https://api.example.com` is invalid — write `api.example.com`.
- **Subdomains must be explicit**: `github.com` does not cover `api.github.com`; use `*.github.com` or both.

## Hosted Web Policy (`network.hosted-web`)

Claude and Codex expose provider-hosted web search/fetch tools that run on provider infrastructure, outside the AWF sandbox — `network.allowed` does not constrain their destinations. Configure a separate policy to control them:

```yaml
network:
  allowed:
    - defaults
  hosted-web:
    allowed:
      - docs.github.com
    max-uses: 5
```

- `allowed` and `blocked` are mutually exclusive; each is a non-empty list of lowercase DNS hostnames (no wildcards).
- `max-uses` optionally caps the number of hosted web tool invocations.
- `hosted-web: false` disables hosted web tools for that workflow.
- **Deny-by-default**: if a Claude or Codex workflow sets `network` explicitly without a `hosted-web` policy, hosted web tools compile to disabled for that engine.
- Setting `hosted-web` on any engine other than Claude or Codex is a compile error.
- On import merge, allowed/blocked lists from imported files combine with the main workflow's, but only when both policies are enabled; the main workflow's scalar settings (such as `max-uses`) and an explicit `hosted-web: false` on either side win — see [syntax-tools-imports.md](syntax-tools-imports.md).
- Copilot has no `hosted-web` policy. Its native web tools are unavailable in offline BYOK mode, so `tools.web-search` and CLI-mode `tools.web-fetch` are compile errors regardless of network restrictions. Use a web MCP server, a supported engine, or SDK mode for custom proxy-aware `web-fetch`.

## Inferring Ecosystem From Repository Files

For workflows that build, test, or install packages, add the matching ecosystem alongside `defaults`:

| File indicators | Ecosystem to add | Enables |
|---|---|---|
| `package.json`, `yarn.lock`, `pnpm-lock.yaml`, `.nvmrc` | `node` | `registry.npmjs.org` |
| `requirements.txt`, `pyproject.toml`, `uv.lock`, `Pipfile` | `python` | `pypi.org`, `files.pythonhosted.org` |
| `go.mod`, `go.sum` | `go` | `proxy.golang.org`, `sum.golang.org` |
| `*.csproj`, `*.sln`, `*.slnx` | `dotnet` | `api.nuget.org` |
| `pom.xml`, `build.gradle` | `java` | `repo1.maven.org` |
| `Gemfile`, `*.gemspec` | `ruby` | `rubygems.org` |
| `Cargo.toml` | `rust` | `crates.io` |
| `Package.swift` | `swift` | `swift.org` |
| `composer.json` | `php` | `packagist.org` |
| `pubspec.yaml` | `dart` | `pub.dev` |

> ⚠️ **`network: defaults` alone is never sufficient for code workflows** — `defaults` covers basic infrastructure (CAs, Ubuntu verification) but not package registries. Always add the language ecosystem.

For artifact-first workflows, include `github-actions` when downloading Actions artifacts. If a missing artifact triggers recomputation, also infer the ecosystem from the manifests needed by the fallback build or tests and allow it alongside `defaults`. Artifact storage and package-registry access are separate needs.

## Common Patterns

Reads GitHub data only:

```yaml
network:
  allowed:
    - defaults
    - github
```

Multi-language project:

```yaml
network:
  allowed:
    - defaults
    - node
    - python
```

Single ecosystem, external APIs, and no-network forms are in [Quick Reference](#quick-reference).
