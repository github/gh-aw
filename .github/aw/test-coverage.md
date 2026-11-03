---
description: Guidance for creating agentic workflows that analyze test coverage — prefer reading pre-computed CI artifacts over re-running tests.
---

# Test Coverage Workflow Guidance

Consult this file when creating or updating a workflow that analyzes test coverage.

## Core Principle: Read Artifacts First

Always prefer fetching pre-computed coverage artifacts from CI over re-running tests. Re-running duplicates CI work.

## Coverage Data Strategy

Include this decision block in every coverage workflow prompt:

```
1. Find the latest successful CI run for this commit:
   `gh run list --commit "$HEAD_SHA" --status success --limit 5 --json databaseId,workflowName`
2. Download the coverage artifact (try names: coverage-report, coverage, test-results):
   `gh run download <run-id> --name coverage-report --dir /tmp/coverage`
3. If found, parse and analyze it — do NOT re-run tests.
4. If not found, run tests with coverage and note in the report that data was computed fresh.
```

## Frontmatter Template

```yaml
engine: copilot
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  actions: read         # download artifacts
network: defaults
tools:
  github:
    toolsets: [default, actions]  # actions toolset enables artifact download
safe-outputs:
  add-comment:
    hide-older-comments: true
  upload-code-coverage:
```

`upload-code-coverage` publishes a Cobertura XML coverage report to GitHub's code coverage API via [`actions/upload-code-coverage`](https://github.com/actions/upload-code-coverage). The compiler automatically grants the dedicated `code-quality: write` (and `pull-requests: read` for push-triggered workflows) permission needed by the upload job — no need to add it to the `permissions:` block above.

## Fallback: Run Tests

Use **only when** no prior CI artifact exists or CI doesn't upload coverage. Supported commands:

- infer the repository ecosystem from project files before running fallback coverage
- configure `network.allowed` to include `defaults` plus the inferred ecosystem(s) (for example `node`, `python`, `go`)
- never run fallback coverage with `network: defaults` alone
- convert the freshly generated coverage report to Cobertura XML format (e.g. `coverage.py xml`, `go-cobertura`, or a JaCoCo/Istanbul Cobertura reporter), stage it under `$RUNNER_TEMP/gh-aw/safeoutputs/upload-code-coverage/`, and call `upload_code_coverage` with `file: "cobertura.xml"`, `language` set to the inferred ecosystem's Linguist name (e.g. `"Go"`, `"Python"`, `"JavaScript"`), and a descriptive `label` (e.g. `"code-coverage/fallback"`)

Example fallback network config:

```yaml
network:
  allowed:
    - defaults
    - node
```

| Language | Command | Cobertura conversion |
|---|---|---|
| Node.js | `npx jest --coverage --coverageReporters=cobertura` | Jest's `cobertura` reporter writes `coverage/cobertura-coverage.xml` directly |
| Python | `python -m pytest --cov=src --cov-report=xml` | `pytest-cov`'s `--cov-report=xml` writes Cobertura-format `coverage.xml` directly |
| Go | `go test ./... -coverprofile=/tmp/coverage.out` | convert with `gocover-cobertura < /tmp/coverage.out > cobertura.xml` |
