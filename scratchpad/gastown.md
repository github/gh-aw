# Gastown: Multi-Agent Orchestration Concepts

**Date**: 2026-01-02  
**Gastown Repository**: https://github.com/steveyegge/gastown  
**Beads Repository**: https://github.com/steveyegge/beads  
**Status**: Conceptual Mapping

## Executive Summary

This document maps concepts from Steve Yegge's **Gastown** multi-agent orchestrator to **GitHub Agentic Workflows** (gh-aw). Gastown provides a multi-agent orchestration system with persistent state management, structured handoffs, and crash recovery mechanisms. While the two systems have different architectural approaches, understanding Gastown's patterns can inform gh-aw's evolution toward multi-agent coordination, persistent state management, and workflow composition.

### Key Insight

Gastown addresses a critical challenge in multi-agent AI development: **coordinating 20-30 concurrent agents with persistent work state that survives crashes and restarts**. While gh-aw focuses on GitHub Actions-based automation with single-agent workflows, Gastown's concepts provide a roadmap for future enhancements in multi-agent orchestration, state persistence, and workflow composition.

---

## 1. Architecture Overview

### Gastown Architecture

```
Town (~/gt/)              Workspace root
├── Mayor                 Global coordinator (cross-rig orchestration)
├── Deacon                Daemon process (lifecycle, plugin execution)
├── Rig (project)         Container for git project + agents
│   ├── Polecats          Ephemeral workers (spawn → work → disappear)
│   ├── Witness           Monitor (watches polecats, nudges stuck workers)
│   └── Refinery          Merge queue processor (PR review, integration)
└── Beads                 Git-backed issue tracker (persistent state)
```

**Key Principles**:
- **Propulsion Principle**: "If your hook has work, RUN IT" - agents autonomously execute work
- **Work Persistence**: All state stored in Beads (git-backed ledger)
- **Crash Recovery**: Any agent can continue where another left off
- **Structured Handoffs**: Agents have mailboxes and identities for coordination

### GitHub Agentic Workflows Architecture

```
Repository
├── .github/workflows/    Agentic workflow markdown files
│   ├── workflow.md       Natural language workflow definition
│   └── workflow.lock.yml Compiled GitHub Actions YAML
├── pkg/workflow/         Workflow compiler and runtime
│   ├── compiler.go       Markdown → YAML compilation
│   └── safe_outputs.go   Safe interaction with GitHub API
└── actions/setup/        Custom GitHub Actions for setup
```

**Key Principles**:
- **Single-Agent Workflows**: Each workflow runs one AI agent per execution
- **Event-Driven**: Triggered by GitHub events (push, PR, issues, schedule)
- **Stateless Execution**: Each run is independent (no persistent state between runs)
- **Safe Outputs**: Read-only by default, writes through sanitized safe-outputs

---

## 2. Core Concepts Mapping

### 2.1 Work Units

| Gastown Concept | gh-aw Equivalent | Notes |
|-----------------|------------------|-------|
| **Bead** (issue) | GitHub Issue | Both track work units, but beads use hash-based IDs (`bd-a1b2`) to prevent conflicts |
| **Convoy** (work group) | Campaign Workflow | Both group related work items; convoys track real-time progress across agents |
| **Molecule** (workflow instance) | Workflow Run | Molecules persist across crashes; workflow runs are ephemeral |
| **Formula** (workflow template) | Workflow Template | Formulas compose and extend; gh-aw imports shared markdown files |
| **Hook** (agent work queue) | Workflow Trigger | Hooks are persistent queues; triggers are event-based |

### 2.2 Agent Roles

| Gastown Role | gh-aw Pattern | Mapping |
|--------------|---------------|---------|
| **Polecat** (worker) | Workflow Job | Ephemeral workers that execute specific tasks |
| **Witness** (monitor) | Health Check Workflow | Monitors workflow health, detects stuck runs |
| **Refinery** (merge queue) | PR Review Workflow | Processes pull requests, runs checks |
| **Mayor** (coordinator) | Orchestrator Workflow | Coordinates across multiple workflows |
| **Deacon** (daemon) | GitHub Actions Runner | Manages lifecycle and execution |
| **Overseer** (human) | Repository Owner/Admin | Sets strategy, reviews output, handles escalations |

