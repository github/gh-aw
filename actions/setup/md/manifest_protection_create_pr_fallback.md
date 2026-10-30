> [!TIP]
> **Your pull request is ready to create! 🎉 ✅**
>
> Everything is OK—the changes have been pushed to a branch. Please review the protected files, then create the pull request when you are ready.
>
> **[Create the pull request]({create_pr_url})**
>
> The original pull request description is below.

---

{main_body}

---

> [!NOTE]
> **Protected files**
>
> This patch modifies protected files, which may affect project dependencies, CI/CD pipelines, or agent behaviour.
>
> <details>
> <summary>Protected files</summary>
>
> {files}
>
> </details>

To route changes like this to a review issue instead of blocking, configure `protected-files: fallback-to-issue` in your workflow configuration.

<details>
<summary>Allow intentional protected-file changes</summary>

If this workflow is intended to modify these files, add specific filenames or folder prefixes to `safe-outputs.create-pull-request.protected-files.exclude` in the workflow Markdown frontmatter. For example, to allow `.changeset/*`, exclude the folder using `.changeset/`:

```yaml
safe-outputs:
  create-pull-request:
    protected-files:
      policy: fallback-to-issue
      exclude:
        - .changeset/
```

Keep your existing policy and exclusions. Folder exclusions use path prefixes, not glob patterns, and allow all files beneath that folder, including subfolders. Other protected files still follow the configured policy. If `allowed-files` is configured, these paths must also match it. Run `gh aw compile` after updating the workflow.

</details>

{footer}
