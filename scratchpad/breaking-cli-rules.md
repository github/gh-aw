# Breaking CLI Rules

This document defines what constitutes a breaking change for the gh-aw CLI. These rules help maintainers and contributors evaluate changes during code review and ensure stability for users.

## Overview

Breaking changes require special attention during development and review because they can disrupt existing user workflows. This document provides clear criteria for identifying breaking changes and guidance on how to handle them.

## Categories of Changes

### Breaking Changes (Major Version Bump)

The following changes are **always breaking** and require:
- A `major` changeset type
- Documentation in CHANGELOG.md with migration guidance
- Review by maintainers

#### 1. Command Removal or Renaming

**Breaking**:
- Removing a command entirely (e.g., removing `gh aw logs`)
- Renaming a command without an alias (e.g., `gh aw compile` → `gh aw build`)
- Removing a subcommand (e.g., removing `gh aw mcp inspect`)

**Examples from past releases**:
- Removing `--no-instructions` flag from compile command (v0.17.0)

#### 2. Flag Removal or Renaming

**Breaking**:
- Removing a flag (e.g., removing `--strict` flag)
- Changing a flag name without backward compatibility (e.g., `--output` → `--out`)
- Changing a flag's short form (e.g., `-o` → `-f`)
- Changing a required flag to have no default when it previously had one

**Examples from past releases**:
- Remove GITHUB_TOKEN fallback for Copilot operations (v0.24.0)

#### 3. Output Format Changes

**Breaking**:
- Changing the structure of JSON output (removing fields, renaming fields)
- Changing the order of columns in table output that users might parse positionally
- Changing exit codes for specific scenarios
- Removing output fields that scripts may depend on

**Examples from past releases**:
- Update status command JSON output structure (v0.21.0): replaced `agent` with `engine_id`, removed `frontmatter` and `prompt` fields

#### 4. Behavior Changes

**Breaking**:
- Changing default values for flags (e.g., `strict: false` → `strict: true`)
- Changing authentication requirements
- Changing permission requirements
- Changing the semantics of existing options

**Examples from past releases**:
- Change strict mode default from false to true (v0.31.0)
- Remove per-tool Squid proxy - unify network filtering (v0.25.0)

#### 5. Schema Changes

**Breaking**:
- Removing fields from workflow frontmatter schema
- Making optional fields required
- Changing the type of a field (e.g., string → object)
- Removing allowed values from enums

**Examples from past releases**:
- Remove "defaults" section from main JSON schema (v0.24.0)
- Remove deprecated "claude" top-level field (v0.24.0)

### Non-Breaking Changes (Minor or Patch Version Bump)

The following changes are **not breaking** and typically require:
- A `minor` changeset for new features
- A `patch` changeset for bug fixes

#### 1. Additions

**Not Breaking**:
- Adding new commands
- Adding new flags with reasonable defaults
- Adding new fields to JSON output
- Adding new optional fields to schema
- Adding new allowed values to enums
- Adding new exit codes for new scenarios

**Examples**:
- Add `--json` flag to status command (v0.20.0)
- Add mcp-server command (v0.17.0)

#### 2. Deprecations

**Not Breaking** (when handled correctly):
- Deprecating commands (with warning, keeping functionality)
- Deprecating flags (with warning, keeping functionality)
- Deprecating schema fields (with warning, keeping functionality)

**Requirements for deprecation**:
- Print deprecation warning to stderr
- Document the deprecation and migration path
- Keep deprecated functionality working for at least one minor release
- Schedule removal in a future major version

#### 3. Bug Fixes

**Not Breaking** (when fixing unintended behavior):
- Fixing incorrect output
- Fixing incorrect exit codes
- Fixing schema validation that was too permissive

**Note**: Fixing a bug that users depend on may require a breaking change notice.

#### 4. Performance Improvements

**Not Breaking**:
- Faster execution
- Reduced memory usage
- Parallel processing optimizations

#### 5. Documentation Changes

**Not Breaking**:
- Improving help text
- Adding examples
- Clarifying error messages

## Decision Tree: Is This Breaking?

