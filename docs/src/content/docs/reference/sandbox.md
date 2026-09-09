---
title: Sandbox Configuration
description: Configure sandbox environments for AI engines including AWF agent container, mounted tools, runtime environments, and MCP Gateway
sidebar:
  order: 1350
disable-agentic-editing: true
---

The `sandbox` field configures sandbox environments for AI engines (coding agents), providing two main capabilities:

1. **Coding Agent Sandbox** - Controls the agent runtime security using AWF (Agent Workflow Firewall)
2. **Model Context Protocol (MCP) Gateway** - Routes MCP server calls through a unified HTTP gateway

## Configuration

### Coding Agent Sandbox

Configure the coding agent sandbox type to control how the AI engine is isolated:

```yaml wrap
# Use AWF (Agent Workflow Firewall) - default
sandbox:
  agent: awf

# Disable coding agent sandbox - requires an explicit feature flag
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false

# Or omit sandbox entirely to use the default (awf)
```

**Default Behavior**

If `sandbox` is not specified in your workflow, it defaults to `sandbox.agent: awf`. The coding agent sandbox is recommended for all workflows.

**Disabling Coding Agent Sandbox**

Setting `sandbox.agent: false` disables the agent firewall while keeping the MCP gateway enabled. This removes a trust boundary and is only supported when `strict: false`.

To disable the agent sandbox, you **must** set `features.dangerously-disable-sandbox-agent: true`. Missing, false, and non-boolean values are rejected by the compiler.

```yaml wrap
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
```

> [!WARNING]
> Disabling the agent sandbox removes a security trust boundary and is always rejected in strict mode. Only use this opt-out in controlled environments where the agent can be trusted with direct network access.

### Runtime Profiles

`sandbox.agent.runtime` is the single selector for the sandbox security and topology profile. Each value resolves to one supported combination of container runtime, AWF privileges, and host access:

| Runtime | Effective behavior |
| --- | --- |
| `docker` (default) | Default Docker runtime, rootless AWF, network isolation |
| `docker-sudo-iptables` | Docker with privileged AWF, legacy `iptables` networking, and host/service access |
| `gvisor` | **Deprecated:** gVisor with strict network isolation |
| `docker-sbx` | **Deprecated:** KVM microVM; the compiler handles the required privileged setup |
| `cloud-hypervisor` | Preview KVM runtime with its required privileged launcher |

Omitting `runtime` is equivalent to `runtime: docker`, which keeps the secure default. Prefer `docker`; `gvisor` and `docker-sbx` are deprecated and will be removed in a future release.

```yaml wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables
    allow-host-ports: [9000]
```

The compiler derives every privilege the selected runtime needs, including the `sudo` used by the gVisor and Docker sbx installation steps. Unsupported combinations — such as `allow-host-ports` outside `docker-sudo-iptables`, or `runtime-install` outside `gvisor` and `docker-sbx` — fail at compile time. See [Agent Runtimes](/gh-aw/reference/agent-runtimes/) for runner prerequisites.

### MCP Gateway (Experimental)

Route MCP server calls through a unified HTTP gateway:

```yaml wrap
features:
  mcp-gateway: true

sandbox:
  mcp:
    port: 8080
    agent-id: "${{ secrets.MCP_GATEWAY_AGENT_ID }}"
```

### Combined Configuration

Use both coding agent sandbox and MCP gateway together:

```yaml wrap
features:
  mcp-gateway: true

sandbox:
  agent: awf
  mcp:
    port: 8080
```

## Coding Agent Sandbox Types

### AWF (Agent Workflow Firewall)

AWF is the default coding agent sandbox that provides network egress control through domain-based access controls. Network permissions are configured through the top-level [`network`](/gh-aw/reference/network/) field.

```yaml wrap
sandbox:
  agent: awf

network:
  firewall: true
  allowed:
    - defaults
    - python
    - "api.example.com"
```

#### Filesystem Access

AWF makes the host filesystem visible inside the container with appropriate permissions:

| Path Type | Mode | Examples |
|-----------|------|----------|
| User paths | Read-write | `$HOME`, `$GITHUB_WORKSPACE`, `/tmp` |
| System paths | Read-only | `/usr`, `/opt`, `/bin`, `/lib` |
| Docker socket | Hidden | `/var/run/docker.sock` (security) |

#### Host Binaries

All host binaries are available without explicit mounts: system utilities, `gh`, language runtimes, build tools, and anything installed via `apt-get` or setup actions. Verify with `which <tool>`.

> [!WARNING]
> Docker socket is hidden for security. Agents cannot spawn containers.

#### Host Service Ports (`services:`)

