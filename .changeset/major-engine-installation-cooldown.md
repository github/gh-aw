---
"gh-aw": major
---

Compiler-managed engine installations now enforce a three-day release-age cooldown by default. Explicit pins to releases newer than the configured cooldown fail.

To skip the release-age check, set `engine.cooldown: 0` only when using the compiler-pinned default or a literal `engine.version` that can be checked against releases; expressions are not allowed with zero. npm installs use the built-in `NPM_CONFIG_MIN_RELEASE_AGE` setting without generated version-check scripts; Node.js runtime overrides must provide npm 11.10.0 or later, since older npm versions may ignore the setting.

Engine definitions that set the deprecated `behaviors.installation.cooldown` field must migrate to workflow-level `engine.cooldown`.