### 2.3 State Management

| Gastown Approach | gh-aw Approach | Gap |
|------------------|----------------|-----|
| **Beads** - Git-backed JSONL storage | GitHub Issues/Discussions | Beads provides persistent graph database; gh-aw relies on GitHub API |
| **Hooks** - Persistent work queues | Event triggers | Hooks persist across restarts; triggers are stateless |
| **Molecules** - Stateful workflow instances | Workflow runs (stateless) | Molecules survive crashes; runs are ephemeral |
| **Mailboxes** - Agent communication | Safe outputs + GitHub API | Mailboxes enable direct agent-to-agent messaging |
| **Protomolecules** - Reusable templates | Imports | Both enable reuse; protomolecules are versioned artifacts |

### 2.4 Workflow Composition

| Gastown Feature | gh-aw Feature | Comparison |
|-----------------|---------------|------------|
| **Formula Cooking** | Workflow Compilation | Both convert templates to executable format |
| **Molecule Pouring** | Workflow Dispatch | Both instantiate workflows with parameters |
| **Formula Extends** | Imports | Gastown supports inheritance; gh-aw concatenates imports |
| **Aspects** | Shared Instructions | Both enable cross-cutting concerns |
| **Steps with Dependencies** | Job Dependencies | Both support DAG-based execution |

---

## 3. Detailed Concept Analysis

### 3.1 The Propulsion Principle

**Gastown**: "If your hook has work, RUN IT"

Agents wake up, check their hook (persistent work queue), and autonomously execute any pending work. This enables:
- **Crash Recovery**: Work persists on hooks; new agents pick up where crashed agents left off
- **Autonomous Execution**: No waiting for commands; agents self-propel through work
- **Scalability**: 20-30 agents can run concurrently without manual coordination

**gh-aw Equivalent**: Event-driven triggers with stateless execution

```yaml
# gh-aw: Event-driven approach
on:
  issues:
    types: [opened]
  schedule:
    - cron: '0 6 * * *'
```

**Key Difference**: gh-aw workflows are triggered by external events (GitHub events), while Gastown agents autonomously execute work from persistent queues.

**Potential gh-aw Enhancement**: 
- Add persistent work queues using GitHub Issues labels or project boards
- Enable workflows to discover and claim available work items
- Implement autonomous polling for ready-to-work items

### 3.2 Molecules and Formulas (MEOW - Molecular Expression of Work)

**States of Matter in Gastown**:

| Phase | Name | Storage | Behavior | Description |
|-------|------|---------|----------|-------------|
| Ice-9 | Formula | `.beads/formulas/` | Source template, composable | Human-written workflow definition |
| Solid | Protomolecule | `.beads/` | Frozen template, reusable | Compiled, versioned template |
| Liquid | Molecule | `.beads/` | Flowing work, persistent | Live workflow instance with state |
| Vapor | Wisp | `.beads/` | Transient, ephemeral | Short-lived tasks (patrols, checks) |

**Operators**:
- `cook`: Formula → Protomolecule (expand macros, flatten)
- `pour`: Protomolecule → Molecule (instantiate as persistent)
- `wisp`: Protomolecule → Wisp (instantiate as ephemeral)
- `squash`: Molecule/Wisp → Digest (condense to permanent record)
- `burn`: Wisp → ∅ (discard without record)

**gh-aw Equivalent**:

```
Workflow .md → Compilation → .lock.yml → GitHub Actions → Workflow Run
```

**Key Differences**:
1. **State Persistence**: Molecules persist state between steps; gh-aw runs are stateless
2. **Crash Recovery**: Molecules can be resumed by any agent; gh-aw runs restart from scratch
3. **Template Versioning**: Protomolecules are versioned artifacts; gh-aw imports are file-based
4. **Ephemeral vs Persistent**: Wisps vs Molecules distinction; gh-aw has only one execution model

