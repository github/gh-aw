---
title: Network Configuration Guide
description: Common network configurations for package registries, CDNs, and development tools
sidebar:
  order: 450
---

This guide provides practical examples for configuring network access in GitHub Agentic Workflows while maintaining security.

## Quick Start

Configure network access by adding ecosystem identifiers to the `network.allowed` list. Always include `defaults` for basic infrastructure:

```yaml
network:
  allowed:
    - defaults      # Required: Basic infrastructure
    - python        # PyPI, conda (for Python projects)
    - node          # npm, yarn, pnpm (for Node.js projects)
    - go            # Go module proxy (for Go projects)
    - containers    # Docker Hub, GHCR (for container projects)
```

## Available Ecosystems

For the full list of ecosystem identifiers and the domains they include, see the [Ecosystem Identifiers reference](/gh-aw/reference/network/#ecosystem-identifiers).

## Common Configuration Patterns

```yaml
# Python project with containers
network:
  allowed:
    - defaults
    - python
    - containers

# Full-stack web development
network:
  allowed:
    - defaults
    - node
    - playwright
    - github

# DevOps automation
network:
  allowed:
    - defaults
    - terraform
    - containers
    - github
```

## Custom Domains

Add specific domains for your services. Both base domains and wildcard patterns are supported:

```yaml
network:
  allowed:
    - defaults
    - python
    - "api.example.com"        # Matches api.example.com and subdomains
    - "*.cdn.example.com"      # Wildcard: matches any subdomain of cdn.example.com
```

**Wildcard pattern behavior:**

- `*.example.com` matches `sub.example.com`, `deep.nested.example.com`, and `example.com`
- Only single wildcards at the start are supported (e.g., `*.*.example.com` is invalid)

> [!TIP]
> Both `example.com` and `*.example.com` match subdomains. Use wildcards when you want to explicitly document that subdomain access is expected.

## Protocol-Specific Filtering

Restrict domains to specific protocols for enhanced security (Copilot engine with AWF firewall):

```yaml
engine: copilot
network:
  allowed:
    - defaults
    - "https://secure.api.example.com"   # HTTPS-only
    - "http://legacy.internal.com"       # HTTP-only
    - "example.org"                      # Both protocols (default)
sandbox:
  agent: awf  # Firewall enabled
```

**Validation:** Invalid protocols (e.g., `ftp://`) are rejected at compile time.

See [Network Permissions - Protocol-Specific Filtering](/gh-aw/reference/network/#protocol-specific-domain-filtering) for complete details.

## Strict Mode and Ecosystem Identifiers

Workflows use [strict mode](/gh-aw/reference/frontmatter/#strict-mode-strict) by default. Listing an individual domain that belongs to a known ecosystem (such as `pypi.org`) produces an informational recommendation to use its ecosystem identifier (such as `python`); it does not reject the domain. Custom hostnames are also allowed while strict mode is enabled. Only the catch-all wildcard `*` is rejected; supported wildcard domain patterns are described in [Wildcard Domain Patterns](/gh-aw/reference/network/#wildcard-domain-patterns).

See [Strict Mode Validation](/gh-aw/reference/network/#strict-mode-validation) for details.

````yaml wrap
strict: true
network:
  allowed:
    - defaults
    - python
    - "api.internal.example.com"  # Custom hostname
````

**Security Note:** Custom domains bypass ecosystem validation, so review each hostname before adding it to `network.allowed`.

## Security Best Practices

1. **Start minimal** - Only add ecosystems you actually use
2. **Use ecosystem identifiers** - Don't list individual domains (use `python` instead of `pypi.org`, `files.pythonhosted.org`, etc.)
3. **Keep strict mode enabled** - Provides enhanced security validation (enabled by default)
4. **Add incrementally** - Start with `defaults`, add ecosystems as needed based on firewall denials

## Troubleshooting Firewall Blocking

View firewall activity with `gh aw logs --run-id <run-id>` to identify blocked domains:

```text
🔥 Firewall Log Analysis
Blocked Domains:
  ✗ registry.npmjs.org:443 (3 requests) → Add `node` ecosystem
  ✗ pypi.org:443 (2 requests) → Add `python` ecosystem
```

Common mappings: npm/Node.js → `node`, PyPI/Python → `python`, Docker → `containers`, Go modules → `go`.

## Advanced Options

Disable all external network access (engine communication still allowed):

```yaml
network: {}
```

View complete ecosystem domain lists in the [ecosystem domains source](https://github.com/github/gh-aw/blob/main/pkg/workflow/data/ecosystem_domains.json).

## Learn More

- [Network Permissions Reference](/gh-aw/reference/network/) - Complete network configuration reference
- [Playwright Reference](/gh-aw/reference/playwright/) - Browser automation and network requirements
- [Security Guide](/gh-aw/introduction/architecture/) - Security best practices
- [Troubleshooting](/gh-aw/troubleshooting/common-issues/) - Common issues and solutions
