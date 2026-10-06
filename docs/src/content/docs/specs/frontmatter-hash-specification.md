---
title: Frontmatter Hash Specification
description: Specification for computing deterministic hashes of agentic workflow frontmatter
version: 1.0.0
status: Draft
publication_date: 2026-05-07
sidebar:
  order: 1360
---

# Frontmatter Hash Specification

**Version**: 1.0.0  
**Status**: Draft  
**Publication Date**: 2026-05-07  
**Latest Version**: [frontmatter-hash-specification](/gh-aw/specs/frontmatter-hash-specification/)  
**Editor**: GitHub Agentic Workflows Team

---

This document specifies the algorithm for computing a deterministic hash of agentic workflow frontmatter, including contributions from imported workflows.

## Purpose

The frontmatter hash provides:
1. **Stale lock detection**: Identify when the compiled lock file is out of sync with the source workflow (e.g. after editing the `.md` file without recompiling)
2. **Reproducibility**: Ensure identical configurations produce identical hashes across languages (Go and JavaScript)
3. **Change detection**: Verify that workflow configuration has not changed between compilation and execution

## Conformance

### Conformance Classes

- **Basic Conformance**: An implementation MUST compute a deterministic SHA-256 hash from canonicalized frontmatter input and MUST produce the same output for identical input.
- **Full Conformance**: An implementation MUST satisfy Basic Conformance and MUST implement cross-language consistency checks between Go and JavaScript implementations.

### Requirements Notation

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this document are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

## Terminology

- **Frontmatter**: The YAML metadata block at the top of a workflow file that defines execution
  behavior, tool configuration, and runtime options.
- **BFS traversal**: Breadth-first traversal of the workflow import graph where imports are processed
  level-by-level and sibling order follows the `imports:` declaration order.
- **Diamond dependency**: An import-graph shape where multiple parent workflows reference the same
  downstream workflow; the shared workflow is included once using first-seen BFS order.
- **Canonical JSON**: A deterministic JSON representation with normalized key ordering and formatting
  rules so equivalent input produces identical byte output across runtimes.

## Hash Algorithm

### 1. Input Collection

Collect all frontmatter from the main workflow and all imported workflows in **breadth-first order** (BFS traversal):

1. **Main workflow frontmatter**: The frontmatter from the root workflow file
2. **Imported workflow frontmatter**: Frontmatter from each imported file in BFS processing order
   - Includes transitively imported files (imports of imports)
   - Agent files (`.github/agents/*.md`) only contribute markdown content, not frontmatter

#### BFS Traversal and Tie-Breaking Rules

The BFS traversal processes imports level by level, starting from the root workflow. When a workflow imports multiple files, they are enqueued left-to-right in the order they appear in the `imports:` list. This ordering is preserved at every level.

**Diamond-import handling**: If a workflow file appears more than once in the import graph (a "diamond" dependency), the **first occurrence** in BFS order determines where that file's frontmatter is merged; all subsequent occurrences of the same file **MUST be silently skipped**. Implementations MUST detect duplicate import paths using canonical path comparison (case-sensitive, no trailing-slash normalization) and discard duplicates without error.

**Example (diamond graph)**:

```
root.md  →  imports: [a.md, b.md]
a.md     →  imports: [shared.md]
b.md     →  imports: [shared.md]
```

BFS queue order: `[root.md, a.md, b.md, shared.md]`  
`shared.md` appears twice but is processed only once (after `a.md` in queue order).  
Canonical hash input order: root → a → b → shared.

If the root import list were reversed to `[b.md, a.md]`, the canonical order would be
`root → b → a → shared`.

The first sibling encountered in BFS order always claims the shared dependency. Later duplicates are
skipped. This rule ensures that the hash is deterministic regardless of which traversal path first
discovers a shared dependency.

### 2. Field Selection

Include the following frontmatter fields in the hash computation:

**Core Configuration:**
- `engine` - AI engine specification
- `on` - Workflow triggers
- `permissions` - GitHub Actions permissions
- `tracker-id` - Workflow tracker identifier

