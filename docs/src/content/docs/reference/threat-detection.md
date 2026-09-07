---
title: Threat Detection
description: Configure automated threat detection to analyze agent output and code changes for security issues before they are applied.
sidebar:
  order: 40
disable-agentic-editing: true
---

GitHub Agentic Workflows includes automatic threat detection to analyze agent output and code changes for potential security issues before they are applied. When safe outputs are configured, a threat detection job automatically runs to identify prompt injection attempts, secret leaks, and malicious code patches.

## How It Works

Threat detection provides an additional security layer by analyzing agent output for malicious content, scanning code changes for suspicious patterns, using workflow context to distinguish legitimate actions from threats, and running automatically after the main job completes but before safe outputs are applied.

**Security Architecture:**

```text
┌─────────────────┐
│ Agentic Job     │ (Read-only permissions)
│ Generates       │
│ Output & Patches│
└────────┬────────┘
         │ artifacts
         ▼
┌─────────────────┐
│ Threat Detection│ (Analyzes for security issues)
│ Job             │
└────────┬────────┘
         │ approved/blocked
         ▼
┌─────────────────┐
│ Safe Output Jobs│ (Write permissions, only if safe)
│ Create Issues,  │
│ PRs, Comments   │
└─────────────────┘
```

## Default Configuration

Threat detection is **automatically enabled** when safe outputs are configured:

```yaml wrap
safe-outputs:
  create-issue:     # Threat detection enabled automatically
  create-pull-request:
```

The default configuration uses AI-powered analysis to detect prompt injection (malicious instructions manipulating AI behavior), secret leaks (exposed API keys, tokens, passwords, credentials), and malicious patches (code changes introducing vulnerabilities, backdoors, or suspicious patterns).

## Configuration Options

### Basic Enabled/Disabled

Control threat detection with a boolean flag:

```yaml wrap
safe-outputs:
  create-issue:
  threat-detection: true   # Explicitly enable (default when safe-outputs exist)

# Or disable entirely:
safe-outputs:
  create-pull-request:
  threat-detection: false  # Disable threat detection entirely
```

The `features.gh-aw-detection` flag controls the detection implementation, not
whether threat detection runs. The external `threat-detect` implementation is
the default; set `features.gh-aw-detection: false` to select the legacy inline
engine implementation. The external binary's version and per-architecture
SHA-256 digests are pinned in the immutable setup action, so installation does
not trust a checksum downloaded alongside the binary.

> [!NOTE]
> When a workflow explicitly sets `threat-detection: false`, that setting takes precedence over any imported fragments. Imported shared workflows that configure safe outputs without a `threat-detection` key will not re-enable threat detection in the importing workflow.

### Advanced Configuration

Use object syntax for fine-grained control:

```yaml wrap
safe-outputs:
  create-issue:
  threat-detection:
    enabled: true                    # Enable/disable detection
    prompt: "Focus on SQL injection" # Additional analysis instructions
    steps:                           # Custom steps run before engine execution
      - name: Setup Security Gateway
        run: echo "Connecting to security gateway..."
    post-steps:                      # Custom steps run after engine execution
      - name: Custom Security Check
        run: echo "Running additional checks"
```

**Configuration Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | boolean | Enable or disable detection (default: `true` when safe-outputs exist) |
| `prompt` | string | Custom instructions appended to default detection prompt |
| `engine` | string/object/false | AI engine config (`"copilot"`, full config object, or `false` for no AI) |
| `runs-on` | string/array/object | Runner for the detection job (default: inherits from workflow `runs-on`) |
| `steps` | array | Additional GitHub Actions steps to run **before** AI analysis (pre-steps) |
| `post-steps` | array | Additional GitHub Actions steps to run **after** AI analysis (post-steps) |
| `max-ai-credits` | integer | AI Credits cap for the detection run, independent of the main agent budget. Defaults to `400` when unset, with runtime override via `vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS`. Accepts plain integers; `-1` disables the detection budget. |

## Detection Budget

Threat-detection runs have their own AI Credits budget, separate from the main agent's `max-ai-credits`. Detection does **not** inherit the main agent's budget — both caps apply independently to their respective jobs.

Set `safe-outputs.threat-detection.max-ai-credits` to override the per-run detection budget:

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    max-ai-credits: 750
```

When unset, the compiler emits a runtime resolution that falls back to the built-in default of `400`:

```yaml
${{ vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS || '400' }}
```

Set the org-wide default with the [`GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS`](/gh-aw/reference/compiler-enterprise-environment-controls/) GitHub Actions variable. A value of `-1` disables AWF budget steering for detection runs.

## AI-Based Detection (Default)

By default, threat detection uses the same AI engine as your main workflow to analyze output for security threats.

**Example with Default AI Detection:**

```yaml wrap
---
on: push
safe-outputs:
  create-pull-request:
  threat-detection: true  # Uses Claude for analysis
---

# Code Review Agent

