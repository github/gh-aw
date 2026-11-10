# cachepolicy Package

> GitHub Actions event sets used to distinguish cache writers from read-only triggers.

## Public API

| Function | Description |
|----------|-------------|
| `WriteEvents` | Returns the default-branch cache-write allowlist, including `pull_request` for caches isolated to a pull-request merge ref. |
| `ReadOnlyEvents` | Returns known events outside the write allowlist for authored-trigger diagnostics. `workflow_call` requires caller-policy review. |

The returned event names describe the compiler's policy boundary; they do not independently authorize cache access. Callers must account for the workflow's actual trigger and cache configuration.
