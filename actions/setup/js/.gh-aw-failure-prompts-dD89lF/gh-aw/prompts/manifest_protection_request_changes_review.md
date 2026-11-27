Protected files were modified in this pull request and require manual scrutiny before merge.

Please verify that each protected-file change is intentional, policy-compliant, and safe:

- Protected files: {files}

To allow intentional changes to specific files or folders in future runs, configure `safe-outputs.create-pull-request.protected-files.exclude` in the workflow Markdown frontmatter. For example, this allows `.changeset/*` by excluding the folder:

```yaml
safe-outputs:
  create-pull-request:
    protected-files:
      policy: request-review
      exclude:
        - .changeset/
```

Keep your existing policy and exclusions. Use folder prefixes, not glob patterns; `.changeset/` also allows subfolders. Other protected files still require review, and `allowed-files`, if configured, must also permit these paths. Run `gh aw compile` after updating the workflow. This configuration change applies to future runs; review this pull request separately.
