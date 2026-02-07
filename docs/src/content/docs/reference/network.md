---
title: Network Permissions
description: Control network access for AI engines using ecosystem identifiers and domain allowlists
sidebar:
  order: 1300
---

Control network access for AI engines using the top-level `network` field to specify which domains and services your agentic workflows can access during execution.

> **Note**: Network permissions are currently supported by the Claude engine and the Copilot engine (when using the [firewall feature](/gh-aw/reference/sandbox/)).

If no `network:` permission is specified, it defaults to `network: defaults` which allows access to basic infrastructure domains (certificates, JSON schema, Ubuntu, common package mirrors, Microsoft sources).

> [!TIP]
> New to Network Configuration?
> See the [Network Configuration Guide](/gh-aw/guides/network-configuration/) for practical examples, common patterns, and troubleshooting tips for package registries and CDNs.

## Configuration

```yaml wrap
# Default: basic infrastructure only
engine:
  id: copilot
network: defaults

# Ecosystems + custom domains
network:
  allowed:
    - defaults              # Basic infrastructure
    - python               # Python/PyPI ecosystem
    - node                 # Node.js/NPM ecosystem
    - "api.example.com"    # Custom domain

# Custom domains with wildcard patterns
network:
  allowed:
    - "api.example.com"      # Exact domain (also matches subdomains)
    - "*.cdn.example.com"    # Wildcard: matches any subdomain of cdn.example.com

# Protocol-specific domain filtering (Copilot engine only)
network:
  allowed:
    - "https://secure.api.example.com"   # HTTPS-only access
    - "http://legacy.example.com"        # HTTP-only access
    - "example.org"                      # Both HTTP and HTTPS (default)

# Protocol-specific domain filtering (Copilot engine only)
network:
  allowed:
    - "https://secure.api.example.com"   # HTTPS-only access
    - "http://legacy.example.com"        # HTTP-only access
    - "example.org"                      # Both HTTP and HTTPS (default)

# No network access
network: {}

# Block specific domains
network:
  allowed:
    - defaults              # Basic infrastructure
    - python               # Python/PyPI ecosystem
  blocked:
    - "tracker.example.com" # Block specific tracking domain
    - "analytics.example.com" # Block analytics

# Block entire ecosystems
network:
  allowed:
    - defaults
    - github
    - node
  blocked:
    - python               # Block Python/PyPI even if in defaults
```

## Blocking Domains

Use the `blocked` field to block specific domains or ecosystems while allowing others. Blocked domains take precedence over allowed domains, enabling fine-grained control:

```yaml wrap
# Block specific tracking/analytics domains
network:
  allowed:
    - defaults
    - github
  blocked:
    - "tracker.example.com"
    - "analytics.example.com"

# Block entire ecosystem within broader allowed set
network:
  allowed:
    - defaults              # Includes many ecosystems
  blocked:
    - python               # Block Python/PyPI specifically

# Combine domain and ecosystem blocking
network:
  allowed:
    - defaults
    - github
    - node
  blocked:
    - python               # Block Python ecosystem
    - "cdn.example.com"    # Block specific CDN
```

> [!TIP]
> When to Use Blocked Domains
> - **Privacy**: Block tracking and analytics domains while allowing legitimate services
> - **Security**: Block known malicious or compromised domains
> - **Compliance**: Enforce organizational network policies
> - **Fine-grained control**: Allow broad ecosystem access but block specific problematic domains

**Key behaviors**:
- Blocked domains are subtracted from the allowed list
- Supports both individual domains and ecosystem identifiers
- Blocked domains include all subdomains (like allowed domains)
- Useful for blocking specific domains within broader ecosystem allowlists

## Configuration

Network permissions follow the principle of least privilege with four access levels:

1. **Default Allow List** (`network: defaults`): Basic infrastructure only
2. **Selective Access** (`network: { allowed: [...] }`): Only listed domains/ecosystems are accessible
3. **No Access** (`network: {}`): All network access denied
4. **Automatic Subdomain Matching**: Listed domains automatically match all subdomains (e.g., `github.com` allows `api.github.com`, `raw.githubusercontent.com`, etc.)
5. **Wildcard Patterns**: Use `*.example.com` to explicitly match any subdomain of `example.com`

## Protocol-Specific Domain Filtering

For fine-grained security control, you can restrict domains to specific protocols (HTTP or HTTPS only). This is particularly useful when:
- Working with legacy systems that only support HTTP
- Ensuring secure connections by restricting to HTTPS-only
- Migrating from HTTP to HTTPS gradually

> [!TIP]
> Copilot Engine Support
> Protocol-specific filtering is currently supported by the Copilot engine with AWF firewall enabled. Domains without protocol prefixes allow both HTTP and HTTPS traffic (backward compatible).

### Usage Examples