**Potential gh-aw Enhancement**:
- Add workflow state serialization for crash recovery
- Version imported templates with content-addressable storage
- Support "ephemeral" workflow mode for health checks and patrols

### 3.3 Beads: Git-Backed Issue Tracker

**Beads Features**:
- **Distributed**: Issues stored as JSONL in `.beads/` directory
- **Versioned**: Git tracks all changes to issues
- **Agent-Optimized**: JSON output, dependency tracking, auto-ready detection
- **Zero Conflict**: Hash-based IDs (`bd-a1b2`) prevent merge collisions
- **Compaction**: Semantic memory decay summarizes old closed tasks
- **Hierarchical**: Epic → Task → Subtask structure (`bd-a3f8.1.1`)

**Example Beads Commands**:
```bash
bd ready                    # List tasks with no open blockers
bd create "Title" -p 0      # Create P0 task
bd dep add <child> <parent> # Link tasks (blocks, related, parent-child)
bd show <id>                # View task details and audit trail
```

**gh-aw Equivalent**: GitHub Issues with GraphQL API

```yaml
tools:
  github:
    toolsets: [issues]
safe-outputs:
  create-issue:
  update-issue:
```

**Key Differences**:

| Aspect | Beads | GitHub Issues |
|--------|-------|---------------|
| **Storage** | Local `.beads/` JSONL files | GitHub cloud database |
| **IDs** | Hash-based `bd-a1b2` | Sequential `#123` |
| **Merge Conflicts** | Zero conflicts (hash-based) | Potential conflicts on forks |
| **Dependency Graph** | Native support (`bd dep`) | Manual labels/references |
| **Agent Access** | Direct file access | API with rate limits |
| **Compaction** | Semantic summarization | No built-in compaction |
| **Stealth Mode** | Local-only without commits | All changes visible |

**Potential gh-aw Enhancement**:
- Add dependency graph support for issues (blocked-by, blocks relationships)
- Implement issue compaction/archival for long-running projects
- Support local issue cache for reduced API calls
- Add "ready to work" detection based on dependency resolution

### 3.4 Convoys: Work Grouping and Tracking

**Gastown Convoys**:
```bash
gt convoy create "Feature X" issue-123 issue-456 --notify --human
gt sling issue-123 myproject    # Assign work to polecat
gt convoy list                  # Real-time progress dashboard
gt convoy status <id>           # Detailed status
```

**Purpose**:
- Group related work items (features, bug fixes, releases)
- Track progress across multiple agents in real-time
- Notify humans when stuck or completed
- Coordinate handoffs between agents

**gh-aw Equivalent**: Campaign Workflows

```markdown
# campaign-manager.md
---
on: workflow_dispatch
tools:
  github:
    toolsets: [issues, pull_requests]
---

Manage a campaign of related work items:
1. Identify issues with label "campaign:feature-x"
2. Track progress across all issues
3. Create daily status reports
4. Notify team when blocked
```

**Key Differences**:
- **Real-time vs Periodic**: Convoys provide real-time dashboards; campaigns run on schedules
- **Agent Assignment**: Convoys explicitly assign work to agents; campaigns trigger workflows
- **Human Notification**: Convoys built-in `--notify --human`; campaigns use safe-outputs
- **Persistence**: Convoys stored in Beads; campaigns track via GitHub Issues/Discussions

**Potential gh-aw Enhancement**:
- Add convoy-style work assignment with explicit agent designation
- Implement real-time progress tracking (polling with short intervals)
- Create dashboard workflows for campaign visualization
- Support explicit human notification/escalation paths

### 3.5 Agent Communication: Mailboxes and Handoffs

**Gastown Mailboxes**:
```bash
gt mail inbox                     # Check messages
gt mail send <addr> -s "..." -m "..."
gt handoff                        # Request session cycle
gt peek <agent>                   # Check agent health
```

**Communication Patterns**:
- **Direct Messaging**: Agent-to-agent communication via mailboxes
- **Handoff Requests**: Polecats signal completion and request shutdown
- **Health Checks**: Witness peeking at agent status
- **Escalation**: Blocked agents send help requests to overseer

