---
title: "Threat Detection Now Uses Its Own Model Default"
description: "gh-aw now defaults threat detection to the detection model alias instead of inheriting the main agent model."
authors:
  - copilot
date: 2026-09-11
metadata:
  seoDescription: "Migrate gh-aw workflows that relied on threat detection inheriting the main agent model by configuring an explicit detection model."
---

Threat detection now defaults to the `detection` model alias when a workflow does not configure a detection-specific model. Previously, threat detection inherited the main agent model when the detection engine could interpret it.

> [!IMPORTANT]
> This is a breaking change for workflows that relied on implicit model inheritance. The main agent model is unchanged; only the separate threat-detection job uses the new default.

The `detection` alias lets the selected detection engine resolve a supported model independently of the main agent. This avoids passing provider-specific main-agent models to a different detection runtime.

## Keep a specific detection model

Workflows that require a particular model for threat detection must configure it explicitly:

```aw wrap
---
on: issues
model: large
safe-outputs:
  create-issue:
  threat-detection:
    engine:
      id: copilot
      model: gpt-4.1-mini
---

Review the issue and create a tracking issue when action is required.
```

The main `model` field continues to configure the primary agent. `safe-outputs.threat-detection.engine.model` configures only threat detection.

Detection model precedence is:

1. `safe-outputs.threat-detection.engine.model`
2. `GH_AW_DEFAULT_DETECTION_MODEL`
3. An engine-specific detection default
4. The `detection` alias

Recompile workflows after upgrading:

```bash
gh aw compile
```

Review generated lock files to confirm that detection jobs use the intended model.
See the [threat detection reference](/gh-aw/reference/threat-detection/) for the complete configuration.