The AWF sandbox reaches GitHub Actions `services:` containers through `--allow-host-service-ports`, which resolves each service's actual (possibly dynamically assigned) host port at runtime. This mechanism, and the explicit `allow-host-ports` escape hatch below, both require `sandbox.agent.runtime: docker-sudo-iptables`: the default (strict) runtime profile does not provide a route to host services, even when host-access flags are combined.

```yaml wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables

services:
  postgres:
    image: postgres:18
    ports:
      - 5432:5432
```

For host daemons that are not declared in `services:`, add an explicit allowlist (also `docker-sudo-iptables` only):

```yaml wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables
    allow-host-ports: [9000]
```

Use `allow-host-ports` only for ports that cannot be represented by `services:`. The compiler rejects values outside the TCP port range `1` through `65535`, and rejects ports AWF always blocks as dangerous (e.g. `22`, `3306`, `5432`, `6379`, `9200`) — reach those through `services:` instead.

#### Environment Variables

AWF passes all environment variables via `--env-all`. The host `PATH` is captured as `AWF_HOST_PATH` and restored inside the container, preserving setup action tool paths.

> [!NOTE]
> Go's "trimmed" binaries require `GOROOT` - AWF automatically captures it after `actions/setup-go`.

#### Runtime Tools

Setup actions work transparently. Runtimes update `PATH`, which AWF captures and restores inside the container.

```yaml wrap
---
jobs:
  setup:
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - uses: actions/setup-python@v5
        with:
          python-version: '3.12'
---

Use `go build` or `python3` - both are available.
```

#### Memory Limit (`sandbox.agent.memory`)

By default, AWF uses its own built-in memory limit for the agent container. Set `sandbox.agent.memory` to override this limit on large-memory runners:

```yaml wrap
sandbox:
  agent:
    memory: 8g
```

Valid values are a positive integer followed by a unit: `b`, `k`, `m`, or `g` (case-insensitive). Examples: `512m`, `4g`, `8g`, `1024m`.

When omitted, AWF's own default memory limit applies. Specifying an invalid format (e.g., `48gb` or `48`) is rejected at compile time.

> [!NOTE]
> Exit code 137 means the process received `SIGKILL`. A memory limit can be one cause, but verify with logs before changing `memory`. If you increase `memory`, leave headroom for the runner OS and other processes.

#### Model fallback (`sandbox.agent.model-fallback`)

AWF's API proxy resolves unrecognized model selections against its built-in model catalog and may rewrite them. Set `model-fallback: false` to pass the configured model through to the provider verbatim:

```yaml wrap
sandbox:
  agent:
    model-fallback: false
```

