---
on:
  workflow_dispatch:
name: Dev
description: Build and test this project
timeout-minutes: 30
strict: false
sandbox:
  agent: awf
engine: copilot
network:
  allowed:
    - defaults
    - ghcr.io
    - pkg-containers.githubusercontent.com
    - proxy.golang.org
    - sum.golang.org
    - storage.googleapis.com
    - objects.githubusercontent.com
    - codeload.github.com

permissions:
  contents: read
  issues: read
  pull-requests: read

safe-outputs:
  create-pull-request:
    expires: 2h
    title-prefix: "[dev] "
    draft: true
imports:
  - shared/mood.md
---

# Build, Test, and Add Poem

Build and test the gh-aw project, then add a single line poem to poems.txt.

**Requirements:**
1. Run `make build` to build the binary (this handles Go module downloads automatically)
2. Run `make test` to run the test suite
3. Report any failures with details about what went wrong
4. If all steps pass, create a file called poems.txt with a single line poem
5. Create a pull request with the poem