**Tool and Integration:**
- `tools` - Tool configurations (GitHub, Playwright, etc.)
- `mcp-servers` - MCP server configurations
- `network` - Network access permissions
- `safe-outputs` - Safe output configurations
- `mcp-scripts` - Safe input configurations

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

#### 2.1 Field Selection and Merge Strategy Table

| Field | Included in hash | Merge strategy |
|---|---|---|
| `engine` | Yes | Replace |
| `on` | Yes | Replace |
| `permissions` | Yes | Deep merge |
| `tools` | Yes | Deep merge |
| `mcp-servers` | Yes | Deep merge |
| `safe-outputs` | Yes | Append |
| `mcp-scripts` | Yes | Append |
| `steps` | Yes | Append |
| `jobs` | Yes | Append |
| `imports` | Yes | Track (ordered list) |
| `inputs` | Yes | Deep merge by input key |

### 3. Canonical JSON Serialization

Transform the collected frontmatter into a canonical JSON representation:

#### 3.1 Merge Strategy

For each workflow in BFS order:
1. Parse frontmatter into a structured object
2. Merge with accumulated frontmatter using these rules:
   - **Replace**: `engine`, `on`, `tracker-id`, `description`, `timeout-minutes`
   - **Deep merge**: `tools`, `mcp-servers`, `network`, `permissions`, `runtimes`, `cache`, `services`
   - **Append**: `steps`, `post-steps`, `safe-outputs`, `mcp-scripts`, `jobs`
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
  "imports": ["shared/mcp/tavily.md", "shared/jqschema.md"],
  "jobs": {},
  "labels": ["audit", "automation"],
  "mcp-servers": {},
  "network": {"allowed": ["api.github.com"]},
  "on": {"schedule": "daily"},
  "permissions": {"actions": "read", "contents": "read"},
  "post-steps": [],
  "runtimes": {"node": {"version": "24"}},
  "mcp-scripts": {},
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

### 5.1 BFS Diamond-Import Test Vectors

The following test vectors are normative and MUST produce the specified SHA-256 hash in both the
Go and JavaScript implementations. These vectors exercise the three required corpus cases from
**R-XLANG-001**: empty frontmatter, single-level import, and the diamond-import pattern.

All hashes are verified in CI via `pkg/parser/frontmatter_hash_cross_language_test.go`
(`TestFrontmatterHashVectorFH_BFS_002` and `TestFrontmatterHashVectorFH_BFS_003`; FH-BFS-001
is covered by the existing `FH-TV-001` case).

#### FH-BFS-001 — Empty Frontmatter

Root file content (explicit empty frontmatter block, no imports):

```markdown
---
---

# Empty Workflow
```

Expected SHA-256: `4c8309afbcf816cd80c0824dce2b50047834b29e14b34b96953e88ae81048c46`

Canonical JSON input: `{"frontmatter-text":""}`

#### FH-BFS-002 — Single-Level Import

Root file (`root.md`) imports one helper. The helper's normalized frontmatter is concatenated into
`imported-frontmatters`.

`root.md`:
```yaml
---
engine: copilot
imports:
  - ./helper.md
---
```

`helper.md`:
```yaml
---
description: Helper agent
---
```

Expected SHA-256: `3946bb0dc0698a31e37a1efc7012071939db1be2c8365f12f8a240bc01ba2e9e`

Canonical JSON input:
```json
{"frontmatter-text":"engine: copilot\nimports:\n  - ./helper.md","imported-frontmatters":"description: Helper agent","imports":["./helper.md"]}
```

#### FH-BFS-003 — Diamond Import (root → a, b → shared)

This vector verifies BFS diamond-import deduplication. `shared.md` is reachable from both `a.md`
and `b.md`, but MUST appear only once in the hash input (at its first BFS encounter via `a.md`).

`root.md`:
```yaml
---
engine: copilot
imports:
  - ./a.md
  - ./b.md
---
```

`a.md`:
```yaml
---
description: Agent A
imports:
  - ./shared.md
---
```

