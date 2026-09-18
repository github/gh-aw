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

Choose the smallest set of ecosystems that matches your workflow:

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

`*.example.com` matches `sub.example.com`, `deep.nested.example.com`, and `example.com`. Only a single leading wildcard is supported, so patterns such as `*.*.example.com` are invalid.

> [!TIP]
> `example.com` and `*.example.com` both match subdomains. Use the wildcard form when you want to make that expectation explicit.

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

Workflows use [strict mode](/gh-aw/reference/frontmatter/#strict-mode-strict) by default, which enforces ecosystem identifiers instead of individual domains for security. This applies to all engines.

````yaml
# ❌ Rejected in strict mode
network:
  allowed:
    - "pypi.org"       # Error: use 'python' ecosystem instead
    - "npmjs.org"      # Error: use 'node' ecosystem instead

# ✅ Accepted in strict mode
network:
  allowed:
    - python           # Ecosystem identifier
    - node             # Ecosystem identifier
````

### Error Messages

Strict mode errors explain whether you should switch to an ecosystem identifier or disable strict mode for a custom domain:

````text
error: strict mode: network domains must be from known ecosystems (e.g., 'defaults',
'python', 'node') for all engines in strict mode. Custom domains are not allowed for
security. Did you mean: 'pypi.org' belongs to ecosystem 'python'?
````

````text
error: strict mode: network domains must be from known ecosystems (e.g., 'defaults',
'python', 'node') for all engines in strict mode. Custom domains are not allowed for
security. Set 'strict: false' to use custom domains.
````

### Using Custom Domains

To use custom domains (domains not in known ecosystems), disable strict mode:

````yaml
---
strict: false    # Required for custom domains
network:
  allowed:
    - python           # Ecosystem identifier
    - "api.example.com"  # Custom domain (only allowed with strict: false)
---
````

**Security Note**: Custom domains bypass ecosystem validation. Only disable strict mode when necessary and ensure you trust the custom domains you allow.

## Security Best Practices

Start with `defaults`, add only the ecosystems you need, prefer ecosystem identifiers over individual domains, and keep strict mode enabled unless you explicitly need custom domains. When a workflow is blocked, add access incrementally based on firewall denials.

## Troubleshooting Firewall Blocking

View firewall activity with `gh aw logs --run-id <run-id>` to identify blocked domains:

```text
🔥 Firewall Log Analysis
Blocked Domains:
  ✗ registry.npmjs.org:443 (3 requests) → Add `node` ecosystem
  ✗ pypi.org:443 (2 requests) → Add `python` ecosystem
```

Common mappings are `node` for npm, `python` for PyPI, `containers` for Docker, and `go` for Go modules.

## Advanced Options

Disable all external network access (engine communication still allowed):

```yaml
network: {}
```

View complete ecosystem domain lists in the [ecosystem domains source](https://github.com/github/gh-aw/blob/main/pkg/workflow/data/ecosystem_domains.json).

## Learn More

- [Network Permissions Reference](/gh-aw/reference/network/) for complete network configuration details
- [Playwright Reference](/gh-aw/reference/playwright/) for browser automation requirements
- [Security Guide](/gh-aw/introduction/architecture/) for broader security guidance
- [Troubleshooting](/gh-aw/troubleshooting/common-issues/) for common issues and fixes