**gh-aw Equivalent**: Safe outputs with GitHub API

```yaml
safe-outputs:
  create-issue:        # Create new work item
  create-discussion:   # Post updates/reports
  update-issue:        # Signal progress
  create-comment:      # Respond to issues/PRs
```

**Key Differences**:

| Feature | Gastown | gh-aw |
|---------|---------|-------|
| **Direct Messaging** | Agent-to-agent mailboxes | Via GitHub API (issues, discussions) |
| **Synchronous** | Yes (agents can wait for responses) | No (async via GitHub events) |
| **Structured Handoffs** | Built-in handoff protocol | Manual via issue labels/assignments |
| **Health Monitoring** | `gt peek <agent>` | Workflow run status API |
| **Escalation** | Decision/help/blocked/emergency | Safe outputs + team mentions |

**Potential gh-aw Enhancement**:
- Implement structured handoff protocol using issue labels and assignments
- Add workflow-to-workflow communication via dispatch events
- Create health monitoring workflows that check status of other workflows
- Support escalation patterns with team mentions and priority labels

---

## 4. Multi-Agent Patterns

### 4.1 Gastown Multi-Agent Workflow

**Typical Flow**:
1. **Human** creates convoy with grouped issues
2. **Mayor** dispatches work across rigs
3. **Polecats** spawn for each assigned issue
4. **Witness** monitors polecat progress, nudges stuck workers
5. **Polecats** complete work, file discovered issues, request handoff
6. **Refinery** processes merge queue, reviews PRs
7. **Convoy** tracks overall progress, notifies human when complete

**Example**:
```bash
# Human: Create work campaign
gt convoy create "Auth Feature" auth-1 auth-2 auth-3 --notify --human

# Mayor: Dispatch work
gt sling auth-1 backend-rig    # Spawns polecat-1
gt sling auth-2 frontend-rig   # Spawns polecat-2
gt sling auth-3 backend-rig    # Spawns polecat-3

# Witness: Monitor (automatic)
# - Detects polecat-3 stuck after 30 minutes
# - Sends nudge message
# - Escalates to mayor if no progress

# Polecats: Execute work
# - polecat-1 completes, creates PR, files bd-xyz for missing tests
# - polecat-2 blocked on API spec, sends help request
# - polecat-3 unstuck, continues work

# Refinery: Process PRs (automatic)
# - Reviews polecat-1's PR
# - Runs integration tests
# - Merges if passing

# Convoy: Report progress
# - 2/3 complete, 1 blocked
# - Notifies human about polecat-2 blocker
```

### 4.2 gh-aw Multi-Workflow Patterns

**Current Approach**: Separate workflows triggered by events

```yaml
# workflow-1-creator.md
on:
  issues:
    types: [opened]
safe-outputs:
  create-pull-request:

# workflow-2-reviewer.md  
on:
  pull_request:
    types: [opened]
safe-outputs:
  create-review:

# workflow-3-monitor.md
on:
  schedule:
    - cron: '0 * * * *'
safe-outputs:
  create-discussion:
```

**Coordination**: Event-driven cascade via GitHub API
- Workflow 1 creates PR → triggers Workflow 2
- Workflow 2 reviews PR → triggers Workflow 3 on merge
- Workflow 3 monitors all → creates reports

**Limitations**:
- No direct workflow-to-workflow communication
- No shared state between workflows
- No centralized orchestration
- Difficult to track multi-workflow campaigns
- No built-in escalation paths

### 4.3 Gastown-Inspired Multi-Agent Architecture for gh-aw

**Proposed Enhancement**: Campaign Orchestrator Pattern

