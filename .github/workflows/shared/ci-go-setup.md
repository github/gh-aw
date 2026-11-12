---
# Go setup shared by CI-oriented agent workflows.
import-schema:
  go-cache:
    type: boolean
    default: true
    description: "Enable setup-go's module cache."
  go-step-name:
    type: string
    default: "Setup Go"
steps:
  - name: ${{ github.aw.import-inputs.go-step-name }}
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: ${{ github.aw.import-inputs.go-cache }}
---