Analyze code and create pull requests with improvements.
```

The AI engine receives the workflow source context and analyzes agent output items (issues, comments, PRs), git patch files with code changes, and workflow intent to distinguish legitimate actions.

**Output Format:**

AI detection produces a structured JSON response:

```json wrap
{
  "prompt_injection": false,
  "secret_leak": false,
  "malicious_patch": false,
  "reasons": []
}
```

If any threat is detected (`true`), the workflow fails and safe outputs are blocked.

## Custom Prompt Instructions

Enhance AI analysis with domain-specific guidance:

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    prompt: |
      Focus on these additional security concerns:
      - SQL injection vulnerabilities in database queries
      - Cross-site scripting (XSS) in user input handling
      - Unsafe deserialization patterns
      - Hardcoded credentials in configuration files
```

The custom prompt is appended to the default threat detection instructions, providing specialized context for your workflow's domain.

## Custom Engine Configuration

Override the main workflow engine for threat detection:

**String Format:**

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    engine: copilot  # Use Copilot instead of main workflow engine
```

**Object Format:**

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    engine:
      id: copilot
      max-turns: 3
```

**Disable AI Engine:**

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    engine: false    # No AI analysis, only custom steps
    steps:
      - name: Custom Scanning
        run: |
          # Run your own security tools
          ./security-scan.sh
```

## Custom Detection Steps

Add specialized security scanning tools alongside or instead of AI detection. You can run steps **before** the AI engine (for setup, gateway connections, etc.) and steps **after** (for additional scanning based on AI results).

### Pre-Steps (`steps:`)

Steps defined under `steps:` run **before** the AI engine executes. Use these for setup tasks such as connecting to a private AI gateway, installing security tools, or preparing artifacts.

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    steps:
      - name: Connect to Security Gateway
        run: |
          echo "Setting up secure connection to analysis gateway..."
          # Authentication and connection setup
```

### Post-Steps (`post-steps:`)

Steps defined under `post-steps:` run **after** the AI engine completes its analysis. Use these for additional security scanning, reporting, or cleanup.

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    post-steps:
      - name: Run Security Scanner
        run: |
          echo "Scanning agent output for threats..."
          # Your custom security logic here

      - name: Check for Secrets
        uses: trufflesecurity/trufflehog@main
        with:
          path: /tmp/gh-aw/threat-detection/
