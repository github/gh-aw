## Problem

The workflow lock files (`.lock.yml`) are out of sync with their source markdown files (`.md`). This means the workflows that run in GitHub Actions are not using the latest configuration.

## What needs to be done

The workflows need to be recompiled to regenerate the lock files from the markdown sources.

## Instructions

If the repository provides a wrapper such as `make recompile`, prefer that command so compilation uses the repository's canonical flags. In `github/gh-aw`, any edit under `.github/workflows/*.md` should be followed by `make recompile` before committing.

Recompile all workflows using one of the following methods:

### Using the repository wrapper

```bash
make recompile
```

### Using gh aw CLI

```bash
gh aw compile --validate --verbose
```

### Using gh-aw MCP Server

If you have the gh-aw MCP server configured, use the `compile` tool:

```json
{
  "tool": "compile",
  "arguments": {
    "validate": true,
    "verbose": true
  }
}
```

This will:
1. Build the latest version of `gh-aw`
2. Compile all workflow markdown files to YAML lock files
3. Ensure all workflows are up to date

After recompiling, commit the changes with a message like:
```
Recompile workflows to update lock files
```

## Detected Changes

The following workflow lock files have changes:

<details>
<summary>View diff</summary>

```diff
{DIFF_CONTENT}
```

</details>

## References

- **Repository:** {REPOSITORY}
