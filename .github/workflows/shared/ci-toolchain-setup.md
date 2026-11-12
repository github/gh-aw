---
# Shared Go and Node.js setup actions for agent job steps.
# Disable an unused toolchain with the corresponding setup input.
import-schema:
  setup-go:
    type: boolean
    default: false
  go-cache:
    type: boolean
    default: true
  go-step-name:
    type: string
    default: "Setup Go"
  setup-node:
    type: boolean
    default: false
  node-version:
    type: string
    default: "24"
  node-cache:
    type: string
    default: ""
  node-cache-dependency-path:
    type: string
    default: ""
  node-step-name:
    type: string
    default: "Setup Node.js"
steps:
  - name: ${{ github.aw.import-inputs.go-step-name }}
    if: ${{ github.aw.import-inputs.setup-go }}
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: ${{ github.aw.import-inputs.go-cache }}
  - name: ${{ github.aw.import-inputs.node-step-name }}
    if: ${{ github.aw.import-inputs.setup-node }}
    uses: actions/setup-node@v7.1.0
    with:
      node-version: ${{ github.aw.import-inputs.node-version }}
      cache: ${{ github.aw.import-inputs.node-cache }}
      cache-dependency-path: ${{ github.aw.import-inputs.node-cache-dependency-path }}
---
