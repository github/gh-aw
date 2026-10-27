# Contributing to GitHub Agentic Workflows

Thank you for your interest in contributing to GitHub Agentic Workflows! We welcome contributions from the community and are excited to work with you.

**⚠️ IMPORTANT: This project uses agentic development by a core team (inner-circle) primarily using Copilot coding agent or local coding agents.**

**🚫 Traditional Pull Requests Are Not Enabled for non-Core team members**: If you are not part of the core team, please do not create pull requests directly. Instead, you create detailed agentic plans in issues, discuss with the team, and a core team member will create and implement the PR for you using agents.

This document deals with the contribution process for non-Core team members.

## Prerequisites

> ⚠️ **Generic dev environments (e.g. manually installed Node.js, Go, or other tools) are not supported.**
> This project is designed to be developed inside a **Dev Container** or **GitHub Codespace**, which automatically configures all required tools and runtimes.

The recommended way to set up a development environment is to use the provided [Dev Container](.devcontainer/devcontainer.json):

- **GitHub Codespaces** (recommended): Open this repository in a Codespace — everything is pre-configured automatically, including Go, Node.js 24, Docker, and the GitHub CLI.
- **VS Code Dev Container**: Install the [Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers), then open the repository folder and choose **Reopen in Container**.

The Dev Container installs all required dependencies and runs `make deps` automatically on creation. No manual setup is needed.

If you encounter errors about Node.js or Go versions when running `make deps` or other build commands, this is a sign that you are not using the Dev Container. Please switch to a Dev Container or Codespace environment.

## 🤖 How Development Works

GitHub Agentic Workflows is developed by a core team using agentic development — primarily GitHub Copilot coding agent and local coding agents. This means:

- ✅ **Core team uses agents to create and manage pull requests** - via Copilot coding agent or local agents
- ✅ **Automated quality assurance** - CI runs all checks on all PRs
- ✅ **Community contributes through agentic plans** - you craft the plan, the core team executes it
- ❌ **Traditional pull requests from non-core members are not enabled** - contribute through issues instead

### Why This Approach?

This project practices what it preaches: agentic development is used to build agentic workflows. Benefits include:

- **Consistency**: All changes go through the same automated quality gates
- **Dogfooding**: We use our own tools to build our tools
- **Best practices**: Agents follow established patterns and guidelines automatically
- **Quality plans**: Encourages contributors to think through the full implementation before work begins

## 🚀 Quick Start for Community Contributors

⚠️ **If you are not part of the core team, do not create pull requests directly.** Instead, craft a detailed agentic plan in an issue and a core team member will pick it up and implement it using agents.

### Step 1: Analyze with an Agent (for bug reports)

**Before filing a contribution request**, use an agent to:

- Scan the source code to identify root causes (for bugs)
- Analyze execution patterns and trace the issue
- Research similar issues and solutions
- Propose specific fixes with code examples
- Create a comprehensive plan for the changes needed

### Step 2: Open an Issue with Your Agentic Plan

**Create an issue** with your detailed agentic plan:

- Describe what you want to contribute
- Include your agent's analysis and findings (for bugs)
- Explain the use case and expected behavior
- Provide a **complete, step-by-step plan** for the agent to follow
- Include specific implementation details and examples
- Tag with appropriate labels (see [Label Guidelines](scratchpad/labels.md))