This is required for providers whose model identifiers are not in the built-in catalog (BYOK Azure OpenAI deployment names, self-hosted routers), where rewriting causes HTTP 404 `model_not_found`. When an [`OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL` custom endpoint](/gh-aw/reference/engines/#custom-api-endpoints-via-environment-variables) is configured in `engine.env`, model fallback is disabled automatically; set this field explicitly to override that default.

#### Token steering (`sandbox.agent.token-steering`)

AWF enables API proxy token steering by default. To keep the explicitly configured provider and model, disable it for a workflow:

```yaml wrap
sandbox:
  agent:
    token-steering: false
```

#### Custom infrastructure images (`sandbox.agent.images`)

Self-hosted and air-gapped deployments can mirror, scan, approve, and preload every privileged image AWF launches. Set `sandbox.agent.images` to select digest-pinned replacements for AWF's infrastructure images (requires AWF v0.28.4 or newer):

```yaml wrap
sandbox:
  agent:
    version: v0.28.4
    images:
      squid: registry.example.com/approved/squid:v0.28.4@sha256:<64-hex-digest>
      agent: registry.example.com/approved/agent:v0.28.4@sha256:<64-hex-digest>
      apiProxy: registry.example.com/approved/api-proxy:v0.28.4@sha256:<64-hex-digest>
```

The compiler emits the manifest as `container.images` in the generated AWF configuration.

Supported image roles: `squid`, `agent`, `apiProxy`, `cliProxy`, `buildTools`, `dohProxy`, `enclaveScript`, `enclaveAgent`, `enclaveMcpServer`, and `dindStaging`.

Rules enforced at compile time:

- Every value must be a literal, registry-qualified reference with both a tag and an immutable digest: `registry/repository:tag@sha256:<64 lowercase hex characters>`. Expressions (`${{ ... }}`), environment interpolation, and any other dynamic value are rejected, so no runtime input can influence an infrastructure image.
- Unknown roles are rejected.
- The manifest must cover every role required by the enabled features: `squid`, `agent`, and `apiProxy` are always required; `cliProxy` is required with [`tools.github.mode: gh-proxy`](/gh-aw/reference/tools/), the `integrity-reactions` feature, or raw `--difc-proxy-host` AWF arguments; `buildTools` is required with [`runner.topology: arc-dind`](/gh-aw/reference/self-hosted-runners/); `dohProxy` is required when legacy-security raw AWF arguments enable `--dns-over-https`; `dindStaging` is required when raw AWF arguments enable `--dind-pre-stage-dirs`, `--dind-stage-engine-binary-path`, or `--dind-stage-engine-binary-target-path`; `enclaveScript` and `enclaveAgent` are required for their corresponding [enclave](/gh-aw/experimental/enclaves/) executors, and `enclaveMcpServer` is required whenever any enclave is enabled. AWF fails closed rather than falling back to a default, so an incomplete manifest is a compile error.
- The manifest cannot be combined with controls that select a different effective image: SSL bump, per-enclave `image` overrides, and AWF arguments such as `--image-tag`, `--image-registry`, `--agent-image`, `--build-local`, `--sysroot-image`, and `--dind-staging-image`. The compiler-owned `container.imageTag` is suppressed when the manifest is set.

Omit the field to keep AWF's default role references and gh-aw's existing digest-pin resolution.

> [!NOTE]
> AWF does not accept registry credentials in configuration. The Docker daemon on the runner must already be authenticated to the registry hosting these images.

> [!NOTE]
> Repository-level `.github/workflows/aw.json` `container_pins` mappings already participate in AWF predownload and lock metadata by redirecting AWF's default references. They do not configure the role references AWF resolves at runtime. `sandbox.agent.images` is the authoritative per-workflow runtime manifest and matching predownload set.
>
> `container_pins` never redirects an AWF role supplied by `sandbox.agent.images`: each manifest reference is already a complete, digest-pinned literal, so gh-aw pre-pulls and records it in lock metadata unchanged. Mappings continue to transform non-AWF workflow containers, and they continue to transform default AWF predownload references when `sandbox.agent.images` is omitted.

#### Copilot BYOK request customization (`sandbox.agent.targets.copilot`)

When routing Copilot through a BYOK-compatible upstream behind the AWF proxy, you can attach custom headers, extra request body fields, and an explicit session identifier on upstream requests:

```yaml wrap
sandbox:
  agent:
    targets:
      copilot:
        extraHeaders:
          x-openrouter-title: my-workflow
          http-referer: https://github.com/${{ github.repository }}
        extraBodyFields:
          custom-field: custom-value
        sessionId: ${{ github.run_id }}
```

Use this for OpenAI-compatible proxies and gateways that expect additional request metadata. `sessionId` is opt-in only; gh-aw does not derive it automatically.

> [!NOTE]
> Set `sessionId` only when your upstream expects a session identifier. Some strict OpenAI-compatible providers reject unknown `session_id` fields, so automatic injection would be unsafe.

#### Go cache paths in AWF (`GOMODCACHE` / `GOCACHE`)

When using `actions/setup-go` in AWF, pin Go cache paths explicitly so restore behavior is predictable:

```yaml wrap
jobs:
  setup:
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: false
      - run: |
          echo "GOMODCACHE=$HOME/go/pkg/mod" >> "$GITHUB_ENV"
          echo "GOCACHE=$HOME/.cache/go-build" >> "$GITHUB_ENV"
```

Then cache those paths via top-level `cache:` (see [Frontmatter cache configuration](/gh-aw/reference/frontmatter/)). Keep cache keys scoped to trusted contexts and avoid sharing writeable keys between untrusted and protected runs.

## MCP Gateway

The MCP Gateway routes all MCP server calls through a unified HTTP gateway, enabling centralized management, logging, and authentication for MCP tools.

## Feature Flags

Some sandbox features require feature flags:

| Feature | Flag | Description |
|---------|------|-------------|
| MCP Gateway | `mcp-gateway` | Enable MCP gateway routing |

Enable feature flags in your workflow:

```yaml wrap
features:
  mcp-gateway: true
```

## Long Build Times

Repositories with lengthy build or test cycles — C++ codebases, large monorepos, or complex integration suites — can exhaust the default 20-minute job timeout or hit individual tool-call time limits. This section describes how to tune those limits.

### Setting the Job Timeout (`timeout-minutes`)

The `timeout-minutes` frontmatter field sets the maximum wall-clock time for the entire agent job. The default is 20 minutes. For repositories where a full build or test run takes 10 minutes or more, increase this value:

```yaml wrap
---
on: issues

timeout-minutes: 60   # 60-minute budget for the agent job
---

Fix the failing test in the C++ core library.
```

**Recommended values by repository type:**

| Repository type | Typical build time | Suggested `timeout-minutes` |
|-----------------|-------------------|------------------------------|
| Small (scripts, docs) | < 2 min | 20 (default) |
| Medium (Go, Python, Node) | 2–10 min | 30–60 |
| Large (C++, Rust, Java monorepo) | 10–30 min | 60–120 |
| Very large (distributed, full CI) | > 30 min | 120–360 |

GitHub Actions enforces a hard upper limit of 360 minutes (6 hours) for a single job.

`timeout-minutes` also accepts a GitHub Actions expression, making it easy to parameterize in `workflow_call` reusable workflows:

```yaml wrap
on:
  workflow_call:
    inputs:
      job-timeout:
        type: number
        default: 60

---

timeout-minutes: ${{ inputs.job-timeout }}
```

### Concrete Example: 30-Minute Timeout for a C++ Repository

```yaml wrap
---
on:
  issues:
    types: [opened, labeled]

engine: copilot

runs-on: [self-hosted, linux, x64, large]   # fast self-hosted runner
timeout-minutes: 30                          # 30-minute agent budget

tools:
  bash: [":*"]
  timeout: 300                               # 5-minute per-tool-call budget

network:
  allowed:
    - defaults
    - go
    - node
---

Reproduce the bug described in this issue, add a regression test, and fix it.
Build with `cmake --build build -j$(nproc)` and verify with `ctest --output-on-failure`.
```

### Splitting Build and Test into Separate Steps

Instead of relying on a single large timeout, break long workflows into a custom `jobs:` setup step that caches build outputs, then runs the agent on the pre-built workspace:

```yaml wrap
---
on: issues

timeout-minutes: 45

jobs:
  setup:
    steps:
      - name: Restore build cache
        uses: actions/cache@v4
        with:
          path: build/
          key: cpp-build-${{ hashFiles('CMakeLists.txt', 'src/**') }}
          restore-keys: cpp-build-
      - name: Build (if cache miss)
        run: |
          cmake -B build -DCMAKE_BUILD_TYPE=Release
          cmake --build build -j$(nproc)
      - name: Save build cache
        uses: actions/cache/save@v4
        with:
          path: build/
          key: cpp-build-${{ hashFiles('CMakeLists.txt', 'src/**') }}
---

The build artifacts are already in `build/`. Run the failing tests with
`ctest --test-dir build --output-on-failure -R <pattern>` and fix any failures.
```

Pre-building in a setup job ensures the agent's `timeout-minutes` budget is spent on analysis and code changes, not waiting for compilation.

### Per-Tool-Call Timeout (`tools.timeout`)

`tools.timeout` controls the maximum time for any single tool invocation (e.g., a `bash` command or MCP server call), in seconds. Increase this when individual commands — such as a full build or a slow test suite — routinely take longer than the engine default:

```yaml wrap
tools:
  timeout: 600   # 10 minutes per tool call (seconds)
```

Default values vary by engine: Claude uses 60 s, Codex uses 120 s. See [Tool Timeout Configuration](/gh-aw/reference/tools/#tool-timeout-configuration) for details.

### Self-Hosted Runners for Fast Hardware

For repositories where build time exceeds 10 minutes on standard GitHub-hosted runners, self-hosted runners with more CPU cores, faster storage, and pre-warmed dependency caches can dramatically reduce wall-clock time:

```yaml wrap
---
on: issues

runs-on: [self-hosted, linux, x64, large]   # 32-core self-hosted runner
timeout-minutes: 30
---

Run the full test suite and fix any failures.
```

See [Self-Hosted Runners](/gh-aw/reference/self-hosted-runners/) for setup instructions, including Docker and `sudo` requirements.

### Caching Build Artifacts Between Runs

Use `actions/cache` in a custom `jobs.setup` block to persist build artifacts across agentic runs. This avoids redundant compilation and keeps the agent job within tighter time budgets:

```yaml wrap
---
on: issues

timeout-minutes: 30

jobs:
  setup:
    steps:
      - uses: actions/cache@v4
        with:
          path: |
            ~/.gradle/caches
            build/
          key: gradle-${{ hashFiles('**/*.gradle*') }}
          restore-keys: gradle-
      - run: ./gradlew build -x test --parallel
---

Review the failing tests and apply a fix. Build artifacts are pre-cached.
```

## Learn More

- [Network Permissions](/gh-aw/reference/network/) - Configure network access controls
- [AI Engines](/gh-aw/reference/engines/) - Engine-specific configuration
- [Tools](/gh-aw/reference/tools/) - Configure MCP tools and servers
- [Self-Hosted Runners](/gh-aw/reference/self-hosted-runners/) - Use custom hardware for long-running jobs
- [Frontmatter Reference](/gh-aw/reference/frontmatter/#run-configuration-run-name-runs-on-runs-on-slim-timeout-minutes) - `timeout-minutes` syntax