```markdown
# orchestrator-workflow.md
---
on: workflow_dispatch
inputs:
  campaign_id:
    description: 'Campaign identifier'
    required: true
tools:
  github:
    toolsets: [issues, pull_requests, workflows]
safe-outputs:
  create-issue:      # Create worker issues
  update-issue:      # Track progress
  dispatch-workflow: # Trigger worker workflows
---

## Campaign Orchestrator

Coordinates multiple worker workflows for a campaign:

1. **Discover Work**: Find issues with label `campaign:{{ inputs.campaign_id }}`
2. **Assign Work**: Create worker issues for each task
3. **Dispatch Workers**: Trigger worker workflows with task assignments
4. **Monitor Progress**: Poll worker issue status every 5 minutes
5. **Detect Blockers**: Check for stuck workers (no progress in 30+ minutes)
6. **Escalate**: Notify team when workers blocked or need help
7. **Report Completion**: Create summary discussion when campaign done
```

```markdown
# worker-workflow.md
---
on:
  issues:
    types: [opened, labeled]
tools:
  github:
    toolsets: [issues, pull_requests]
safe-outputs:
  create-pull-request:
  update-issue:        # Signal progress
  create-issue:        # File discovered work
---

## Campaign Worker

Executes assigned task from orchestrator:

1. **Read Assignment**: Parse issue body for task details
2. **Execute Task**: Complete the assigned work
3. **Signal Progress**: Update issue with progress every 10 minutes
4. **Handle Blockers**: Update issue with `blocked` label if stuck
5. **Complete Work**: Close issue, create PR, file follow-up issues
```

**Benefits**:
- Centralized orchestration (like Mayor)
- Progress monitoring (like Witness)
- Explicit work assignment (like slinging)
- Escalation paths (like help requests)
- Persistent state (via GitHub Issues)

---

## 5. Security and Safety Comparison

### 5.1 Gastown Security Model

**Permissions**: User-managed, runs with local user permissions

**Safety Features**:
- **Stealth Mode**: Run locally without committing to main repo
- **Human Oversight**: Designed for human review before merge
- **Git-backed**: All changes versioned, rollback available
- **Sandboxing**: None - runs with full user permissions

**Risk Profile**: High trust in AI agents, relies on human review

### 5.2 gh-aw Security Model

**Permissions**: Explicit, read-only by default

```yaml
permissions:
  contents: read      # Default: read-only
  issues: write       # Explicit write permission
```

**Safety Features**:
- **Strict Mode**: Enforces security constraints at compile time
- **Safe Outputs**: All write operations sanitized and validated
- **Network Isolation**: Explicit allowlist for network access
- **SHA-pinned Dependencies**: Supply chain security
- **Sandboxed Execution**: Runs in isolated GitHub Actions containers
- **Input Sanitization**: Template injection prevention

**Risk Profile**: Low trust, defense-in-depth, strict boundaries

### 5.3 Key Differences

| Aspect | Gastown | gh-aw |
|--------|---------|-------|
| **Default Permissions** | Full user permissions | Read-only |
| **Write Operations** | Direct file/API access | Sanitized safe-outputs |
| **Network Access** | Unrestricted | Explicit allowlist |
| **Execution Environment** | Local user environment | Isolated container |
| **Human Approval** | Post-facto review | Optional pre-merge gates |
| **Audit Trail** | Git commit history | Workflow run logs + GitHub audit |

---

## 6. Workflow Composition and Reuse

### 6.1 Gastown Formula System

**Formula Example**:
```toml
# .beads/formulas/shiny.formula.toml
formula = "shiny"
description = "Design before code, review before ship"

[[steps]]
id = "design"
description = "Think about architecture"

[[steps]]
id = "implement"
needs = ["design"]

[[steps]]
id = "test"
needs = ["implement"]

[[steps]]
id = "submit"
needs = ["test"]
```

**Formula Composition**:
```toml
# Extend an existing formula
formula = "shiny-enterprise"
extends = ["shiny"]

[compose]
aspects = ["security-audit"]  # Add cross-cutting concerns
```

**Usage**:
```bash
bd formula list                     # See available formulas
bd cook shiny                       # Cook into protomolecule
bd mol pour shiny --var feature=auth  # Create runnable molecule
gt convoy create "Auth" gt-xyz      # Track with convoy
gt sling gt-xyz myproject           # Assign to worker
```

