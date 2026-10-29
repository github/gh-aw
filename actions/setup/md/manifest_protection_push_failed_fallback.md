{main_body}

---

> [!WARNING]
> **Protected Files — Push Permission Denied**
>
> This was originally intended as a pull request, but the change modifies protected files. A human must create the pull request manually.
>
> <details>
> <summary>Protected files</summary>
>
> {files}
>
> The push was rejected because GitHub Actions does not have `workflows` permission to push these changes, and is never allowed to make such changes, or other authorization being used does not have this permission.
>
> </details>

<details>
<summary>Create the pull request manually</summary>

```sh
{apply_instructions}

# Push the branch and create the pull request
git push origin {branch_name}
gh pr create --title '{title}' --base {base_branch} --head {branch_name} --repo {repo}
```

</details>

<details>
<summary>Allow intentional protected-file changes</summary>

If this workflow is intended to modify these files, add a supported exclusion to `safe-outputs.create-pull-request.protected-files.exclude` in the workflow Markdown frontmatter. For example, to allow `.changeset/*`, exclude the top-level dot-folder using `.changeset/`:

```yaml
safe-outputs:
  create-pull-request:
    protected-files:
      policy: fallback-to-issue
      exclude:
        - .changeset/
```

Keep your existing policy and exclusions. Top-level dot-folder exclusions use folder names, not glob patterns; `.changeset/` allows all files beneath the top-level `.changeset` folder, including subfolders. Other protected files still follow the configured policy. If `allowed-files` is configured, these paths must also match it. Run `gh aw compile` after updating the workflow.

</details>

{footer}
