---
"gh-aw": patch
---

Fixed plugin import format in smoke workflows from marketplace URL syntax (`explanatory-output-style@claude-plugins-official`) to GitHub repository path (`anthropics/claude-code/plugins/explanatory-output-style`). The marketplace URL format was not recognized by the Copilot CLI, causing smoke test failures. Updated documentation and schema to clarify that plugins must use GitHub repository paths.