### 6.2 gh-aw Import System

**Import Example**:
```yaml
imports:
  - shared/jqschema.md
  - shared/python-dataviz.md
  - shared/reporting.md
```

**Shared Instructions**:
```markdown
# shared/reporting.md

## Reporting Guidelines

Use HTML details/summary for collapsible sections:

<details>
<summary><b>📊 Full Report Details</b></summary>

[Report content here]

</details>
```

**Limitations**:
- No composition/extension
- No parameterization
- No versioning
- Simple concatenation

### 6.3 Comparison

| Feature | Gastown Formulas | gh-aw Imports |
|---------|------------------|---------------|
| **Composition** | Extends, aspects | Concatenation only |
| **Parameterization** | `--var` flags | GitHub Actions expressions |
| **Versioning** | Protomolecule hashes | File content |
| **Dependency DAG** | Step-level `needs` | Job-level `needs` |
| **Reuse** | Compiled templates | Raw markdown files |
| **Distribution** | Central formula registry | Repository files |

**Potential gh-aw Enhancement**:
- Add formula-style composition with `extends`
- Support aspect-oriented programming for cross-cutting concerns
- Version imported templates with content hashing
- Create template registry for community sharing
- Enable parameterized template instantiation

---

## 7. Crash Recovery and State Persistence

### 7.1 Gastown Crash Recovery

**Mechanism**: Molecules persist in Beads

**Process**:
1. Polecat-1 starts executing molecule (steps: design → implement → test → submit)
2. Completes design and implement steps
3. Crashes during test step
4. Polecat-2 spawned, reads same molecule
5. Sees design=complete, implement=complete, test=in-progress
6. Continues from test step

**Benefits**:
- Zero work loss on agent crash
- Any agent can continue work
- Scales to many agents per task
- Supports long-running workflows (days/weeks)

### 7.2 gh-aw Execution Model

**Mechanism**: Stateless workflow runs

**Process**:
1. Workflow triggered by event
2. Runs all steps from beginning
3. If failure, entire run marked failed
4. Manual re-trigger runs from beginning
5. No state persistence between runs

**Limitations**:
- Failure = restart from scratch
- Long workflows inefficient on retry
- No checkpoint/resume capability
- Limited to GitHub Actions timeout (max 6 hours)

### 7.3 Potential Enhancements

**Checkpoint/Resume Pattern**:
```yaml
# checkpoint-workflow.md
---
on: workflow_dispatch
inputs:
  checkpoint_id:
    description: 'Resume from checkpoint'
    required: false
---

## Task with Checkpoints

1. **Check for existing checkpoint**: Load state from issue or artifact
2. **Execute next phase**: Run only incomplete steps
3. **Save checkpoint**: Update issue with completed steps
4. **On failure**: Next run resumes from last checkpoint
```

**State Storage Options**:
- GitHub Issues (structured state in issue body)
- Workflow Artifacts (files with serialized state)
- GitHub Discussions (long-running task threads)
- Repository files (`.gh-aw/state/` directory)

---

## 8. Agent Discovery and Work Assignment

### 8.1 Gastown Work Discovery

**Ready-to-Work Detection**:
```bash
bd ready  # Lists tasks with no open blockers
```

**Criteria**:
- All parent tasks complete
- All blocking dependencies resolved
- Not already assigned to another agent
- Priority ordering (P0, P1, P2...)

**Slinging (Assignment)**:
```bash
gt sling issue-123 myproject  # Explicit assignment to rig
```

**Automatic Discovery**: Agents can autonomously discover and claim work

### 8.2 gh-aw Work Discovery

**Current Approach**: Event-driven triggers

```yaml
on:
  issues:
    types: [labeled]  # Trigger when labeled
```

**Manual Assignment**: Use issue labels or assignments

```markdown
Find issues with label "ready-for-ai":
1. Query issues: `gh issue list --label "ready-for-ai"`
2. Claim work: Add "in-progress" label
3. Execute task
4. Complete: Remove "ready-for-ai", add "done"
```