```yaml wrap
engine: copilot
network:
  allowed:
    - "https://secure.api.example.com"   # HTTPS-only access
    - "http://legacy.example.com"        # HTTP-only access  
    - "example.org"                      # Both protocols (default)
    - "https://*.api.example.com"        # HTTPS wildcard
```

**Compiled to AWF:**
```bash
--allow-domains ...,example.org,http://legacy.example.com,https://secure.api.example.com,...
```

### Supported Protocols

- `https://` - HTTPS-only access
- `http://` - HTTP-only access
- No prefix - Both HTTP and HTTPS (backward compatible)

> [!CAUTION]
> Protocol Validation
> Invalid protocols (e.g., `ftp://`, `ws://`) are rejected at compile time with a clear error message:
> ```
> error: network.allowed[0]: domain pattern 'ftp://invalid.example.com' 
> has invalid protocol, only 'http://' and 'https://' are allowed
> ```

### Best Practices

- **Prefer HTTPS**: Use `https://` prefix for all external APIs and services
- **Legacy Systems**: Only use `http://` for internal or legacy systems that don't support HTTPS
- **Default Behavior**: Omit the protocol prefix for domains that should accept both protocols
- **Gradual Migration**: Use protocol-specific filtering to migrate from HTTP to HTTPS incrementally

## Content Sanitization

The `network:` configuration also controls which domains are allowed in sanitized content. URLs from domains not in the allowed list are replaced with `(redacted)` to prevent potential data exfiltration through untrusted links.

> [!TIP]
> If you see `(redacted)` in workflow outputs, add the domain to your `network.allowed` list. This applies the same domain allowlist to both network egress (when firewall is enabled) and content sanitization.

GitHub domains (`github.com`, `githubusercontent.com`, etc.) are always allowed by default.


## Ecosystem Identifiers

Mix ecosystem identifiers with specific domains for fine-grained control:

| Identifier | Includes |
|------------|----------|
| `defaults` | Basic infrastructure (certificates, JSON schema, Ubuntu, package mirrors) |
| `github` | GitHub domains |
| `containers` | Docker Hub, GitHub Container Registry, Quay |
| `linux-distros` | Debian, Alpine, and other Linux package repositories |
| `dotnet`, `dart`, `go`, `haskell`, `java`, `node`, `perl`, `php`, `python`, `ruby`, `rust`, `swift` | Language-specific package managers and registries |
| `terraform` | HashiCorp and Terraform domains |
| `playwright` | Playwright testing framework domains |

> [!TIP]
> Common Use Cases
> - **Python projects**: Add `python` for PyPI, pip, and files.pythonhosted.org
> - **Node.js projects**: Add `node` for registry.npmjs.org, yarn, and pnpm
> - **Container builds**: Add `containers` for Docker Hub and other registries
> - **Go projects**: Add `go` for proxy.golang.org and sum.golang.org
>
> See the [Network Configuration Guide](/gh-aw/guides/network-configuration/) for complete examples and domain lists.


## Implementation

Network permissions are enforced differently depending on the AI engine:

### Copilot Engine

