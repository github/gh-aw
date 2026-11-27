**Agent Timed Out**: The agent job exceeded the maximum allowed execution time ({current_minutes} minutes).

To increase the timeout, add or update the `timeout-minutes` setting in your workflow's frontmatter:

```yaml
---
timeout-minutes: {suggested_minutes}
---
```