`b.md`:
```yaml
---
description: Agent B
imports:
  - ./shared.md
---
```

`shared.md`:
```yaml
---
description: Shared helper
---
```

BFS traversal order: `root.md → a.md → b.md → shared.md` (the second occurrence of `shared.md`
via `b.md` is silently skipped).

Expected SHA-256: `13f1c69f5761454beac63c7dc259fa212f020d3dab9e0dd04d2e1bdcc242b108`

Canonical JSON input:
```json
{"frontmatter-text":"engine: copilot\nimports:\n  - ./a.md\n  - ./b.md","imported-frontmatters":"description: Agent A\nimports:\n  - ./shared.md\n---\ndescription: Agent B\nimports:\n  - ./shared.md\n---\ndescription: Shared helper","imports":["./a.md","./b.md","./shared.md"]}
```

**Note**: The `imports` array and `imported-frontmatters` entries in the canonical JSON are sorted
alphabetically. The BFS traversal order determines *which* files are included (deduplication), but
the canonical JSON representation sorts the collected entries before hashing so the hash is stable
regardless of the order sibling imports appear in the frontmatter.

### 5.2 Cross-Language Validation Protocol

The project maintains Go and JavaScript implementations of the frontmatter hash algorithm. A
conforming change to either implementation MUST follow this validation protocol:

1. Update both implementations in the same change whenever the authoritative runtime algorithm or
   normalization behavior changes.
2. Execute the shared cross-language test vectors so each implementation validates the other
   implementation's output, not just its own fixtures.
3. Treat any byte-level mismatch in canonical JSON or final SHA-256 output as a release-blocking
   failure until both implementations are aligned.
4. Recompile workflow lock files only after the cross-language checks pass, so newly generated hashes
   reflect a synchronized algorithm.

**R-XLANG-001**: The shared validation corpus **MUST** include at least one empty-frontmatter case,
one single-file case, one multi-level import case, and one diamond-import case.

**R-XLANG-002**: A change that alters canonical JSON generation in either language **MUST** update
the shared validation corpus in the same change.

**R-XLANG-003**: CI or pre-release validation **MUST** fail if Go and JavaScript produce different
hashes for any corpus member.

## Implementation Notes

### Caller Operations

`pkg/cli/hash_command.go` MUST invoke the hash API with the following operational sequence:

1. The caller MUST resolve the target workflow markdown file path and fail with a descriptive error
   when the file cannot be read.
2. The caller MUST pass workflow content and repository path context to the frontmatter hash
   implementation so imports can be traversed deterministically.
3. The caller MUST return the computed 64-character lowercase SHA-256 hash string on success.
4. The caller MUST surface deterministic hash-computation failures (parse/import/read errors) as
   command errors without fallback hashing.

### Go Implementation

The current Go implementation (`pkg/parser/frontmatter_hash.go`) uses a **text-based approach** that diverges from the field-selection model described in Section 2 ("Field Selection") of this specification:

- **Actual behavior**: The entire normalized frontmatter text is hashed as a single opaque string (`frontmatter-text` key in the canonical JSON), alongside a sorted list of imported file paths and their normalized texts. This means _all_ frontmatter fields — including excluded ones such as comments — affect the hash value.
- **Specified behavior**: The specification calls for selecting individual named fields and merging them by type (replace, deep-merge, append, union).

**Implication**: The text-based approach is more conservative (any frontmatter change invalidates the hash, including whitespace-only changes after normalization) and simpler to implement cross-language. The trade-off is that it cannot support selective field exclusion without modifying the text normalization step.

**Sync status** (verified 2026-05-06): The Go implementation is consistent with the JavaScript implementation in `actions/setup/js/` for the text-based approach. Both produce identical hashes for the same input. The field-selection model in Section 2 documents the _logical_ intent; the text-based implementation is the authoritative runtime behavior until a future revision aligns them.

