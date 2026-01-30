---
title: Frontmatter Hash Specification
description: Specification for computing deterministic hashes of agentic workflow frontmatter
---

# Frontmatter Hash Specification

This document specifies the algorithm for computing a deterministic hash of agentic workflow frontmatter, including contributions from imported workflows.

## Purpose

The frontmatter hash provides:
1. **Change detection**: Verify that workflow configuration has not changed between compilation and execution
2. **Reproducibility**: Ensure identical configurations produce identical hashes across languages (Go and JavaScript)
3. **Security**: Detect unauthorized modifications to workflow frontmatter

## Hash Algorithm

### 1. Input Collection

Collect all frontmatter from the main workflow and all imported workflows in **breadth-first order** (BFS traversal):

1. **Main workflow frontmatter**: The frontmatter from the root workflow file
2. **Imported workflow frontmatter**: Frontmatter from each imported file in BFS processing order
   - Includes transitively imported files (imports of imports)
   - Agent files (`.github/agents/*.md`) only contribute markdown content, not frontmatter

### 2. Field Selection

Include the following frontmatter fields in the hash computation:

**Core Configuration:**
- `engine` - AI engine specification
- `on` - Workflow triggers
- `permissions` - GitHub Actions permissions
- `tracker-id` - Workflow tracker identifier

**Tool and Integration:**
- `tools` - Tool configurations (Playwright, etc.)
- `mcp-servers` - MCP server configurations
- `network` - Network access permissions
- `safe-outputs` - Safe output configurations
- `safe-inputs` - Safe input configurations

**Runtime Configuration:**
- `runtimes` - Runtime version specifications (Node.js, Python, etc.)
- `services` - Container services
- `cache` - Caching configuration

**Workflow Structure:**
- `steps` - Custom workflow steps
- `post-steps` - Post-execution steps
- `jobs` - GitHub Actions job definitions

**Metadata:**
- `description` - Workflow description
- `labels` - Workflow labels
- `bots` - Authorized bot list
- `timeout-minutes` - Workflow timeout
- `secret-masking` - Secret masking configuration

**Import Metadata:**
- `imports` - List of imported workflow paths (for traceability)
- `inputs` - Input parameter definitions

**Excluded Fields:**
- Markdown body content (not part of frontmatter)
- Comments and whitespace variations
- Field ordering (normalized during processing)

### 3. Canonical JSON Serialization

Transform the collected frontmatter into a canonical JSON representation:

#### 3.1 Merge Strategy

For each workflow in BFS order:
1. Parse frontmatter into a structured object
2. Merge with accumulated frontmatter using these rules:
   - **Replace**: `engine`, `on`, `tracker-id`, `description`, `timeout-minutes`
   - **Deep merge**: `tools`, `mcp-servers`, `network`, `permissions`, `runtimes`, `cache`, `services`
   - **Append**: `steps`, `post-steps`, `safe-outputs`, `safe-inputs`, `jobs`
   - **Union**: `labels`, `bots` (deduplicated)
   - **Track**: `imports` (list of all imported paths)

#### 3.2 Normalization Rules

Apply these normalization rules to ensure deterministic output:

1. **Key Sorting**: Sort all object keys alphabetically at every level
2. **Array Ordering**: Preserve array order as-is (no sorting of array elements)
3. **Whitespace**: Use minimal whitespace (no pretty-printing)
4. **Number Format**: Represent numbers without exponents (e.g., `120` not `1.2e2`)
5. **Boolean Values**: Use lowercase `true` and `false`
6. **Null Handling**: Include `null` values explicitly
7. **Empty Containers**: Include empty objects `{}` and empty arrays `[]`
8. **String Escaping**: Use JSON standard escaping (quotes, backslashes, control characters)

#### 3.3 Serialization Format

The canonical JSON includes all frontmatter fields plus version information:

```json
{
  "bots": ["copilot"],
  "cache": {},
  "description": "Daily audit of workflow runs",
  "engine": "claude",
  "imports": ["shared/mcp/gh-aw.md", "shared/jqschema.md"],
  "jobs": {},
  "labels": ["audit", "automation"],
  "mcp-servers": {},
  "network": {"allowed": ["api.github.com"]},
  "on": {"schedule": "daily"},
  "permissions": {"actions": "read", "contents": "read"},
  "post-steps": [],
  "runtimes": {"node": {"version": "20"}},
  "safe-inputs": {},
  "safe-outputs": {"create-discussion": {"category": "audits"}},
  "services": {},
  "steps": [],
  "template-expressions": ["${{ env.MY_VAR }}"],
  "timeout-minutes": 30,
  "tools": {"repo-memory": {"branch-name": "memory/audit"}},
  "tracker-id": "audit-workflows-daily",
  "versions": {
    "agents": "v0.0.84",
    "awf": "v0.11.2",
    "gh-aw": "dev"
  }
}
```

### 4. Version Information

The hash includes version numbers to ensure hash changes when dependencies are upgraded:

- **gh-aw**: The compiler version (e.g., "0.1.0" or "dev")
- **awf**: The firewall version (e.g., "v0.11.2")
- **agents**: The MCP gateway version (e.g., "v0.0.84")

This ensures that upgrading any component invalidates existing hashes.

1. **Serialize**: Convert the merged and normalized frontmatter to canonical JSON
2. **Add Versions**: Include version information for gh-aw, awf (firewall), and agents (MCP gateway)
3. **Hash**: Compute SHA-256 hash of the JSON string (UTF-8 encoded)
4. **Encode**: Represent the hash as a lowercase hexadecimal string (64 characters)

**Example:**
```
Input JSON: {"engine":"copilot","on":{"schedule":"daily"},"versions":{"agents":"v0.0.84","awf":"v0.11.2","gh-aw":"dev"}}
SHA-256: a1b2c3d4e5f6...  (64 hex characters)
```

### 5. Cross-Language Consistency

Both Go and JavaScript implementations MUST:
- Use the same field selection and merging rules
- Produce identical canonical JSON (byte-for-byte)
- Use SHA-256 hash function
- Encode output as lowercase hexadecimal

**Test cases** must verify identical hashes across both implementations for:
- Empty frontmatter
- Single-file workflows (no imports)
- Multi-level imports (2+ levels deep)
- All field types (strings, numbers, booleans, arrays, objects)
- Special characters and escaping
- All workflows in the repository

## Implementation Notes

### Go Implementation

- Use `encoding/json` with `json.Marshal()`
- Sort keys using a custom marshaler or post-processing
- Use `crypto/sha256` for hashing
- Use `hex.EncodeToString()` for hexadecimal encoding

### JavaScript Implementation

- Use native `JSON.stringify()` with sorted keys
- Use Node.js `crypto.createHash('sha256')` for hashing
- Use `.digest('hex')` for hexadecimal encoding
- Ensure identical key sorting as Go implementation

### Hash Storage and Verification

1. **Compilation**: The Go compiler computes the hash and writes it to the workflow log file
2. **Execution**: The JavaScript custom action:
   - Reads the hash from the log file
   - Recomputes the hash from the workflow file
   - Compares the two hashes
   - Creates a GitHub issue if they differ (indicating frontmatter modification)

## Security Considerations

- The hash is **not cryptographically secure** for authentication (no HMAC/signing)
- The hash **detects accidental or malicious changes** to frontmatter after compilation
- The hash **does not protect** against modifications before compilation
- Always validate workflow sources through proper code review processes

## Versioning

This is version 1.0 of the frontmatter hash specification.

Future versions may:
- Add additional fields
- Change normalization rules
- Use different hash algorithms

Version changes will be documented and backward compatibility maintained where possible.
