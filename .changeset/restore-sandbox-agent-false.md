---
"gh-aw": major
---

Restore `sandbox.agent: false` as a supported non-strict mode and require `features.dangerously-disable-sandbox-agent: true` to enable it.

Rename the feature flag from `features.dangerously-disable-sandbox` to `features.dangerously-disable-sandbox-agent`. The restored opt-out requires the renamed flag to be set to `true`:

```yaml
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
```