The Copilot engine supports network permissions through AWF (Agent Workflow Firewall). AWF is a network firewall wrapper sourced from [github.com/github/gh-aw-firewall](https://github.com/github/gh-aw-firewall) that wraps Copilot CLI execution and enforces domain-based access controls.

Enable network permissions in your workflow:

```yaml wrap
engine: copilot

network:
  firewall: true           # Enable AWF enforcement
  allowed:
    - defaults             # Basic infrastructure
    - python              # Python ecosystem
    - "api.example.com"   # Custom domain
```

When enabled, AWF:
- Wraps the Copilot CLI execution command
- Enforces domain allowlisting using the `--allow-domains` flag
- Automatically includes all subdomains (e.g., `github.com` allows `api.github.com`)
- Supports wildcard patterns (e.g., `*.cdn.example.com` matches `img.cdn.example.com`)
- Logs all network activity for audit purposes
- Blocks access to domains not explicitly allowed

### Firewall Log Level

Control the verbosity of AWF firewall logs using the `log-level` field:

```yaml wrap
network:
  firewall:
    log-level: info      # Options: debug, info, warn, error
  allowed:
    - defaults
    - python
```

Available log levels:
- `debug`: Detailed diagnostic information for troubleshooting
- `info`: General informational messages (default)
- `warn`: Warning messages for potential issues
- `error`: Error messages only

The default log level is `info`, which provides a balance between visibility and log volume. Use `debug` for troubleshooting network access issues or `error` to minimize log output.

### SSL Bump for HTTPS Inspection

Enable SSL bump to allow the AWF firewall to inspect HTTPS traffic and filter by URL path patterns:

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

The `ssl-bump` feature enables deep packet inspection of HTTPS traffic, allowing the firewall to filter based on URL paths instead of just domain names. When SSL bump is enabled, use `allow-urls` to specify HTTPS URL patterns that should be permitted through the firewall.

**Configuration Options:**

- `ssl-bump`: Boolean flag to enable SSL Bump for HTTPS content inspection (default: `false`)
- `allow-urls`: Array of HTTPS URL patterns to allow when SSL bump is enabled. Each pattern:
  - Must use the `https://` scheme
  - Supports wildcards (`*`) for flexible path matching
  - Example patterns: `https://github.com/githubnext/*`, `https://api.github.com/repos/*/issues`

**Usage Example with Log Level:**

```yaml wrap
network:
  firewall:
    ssl-bump: true
    allow-urls:
      - "https://github.com/githubnext/*"
      - "https://api.github.com/repos/*"
    log-level: debug
  allowed:
    - defaults
    - "github.com"
    - "api.github.com"
```

> [!CAUTION]
> Security Considerations
> - SSL bump intercepts and decrypts HTTPS traffic for inspection, acting as a man-in-the-middle
> - Only enable SSL bump when URL-level filtering is necessary for your security requirements
> - Use `allow-urls` patterns carefully to avoid breaking legitimate HTTPS connections
> - This feature is specific to AWF (Agent Workflow Firewall) and does not apply to Sandbox Runtime (SRT) or other sandbox configurations
> - Requires AWF version 0.9.0 or later

> [!TIP]
> When to Use SSL Bump
> - You need to filter HTTPS traffic by specific URL paths, not just domain names
> - You want to allow access to specific API endpoints while blocking others on the same domain
> - You need fine-grained control over HTTPS resources accessed by the AI engine

See the [Sandbox Configuration](/gh-aw/reference/sandbox/) documentation for detailed AWF configuration options.

### Disabling the Firewall

> [!CAUTION]
> Deprecated
> The `network.firewall` field is deprecated. The agent sandbox is now mandatory and defaults to AWF. See [Sandbox Configuration](/gh-aw/reference/sandbox/) for details.

The firewall is always enabled via the default `sandbox.agent: awf` configuration:

```yaml wrap
engine: copilot
network:
  allowed:
    - defaults
    - python
    - "api.example.com"
# sandbox.agent defaults to 'awf' if not specified
```

**Legacy approach (deprecated):**

```yaml wrap
strict: false
network:
  allowed:
    - defaults
    - python
    - "api.example.com"
  firewall: false
```

When the firewall is disabled:
- Network permissions are still applied for content sanitization
- The agent can make network requests without firewall enforcement
- This is useful during development or when the firewall is incompatible with your workflow

For production workflows, enabling the firewall is recommended for better network security.

## Wildcard Domain Patterns

Use wildcard patterns (`*.example.com`) to match any subdomain of a domain. Wildcards provide explicit control when you need to allow a family of subdomains.

```yaml wrap
network:
  allowed:
    - defaults
    - "*.cdn.example.com"     # Matches img.cdn.example.com, static.cdn.example.com
    - "*.storage.example.com" # Matches files.storage.example.com
```

**Wildcard matching behavior:**
- `*.example.com` matches `subdomain.example.com` and `deep.nested.example.com`
- `*.example.com` also matches the base domain `example.com`
- Only a single wildcard at the start is allowed (e.g., `*.*.example.com` is invalid)
- The wildcard must be followed by a dot and domain (e.g., `*` alone is not allowed in strict mode)

> [!TIP]
> When to Use Wildcards vs Base Domains
> Both approaches work for subdomain matching:
> - **Base domain** (`example.com`): Simpler syntax, automatically matches all subdomains
> - **Wildcard pattern** (`*.example.com`): Explicit about subdomain matching intent, useful when you want to clearly document that subdomains are expected

## Best Practices

Follow the principle of least privilege by only allowing access to domains and ecosystems actually needed. Prefer ecosystem identifiers over listing individual domains. For custom domains, both base domains (e.g., `trusted.com`) and wildcard patterns (e.g., `*.trusted.com`) work for subdomain matching.

## Troubleshooting

If you encounter network access blocked errors, verify that required domains or ecosystems are included in the `allowed` list. Start with `network: defaults` and add specific requirements incrementally. Network access violations are logged in workflow execution logs.

Use `gh aw logs --run-id <run-id>` to view firewall activity and identify blocked domains. See the [Network Configuration Guide](/gh-aw/guides/network-configuration/#troubleshooting-firewall-blocking) for detailed troubleshooting steps and common solutions.

## Related Documentation

- [Network Configuration Guide](/gh-aw/guides/network-configuration/) - Practical examples and common patterns
- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration guide
- [Tools](/gh-aw/reference/tools/) - Tool-specific network access configuration
- [Security Guide](/gh-aw/introduction/architecture/) - Comprehensive security guidance
