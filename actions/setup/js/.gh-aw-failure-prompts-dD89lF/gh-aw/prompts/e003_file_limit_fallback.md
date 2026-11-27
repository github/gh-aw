> [!WARNING]
> {error_message}

The pull request could not be created because the patch contains more files than the configured limit.

To increase the limit, add `max-patch-files` to your workflow frontmatter:

```yaml
safe-outputs:
  create-pull-request:
    max-patch-files: {suggested_limit}  # adjust as needed
```