```text
┌─────────────────────────────────────────────────┐
│  Is the change removing or renaming a command,  │
│  subcommand, or flag?                           │
└──────────────────────┬──────────────────────────┘
                       │
            YES ───────┼───────── BREAKING
                       │
                       ▼ NO
┌─────────────────────────────────────────────────┐
│  Does the change modify JSON output structure   │
│  (remove fields, rename fields, change types)?  │
└──────────────────────┬──────────────────────────┘
                       │
            YES ───────┼───────── BREAKING
                       │
                       ▼ NO
┌─────────────────────────────────────────────────┐
│  Does the change alter default behavior that    │
│  users may rely on?                             │
└──────────────────────┬──────────────────────────┘
                       │
            YES ───────┼───────── BREAKING
                       │
                       ▼ NO
┌─────────────────────────────────────────────────┐
│  Does the change modify exit codes for existing │
│  scenarios?                                     │
└──────────────────────┬──────────────────────────┘
                       │
            YES ───────┼───────── BREAKING
                       │
                       ▼ NO
┌─────────────────────────────────────────────────┐
│  Does the change remove schema fields or make   │
│  optional fields required?                      │
└──────────────────────┬──────────────────────────┘
                       │
            YES ───────┼───────── BREAKING
                       │
                       ▼ NO
                  NOT BREAKING
```text

## Guidelines for Contributors

### When Making CLI Changes

1. **Check the decision tree** before implementing changes
2. **Document breaking changes** in the changeset with deprecation notice, migration path, and timeline
3. **Provide migration guidance** for users affected by breaking changes
4. **Consider backward compatibility** - can you add an alias instead of renaming?
5. **Use deprecation warnings** for at least one minor release before removal

### Changeset Format for Breaking Changes

When creating a changeset for a breaking change:

```markdown
---
"gh-aw": major
---

Remove deprecated `--old-flag` option

**⚠️ Breaking Change**: The `--old-flag` option has been removed.

**Migration guide:**
- If you used `--old-flag value`, use `--new-flag value` instead
- Scripts using this flag will need to be updated

**Reason**: The option was deprecated in v0.X.0 and has been removed to simplify the CLI.
```text

### Changeset Format for Non-Breaking Changes

For new features:
```markdown
---
"gh-aw": minor
---

Add --json flag to logs command for structured output
```text

For bug fixes:
```markdown
---
"gh-aw": patch
---

Fix incorrect exit code when workflow file not found
```text

## Review Checklist for CLI Changes

Reviewers should verify:

- [ ] **Breaking change identified correctly** - Does this change match any breaking change criteria?
- [ ] **Changeset type appropriate** - Is it marked as major/minor/patch correctly?
- [ ] **Migration guidance provided** - For breaking changes, is there clear migration documentation?
- [ ] **Deprecation warning added** - If deprecating, does it warn users?
- [ ] **Backward compatibility considered** - Could this be done without breaking compatibility?
- [ ] **Tests updated** - Do tests cover the changed behavior?
- [ ] **Help text updated** - Is the CLI help accurate?

## Exit Code Standards

The CLI uses standard exit codes:

| Exit Code | Meaning | Breaking to Change |
|-----------|---------|-------------------|
| 0 | Success | No (adding is fine) |
| 1 | General error | No (for new errors) |
| 2 | Invalid usage | No (for new checks) |

**Breaking**: Changing the exit code for an existing scenario (e.g., changing from 1 to 2 for a specific error type).

## JSON Output Standards

When adding or modifying JSON output:

1. **Never remove fields** without a major version bump
2. **Never rename fields** without a major version bump
3. **Never change field types** without a major version bump
4. **Adding new fields is safe** - parsers should ignore unknown fields
5. **Adding new enum values is safe** - parsers should handle unknown values gracefully

## Strict Mode and Security Changes

Special consideration for strict mode changes:

- **Making strict mode validation refuse instead of warn** is breaking (e.g., v0.30.0)
- **Changing strict mode defaults** is breaking (e.g., v0.31.0)
- **Adding new strict mode validations** is not breaking (strictness is opt-in initially)

## References

- **Changeset System**: See `scratchpad/changesets.md` for version management
- **CHANGELOG**: See `CHANGELOG.md` for examples of breaking changes
- **Semantic Versioning**: https://semver.org/

---

**Last Updated**: 2025-11-27
