# Agent Container Testing

This document describes the smoke test workflow for validating that common development tools are available in the agent container environment.

## Overview

The agent container smoke test (`.github/workflows/smoke-test-tools.md`) provides quick validation that essential development tools are accessible in the GitHub Actions agent container. This test catches tool availability regressions early in the CI pipeline.

## Purpose

When agentic workflows run in GitHub Actions, they depend on various system tools being available. This smoke test validates:

- **Shell environments** work correctly (bash, sh)
- **Version control** is available (git)
- **Data processing tools** are present (jq, yq)
- **HTTP clients** are accessible (curl)
- **GitHub integration** works (gh CLI)
- **Programming runtimes** are installed (node, python3, go, java, dotnet)

## Tested Tools

| Tool | Purpose | Required By |
|------|---------|-------------|
| `bash` | Shell scripting | All bash-based workflows |
| `sh` | POSIX shell | Fallback shell scripts |
| `git` | Version control | Repository operations |
| `jq` | JSON processing | Data parsing, API responses |
| `yq` | YAML processing | Configuration file handling |
| `curl` | HTTP requests | API calls, downloads |
| `gh` | GitHub CLI | Repository interactions |
| `node` | Node.js runtime | JavaScript-based tools |
| `python3` | Python runtime | Python-based scripts |
| `go` | Go runtime | Go-based tools |
| `java` | Java runtime | Java/JVM-based tools |
| `dotnet` | .NET runtime | C#/.NET-based tools |

## Running the Smoke Test

### Via Pull Request Label

1. Add the `smoke` label to any pull request
2. The smoke test workflow will trigger automatically
3. Results are posted as a PR comment

### Via Manual Dispatch

```bash
# Trigger manually via GitHub CLI
gh workflow run smoke-test-tools.md
```

### Via Schedule

The smoke test runs automatically every 12 hours to catch environment drift.

## Expected Output

A successful run produces a summary table:

```markdown
## Agent Container Tool Check

| Tool | Status | Version |
|------|--------|---------|
| bash | ✅ | 5.2.x |
| sh   | ✅ | available |
| git  | ✅ | 2.x.x |
| jq   | ✅ | 1.x |
| yq   | ✅ | 4.x |
| curl | ✅ | 8.x |
| gh   | ✅ | 2.x |
| node | ✅ | 20.x |
| python3 | ✅ | 3.x |
| go   | ✅ | 1.24.x |
| java | ✅ | 21.x |
| dotnet | ✅ | 8.x |

**Result:** 12/12 tools available ✅
```

## Failure Modes

### Tool Not Found

If a tool is missing from the container:

```
| jq | ❌ | not found |
```

**Cause:** The tool wasn't included in the container image or PATH is misconfigured.

**Resolution:** 
1. Check the container image build configuration
2. Verify the tool is installed in the Dockerfile
3. Ensure PATH includes the tool's location

### Tool Version Too Old

If a tool version is incompatible:

```
| node | ⚠️ | 16.x (expected 20.x) |
```

**Cause:** The container image has an outdated version.

**Resolution:** Update the container image to include a newer version.

### Permission Denied

If a tool exists but isn't executable:

```
| curl | ❌ | permission denied |
```

**Cause:** File permissions are incorrect.

**Resolution:** Fix file permissions in the container image build.

## Local Testing

To test tool availability locally before pushing:

```bash
# Check individual tools
bash --version
git --version
jq --version
yq --version
curl --version
gh --version
node --version
python3 --version
go version
java --version
dotnet --version
```

Or run validation checks for all required tools:

```bash
for tool in bash git jq yq curl gh node python3 go java dotnet; do
  if command -v $tool &> /dev/null; then
    echo "✅ $tool: $($tool --version 2>&1 | head -1)"
  else
    echo "❌ $tool: not found"
  fi
done
```

## Integration with CI

The smoke test serves as an early warning system:

1. **Quick validation** - Expected to run in under 2 minutes (5 minute timeout limit)
2. **Blocks regressions** - Fails if essential tools are missing
3. **Clear reporting** - Provides specific tool-by-tool status
4. **Scheduled runs** - Catches environment drift between PRs

## Extending the Smoke Test

To add new tools to the smoke test:

1. Edit `.github/workflows/smoke-test-tools.md`
2. Add the new tool to the test list in the prompt
3. Update the expected output format
4. Run `gh aw compile smoke-test-tools.md`
5. Update this documentation with the new tool

## Related Resources

- [Agent Container Image](../Dockerfile) - Container configuration
- [CI Workflow](../.github/workflows/ci.yml) - Main CI pipeline
- [Smoke Copilot](../.github/workflows/smoke-copilot.md) - Copilot engine validation
- [Smoke Codex](../.github/workflows/smoke-codex.md) - Codex engine validation