```

**Available Artifacts:** Custom steps have access to `/tmp/gh-aw/threat-detection/prompt.txt` (workflow prompt), `agent_output.json` (safe output items), and `aw.patch` (git patch file). gh-aw also stages `/tmp/gh-aw/threat-detection/aw-prompts/prompt-template.txt`, `/tmp/gh-aw/threat-detection/aw-prompts/prompt-import-tree.json`, `/tmp/gh-aw/threat-detection/aw_info.json`, and any restored `/tmp/gh-aw/threat-detection/comment-memory/*.md` files so detectors can analyze prompt structure, activation context, and persisted comment memory.

**Execution Order:** Download artifacts → Stage detection inputs → Execute pre-steps (`steps:`) → Run AI analysis (if enabled) → Execute post-steps (`post-steps:`) → Upload detection log.

## Example: LlamaGuard Integration

Use Ollama with LlamaGuard 3 for specialized threat detection running after AI analysis:

```yaml wrap
---
on: push
engine: copilot
safe-outputs:
  create-pull-request:
  threat-detection:
    post-steps:
      - name: Ollama LlamaGuard 3 Scan
        uses: actions/github-script@v8
        with:
          script: |
            // Install and start Ollama service
            await exec.exec('curl', ['-fsSL', 'https://ollama.com/install.sh', '-o', '/tmp/install.sh']);
            await exec.exec('sh', ['/tmp/install.sh']);
            exec.exec('ollama', ['serve'], { detached: true });

            // Pull model and scan output
            await exec.exec('ollama', ['pull', 'llama-guard3:1b']);
            const content = require('fs').readFileSync('/tmp/gh-aw/threat-detection/agent_output.json', 'utf8');
            const response = await exec.getExecOutput('curl', [
              '-X', 'POST', 'http://localhost:11434/api/chat',
              '-H', 'Content-Type: application/json',
              '-d', JSON.stringify({ model: 'llama-guard3:1b', messages: [{ role: 'user', content }] })
            ]);

            const result = JSON.parse(response.stdout);
            const isSafe = result.message?.content.toLowerCase().includes('safe');
            if (!isSafe) core.setFailed('LlamaGuard detected threat');

timeout-minutes: 20
---

# Code Review Agent
```

> [!TIP]
> For a complete implementation with error handling and service readiness checks, see `.github/workflows/shared/ollama-threat-scan.md` in the repository.

## Combined AI and Custom Detection

Use both AI analysis and custom tools for defense-in-depth:

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    prompt: "Check for authentication bypass vulnerabilities"
    engine: copilot
    post-steps:
      - name: Static Analysis
        run: |
          # Run static analysis tool
          semgrep --config auto /tmp/gh-aw/threat-detection/

      - name: Secret Scanner
        uses: trufflesecurity/trufflehog@main
        with:
          path: /tmp/gh-aw/threat-detection/aw.patch
```

## Example: Private AI Gateway

Connect to a private AI gateway before running the detection engine:

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    steps:
      - name: Connect to AI Gateway
        run: |
          # Authenticate and set up connection to private AI gateway
          echo "Setting up gateway connection..."
          ./scripts/setup-gateway.sh
    engine:
      id: copilot
```

## Error Handling

**When Threats Are Detected:**

The threat detection job fails with a clear error message and safe output jobs are skipped:

```text
❌ Threat detected: Potential SQL injection in code changes
Reasons:
- Unsanitized user input in database query
- Missing parameterized query pattern
```

**When Detection Fails:**

If the detection process itself fails (e.g., network issues, tool errors), the workflow stops and safe outputs are not applied. This fail-safe approach prevents potentially malicious content from being processed.

**When Detection Returns a Warning:**

A warning is a lower-severity signal than a hard threat: the safe output is allowed to proceed, but human review is required before merge. When `create-pull-request` is the safe output, the handler submits a `REQUEST_CHANGES` pull request review whose body includes the detection reason and a link to the workflow run logs. If a `request_review` protected-files gate also fires in the same run, both signals are composed into a single review body separated by a horizontal rule.

## Supply Chain Protection (Protected Files)

Beyond AI-powered threat detection, GitHub Agentic Workflows includes a static, rule-based protection layer that guards against **supply chain attacks** — cases where an AI agent could (intentionally or accidentally) modify files that control how software is built, tested, or deployed.

### The Threat

An AI agent operating in a repository can be tricked (through prompt injection or misconfigured tasks) into modifying:

- **Dependency manifests** (`package.json`, `go.mod`, `requirements.txt`, `Gemfile`, `pom.xml`, etc.) — changing what third-party code is installed.
- **CI/CD configuration** (`.github/workflows/*.yml`, `.github/dependabot.yml`, etc.) — altering how and when pipelines run, potentially exfiltrating secrets or bypassing security checks.
- **Agent instruction files** (`AGENTS.md`, `CLAUDE.md`, `.claude/settings.json`, `.agents/`, etc.) — redirecting the AI agent's behavior on subsequent runs.

### Default Remediation

Protected file protection is **enabled by default** for `create-pull-request` and `push-to-pull-request-branch`. Any patch that touches a protected file or directory causes the safe output to fail with a clear error:

```
Cannot create pull request: patch modifies protected files (package.json).
Set protected-files: fallback-to-issue to create a review issue instead.
```

This error is also surfaced as a **🛡️ Protected Files** section in the agent failure issue or comment created by the conclusion job.

### Policy Options

Configure how each safe output handles protected file changes using the `protected-files` field:

| Value | Behavior |
|-------|-----------|
| `request_review` (default) | Create the pull request and submit a `REQUEST_CHANGES` review listing the protected files. A human reviewer must approve before merge. |
| `blocked` | Hard-block: the safe output fails with an error message |
| `allowed` | No restriction — all protected file changes are permitted |
| `fallback-to-issue` | Create a review issue instead of a PR / push, so a human can inspect and apply the changes manually |

```yaml wrap
safe-outputs:
  create-pull-request:
    protected-files: fallback-to-issue  # human review required for protected file changes

  push-to-pull-request-branch:
    protected-files: fallback-to-issue  # create issue instead of pushing protected file changes
```

### Protected Files

The protection list is composed of four sources:

1. **Runtime dependency manifests** — one entry per supported package manager (npm, Go, Python, Ruby, Java, Rust, Elixir, Haskell, .NET, Bun, Deno, uv).
2. **Engine instruction files** — added automatically based on the active AI engine:
   - **Copilot**: `AGENTS.md`
   - **Claude**: `CLAUDE.md`; directory prefix `.claude/`
   - **Codex**: `AGENTS.md`; directory prefix `.codex/`
3. **Repository security configuration** — the `.github/` and `.agents/` path prefixes (`.github/` covers GitHub Actions workflows, Dependabot config; `.agents/` covers generic agent instruction and configuration files).
4. **Repository access control files** — matched by filename anywhere in the repository: `CODEOWNERS` (governs required code reviewers; valid at the repository root, `.github/`, or `docs/`).

> [!TIP]
> If your workflow is explicitly designed to update dependencies or CI configuration, set `protected-files: allowed` for that safe output. In repositories where human oversight is preferred, `protected-files: fallback-to-issue` provides a middle ground: the agent performs all other operations normally, and a review issue is created for runs that involve protected files.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| **AI detection always fails** | Review custom prompt for overly strict instructions, check if legitimate patterns trigger detection, adjust prompt context, or temporarily disable to test |
| **Custom steps not running** | Verify YAML indentation, ensure steps array is properly formatted, review compilation output, check if AI detection failed first |
| **Large patches cause timeouts** | Increase `jobs.detection.timeout-minutes` (10 minutes by default, or `vars.GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES`), configure `max-patch-size`, truncate content before analysis, or split changes into smaller PRs |
| **False positives** | Refine prompt with specific exclusions, adjust tool thresholds, add workflow context explaining patterns, review detection logs |

## Learn More

- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Complete safe outputs configuration
- [Security Guide](/gh-aw/introduction/architecture/) - Overall security best practices
- [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/) - Creating custom output types
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - All configuration options