See [Reporting Issues and Feature Requests](#reporting-issues-and-feature-requests) for complete guidelines.

**Example agentic plan in an issue:**

```markdown
## Add support for custom MCP server timeout configuration

### Analysis
The current MCP server configuration lacks a timeout field, which can cause workflows to hang indefinitely when servers don't respond.

### Implementation Plan
Please implement the following changes:

1. **Update Schema** (`pkg/parser/schemas/frontmatter.json`):
   - Add a `timeout` field to MCP server configuration schema
   - Type: integer
   - Range: 5-300 seconds
   - Default: 30 seconds

2. **Update Validation** (`pkg/workflow/mcp_validation.go`):
   - Add validation for timeout values between 5-300 seconds
   - Use error message format: "[what's wrong]. [what's expected]. [example]"
   - Example error: "timeout value 400 exceeds maximum. Expected value between 5-300 seconds. Example: timeout: 60"

3. **Add Tests** (`pkg/workflow/mcp_validation_test.go`):
   - Test valid timeout values (5, 30, 300)
   - Test invalid timeout values (0, 4, 301, 1000)
   - Test missing timeout (should use default)

4. **Update Documentation** (`docs/src/content/docs/reference/frontmatter.md`):
   - Add timeout field to MCP server configuration examples
   - Explain timeout behavior and defaults
   - Show example with custom timeout value

5. **Follow Guidelines**:
   - Use console formatting from `pkg/console` for CLI output
   - Follow error message style guide
   - Run `make agent-finish` before completing
```

### Step 3: Discuss and Refine with the Team

Once you've opened the issue:

1. **Core team reviews your plan**: A core team member will look at your issue and may ask clarifying questions
2. **Iterate on the plan**: Discuss and refine the implementation approach based on team feedback
3. **Plan gets approved**: A core team member signals they'll pick it up

### Step 4: A Core Team Member Implements the PR

A core team member will:

- Take your agentic plan and use a coding agent (Copilot or local) to implement it
- Read relevant documentation and specifications
- Make code changes following established patterns
- Run `make agent-finish` to validate changes
- Create a PR and handle review feedback and adjustments

**You don't create or own the PR** — the core team member does, using agents as their implementation tool.

## 📝 How to Contribute as a Community Member

All community contributions flow through **agentic plans in GitHub issues**. A core team member then picks up the issue and uses a coding agent to implement it in a pull request.

### How the Process Works

1. **You create an issue** with a detailed agentic plan describing what needs to be done
2. **Core team reviews** your plan and may ask questions or suggest refinements
3. **A core team member picks it up** and uses a coding agent to implement your plan
4. **The agent follows your instructions** and handles the technical details
5. **Core team reviews the PR** and provides feedback
6. **Agent iterates** based on review comments until approved
7. **PR is merged** when all checks pass and reviews are satisfied

**You do not create pull requests yourself.** Instead, you craft comprehensive plans that a core team member executes using agents.

### What the Implementing Agent Handles

When a core team member implements your plan, the coding agent they use will:

- **Read specifications** from `scratchpad/`, `skills/`, and `.github/instructions/`
- **Follow code organization patterns** (see [scratchpad/code-organization.md](scratchpad/code-organization.md))
- **Implement validation** following the architecture in [scratchpad/validation-architecture.md](scratchpad/validation-architecture.md)
- **Use console formatting** from `pkg/console` for CLI output
- **Write error messages** following the [Error Message Style Guide](.github/skills/error-messages/SKILL.md) and [docs guide](docs/src/content/docs/troubleshooting/error-messages.md)
- **Run all quality checks**: `make agent-finish` (build, test, recompile, format, lint)
- **Update documentation** for new features
- **Create tests** for new functionality

### Reporting Issues and Feature Requests

Before filing an issue, **use an agent to perform thorough analysis and research**. This accelerates implementation and helps maintainers focus on high-quality contributions.

#### 🤖 Use Agents for Bug Analysis

**Bug reports submitted with minimal analysis or research are likely to be ignored.**

Use an agent to analyze the source code, identify root causes, propose fixes, and research similar issues before filing a bug report.

#### 🐛 Debugging Workflow Failures

For workflow failures, use this prompt with your agent:

```markdown
Please debug this workflow failure:
https://github.com/owner/repo/actions/runs/RUN_ID

Follow https://raw.githubusercontent.com/github/gh-aw/main/debug.md and investigate:
- Why the workflow failed
- What tools were missing
- How to fix the configuration

Generate an investigation report and a plan to address the issue for an agent.
```

The agent will use `gh aw` or the mcp server to analyze the failure. See [`.github/aw/debug-agentic-workflow.md`](.github/aw/debug-agentic-workflow.md) for details.

#### 📝 Issue Guidelines

When filing issues with agentic plans:

- **Bugs**: Include thorough agent analysis, root cause, proposed fix, and detailed implementation plan
- **Features**: Explain the use case, provide examples, suggest implementation approach with step-by-step instructions
- **Workflow failures**: Debug with agents first, then report with analysis and remediation plan
- **Implementation details**: Be specific about file changes, function names, validation rules, test cases
- **Complete plans**: The more detailed your plan, the easier it is for the core team to execute it with an agent
- Follow [Label Guidelines](scratchpad/labels.md)
- A core team member will pick up the issue and implement your plan using a coding agent

**Quality of the agentic plan directly impacts implementation success.** Provide comprehensive, step-by-step instructions with specific details.

#### ✍️ Writing Prompts for Copilot-Assigned Issues

When an issue will be assigned to Copilot coding agent (or another SWE agent), how you write the task description measurably affects whether the resulting PR merges. Recurring "Copilot PR Prompt Pattern Analysis" reports (see [discussion #53203](https://github.com/github/gh-aw/discussions/53203)) show a consistent trend: merged PRs average **~151 words** per task prompt and name concrete files or subsystems (e.g. `pkg`, `engine`, a specific package), while closed/unsuccessful PRs average **~229 words** — roughly 50% longer — and skew toward open-ended, exploratory language without a concrete acceptance criterion.

To improve merge rate for agent-assigned work:

- **Be concise.** Aim for a short, focused prompt rather than a long, exploratory one. Prompts longer than ~200 words without a concrete acceptance criterion correlate with lower merge rates.
- **Name concrete files or subsystems.** Reference specific packages, files, or functions (e.g. `pkg/workflow`, `cmd/gh-aw`) instead of describing the problem in the abstract.
- **State an explicit acceptance criterion.** Describe what "done" looks like (e.g. "the CLI flag `--foo` is parsed and validated" or "add a test in `pkg/parser/parser_test.go` covering X") instead of open-ended language like "investigate and fix as appropriate."
- **Avoid pure exploration prompts.** Prompts that only ask the agent to "look into" or "figure out" a problem without a concrete target tend to result in longer, less-scoped PRs that are more likely to be closed.

Note: automated CVE/dependency-tracker PRs are excluded from this signal, since their close rate reflects triage policy rather than prompt quality.

### Code Quality Standards

Core team members and the agents they use follow these standards:

#### Error Messages

All validation errors follow the template: **[what's wrong]. [what's expected]. [example]**

```go
// Agent produces error messages like this:
return fmt.Errorf("invalid time delta format: +%s. Expected format like +25h, +3d, +1w, +1mo. Example: +3d", deltaStr)
```

The agent runs `make lint-errors` to verify error message quality.

#### Console Output

The agent uses styled console functions from `pkg/console`:

```go
import "github.com/github/gh-aw/pkg/console"

fmt.Println(console.FormatSuccessMessage("Operation completed"))
fmt.Println(console.FormatInfoMessage("Processing workflow..."))
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
```

#### File Organization

The agent follows these principles:

- **Prefer many small files** over large monolithic files
- **Group by functionality**, not by type
- **Use descriptive names** that clearly indicate purpose
- **Follow established patterns** from the codebase

**Key Patterns the Agent Uses**:

1. **Create Functions Pattern** - One file per GitHub entity creation
   - Examples: `create_issue.go`, `create_pull_request.go`, `create_discussion.go`

2. **Engine Separation Pattern** - Each engine has its own file
   - Examples: `copilot_engine.go`, `claude_engine.go`, `codex_engine.go`
   - Shared helpers in `engine_helpers.go`

3. **Focused Utilities Pattern** - Self-contained feature files
   - Examples: `expressions.go`, `strings.go`, `artifacts.go`

See [Code Organization Patterns](scratchpad/code-organization.md) for details.

#### Validation Patterns

The agent places validation logic appropriately:

**Centralized validation** (`pkg/workflow/validation.go`):

- Cross-cutting concerns
- Core workflow integrity
- GitHub Actions compatibility

**Domain-specific validation** (dedicated files):

- `strict_mode_validation.go` - Security enforcement
- `pip_validation.go` - Python packages
- `npm_validation.go` - NPM packages
- `docker_validation.go` - Docker images
- `expression_safety.go` - Expression security

See [Validation Architecture](scratchpad/validation-architecture.md) for the complete decision tree.

#### File Path Security

All file operations must validate paths to prevent path traversal attacks:

**Use `fileutil.ValidateAbsolutePath` before file operations:**

```go
import "github.com/github/gh-aw/pkg/fileutil"

// Validate path before reading/writing files
cleanPath, err := fileutil.ValidateAbsolutePath(userInputPath)
if err != nil {
    return fmt.Errorf("invalid path: %w", err)
}
content, err := os.ReadFile(cleanPath)
```

**Security checks performed:**
- Normalizes path using `filepath.Clean` (removes `.` and `..` components)
- Verifies path is absolute (blocks relative path traversal)
- Returns descriptive errors for invalid paths

**When to use:**
- Before `os.ReadFile`, `os.WriteFile`, `os.Stat`, `os.Open`
- Before `os.MkdirAll` or other directory operations
- After constructing paths with `filepath.Join`
- When processing user-provided file paths

This provides defense-in-depth against path traversal vulnerabilities (e.g., `../../../etc/passwd`).

#### CLI Breaking Changes

The agent evaluates whether changes are breaking:

- **Breaking**: Removing/renaming commands or flags, changing JSON output structure, altering defaults
- **Non-breaking**: Adding new commands/flags, adding output fields, bug fixes

For breaking changes, the agent:

- Uses `major` changeset type
- Provides migration guidance
- Documents in CHANGELOG.md

See [Breaking CLI Rules](scratchpad/breaking-cli-rules.md) for details.

## 🔄 Pull Request Process for Community Contributions

All community-sourced pull requests are created and managed by core team members using coding agents:

1. **Create an issue with your agentic plan:**
   - Open an issue describing what needs to be done in detail
   - Provide a complete, step-by-step implementation plan
   - Include clear context, examples, and specific technical details
   - Tag appropriately using [Label Guidelines](scratchpad/labels.md)

2. **Core team reviews and engages:**
   - A core team member reviews your agentic plan
   - They may ask questions, suggest changes, or refine the approach
   - If the plan looks good, a core team member picks it up

3. **Core team member creates and implements the PR using an agent:**
   - They run the plan through a coding agent (Copilot or local)
   - The agent reads specifications and guidelines
   - The agent makes changes following established patterns
   - The agent runs `make agent-finish` automatically

4. **Automated quality checks:**
   - CI runs on all PRs
   - All checks must pass (build, test, lint, recompile)
   - The core team member addresses any CI failures

5. **Review and iterate:**
   - Core team reviews the PR
   - Provide feedback as comments
   - Agent-assisted revisions are made as needed
   - Once approved, PR is merged

### PR Lifecycle Tip (Core Team)

When implementing community contributions using an agent:

- Create the pull request as **draft**.
- Move it to **Ready for review** and approve required CI workflows.
- Run the `pr-finisher` skill (automates final review/check/mergeability hardening) to get to green.
- For features that deeply impact the engine, add the `smoke` label and approve workflows.
- If no smoke run is queued after setting `smoke`, or additional changes require another smoke run, toggle the `smoke` label (remove and re-add) and approve workflows again.

### PR Triage Labels (Core Team)

PR triage assigns one risk label (`pr-risk:low`, `pr-risk:medium`, or
`pr-risk:high`), a priority label (`pr-priority:*`), and one action label.
Priority reflects impact, urgency, and quality; quality includes CI status,
test coverage, and the PR description.

- `pr-action:fast_track` identifies a high-value, high-priority PR that is
  ready for expedited human review. The risk label determines the review
  scope; fast-track does not bypass required CI or approvals.
- `pr-action:batch_review` groups similar low- or medium-risk PRs for review
  together, typically when a cluster contains three or more related PRs.
- `pr-action:defer` identifies lower-value, lower-priority work that remains
  open but is not currently prioritized.

**Remember: As a community contributor, you don't create the PR yourself.** You create an issue with a detailed plan, discuss it with the team, and a core team member creates the PR using agents.

### What Gets Validated

Every agent-created PR automatically runs:

- `make build` - Ensures Go code compiles
- `make test` - Runs all unit and integration tests
- `make lint` - Checks code quality and style
- `make recompile` - Recompiles all workflows to ensure compatibility. If you edit `.github/workflows/*.md`, run this before committing so the paired `.lock.yml` files stay in sync with CI.
- `make fmt` - Formats Go code
- `make lint-errors` - Validates error message quality

## 🏗️ Project Structure

This structure is useful context when writing your agentic plan, so the core team's agent can navigate the codebase effectively:

```text
/
├── cmd/gh-aw/           # Main CLI application
├── pkg/                 # Core Go packages
│   ├── cli/             # CLI command implementations
│   ├── console/         # Console formatting utilities
│   ├── parser/          # Markdown frontmatter parsing
│   └── workflow/        # Workflow compilation and processing
├── scratchpad/               # Technical specifications the agent reads
├── skills/              # Specialized knowledge for agents
├── .github/             # Instructions and sample workflows
│   ├── instructions/    # Agent instructions
│   └── workflows/       # Sample workflows and CI
└── Makefile             # Build automation (agent uses this)
```

## 📋 Dependency License Policy

This project uses an MIT license and only accepts dependencies with compatible licenses.

### Allowed Licenses

The following open-source licenses are compatible with our MIT license:

- **MIT** - Most permissive, allows reuse with minimal restrictions
- **Apache-2.0** - Permissive license with patent grant
- **BSD-2-Clause, BSD-3-Clause** - Simple permissive licenses
- **ISC** - Simplified permissive license similar to MIT

### Disallowed Licenses

The following licenses are **not allowed** as they conflict with our MIT license or impose unacceptable restrictions:

- **GPL, LGPL, AGPL** - Copyleft licenses that would force us to release under GPL
- **SSPL** - Server Side Public License with restrictive requirements
- **Proprietary/Commercial** - Closed-source licenses requiring payment or special terms

### Container Base OS Packages

The license policy in `.grant.yaml` is also applied to the container images referenced by compiled workflows (`gh aw compile --grant`). Packages that ship with the upstream base images are listed under `ignore-packages` as a documented exception: the Alpine base OS packages (`busybox`, `apk-tools`, `alpine-baselayout`, `musl-utils`, `git`, `libgcc`, `libstdc++`, and their variants), the Debian base OS packages of the `ghcr.io/github/github-mcp-server` image (`base-files`, `libc6`, `libssl3`, `media-types`, `netbase`, `tzdata`), and the Node.js/npm runtime with npm's bundled dependencies (`node`, `npm`, `tar`, `glob`, `minipass`, and friends). They are executed as part of a third-party image, never linked into or redistributed with gh-aw, and cannot be changed without replacing the upstream image. Every other package in those images is still evaluated against the allowlist above.

`.grant.yaml` also supports an `ignore-images` list of glob patterns matched against the image reference. It excludes an entire image from license scanning and is reserved for third-party images whose bundled toolchains produce license findings that cannot be acted on, such as `ghcr.io/oraios/serena` (a Debian development image whose cargo registry ships hundreds of crates without SPDX license metadata). This key is specific to gh-aw; grant ignores it. Vulnerability scanning still covers every image, including the ignored ones.

`libatomic` is an explicitly approved runtime-package exception for the vendored `ghcr.io/github/gh-aw-node` image. The verified Linux/amd64 image inherited Alpine 3.24's `libatomic@15.2.0-r5` unchanged from its Node base. Its [Alpine package metadata](https://pkgs.alpinelinux.org/package/v3.24/main/x86_64/libatomic) declares `GPL-2.0-or-later AND LGPL-2.1-or-later`, inherited from the GCC packaging recipe; [GCC 15.2 libatomic source notices](https://github.com/gcc-mirror/gcc/blob/releases/gcc-15.2.0/libatomic/gload.c#L1-L24) specify `GPL-3.0-or-later WITH GCC-exception-3.1`. This is a policy acceptance, not a finding that publishing the runtime has no license obligations: the [GCC Runtime Library Exception](https://github.com/gcc-mirror/gcc/blob/releases/gcc-15.2.0/COPYING.RUNTIME) does not waive applicable notice or source-distribution requirements for the library itself. Grant's `ignore-packages` mechanism applies by package name across scanned images and versions. The general license allowlist, vulnerability scanning, and vendored-image gates remain unchanged.

### Container Vulnerability Exceptions

`gh aw compile --grype` reads the optional `.grype.yaml` file at the repository root and applies its `ignore` rules. The file is reserved for documented risk acceptances: findings that have no fixed version available upstream, where there is nothing to upgrade to. Each rule is scoped to a specific vulnerability ID, package, and affected version so newly disclosed vulnerabilities and rebuilt packages are still reported, and each carries a `reason` explaining the acceptance. Remove a rule as soon as the upstream image ships a fix — the daily container scan runs `gh aw compile --force-refresh-container-pins` and picks up rebuilt base images automatically. When `.grype.yaml` is absent, grype runs with its defaults.

### Before Adding a Dependency

GitHub Copilot Agent automatically checks licenses when adding dependencies. However, if you're evaluating a dependency:

1. **Check its license**: Run `make license-check` after adding the dependency
2. **Review the report**: Run `make license-report` to generate a CSV of all licenses
3. **If unsure**: Ask in your PR - maintainers will help evaluate edge cases

### License Checking

The project includes automated license compliance checking:

- **CI Workflow**: `.github/workflows/license-check.yml` runs on every PR that changes `go.mod`
- **Local Check**: Run `make license-check` to verify all dependencies (installs `go-licenses` on-demand)
- **License Report**: Run `make license-report` to see detailed license information

All dependencies are automatically scanned using Google's `go-licenses` tool in CI, which classifies licenses by type and identifies potential compliance issues. Note that `go-licenses` is not actively maintained, so we install it on-demand rather than as a regular build dependency.

## 🤖 Automated Dependency Updates (Dependabot)

This project uses GitHub Dependabot to automatically keep dependencies up-to-date with weekly security patches and version updates.

### What Dependabot Monitors

Dependabot is configured in `.github/dependabot.yml` to monitor:

1. **Go modules** (`/go.mod`) - Weekly updates for Go dependencies
2. **npm packages** - Weekly updates for:
   - Documentation site (`/docs/package.json`)
   - GitHub Actions setup scripts (`/actions/setup/js/package.json`)
   - Workflow dependencies (`/.github/workflows/package.json`)
3. **Python packages** (`/.github/workflows/requirements.txt`) - Weekly updates for workflow scripts

### Expected Behavior

- **Schedule**: Dependabot checks for updates **every Monday** (weekly interval)
- **Pull Requests**: Creates automated PRs from `dependabot[bot]` for:
  - Security vulnerabilities (immediate)
  - Version updates (weekly batch)
- **Limit**: Maximum of 10 open PRs per ecosystem to prevent overwhelming maintainers

### What to Expect from Dependabot PRs

Dependabot PRs will:
- Have clear titles like "Bump lodash from 4.17.20 to 4.17.21 in /docs"
- Include changelog links and release notes
- Show compatibility score based on semantic versioning
- Automatically rebase when the base branch changes

### Troubleshooting Dependabot

If Dependabot stops creating PRs:

1. **Check repository settings**: Go to Settings → Security → Dependabot
   - Ensure "Dependabot alerts" is enabled
   - Ensure "Dependabot security updates" is enabled
   - Ensure "Dependabot version updates" is enabled

2. **Verify configuration**: Check `.github/dependabot.yml` syntax
   - Directory paths must match locations of dependency files
   - Ecosystem names must be exact: `gomod`, `npm`, `pip`

3. **Check for rate limits**: Dependabot may be rate-limited if there are too many updates

4. **Manual trigger**: You can manually trigger Dependabot from repository Settings → Security → Dependabot

### Handling Dependabot PRs

When reviewing Dependabot PRs:

1. **Review the changes**: Check the changelog and compatibility score
2. **Let CI run**: Wait for all GitHub Actions checks to pass
3. **Test if needed**: For major version updates, test locally or let the agent verify
4. **Merge quickly**: Security updates should be merged as soon as CI passes
5. **Batch updates**: For minor version updates, you can merge multiple PRs at once

### Security Patches

Dependabot prioritizes security patches:
- Security vulnerabilities are updated **immediately** (not weekly)
- PRs are tagged with severity level (critical, high, medium, low)
- Security PRs should be reviewed and merged within 24-48 hours

## 🧪 Testing

For comprehensive testing guidelines including assert vs require usage, table-driven test patterns, and best practices, see **[scratchpad/testing.md](scratchpad/testing.md)**.

Quick reference:
- `make test-unit` - Fast unit tests (~25s)
- `make test` - Full test suite (~30s)
- `make agent-finish` - Complete validation before committing

### Compiling against a different actions repository

When working on changes that span both `github/gh-aw` and `github/gh-aw-actions`, you can compile workflows against a fork or branch of the actions repository:

```bash
# Compile against a fork with a specific branch
./gh-aw compile --action-mode action \
  --actions-repo myorg/my-aw-actions \
  --action-tag my-feature-branch \
  .github/workflows/my-workflow.md

# Compile with a specific tag or SHA
./gh-aw compile --action-mode action \
  --action-tag abc123def456 \
  .github/workflows/my-workflow.md
```

- `--action-mode action` — Required with `--actions-repo`. References actions as GitHub Actions from an external repository instead of inlining scripts locally.
- `--actions-repo <owner/repo>` — Override the default `github/gh-aw-actions` repository.
- `--action-tag <tag-or-sha>` — Pin action references to a specific tag, branch, or commit SHA.

Use these flags when testing workflow behavior against a branch in `github/gh-aw-actions` before a release is cut.

## 🚫 Spam Prevention

**Be nice, don't spam.** The project maintainers reserve the right to clean up spam, unsolicited promotions, or off-topic content as needed to keep discussions focused and valuable for all contributors.

This includes but is not limited to:
- Repeated identical or similar comments across multiple issues or pull requests
- Unsolicited promotional content or advertisements
- Off-topic comments that don't contribute to the discussion
- Automated bot comments without prior approval

## 🤝 Community

- Join the `#continuous-ai` channel in the [GitHub Next Discord](https://gh.io/next-discord)
- Participate in discussions on GitHub issues
- Collaborate by crafting high-quality agentic plans for the core team to implement

## 📜 Code of Conduct

This project follows the GitHub Community Guidelines. Please be respectful and inclusive in all interactions.

## ❓ Getting Help

- **For bugs or features**: Open a GitHub issue with a detailed agentic plan
- **For questions**: Ask in issues, discussions, or Discord
- **For examples**: Look at existing issues and PRs created by core team members
- **Remember**: You don't create PRs - you create issues with plans that a core team member implements using agents

## 🚀 Release Process

> **For core team maintainers only.** Community members do not participate in releasing.

Releases are defined in `.github/workflows/release.md` and triggered from the compiled GitHub Actions workflow.

The team follows a **weekly or bi-weekly minor release cadence**, similar to VS Code's release practices. Version numbers increment the minor component on each release cycle — not on the basis of change scope. Patch releases are reserved for urgent fixes between cycles; major releases are used for significant breaking changes only.

> **Note:** The release workflow publishes the new version as a **prerelease** on GitHub with `latest=false`. Prereleases are floated for a few days. On Monday, maintainers promote the last known good prerelease to stable so `latest` resolves to that release. Immediately after promotion, a new minor pre-release is kicked off to start the next cycle.

### Release credential changes

Changes to release credential minting, token selection, permissions, or checkout credential persistence require an explicit human security sign-off before merge, including any attempt to reintroduce GitHub App authentication. Record the approving maintainer/security reviewer's identity and link to their approval in the PR description. Automated review, compilation, and the release environment approval are not substitutes for this sign-off.

The PR must document:

- **Scope:** The credential's target repositories and minimum permissions, with a justification for each write permission. A tag-only push does not require `workflows: write`.
- **Availability and exposure:** For an App token, evidence that the App is installed on the target repository with the required permissions; which steps receive the token; whether checkout persists it for later build steps; and how it is revoked or removed. Do not include token or private-key values.
- **Validation:** Review both the workflow source and regenerated lock file, including failure handling and downstream-trigger behavior. App-token tag pushes can trigger workflows that `GITHUB_TOKEN` pushes do not.
- **Rollback:** The known-good credential path, rollback steps, and any previous revert's cause and unresolved review findings.

#### Previous App-token revert

[PR #60214](https://github.com/github/gh-aw/pull/60214) added an App token to the release tag checkout. [PR #60218](https://github.com/github/gh-aw/pull/60218) records the revert reason: the configured `APP_ID` was not installed on `github/gh-aw`, so token minting failed with HTTP 404 before tag creation. The revert restored the `GITHUB_TOKEN`-backed checkout and tag push; it was not a security sign-off for a future attempt. The original [automated review](https://github.com/github/gh-aw/pull/60214#discussion_r3986689467) also flagged unnecessary workflow-write permission and credential exposure to later build steps. Address both the installation failure and these review concerns before reintroducing App-token minting.

### Steps

1. **Launch the release action**

   Go to [Actions → Release](https://github.com/github/gh-aw/actions/workflows/release.lock.yml) and click **Run workflow**. Select the release type (`patch`, `minor`, or `major`) and start the run.

   The workflow will build the release binaries, push a new git tag, and then pause — waiting for a required manual step before publishing.

2. **Complete the sync in `github/gh-aw-actions`**

   While the workflow is paused at the `gh-aw-actions-release` environment gate, complete the required handoff in [`github/gh-aw-actions`](https://github.com/github/gh-aw-actions/actions/workflows/sync-actions.yml):

   a. **Run the sync-actions workflow** — go to [Actions → sync-actions](https://github.com/github/gh-aw-actions/actions/workflows/sync-actions.yml) and trigger it with the new release tag (e.g. `v1.2.3`).

   b. **Merge the PR** — the sync-actions workflow will open a pull request in `github/gh-aw-actions`. Review and merge it to bring the tag into that repository. The downstream release is created as a **draft**; leave it unpublished while the `gh-aw` prerelease is being evaluated.

3. **Approve the environment gate**

   Return to the paused release run in [`github/gh-aw`](https://github.com/github/gh-aw/actions). Approve the **`gh-aw-actions-release`** environment gate. The workflow will verify that the new tag exists in `github/gh-aw-actions` and then publish the GitHub release as a **prerelease**.

4. **Promote to latest on Monday** _(manual step)_

   After floating prereleases for a few days and confirming stability, on Monday:

   a. **Publish `gh-aw-actions` first** — open the matching tag's draft on the [`github/gh-aw-actions` Releases page](https://github.com/github/gh-aw-actions/releases). Ensure **This is a pre-release** is unchecked, select **Set as the latest release**, and publish it. If it is already published, edit it to make it stable and latest. This manual step is mandatory: no cross-repository write token is maintained to publish or promote it automatically.

   b. **Promote the same tag in `gh-aw`** — run [Actions → Promote Release to Latest](https://github.com/github/gh-aw/actions/workflows/promote-release.yml) with that exact version. The workflow refuses promotion unless `github/gh-aw-actions` already reports that tag as its published, stable latest release, then clears the `gh-aw` prerelease flag and explicitly sets it as latest.

   c. **Verify alignment** — confirm that both repositories' latest releases use the same tag. The **Validate latest release alignment** CI check also verifies this on every CI run, including hourly scheduled runs without recent commits.

   Users who install with `version: latest` (the default) will now receive the new release.

5. **Start the next release cycle** _(immediately after promotion)_

   Following the weekly/bi-weekly cadence, kick off a new `minor` release right after promoting the previous one to stable. Repeat steps 1–3 to publish it as a prerelease. This prerelease then floats until the next Monday, when it becomes the new stable release.

   > [!TIP]
   > Always select `minor` when starting a new cycle. Use `patch` only for urgent fixes within a cycle, and `major` only for significant breaking changes.

### Summary

```
Launch release action (minor)
        │
        ▼
Workflow pushes tag & pauses
        │
        ▼
Run sync-actions in github/gh-aw-actions (with new tag)
        │
        ▼
Merge the sync-actions PR
        │
        ▼
gh-aw-actions release remains a draft
        │
        ▼
Approve the gh-aw-actions-release environment gate
        │
        ▼
Release published as prerelease 🎉
        │
        ▼  (Monday — manual)
Manually publish gh-aw-actions draft as stable & latest FIRST
        │
        ▼
Run Promote Release to Latest in gh-aw with the same tag
        │
        ▼
Both repositories' latest releases now match ✅
        │
        ▼  (same day — start next cycle)
Launch next minor release action → new prerelease published
```

## 🎯 Why This Contribution Model?

This project is built by a core team using agentic development to demonstrate and dogfood the capabilities of GitHub Agentic Workflows:

- **Dogfooding**: We use our own tools to build our tools
- **Consistency**: All changes go through the same automated quality gates
- **Best practices**: Agents follow established guidelines automatically
- **Focus on outcomes**: Describe what you want, not how to build it
- **Quality plans**: Encourages contributors to think through the full implementation before work begins

Community members contribute by crafting detailed agentic plans that the core team picks up and implements. **This keeps the bar high** — well-thought-out plans lead to well-executed PRs.

The [Development Guide](DEVGUIDE.md) is the reference guide used by core team members and their agents.

Thank you for contributing to GitHub Agentic Workflows! 🤖🎉


## Repository Topics (Maintainer Note)

To improve discoverability in GitHub search and AI assistant recommendations, the repository should have these topics set (via repository Settings → Topics — must be done by a maintainer with admin access):

```
github-actions  ai-agents  claude  copilot  automation  openai  gemini  agentic-workflows  continuous-ai  safe-outputs
```

These map to the most common search queries the project serves: "claude code github actions", "ai agents github", "copilot automation", "automated pr review ai", etc.
