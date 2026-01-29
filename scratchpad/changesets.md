# Changeset CLI

A minimalistic implementation for managing version releases, inspired by `@changesets/cli`.

## Usage

```bash
# Preview next version from changesets (always read-only)
node scripts/changeset.js version
# Or use make target
make version

# Create release and update CHANGELOG
node scripts/changeset.js release
# Or use make target (recommended - runs tests first)
make release

# Create specific release type
node scripts/changeset.js release patch
node scripts/changeset.js release minor
node scripts/changeset.js release major

# Skip confirmation prompt
node scripts/changeset.js release --yes
node scripts/changeset.js release patch -y
```text

**Note:** Using `make release` is recommended as it automatically runs tests before creating the release, ensuring code quality.

## Commands

### `version`

The `version` command always operates in preview mode and never modifies files. It shows what the next version would be based on the changesets.

```bash
node scripts/changeset.js version
```text

This command:
- Reads all changeset files from `.changeset/` directory
- Determines the appropriate version bump (major > minor > patch)
- Shows a preview of the CHANGELOG entry that would be added
- Never modifies any files

### `release [type] [--yes|-y]`

The `release` command creates an actual release by updating files.

```bash
node scripts/changeset.js release
# Or specify type explicitly
node scripts/changeset.js release minor
# Skip confirmation prompt
node scripts/changeset.js release --yes
node scripts/changeset.js release patch -y
```text

This command:
- Checks prerequisites (clean tree, main branch)
- Updates `CHANGELOG.md` with the new version and changes
- Deletes processed changeset files (if any exist)
- Automatically commits the changes
- Creates and pushes a git tag for the release

**Behavior when no changeset files exist:**
- Defaults to `patch` release if no type is specified
- Adds a generic maintenance entry to the CHANGELOG

**Flags:**
- `--yes` or `-y`: Skip confirmation prompt and proceed automatically

## Changeset File Format

Changeset files are markdown files in `.changeset/` directory with YAML frontmatter:

```markdown
---
"gh-aw": patch
---

Brief description of the change
```text

**Bump types:**
- `patch` - Bug fixes and minor changes (0.0.x)
- `minor` - New features, backward compatible (0.x.0)
- `major` - Breaking changes (x.0.0)

## Prerequisites for Release

When running `node changeset.js release`, the script checks:

1. **Clean working tree**: All changes must be committed or stashed
2. **On main branch**: Must be on the `main` branch to create a release

Example error when not on main branch:
```bash
$ node scripts/changeset.js release
✗ Must be on 'main' branch to create a release (currently on 'feature-branch')
```text

## Release Workflow

### Standard Workflow (with changesets)

1. **Add changeset files** to `.changeset/` directory for each change:
   ```bash
   # Create a changeset file
   cat > .changeset/fix-bug.md << EOF
   ---
   "gh-aw": patch
   ---
   
   Fix critical bug in workflow compilation
   EOF
   ```

2. **Preview the release:**
   ```bash
   node scripts/changeset.js version
   ```

3. **Create the release:**
   ```bash
   node scripts/changeset.js release
   ```
   
   This will automatically:
   - Update CHANGELOG.md
   - Delete changeset files
   - Commit the changes
   - Create a git tag
   - Push the tag to remote

### Releasing Without Changesets

For maintenance releases with dependency updates or minor improvements that don't require individual changeset files:

1. **Run release without changesets:**
   ```bash
   # Defaults to patch release
   node scripts/changeset.js release
   # Or specify release type explicitly
   node scripts/changeset.js release minor
   # Skip confirmation with --yes flag
   node scripts/changeset.js release --yes
   ```

2. The script will:
   - Default to patch release if no type is specified
   - Add a generic "Maintenance release" entry to CHANGELOG.md
   - Commit the changes
   - Create a git tag
   - Push the tag to remote

## Features

- ✅ **Automatic Version Determination**: Analyzes all changesets and picks the highest priority bump type
- ✅ **CHANGELOG Generation**: Creates formatted entries with proper categorization (Breaking Changes, Features, Bug Fixes)
- ✅ **Git Integration**: Reads current version from git tags
- ✅ **Automated Git Operations**: Automatically commits, tags, and pushes releases
- ✅ **Safety First**: Requires explicit specification for major releases
- ✅ **Flexible Releases**: Supports releases with or without changeset files
- ✅ **Clean Workflow**: Deletes processed changesets after release
- ✅ **No External Dependencies**: Implemented using only Node.js standard library

## Requirements

- Node.js (any recent version)
- Git repository with semantic version tags (e.g., `v1.2.3`)
