> [!CAUTION]
> Protected files were modified in this change.
> This pull request is in `request-review` mode and requires explicit human scrutiny before merge.
>
> Protected files: {files}

<details>
<summary>Allow intentional protected-file changes</summary>

If this workflow is intended to modify these files, add specific filenames or folder prefixes to `safe-outputs.create-pull-request.protected-files.exclude` in the workflow Markdown frontmatter. For example, to allow `.changeset/*`, exclude the folder using `.changeset/`:

```yaml
safe-outputs:
  create-pull-request:
    protected-files:
      policy: request-review
      exclude:
        - .changeset/
```

Keep your existing policy and exclusions. Folder exclusions use path prefixes, not glob patterns, and allow all files beneath that folder, including subfolders. Other protected files still require review. If `allowed-files` is configured, these paths must also match it. Run `gh aw compile` after updating the workflow.

</details>