**Resolution** (2026-05-08): The project officially adopts the **text-based approach** as the authoritative runtime behavior (option b). Section 2 ("Field Selection") documents the intended logical model for future alignment, but is non-normative until a dedicated migration milestone is scheduled. No immediate changes to the Go or JavaScript implementations are required. A future v2.0.0 revision of this specification MAY align both implementations to the field-selection model if selective field exclusion becomes a concrete requirement; that revision MUST include updated cross-language test vectors and a migration guide. Until then, implementations MUST continue to use the text-based approach and MUST NOT selectively exclude fields from the hash input.

- Use `crypto/sha256` for hashing (`crypto/sha256.Sum256`)
- Use `hex.EncodeToString()` for hexadecimal encoding

### JavaScript Implementation

- Uses the same text-based approach as the Go implementation
- Uses Node.js `crypto.createHash('sha256')` for hashing
- Uses `.digest('hex')` for hexadecimal encoding
- The JavaScript cross-language test suite in `pkg/parser/frontmatter_hash_cross_language_test.go` verifies identical output between the two implementations

### Hash Storage and Verification

1. **Compilation**: The Go compiler computes the hash and writes it to the workflow log file
2. **Execution**: The JavaScript custom action:
   - Reads the hash from the log file
   - Recomputes the hash from the workflow file
   - Compares the two hashes
   - Creates a GitHub issue if they differ (indicating frontmatter modification)

## Safeguards

This section describes known risks associated with the frontmatter hash mechanism and the recommended mitigations.

### S-1: Hash Collision Risk

SHA-256 produces a 256-bit output, giving a collision probability of approximately 2⁻¹²⁸ for any two distinct inputs under the birthday paradox. For the expected number of compiled workflows in a repository (typically <10,000), the probability of an accidental collision is negligible and does not require mitigation at the application layer.

However, implementations MUST NOT rely on the hash as a cryptographic commitment or security boundary. The hash is an integrity check for stale-lock detection only.

**Mitigation**: If future use cases require stronger collision resistance (e.g., content-addressed storage), implementations SHOULD upgrade to SHA-512 or SHA3-256 and bump the specification version.

### S-2: Tamper Detection Limits

The frontmatter hash detects accidental drift between the `.md` source and the compiled `.lock.yml` file. It does **not** prevent intentional tampering. Any user with write access to the repository can modify both files simultaneously:

1. Edit the `.md` source.
2. Recompile to regenerate the `.lock.yml` with the new hash.
3. Commit both files in a single push.

This bypass is by design — the hash mechanism is intended to catch _accidental_ stale locks, not to enforce a security boundary.

**Mitigation**: Enforce required code reviews via branch protection rules. Require signed commits for critical workflows. Use separate compilation and merge workflows with protected branches to prevent direct pushes to the default branch.

### S-3: Inclusion of Sensitive Configuration in Hash Input

The canonical JSON used for hash computation includes all frontmatter fields, some of which may encode sensitive topology information (e.g., MCP server addresses in `mcp-servers:`, secret names in `mcp-scripts:`, or branch names in `tools.repo-memory`). This information is embedded in the `.lock.yml` file at compile time and is visible to anyone who can read the repository.

**Mitigation**: Treat repository visibility as the primary access control boundary. Avoid storing secret _values_ in frontmatter (use GitHub Actions secrets instead). Periodically audit lock files for inadvertently committed sensitive configuration.

### S-4: Version-Bump-Forced Recompilation

The hash includes `versions.gh-aw`, `versions.awf`, and `versions.agents`. Upgrading any of these components will invalidate all existing hashes, triggering stale-lock warnings on all workflows until they are recompiled. In a repository with many workflows, this can create a noisy wave of false-positive stale-lock issues.

**Mitigation**: Coordinate component upgrades with a bulk `make recompile` step. Automate recompilation in the upgrade PR so that lock files are always fresh after a version bump.

### S-5: Cross-Language Hash Divergence

The Go and JavaScript implementations must produce byte-for-byte identical canonical JSON. Any divergence in key sorting, number representation, or null/undefined handling between the two implementations will cause the JavaScript runtime to report a false stale-lock mismatch for every workflow run.

