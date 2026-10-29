> [!WARNING]
> **Protected Files**
>
> The push to pull request branch was blocked because the change modifies protected files.
>
> **Target Pull Request:** [#{pull_number}]({pr_url})
>
> **Please review the changes carefully** before pushing them to the pull request branch. These files may affect project dependencies, CI/CD pipelines, or agent behaviour.
>
> <details>
> <summary>Protected files</summary>
>
> {files}
>
> </details>

---

<details>
<summary>Apply the patch after review</summary>

The changes are available in the workflow run artifacts:

**Workflow Run:** [View run details and download the artifact]({run_url})

```sh
{apply_instructions}
```

</details>

To route changes like this to a review issue instead of blocking, configure `protected-files: fallback-to-issue` in your workflow configuration.

<details>
<summary>Allow intentional protected-file changes</summary>

If this workflow is intended to modify these files, add specific filenames or folder prefixes to `safe-outputs.push-to-pull-request-branch.protected-files.exclude` in the workflow Markdown frontmatter. For example, to allow `.changeset/*`, exclude the folder using `.changeset/`:

```yaml
safe-outputs:
  push-to-pull-request-branch:
    protected-files:
      policy: fallback-to-issue
      exclude:
        - .changeset/
```

Keep your existing policy and exclusions. Folder exclusions use path prefixes, not glob patterns, and allow all files beneath that folder, including subfolders. Other protected files still follow the configured policy. If `allowed-files` is configured, these paths must also match it. Run `gh aw compile` after updating the workflow.

</details>
