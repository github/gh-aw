---
engine:
  id: agy
  display-name: Google Antigravity CLI
  description: Experimental native Agy CLI with Gemini API-key authentication
  experimental: true
  detection-engine: copilot
  runtime-id: agy
  version: "1.3.1"
  provider:
    name: google
  models:
    default: gemini-3.8-flash-medium
---

<!-- Runtime behavior is implemented by AgyEngine in the Go compiler. -->
