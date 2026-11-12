---
# Combined Go and Node.js setup for workflows that need both toolchains.
# Import ci-go-setup.md or ci-node-setup.md directly when only one is needed.
import-schema:
  go-cache:
    type: boolean
    default: true
    description: "Enable setup-go's module cache."
  go-step-name:
    type: string
    default: "Setup Go"
  node-version:
    type: string
    default: "24"
  node-cache:
    type: string
    default: ""
    description: "Optional setup-node cache package manager (npm, yarn, or pnpm)."
  node-cache-dependency-path:
    type: string
    default: ""
    description: "Optional dependency-file path used by setup-node's cache."
  node-step-name:
    type: string
    default: "Setup Node.js"
imports:
  - path: shared/ci-go-setup.md
    inputs:
      go-cache: ${{ github.aw.import-inputs.go-cache }}
      go-step-name: "${{ github.aw.import-inputs.go-step-name }}"
  - path: shared/ci-node-setup.md
    inputs:
      node-version: "${{ github.aw.import-inputs.node-version }}"
      node-cache: "${{ github.aw.import-inputs.node-cache }}"
      node-cache-dependency-path: "${{ github.aw.import-inputs.node-cache-dependency-path }}"
      node-step-name: "${{ github.aw.import-inputs.node-step-name }}"
---