**Mitigation**: Maintain a shared test-vector file (at minimum: empty frontmatter, single-field workflow, multi-level imports, all field types). Run cross-language hash tests in CI. Any change to the serialization algorithm in either language MUST be accompanied by updated test vectors verified against both implementations.

### S-6: Maximum Frontmatter Input Size

Very large frontmatter payloads can cause excessive memory use and hash-computation latency during compilation and runtime verification. This can degrade CI reliability and increase stale-lock false positives due to timeout or resource pressure.

**Mitigation**: Implementations MUST enforce a maximum cumulative normalized frontmatter input size
of **1,048,576 bytes (1 MiB)** and MUST fail deterministically with a descriptive error when that
limit is exceeded. This limit bounds worst-case memory/latency behavior while remaining well above
typical workflow frontmatter sizes.

---

## Sync Notes

This section maps the frontmatter hash specification to the source files that implement it. Use this mapping to verify that specification changes are reflected in both implementations.

| Component | File(s) |
|-----------|---------|
| Go hash computation | `pkg/parser/frontmatter_hash.go` (`computeFrontmatterHashTextBased`, `computeFrontmatterHashTextBasedWithReader`) |
| JavaScript hash computation | `actions/setup/js/frontmatter_hash.cjs` |
| Cross-language test | `pkg/parser/frontmatter_hash_cross_language_test.go` |
| Text normalization | `pkg/parser/frontmatter_hash.go` (`normalizeFrontmatterText`) |
| Import processing | `pkg/parser/frontmatter_hash.go` (`processImportsTextBased`) |

**After any change to the hash algorithm:**
1. Update the Go implementation in `pkg/parser/frontmatter_hash.go`
2. Update the JavaScript implementation in `actions/setup/js/frontmatter_hash.cjs`
3. Run the cross-language test: `go test ./pkg/parser/ -run TestFrontmatterHash`
4. Run `make recompile` to regenerate all lock files with fresh hashes
5. Verify cross-language consistency for the test cases listed in Section 5
6. Verify BFS diamond-import tie-breaking remains deterministic: when the same imported file is
   reachable through multiple import paths at the same depth, the canonical traversal MUST prefer
   first-seen path order and MUST NOT duplicate imported content in hash input.

**Runtime behavior**: text-based approach is authoritative (see Implementation Notes § Resolution).

