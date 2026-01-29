# Hierarchical Agent Management

This document describes the hierarchical agent management system in GitHub Agentic Workflows, which provides meta-orchestration capabilities to manage the 120+ agents in the repository.

## Overview

The hierarchical agent system consists of specialized meta-orchestrator workflows that oversee, coordinate, and optimize the agent ecosystem. These meta-orchestrator agents operate at a higher level than regular workflows, monitoring and managing multiple agents to ensure overall system health and effectiveness.

## Meta-Orchestrator Agents

### 1. Campaign Manager

**File:** `.github/workflows/campaign-manager.md`

**Purpose:** Strategic management of all active campaigns

**Responsibilities:**
- Discover and analyze all campaign specifications
- Monitor campaign health and performance
- Coordinate between campaigns to avoid conflicts
- Aggregate metrics across campaigns
- Make strategic decisions about campaign priorities
- Generate executive reports on campaign portfolio

**Schedule:** Daily

**Key Capabilities:**
- Cross-campaign coordination
- Resource optimization
- Performance trend analysis
- Strategic priority management
- Conflict detection and resolution

**Safe Outputs:**
- `create-issue`: Flag campaigns needing attention
- `add-comment`: Add coordination notes and recommendations
- `create-discussion`: Generate strategic reports
- `update-project`: Adjust campaign priorities

### 2. Workflow Health Manager

**File:** `.github/workflows/workflow-health-manager.md`

**Purpose:** Monitor and maintain health of all agentic workflows

**Responsibilities:**
- Track compilation status of all workflows
- Monitor workflow execution success/failure rates
- Analyze error patterns across workflows
- Map workflow dependencies and interactions
- Identify resource utilization issues
- Create maintenance issues proactively

**Schedule:** Daily

**Key Capabilities:**
- System-wide health monitoring (compilation status, execution rates, error patterns)
- Systemic issue detection
- Dependency mapping
- Performance optimization
- Proactive maintenance

**Safe Outputs:**
- `create-issue`: Report workflow problems (max: 10)
- `add-comment`: Update status and provide recommendations (max: 15)
- `update-issue`: Close resolved issues (max: 5)

### 3. Agent Performance Analyzer

**File:** `.github/workflows/agent-performance-analyzer.md`

**Purpose:** Analyze quality and effectiveness of AI agents

**Responsibilities:**
- Evaluate output quality of agent-created issues/PRs/comments
- Measure agent effectiveness and task completion rates
- Identify behavioral patterns and problematic behaviors
- Assess coverage and gaps in the agent ecosystem
- Generate quality improvement recommendations
- Track agent performance trends

**Schedule:** Daily

**Key Capabilities:**
- Quality assessment across all agent outputs
- Effectiveness measurement
- Behavioral pattern detection
- Ecosystem health analysis
- Actionable improvement recommendations

**Safe Outputs:**
- `create-issue`: Agent improvement recommendations (max: 5)
- `create-discussion`: Performance reports (max: 2)
- `add-comment`: Provide feedback and guidance (max: 10)

## Architecture

### Hierarchical Structure

```text
┌─────────────────────────────────────────────┐
│      Meta-Orchestrators (Managerial)        │
│  ┌─────────────┐  ┌─────────────┐  ┌──────┐│
│  │  Campaign   │  │  Workflow   │  │Agent ││
│  │  Manager    │  │  Health     │  │Perf. ││
│  │             │  │  Manager    │  │Anal. ││
│  └──────┬──────┘  └──────┬──────┘  └───┬──┘│
└─────────┼─────────────────┼─────────────┼───┘
          │                 │             │
          ▼                 ▼             ▼
┌─────────────────────────────────────────────┐
│        Campaign Orchestrators               │
│  ┌─────────────┐         ┌─────────────┐   │
│  │  Campaign   │         │  Campaign   │   │
│  │  Alpha.g.md │         │  Beta.g.md  │   │
│  └──────┬──────┘         └──────┬──────┘   │
└─────────┼────────────────────────┼──────────┘
          │                        │
          ▼                        ▼
┌─────────────────────────────────────────────┐
│          Worker Workflows                   │
│  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐   │
│  │Worker│  │Worker│  │Worker│  │Worker│   │
│  │  1   │  │  2   │  │  3   │  │  4   │   │
│  └──────┘  └──────┘  └──────┘  └──────┘   │
└─────────────────────────────────────────────┘
```text