### 8.3 Potential Enhancement: Work Queue Pattern

```markdown
# work-queue-agent.md
---
on:
  schedule:
    - cron: '*/5 * * * *'  # Every 5 minutes
permissions:
  issues: write
  contents: write
---

## Work Queue Agent

Autonomously discovers and claims available work:

1. **Query Ready Work**: Find issues with:
   - Label: `ready-for-ai`
   - No label: `in-progress`
   - All blocked-by issues closed
   
2. **Claim Work**: 
   - Add label: `in-progress`
   - Add comment: "Claimed by workflow run {{ github.run_id }}"
   
3. **Execute Task**: Parse issue body for instructions

4. **Complete or Block**:
   - Success: Remove `ready-for-ai`, add `completed`
   - Blocked: Add `blocked`, create comment explaining blocker
   
5. **Discover Next**: If time remaining, claim next ready issue
```

**Benefits**:
- Autonomous work discovery (like `bd ready`)
- Work queue persistence (via GitHub Issues)
- Progress tracking (via labels)
- Scales to multiple concurrent agents

---

## 9. Communication Patterns

### 9.1 Gastown Communication Patterns

**Agent-to-Agent**:
```bash
gt mail send polecat-1 -s "Design Complete" -m "API spec at /docs/api.md"
```

**Agent-to-Human**:
```bash
# Polecat requests help
gt mail send overseer -s "BLOCKED" -m "Need clarification on auth requirements"

# Convoy notifies human
gt convoy create "Feature X" ... --notify --human
```

**Agent-to-System**:
```bash
# Polecat signals completion
gt handoff  # Request session cycle

# Witness checks health
gt peek polecat-1  # Get status
```

### 9.2 gh-aw Communication Patterns

**Workflow-to-GitHub**:
```yaml
safe-outputs:
  create-issue:        # File new work
  create-discussion:   # Post reports
  create-comment:      # Respond to issues/PRs
```

**Workflow-to-Workflow** (indirect via events):
```yaml
# Workflow 1: Creates issue
safe-outputs:
  create-issue:
    labels: ["needs-review"]

# Workflow 2: Triggered by label
on:
  issues:
    types: [labeled]
```

**Workflow-to-Human**:
```yaml
safe-outputs:
  create-discussion:
    body: |
      @team: This workflow is blocked and needs help.
      
      Issue: {{ github.event.issue.number }}
      Reason: API spec unclear
```

### 9.3 Comparison

| Pattern | Gastown | gh-aw |
|---------|---------|-------|
| **Synchronous** | Mailboxes (wait for reply) | No (event-driven) |
| **Agent-to-Agent** | Direct messaging | Via GitHub API (async) |
| **Structured Handoffs** | Built-in protocol | Manual via labels |
| **Human Escalation** | `--notify --human` | Mentions in safe-outputs |
| **Health Checks** | `gt peek` command | Workflow run API |
| **Work Claiming** | `gt sling` command | Issue labels/assignments |

---

## 10. Implementation Recommendations for gh-aw

Based on Gastown's proven patterns, here are recommended enhancements for gh-aw:

### 10.1 Short-Term Enhancements (Low Complexity)

1. **Work Queue Pattern**
   - Add workflow that polls for "ready-for-ai" issues
   - Implement claim/release protocol with labels
   - Support priority ordering (P0, P1, P2 labels)

2. **Progress Tracking**
   - Standardize progress labels (ready, claimed, in-progress, blocked, done)
   - Create progress dashboard workflow
   - Add "last heartbeat" tracking via issue comments

3. **Escalation Protocol**
   - Define standard labels: `blocked`, `needs-help`, `emergency`
   - Create escalation workflow triggered by these labels
   - Support team mentions for notifications

4. **Campaign Tracking**
   - Add campaign label pattern: `campaign:feature-name`
   - Create campaign dashboard workflow
   - Track progress across related issues

### 10.2 Medium-Term Enhancements (Moderate Complexity)

1. **Checkpoint/Resume Support**
   - Store workflow state in issues or artifacts
   - Enable resume from last checkpoint on retry
   - Support long-running workflows (multi-day)