**Resolution log (2026-05-08, authoritative)**: Text-based canonicalization is the resolved,
runtime-authoritative algorithm. **Formal deferral (2026-05-26)**: Section 2 ("Field
Selection") is formally deferred pending completion of the v2.0.0 migration prerequisites
tracked in [#31983](https://github.com/github/gh-aw/issues/31983). Rationale: no concrete
selective field-exclusion use case has been identified, and the text-based approach has proven
sufficient for all current requirements. Section 2 remains non-normative design intent until
those prerequisites are met and the migration milestone is approved.

**Sync verification (2026-05-12)**: SPDD review reconfirmed that the 2026-05-08 text-based
resolution remains in force.

**S-6 sync verification (2026-05-28)**: The 1 MiB cumulative input-size guard (S-6) is confirmed
implemented in `pkg/parser/frontmatter_hash.go` via `validateFrontmatterHashInputSize`. The function
accumulates normalized byte lengths across the main and all imported frontmatter texts and returns a
deterministic error `"frontmatter hash input exceeds 1048576 bytes after normalization"` when the
cumulative size exceeds `maxFrontmatterHashInputBytes` (1,048,576). The limit is equivalently
enforced in `actions/setup/js/frontmatter_hash_pure.cjs` via
`validateNormalizedFrontmatterHashInputSize`. Boundary-condition compliance is verified by
`TestFrontmatterHashInputSizeLimit` in `pkg/parser/frontmatter_hash_test.go` (uses an input of
`maxFrontmatterHashInputBytes + 1` bytes and asserts the exact error string). **Status: Verified
2026-05-28.**

---

## Security Considerations

- The hash is **not cryptographically secure** for authentication (no HMAC/signing)
- The hash is designed to **detect stale lock files** — it catches cases where the frontmatter has changed since the lock file was last compiled
- The hash **does not guarantee tamper protection**: anyone with write access to the repository can modify both the `.md` source and the `.lock.yml` file together, bypassing detection
- Always validate workflow sources through proper code review processes

## Versioning

This is version 1.0 of the frontmatter hash specification.

Future versions may:
- Add additional fields
- Change normalization rules
- Use different hash algorithms

Version changes will be documented and backward compatibility maintained where possible.

### Future Versions (v2.0.0 Planning)

Per the **Resolution (2026-05-08)** in Implementation Notes, the text-based algorithm remains
authoritative until a dedicated migration milestone is approved.

**Decision entry (2026-05-27)**: v2.0.0 field-selection alignment is formally **deferred**. The
project will revisit scheduling only after the acceptance criteria below are met and reviewed in
[#31983](https://github.com/github/gh-aw/issues/31983).

Tracking issue: [#31983](https://github.com/github/gh-aw/issues/31983)

The project **MUST NOT** schedule a v2.0.0 migration to the field-selection model until all of
the following tracked tasks are complete:

- [ ] Confirm and document a selective field-exclusion use case in [#31983](https://github.com/github/gh-aw/issues/31983).
- [ ] Draft a migration guide in [#31983](https://github.com/github/gh-aw/issues/31983), including lock-file invalidation and recompilation steps.
- [ ] Write candidate v2.0.0 cross-language test vectors in [#31983](https://github.com/github/gh-aw/issues/31983) and verify they pass in CI.
- [ ] Approve a rollout plan in [#31983](https://github.com/github/gh-aw/issues/31983), including backward-compatibility impact analysis.

Until these prerequisites are met, implementations **MUST** continue using the text-based
algorithm and **MUST NOT** selectively exclude frontmatter fields from hash input.

## Appendix A: Cross-Language Test Vectors (Text-Based Algorithm)

The following vectors are normative for the current authoritative text-based algorithm.

Validation status: Each vector hash is verified to match in both implementations via automated
cross-language tests in CI.

### FH-TV-001

Expected hash: `4c8309afbcf816cd80c0824dce2b50047834b29e14b34b96953e88ae81048c46`

This vector represents an intentionally empty frontmatter block (`---` followed immediately by
`---`) rather than a file with no frontmatter delimiter. These are treated as different input
forms for conformance testing and MUST be validated independently; this vector defines only the
explicit-empty-block form.

```yaml
---
---

# Empty Workflow
```

### FH-TV-002

Expected hash: `b9def9907e3328e2e03e8c47c315723df39788f251627313b1a984bb61b9cbce`

```yaml
---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow
```

### FH-TV-003

Expected hash: `8c63a05ef42cbfaff9be87a06257282cb4dcb952f71481d9d65ec3037003dbe8`

```yaml
---
engine: claude
description: Complex workflow
tracker-id: complex-test
timeout-minutes: 30
on:
  schedule: daily
  workflow_dispatch: true
permissions:
  contents: read
  actions: read
tools:
  playwright:
    version: v1.41.0
labels:
  - test
  - complex
bots:
  - copilot
---

# Complex Workflow
```

### FH-TV-004

Expected hash: `701dc12776a417c6ce4c82b16d1fcc9de343130efb554fda27a701386b17d134`

This vector validates deterministic hash input when frontmatter includes agent file imports.
It also exercises BFS diamond-import tie-breaking where multiple import branches reference the same
transitive file.

```yaml
---
engine: copilot
imports:
  - ./agents/router.agent.md
  - ./agents/summarizer.agent.md
---

# Import-based Workflow
```

### FH-TV-NEG-001 (Oversized Input Rejection)

Input description:

- A workflow (including imported frontmatter content) whose cumulative normalized frontmatter input
  exceeds **1,048,576 bytes**.

Expected result:

- Hash computation is rejected before digest generation.
- Error text includes: `frontmatter input exceeds 1048576-byte limit`.