### Key Principles

1. **Separation of Concerns:** Each meta-orchestrator has a distinct focus area
2. **Non-Intrusive:** Meta-orchestrators observe and recommend, they don't directly control workers
3. **Evidence-Based:** All decisions based on concrete metrics and data
4. **Actionable:** Outputs are specific recommendations with clear next steps
5. **Coordinated:** Meta-orchestrators complement each other without overlap
6. **Shared Memory:** Meta-orchestrators use common repo memory to share insights and coordinate actions

### Shared Memory System

Meta-orchestrators use a shared repository memory branch (`memory/meta-orchestrators`) to persist data across runs and coordinate with each other.

**Memory Location:** `/tmp/gh-aw/repo-memory-default/memory/meta-orchestrators/`

**Shared Files:**
- `campaign-manager-latest.md` - Campaign portfolio health and decisions
- `workflow-health-latest.md` - Workflow compilation and execution status
- `agent-performance-latest.md` - Agent quality scores and patterns
- `shared-alerts.md` - Cross-orchestrator coordination notes and alerts

**Benefits:**
- **Avoid Duplicate Work:** Each orchestrator can see what others have already flagged
- **Coordinate Actions:** Orchestrators can build on each other's insights
- **Track Trends:** Historical data enables trend analysis across runs
- **Share Context:** Common understanding of ecosystem state

**Example Use Cases:**
- Campaign Manager sees that Workflow Health Manager flagged a failing workflow used by Campaign X
- Workflow Health Manager identifies that Agent Performance Analyzer found quality issues in a specific workflow
- Agent Performance Analyzer notes that Campaign Manager deprioritized a campaign, reducing urgency of related improvements

## How Meta-Orchestrators Work

### Discovery

All meta-orchestrators start by discovering the current state:
- Scan repository for relevant files (workflows, campaigns, etc.)
- Parse metadata and configuration
- Build an inventory of what they manage

### Analysis

Meta-orchestrators analyze the discovered data:
- Calculate health scores and metrics
- Identify patterns and trends
- Detect issues and opportunities
- Compare against baselines and expectations

### Decision Making

Based on analysis, meta-orchestrators make strategic decisions:
- Prioritize issues (P0, P1, P2, P3)
- Generate specific recommendations
- Identify action items
- Determine what needs escalation

### Execution

Meta-orchestrators take action through safe outputs:
- Create issues for problems that need fixing
- Generate reports and discussions for visibility
- Add comments to provide context and guidance
- Update project boards to adjust priorities

### Reporting

Meta-orchestrators provide detailed reports including:
- Executive summaries for high-level overview
- Detailed findings for investigation
- Actionable recommendations for improvement
- Trends and metrics for tracking progress

## Using Meta-Orchestrators

### For Repository Maintainers

**Monitor the health dashboards:**
- Check the issues created by meta-orchestrators
- Review weekly/daily reports in discussions
- Act on P0/P1 issues promptly
- Track improvement trends over time

**Use the insights:**
- Prioritize work based on meta-orchestrator recommendations
- Adjust campaign strategies based on portfolio analysis
- Fix systemic issues identified across multiple workflows
- Optimize resource allocation based on utilization data

**Provide feedback:**
- Close resolved issues to keep tracking clean
- Comment on recommendations if you disagree
- Adjust meta-orchestrator schedules if needed
- Refine prompts if behavior needs tuning

### For Workflow Authors

**Learn from performance analysis:**
- Review quality scores for your workflows
- Implement recommendations for improvement
- Study high-performing workflows for patterns
- Fix issues identified in health checks

