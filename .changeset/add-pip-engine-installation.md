---
"gh-aw": minor
---

Add declarative hash-locked Python installation for behavior-defined engines: `behaviors.installation.package-manager: pip` with a `requirements` list of `name`/`version`/`sha256` entries installs binary wheels with `pip --require-hashes` before the agent sandbox starts and verifies the engine version. The Aider and LangGraph samples now use it.
