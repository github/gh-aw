# Contributing to GitHub Agentic Workflows

Thank you for your interest in contributing to GitHub Agentic Workflows! We welcome contributions from the community and are excited to work with you.

**⚠️ IMPORTANT: This project requires agentic development using GitHub Copilot Agent. No local development environment is needed or expected.**

## 🤖 Agentic Development Workflow

GitHub Agentic Workflows is developed **exclusively through GitHub Copilot Agent**. This means:

- ✅ **All development happens in pull requests** created by GitHub Copilot Agent
- ✅ **No local setup required** - agents handle building, testing, and validation
- ✅ **Automated quality assurance** - CI runs all checks on agent-created PRs
- ❌ **Local development is not supported** - all work is done through the agent

### Why Agentic Development?

This project practices what it preaches: agentic workflows are used to build agentic workflows. Benefits include:

- **Consistency**: All changes go through the same automated quality gates
- **Accessibility**: No need to set up local development environments
- **Best practices**: Agents follow established patterns and guidelines automatically
- **Dogfooding**: We use our own tools to build our tools

## 🚀 Quick Start for Contributors

### Step 1: Fork the Repository

Fork <https://github.com/github/gh-aw/> to your GitHub account

### Step 2: Open an Issue or Discussion

- Describe what you want to contribute
- Explain the use case and expected behavior
- Provide examples if applicable
- Tag with appropriate labels (see [Label Guidelines](scratchpad/labels.md))

### Step 3: Create a Pull Request with GitHub Copilot Agent

Use GitHub Copilot Agent to implement your contribution:

1. **Start from the issue**: Reference the issue number in your PR description
2. **Provide clear instructions**: Tell the agent what changes you want
3. **Let the agent work**: The agent will read guidelines, make changes, run tests
4. **Review and iterate**: The agent will respond to feedback and update the PR

**Example PR description:**

```markdown
Fix #123 - Add support for custom MCP server timeout configuration

@github-copilot agent, please:
- Add a `timeout` field to MCP server configuration schema
- Update validation to accept timeout values between 5-300 seconds
- Add tests for timeout validation
- Update documentation with timeout examples
- Follow error message style guide for validation messages
```

### Step 4: Agent Handles Everything

The GitHub Copilot Agent will:

- Read relevant documentation and specifications
- Make code changes following established patterns
- Run `make agent-finish` to validate changes
- Format code, run linters, execute tests
- Recompile workflows to ensure compatibility
- Respond to review feedback and make adjustments

### No Local Setup Needed

You don't need to install Go, Node.js, or any dependencies. The agent runs in GitHub's infrastructure with all tools pre-configured.

## 📝 How to Contribute via GitHub Copilot Agent

All contributions are made through GitHub Copilot Agent in pull requests. The agent has access to comprehensive documentation and follows established patterns automatically.

### What the Agent Handles

The GitHub Copilot Agent automatically:

- **Reads specifications** from `scratchpad/`, `skills/`, and `.github/instructions/`
- **Follows code organization patterns** (see [scratchpad/code-organization.md](scratchpad/code-organization.md))
- **Implements validation** following the architecture in [scratchpad/validation-architecture.md](scratchpad/validation-architecture.md)
- **Uses console formatting** from `pkg/console` for CLI output
- **Writes error messages** following the [Error Message Style Guide](.github/instructions/error-messages.instructions.md)
- **Runs all quality checks**: `make agent-finish` (build, test, recompile, format, lint)
- **Updates documentation** for new features
- **Creates tests** for new functionality

### Reporting Issues

Use the GitHub issue tracker to report bugs or request features:

- Include detailed steps to reproduce issues
- Explain the use case for feature requests
- Provide examples if applicable
- Follow [Label Guidelines](scratchpad/labels.md)
- The agent will read the issue and implement fixes in a PR

### Code Quality Standards

GitHub Copilot Agent automatically enforces:

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

## 🔄 Pull Request Process via GitHub Copilot Agent

All pull requests are created and managed by GitHub Copilot Agent:

1. **Issue or discussion first:**
   - Open an issue describing what needs to be done
   - Provide clear context and examples
   - Tag appropriately using [Label Guidelines](scratchpad/labels.md)

2. **Agent creates the PR:**
   - Mention `@github-copilot agent` with instructions
   - Agent reads specifications and guidelines
   - Agent makes changes following established patterns
   - Agent runs `make agent-finish` automatically

3. **Automated quality checks:**
   - CI runs on agent-created PRs
   - All checks must pass (build, test, lint, recompile)
   - Agent responds to CI failures and fixes them

4. **Review and iterate:**
   - Maintainers review the PR
   - Provide feedback as comments
   - Agent responds to feedback and makes adjustments
   - Once approved, PR is merged

### What Gets Validated

Every agent-created PR automatically runs:

- `make build` - Ensures Go code compiles
- `make test` - Runs all unit and integration tests
- `make lint` - Checks code quality and style
- `make recompile` - Recompiles all workflows to ensure compatibility
- `make fmt` - Formats Go code
- `make lint-errors` - Validates error message quality

## 🏗️ Project Structure (For Agent Reference)

The agent understands this structure:

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

## 🤝 Community

- Join the `#continuous-ai` channel in the [GitHub Next Discord](https://gh.io/next-discord)
- Participate in discussions on GitHub issues
- Collaborate through GitHub Copilot Agent PRs

## 📜 Code of Conduct

This project follows the GitHub Community Guidelines. Please be respectful and inclusive in all interactions.

## ❓ Getting Help

- **For bugs or features**: Open a GitHub issue and work with the agent
- **For questions**: Ask in issues, discussions, or Discord
- **For examples**: Look at existing agent-created PRs

## 🎯 Why No Local Development?

This project is built using agentic workflows to demonstrate their capabilities:

- **Dogfooding**: We use our own tools to build our tools
- **Accessibility**: No need for complex local setup
- **Consistency**: All changes go through the same automated process
- **Best practices**: Agents follow guidelines automatically
- **Focus on outcomes**: Describe what you want, not how to build it

The [Development Guide](DEVGUIDE.md) exists as reference for the agent, not for local setup.

Thank you for contributing to GitHub Agentic Workflows! 🤖🎉