**Coordinate through meta-orchestrators:**
- Check for conflicts with other workflows
- Use campaign coordination notes
- Follow strategic priorities
- Contribute to ecosystem optimization

### For Campaign Owners

**Use campaign management insights:**
- Track your campaign's health score
- Address flagged issues promptly
- Coordinate with related campaigns
- Adjust priorities based on portfolio analysis

**Leverage cross-campaign coordination:**
- Avoid conflicts by checking coordination notes
- Share resources with related campaigns
- Sequence work appropriately
- Contribute to overall success

## Best Practices

### For Meta-Orchestrators

1. **Be Data-Driven:** Base all assessments on measurable metrics
2. **Be Specific:** Provide actionable recommendations with examples
3. **Be Balanced:** Recognize successes as well as identifying problems
4. **Be Timely:** Report issues before they become critical
5. **Be Collaborative:** Suggest, don't dictate - respect ownership

### For the Ecosystem

1. **Review Reports Regularly:** Don't let meta-orchestrator insights go unread
2. **Act on Recommendations:** Implement improvements promptly
3. **Maintain Feedback Loops:** Update meta-orchestrators when fixing issues
4. **Keep Clean Hygiene:** Close resolved issues, update statuses
5. **Evolve the System:** Refine meta-orchestrator prompts as needs change

## Metrics and Success

### Campaign Manager Success Metrics
- Campaign completion rates improving
- Fewer campaign conflicts and resource contention
- Better strategic alignment across portfolio
- Higher ROI on campaign investments
- Reduced time to identify and resolve issues

### Workflow Health Manager Success Metrics
- Overall workflow success rate increasing
- Faster detection and resolution of failures
- Reduced cascading failures
- Better resource utilization
- Fewer manual interventions needed

### Agent Performance Analyzer Success Metrics
- Agent quality scores improving over time
- Higher PR merge rates for agent outputs
- Fewer duplicate or low-quality outputs
- Better coverage across repository areas
- More effective agent collaboration

## Future Enhancements

Potential improvements to the hierarchical system:

1. **Automated Remediation:** Meta-orchestrators could automatically fix issues such as outdated dependencies, formatting violations, and broken links
2. **Predictive Analytics:** Use ML to predict failures before they occur
3. **Self-Optimization:** Meta-orchestrators could tune their own parameters
4. **Cross-Repository Management:** Extend to manage agents across multiple repos
5. **Real-Time Monitoring:** Move from scheduled to event-driven monitoring
6. **Agent Learning:** Share learnings across agents automatically

## Troubleshooting

### Meta-Orchestrator Not Running

**Check:**
- Workflow compiled successfully (`.lock.yml` exists)
- Schedule trigger is configured correctly
- No workflow disable labels
- Permissions are correct

**Fix:**
- Recompile with `gh aw compile <workflow>.md`
- Check workflow runs in GitHub Actions UI
- Review error logs if failing

### Reports Not Being Generated

**Check:**
- Safe outputs configured correctly
- GitHub MCP server accessible
- No rate limiting issues
- Permissions sufficient for creating discussions/issues

**Fix:**
- Verify safe output configuration
- Check GitHub API rate limits
- Review workflow run logs for errors

### Recommendations Not Actionable

**Check:**
- Meta-orchestrator prompt clarity
- Sufficient context in recommendations
- Appropriate priority assignment
- Clear next steps provided

**Fix:**
- Refine meta-orchestrator prompt
- Add more specific examples
- Include implementation guidance
- Link to relevant documentation

## Related Documentation

- [Campaign System](../docs/campaigns.md)
- [Safe Outputs](../docs/safe-outputs.md)
- [Workflow Development](../DEVGUIDE.md)
- [Agent Design Patterns](../docs/agent-patterns.md)

## Conclusion

The hierarchical agent management system provides essential oversight and coordination for large-scale agentic workflow deployments. By using specialized meta-orchestrators to manage campaigns, workflow health, and agent performance, you can ensure a healthy, effective, and continuously improving agent ecosystem.
