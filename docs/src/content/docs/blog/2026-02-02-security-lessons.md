---
title: "Security Lessons from the Agent Factory"
description: "Designing safe environments where agents can't accidentally cause harm"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-02-02
draft: true
prev:
  link: /gh-aw/blog/2026-01-30-imports-and-sharing/
  label: Imports & Sharing
next:
  link: /gh-aw/blog/2026-02-05-how-workflows-work/
  label: How Workflows Work
---

[Previous Article](/gh-aw/blog/2026-01-30-imports-and-sharing/)

---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

Gather 'round, gather 'round for *the next delicious treat* in the Peli's Agent Factory series! Having just sampled the wonders of [imports and sharing](/gh-aw/blog/2026-01-30-imports-and-sharing/), we now venture into the *vault* - the most critical chamber of all - where we discuss security!

Security in agentic workflows isn't just about locking things down - it's about designing environments where agentic workflows can do their jobs safely, even if they make mistakes. Our collection of workflows in practice has taught us a ton about what works (and what doesn't).

Here's the thing: **safety isn't just about permissions**. It's about creating guardrails that let workflows be productive while preventing them from accidentally causing harm. Many of the security features in GitHub Agentic Workflows came directly from lessons we learned in the factory.

Let's share what we've figured out so you can build secure agent ecosystems from day one.

## Core Security Principles

### Least Privilege, Always

**Start with read-only. Add write permissions only when absolutely necessary, and always through constrained safe outputs.**

Every workflow begins with `permissions: contents: read`. That's our default stance. Write permissions (`contents: write`, `pull-requests: write`, `issues: write`) get granted sparingly and only through safe output mechanisms.

**Example**: The [`audit-workflows`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md) agent has read-only access to workflow runs but creates reports via discussions, which are append-only by nature.

**Why this works**: If an agent can only read, the worst it can do is waste compute time. It can't delete code, close important issues, or push malicious changes.

### Safe Outputs as the Gateway

**All effectful operations go through safe outputs with built-in limits.**

Safe outputs are hands-down the factory's most important security control. They provide a constrained API for agents to interact with GitHub, with guardrails that prevent common mistakes:

**Built-in Protections:**

- Maximum items to create (prevents spam)
- Expiration times (prevents forgotten issues)
- "Close older duplicates" logic (prevents duplication)
- "If no changes" guards (prevents empty PRs)
- Template validation (enforces structure)
- Rate limiting (prevents abuse)

**Example**: An agent creating issues through safe outputs can specify:

```yaml
safe_outputs:
  create_issue:
    title: "Found security vulnerability"
    body: "Details here"
    labels: ["security"]
    max_items: 3  # Only create 3 issues max
    close_older: true  # Close old instances
    expire: "+7d"  # Auto-close if not addressed
```

**Why this works**: Safe outputs transform "can the agent do X?" into "under what constraints can the agent do X?" The agent has power but can't abuse it. Pretty clever, right?

### Role-Gated Activation

**Powerful agents (fixers, optimizers) require specific roles to invoke.**

Not every mention or workflow event should trigger powerful agents. We use role-gating to ensure only authorized users can invoke sensitive operations.

**Example**: The [`q`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/q.md) optimizer requires the user commenting `/q` to be a repository maintainer. Random contributors can't trigger expensive optimization runs.

**Implementation**:

```yaml
on:
  issue_comment:
    types: [created]

jobs:
  check:
    if: |
      contains(github.event.comment.body, '/q') &&
      (github.event.comment.author_association == 'OWNER' ||
       github.event.comment.author_association == 'MEMBER')
```

**Why this works**: Authorization gets enforced at the GitHub platform level, not by the agent. The agent never even runs if the user lacks permissions.

### ⏱️ Time-Limited Experiments

**Experimental agents include `stop-after: +1mo` to automatically expire.**

We encourage experimentation, but experiments shouldn't run forever. Time limits prevent forgotten demos from consuming resources or causing confusion.

**Example**:

```yaml
---
description: Experimental code deduplication agent
stop-after: +1mo
---
```

After one month, the workflow automatically disables itself. If the experiment works out, you can graduate it to production without the time limit.

**Why this works**: Explicit expiration forces intentional decisions. Every agent running in the factory is deliberately there, not just forgotten.

### Explicit Tool Lists

**Workflows declare exactly which tools they use. No ambient authority.**

Every workflow explicitly lists its tool requirements. There's no "give me access to everything" permission. This makes security review straightforward and catches tool misuse early.

**Example**:

```yaml
tools:
  github:
    toolsets: [repos, issues]  # Only repos and issues
  bash:
    commands: [git, jq, python]  # Only these commands
network:
  allowed:
    - "api.github.com"  # Only GitHub API
```

**Why this works**: Explicit beats implicit every time. Reviewers can quickly assess risk. Agents can't accidentally use tools they shouldn't have.

### 📋 Auditable by Default

**Discussions and assets create a natural "agent ledger." You can always trace what an agent did and when.**

Every agent action leaves a trail:

- Issues and PRs are timestamped
- Comments are attributed
- Discussions are permanent
- Artifacts are versioned
- Workflow runs are logged

**Example**: The [`agent-performance-analyzer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/agent-performance-analyzer.md) creates weekly discussion posts. You can scroll back months to see how agent quality has evolved over time.

**Why this works**: Transparency builds trust. When something goes wrong, the audit trail makes debugging straightforward. When something goes right, the evidence is right there for everyone to see.

## Security Patterns

### Pattern 1: Read-Only Analysts

The safest agents are read-only. They observe, analyze, and report - but never modify anything.

**Security Properties:**

- ✅ Zero risk of code damage
- ✅ Can't close or modify issues
- ✅ Can't create spam
- ✅ Safe to run at any frequency

**Use case**: Metrics collection, health monitoring, research, auditing

**Example**: All 15 read-only analyst workflows in the factory have perfect security records - zero incidents. That says something!

### Pattern 2: Safe Output Bounded Writes

When agents need write access, use safe outputs with strict bounds.

**Security Properties:**

- ✅ Constrained by max items
- ✅ Auto-expiring issues/PRs
- ✅ Duplicate detection
- ✅ Template enforcement
- ✅ Rate limited

**Use case**: Issue triage, PR creation, documentation updates

**Example**: [`issue-triage-agent`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/issue-triage-agent.md) can add labels but can't close issues or modify code.

### Pattern 3: Human-in-the-Loop

For high-impact operations, require human approval before execution.

**Security Properties:**

- ✅ Human reviews PR before merge
- ✅ Explicit approval step
- ✅ Can be reverted
- ✅ Blame trail maintained

**Use case**: Code changes, dependency updates, configuration changes

**Example**: [`daily-workflow-updater`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/daily-workflow-updater.md) creates PRs for dependency updates but never merges them automatically.

### Pattern 4: Role-Gated ChatOps

Interactive agents that require authorization to invoke.

**Security Properties:**

- ✅ Platform-enforced authorization
- ✅ Clear invocation trail
- ✅ User attribution
- ✅ Can be disabled per-user

**Use case**: Code review, optimization, debugging assistance

**Example**: [`grumpy-reviewer`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/grumpy-reviewer.md) requires collaborator access to invoke via `/grumpy`.

### Pattern 5: Network Restricted

Limit network access to specific allowlisted domains.

**Security Properties:**

- ✅ Prevents data exfiltration
- ✅ Blocks unauthorized API calls
- ✅ Enforced at infrastructure level
- ✅ Clear audit of network usage

**Use case**: Any agent that doesn't need external network access

**Example**: Most analysis agents only need `api.github.com` access - nothing more.

## Key Takeaways

Building secure agent ecosystems isn't about saying "no" to everything. It's about designing environments where agents can be productive while staying safe:

1. **Start read-only** - Add write permissions only when necessary
2. **Use safe outputs** - They're your most important security control
3. **Gate powerful operations** - Role-based access prevents abuse
4. **Time-limit experiments** - Prevent forgotten demos from running forever
5. **Be explicit about tools** - No ambient authority
6. **Embrace auditability** - Transparency builds trust
7. **Combine patterns** - Layer security controls for defense in depth

Security in agentic workflows is about enabling innovation safely. With the right guardrails, agents can do amazing things without keeping you up at night.

- ✅ Can't exfiltrate data
- ✅ Can't access internal services
- ✅ Can't download malicious payloads
- ✅ Enforced at infrastructure level

**Use case**: Workflows needing external APIs

**Example**: Workflows using Tavily search can only access `api.tavily.com`, not arbitrary websites.

## Common Security Mistakes

### Mistake 1: Overly Permissive Defaults

**Problem**: Granting `contents: write` when `contents: read` suffices.

**Impact**: Agent can accidentally push code changes.

**Solution**: Start with least privilege. Add permissions only when safe outputs require them.

### Mistake 2: Unbounded Safe Outputs

**Problem**: Forgetting `max_items` limit on safe output creation.

**Impact**: Agent creates hundreds of duplicate issues.

**Solution**: Always set `max_items`, `expire`, and `close_older` on safe outputs.

### Mistake 3: No Tool Allowlisting

**Problem**: Allowing `bash: "*"` (all bash commands).

**Impact**: Agent can run `rm -rf` or other destructive commands.

**Solution**: Explicitly list allowed commands: `bash: [git, jq, python]`.

### Mistake 4: Missing Role Gates

**Problem**: Anyone can trigger `/deploy` command.

**Impact**: Malicious actor triggers expensive or destructive operations.

**Solution**: Add author association checks for sensitive operations.

### Mistake 5: No Network Restrictions

**Problem**: Allowing open network access.

**Impact**: Agent can access internal services or exfiltrate data.

**Solution**: Use `network.allowed` to allowlist specific domains.

## Security Incidents and Response

The factory experienced a few security-adjacent incidents that taught valuable lessons:

### Incident 1: Issue Spam

**What happened**: Agent with unbounded `create_issue` safe output created 50+ duplicate issues.

**Root cause**: Missing `max_items` and `close_older` constraints.

**Fix**: Added `max_items: 3` and `close_older: true` to all issue creation safe outputs.

**Lesson**: Safe outputs need explicit bounds, not just permission gates.

### Incident 2: Expensive Workflow Loop

**What happened**: Agent triggered itself recursively, creating workflow loop.

**Root cause**: Workflow triggered on `workflow_run: completed` without filtering.

**Fix**: Added workflow name filter to prevent self-triggering.

**Lesson**: Event filters are security controls, not just optimizations.

### Incident 3: Leaked Secret Reference

**What happened**: Agent logged GitHub token in error message.

**Root cause**: Overly verbose error handling.

**Fix**: Sanitized all error messages. Added secret scanning to CI.

**Lesson**: Treat logs as public. Never log credentials.

### Incident 4: Permission Escalation Attempt

**What happened**: User tried to invoke `/q` without permissions.

**Root cause**: Role check was commented out during debugging.

**Fix**: Re-enabled role check. Added test to verify it.

**Lesson**: Security controls must be tested and visible.

## Security Architecture Reference

For deeper technical details, see:

- [Security Architecture](https://github.github.com/gh-aw/introduction/architecture/)
- [Safe Outputs Documentation](https://github.github.com/gh-aw/reference/safe-outputs/)

## Defense in Depth

The factory's security isn't a single mechanism - it's layered:

1. **Platform**: GitHub Actions isolation, runner sandboxing
2. **Permissions**: Least privilege via GITHUB_TOKEN
3. **Safe Outputs**: Constrained API with guardrails
4. **Role Gates**: Authorization checks
5. **Network**: Allowlisted domains
6. **Tools**: Explicitly listed, no wildcards
7. **Audit**: Complete activity logs
8. **Time Limits**: Auto-expiration for experiments
9. **Code Review**: Security review before merge
10. **Monitoring**: Meta-agents watch for anomalies

If one layer fails, others still provide protection.

## What's Next?

With security fundamentals in place, we can explore how agentic workflows actually work under the hood - from natural language markdown to secure execution on GitHub Actions.

In our next article, we'll walk through the technical architecture that powers the factory.

_More articles in this series coming soon._

[Previous Article](/gh-aw/blog/2026-01-30-imports-and-sharing/)
