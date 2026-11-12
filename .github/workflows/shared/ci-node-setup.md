---
# Node.js setup shared by CI-oriented agent workflows.
import-schema:
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
steps:
  - name: ${{ github.aw.import-inputs.node-step-name }}
    uses: actions/setup-node@v7.1.0
    with:
      node-version: ${{ github.aw.import-inputs.node-version }}
      cache: "${{ github.aw.import-inputs.node-cache }}"
      cache-dependency-path: "${{ github.aw.import-inputs.node-cache-dependency-path }}"
---
