---
"gh-aw": patch
---

Soft-deprecate the Gemini agentic engine in favor of experimental Agy. Gemini remains selectable and existing workflows keep their binary, version pins, authentication, and log identity. Compilation emits an informational notice without adding warnings or changing strict-mode success. Retain Gemini for Google WIF and capabilities Agy does not support.
