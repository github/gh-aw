---
"gh-aw": major
---

Compiler-managed engine installations now enforce a three-day release-age cooldown by default. Explicit pins to releases newer than the configured cooldown fail, and strict-mode workflows cannot opt out.

To keep using a recently published pin in a non-strict workflow, set `engine.cooldown: 0`. Strict-mode workflows must use a release old enough to satisfy the cooldown. npm installs use the built-in `NPM_CONFIG_MIN_RELEASE_AGE` setting without generated version-check scripts; Node.js runtime overrides must provide npm 11.10.0 or later, since older npm versions may ignore the setting.

Engine definitions that set the deprecated `behaviors.installation.cooldown` field must migrate to workflow-level `engine.cooldown`. A zero value is permitted only outside strict mode.