2. **Workflow Composition**
   - Add `extends` keyword for template inheritance
   - Support parameterized templates
   - Version imported templates with content hashing

3. **Dependency Graph**
   - Add `blocked-by` and `blocks` relationships for issues
   - Implement `bd ready` equivalent: query for unblocked issues
   - Visualize dependency graph

4. **Agent Health Monitoring**
   - Create health check workflow
   - Monitor workflow run status
   - Detect stuck workflows (timeout exceeded, no progress)
   - Auto-retry or escalate

### 10.3 Long-Term Enhancements (High Complexity)

1. **Multi-Agent Orchestration**
   - Implement Mayor-like orchestrator workflow
   - Support explicit work assignment to specific workflows
   - Coordinate handoffs between workflows
   - Track real-time progress across multiple agents

2. **Persistent State Layer**
   - Add optional state persistence (beyond GitHub Issues)
   - Support workflow state serialization
   - Enable crash recovery with state restore
   - Consider integration with beads-like local storage

3. **Structured Communication**
   - Implement mailbox pattern using GitHub API
   - Support workflow-to-workflow messaging
   - Add synchronous communication (workflow waits for response)
   - Create message queue abstraction

4. **Formula System**
   - Create workflow template registry
   - Support formula composition and aspects
   - Enable community template sharing
   - Version and distribute compiled templates

---

## 11. Key Takeaways

### What gh-aw Does Well
- **Security First**: Strict mode, safe-outputs, sandboxed execution
- **GitHub Integration**: Native GitHub Actions, API access, authentication
- **Event-Driven**: Comprehensive trigger system for automation
- **Declarative**: Natural language markdown workflows
- **Supply Chain Security**: SHA-pinned dependencies

### What gh-aw Can Learn from Gastown
- **Multi-Agent Coordination**: Orchestration patterns for 20-30 concurrent agents
- **State Persistence**: Work survives crashes and restarts
- **Autonomous Discovery**: Agents find and claim work without external triggers
- **Structured Handoffs**: Explicit protocols for agent-to-agent coordination
- **Progress Monitoring**: Real-time dashboards and health checks
- **Workflow Composition**: Formula system with extends and aspects

### Complementary Strengths

**Use gh-aw for**:
- GitHub-native automation (issues, PRs, releases)
- Scheduled/event-driven workflows
- Security-sensitive operations
- CI/CD integration
- Team collaboration workflows

**Use Gastown for**:
- Multi-agent development teams
- Long-running feature development
- Crash-resilient workflows
- Local development with stealth mode
- Complex workflow composition

### Hybrid Approach

**Best of Both Worlds**:
- Use gh-aw for GitHub-facing automation and CI/CD
- Use Gastown (with beads) for local multi-agent development
- Bridge with gh-aw workflows that sync with beads state
- Share formula patterns between systems
- Leverage GitHub as communication layer for gastown agents

---

## 12. Conclusion

Gastown provides a mature, battle-tested architecture for multi-agent coordination with persistent state and crash recovery. While gh-aw and Gastown target different use cases (GitHub Actions vs local multi-agent orchestration), understanding Gastown's patterns reveals opportunities for gh-aw evolution:

1. **State Persistence**: Enable workflows to survive crashes and resume from checkpoints
2. **Multi-Agent Orchestration**: Coordinate multiple concurrent workflows with structured handoffs
3. **Autonomous Discovery**: Let workflows discover and claim available work items
4. **Workflow Composition**: Support template inheritance and aspect-oriented programming
5. **Progress Monitoring**: Real-time dashboards and health checks for multi-workflow campaigns

These enhancements would position gh-aw as a full-featured agentic workflow platform capable of both basic automation and complex multi-agent coordination, while maintaining its security-first approach and GitHub-native integration.

---

**References**:
- Gastown: https://github.com/steveyegge/gastown
- Beads: https://github.com/steveyegge/beads
- gh-aw Documentation: https://github.github.com/gh-aw/

**Last Updated**: 2026-01-02
