---
steps:
  - name: Setup Node.js
    uses: actions/setup-node@v7.1.0
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  - name: Setup Go
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
---
