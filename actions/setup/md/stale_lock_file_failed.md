> [!WARNING]
> **Lock File Out of Sync**: The workflow could not start because its compiled lock file no longer matches the source markdown.

This means the workflow's `.md` file was edited but `gh aw compile` was not run afterwards to regenerate the corresponding `.lock.yml` file. The agent is prevented from running against a stale configuration to avoid unexpected behaviour.

**To fix**, recompile the workflow:

```bash
gh aw compile
```

Then commit and push the updated `.lock.yml` file.

<details>
<summary>More ways to recompile</summary>

**Using the gh-aw MCP server** (if configured):

```json
{ "tool": "compile", "arguments": { "validate": true } }
```

**Recompile all workflows at once:**

```bash
gh aw compile --all
```

**Verify the result:**

```bash
gh aw compile --validate
```

</details>

<details>
<summary>How to investigate the mismatch</summary>

The workflow run logs contain a verbose debug pass that shows exactly what was hashed. Search the **Check workflow lock file** step logs for lines starting with `[hash-debug]` to see:

- The raw frontmatter text that was used as input
- Any imported files that were included in the hash
- The canonical JSON that was fed to SHA-256
- The resulting hash value

This makes it easy to spot accidental whitespace changes, encoding differences, or import path drift.

This mismatch can also happen on a **fresh install** (even without any manual edits) if `gh aw add <path>@<ref>` could not resolve the ref to an exact commit SHA during installation. In that case, rerun the add command with an exact SHA (or retry when API/rate-limit conditions recover), then recompile:

```bash
gh aw add <path>@<exact-sha>
gh aw compile
```

</details>

<details>
<summary>How to disable this check</summary>

> **Caution:** Disabling this check means the agent can run against an out-of-date compiled workflow. Only disable it if you have an alternative mechanism to keep lock files in sync.

Set `stale-check: false` in the `on:` section of your workflow frontmatter:

```yaml
on:
  issues:
    types: [opened]
  stale-check: false
```

After editing, recompile the workflow: `gh aw compile`

</details>
