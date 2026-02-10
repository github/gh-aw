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
  threat-detection: false  # Disable threat detection
```

### Advanced Configuration

Use object syntax for fine-grained control:

```yaml wrap
safe-outputs:
  create-issue:
  threat-detection:
    enabled: true                    # Enable/disable detection
    prompt: "Focus on SQL injection" # Additional analysis instructions
    steps:                           # Custom detection steps
      - name: Custom Security Check
        run: echo "Running additional checks"
```

**Configuration Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | boolean | Enable or disable detection (default: `true` when safe-outputs exist) |
| `prompt` | string | Custom instructions appended to default detection prompt |
| `engine` | string/object/false | AI engine config (`"copilot"`, full config object, or `false` for no AI) |
| `steps` | array | Additional GitHub Actions steps to run after AI analysis |

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

Add specialized security scanning tools alongside or instead of AI detection:

```yaml wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    steps:
      - name: Run Security Scanner
        run: |
          echo "Scanning agent output for threats..."
          # Your custom security logic here

      - name: Check for Secrets
        uses: trufflesecurity/trufflehog@main
        with:
          path: /tmp/gh-aw/threat-detection/
```

**Available Artifacts:** Custom steps have access to `/tmp/gh-aw/threat-detection/prompt.txt` (workflow prompt), `agent_output.json` (safe output items), and `aw.patch` (git patch file).

**Execution Order:** Download artifacts → Run AI analysis (if enabled) → Execute custom steps → Upload detection log.

## Example: LlamaGuard Integration

Use Ollama with LlamaGuard 3 for specialized threat detection:

```yaml wrap
---
on: push
engine: copilot
safe-outputs:
  create-pull-request:
  threat-detection:
    steps:
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
    steps:
      - name: Static Analysis
        run: |
          # Run static analysis tool
          semgrep --config auto /tmp/gh-aw/threat-detection/

      - name: Secret Scanner
        uses: trufflesecurity/trufflehog@main
        with:
          path: /tmp/gh-aw/threat-detection/aw.patch
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

## Best Practices

**Use AI-based detection** for analyzing natural language content, detecting sophisticated prompt injection, understanding context-specific risks, and identifying intent-based threats.

**Add custom steps** for integrating specialized security tools (Semgrep, Snyk, TruffleHog), enforcing organization policies, scanning domain-specific vulnerabilities, and meeting compliance requirements.

**Performance:** AI analysis typically completes in 10-30 seconds. Custom tools vary (LlamaGuard: 5-15 minutes with model download). Set appropriate `timeout-minutes` and truncate large patches if needed.

**Security:** Use defense-in-depth with both AI and custom detection for critical workflows. Keep tools updated, validate with known malicious samples, monitor false positives, and document detection rationale.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| **AI detection always fails** | Review custom prompt for overly strict instructions, check if legitimate patterns trigger detection, adjust prompt context, or temporarily disable to test |
| **Custom steps not running** | Verify YAML indentation, ensure steps array is properly formatted, review compilation output, check if AI detection failed first |
| **Large patches cause timeouts** | Increase `timeout-minutes`, configure `max-patch-size`, truncate content before analysis, or split changes into smaller PRs |
| **False positives** | Refine prompt with specific exclusions, adjust tool thresholds, add workflow context explaining patterns, review detection logs |

## Related Documentation

- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Complete safe outputs configuration
- [Security Guide](/gh-aw/introduction/architecture/) - Overall security best practices
- [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/) - Creating custom output types
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - All configuration options
