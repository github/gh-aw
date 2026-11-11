---
engine:
  id: agy
  display-name: Google Antigravity CLI
  description: Native Agy CLI with Gemini API-key authentication
  experimental: false
  detection-engine: copilot
  runtime-id: agy
  version: "1.3.6"
  provider:
    name: google
  models:
    default: gemini-3.8-flash-medium
---

<!-- Runtime behavior is implemented by AgyEngine in the Go compiler. -->
