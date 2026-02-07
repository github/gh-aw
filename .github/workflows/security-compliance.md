---
name: Security Compliance Campaign
description: Fix critical vulnerabilities before audit deadline with full tracking and reporting
timeout-minutes: 30
strict: true

on:
  workflow_dispatch:
    inputs:
      audit_date:
        description: 'Audit deadline (YYYY-MM-DD)'
        required: true
      severity_threshold:
        description: 'Minimum severity to fix (critical, high, medium)'
        required: false
        default: 'high'
      max_issues:
        description: 'Maximum vulnerabilities to process'
        required: false
        default: '500'

permissions:
  contents: read
  security-events: read

engine: copilot

safe-outputs:
  create-issue:
    expires: 2d
    max: 100  # 1 epic + vulnerability tasks
    labels: [security, campaign-tracker, cookie]
    group: true

tools:
  github:
    toolsets: [repos, search, code_security]
  repo-memory:
    branch-name: memory/campaigns
    file-glob: "memory/campaigns/security-compliance-*/**"

imports:
  - shared/mood.md
---

# Security Compliance Campaign

**Pain Point**: Enterprise faces audit deadline with hundreds of unresolved security vulnerabilities across multiple repositories. Need coordinated remediation with executive visibility, cost tracking, and compliance documentation.

**Campaign ID**: `security-compliance-${{ github.run_id }}`

**Business Context**:
- Audit deadline: ${{ github.event.inputs.audit_date }}
- Compliance requirement: SOC2, GDPR, or internal security policy
- Executive sponsor: CISO
- Budget: Approved by security and finance teams
- Risk: Audit failure = customer trust loss, regulatory fines

## Campaign Goals

1. **Identify** all critical/high vulnerabilities across organization repos
2. **Prioritize** by severity, exploitability, and business impact
3. **Remediate** vulnerabilities before audit deadline
4. **Document** fixes for compliance audit trail
5. **Report** progress to CISO and audit team weekly

## Success Criteria

- ✅ 100% of critical vulnerabilities fixed
- ✅ 95%+ of high vulnerabilities fixed
- ✅ All fixes documented with CVE references
- ✅ Audit trail in repo-memory for compliance
- ✅ Final report delivered to CISO 1 week before audit

## Campaign Execution

### 1. Scan & Baseline

**Discover vulnerabilities**:
- Query GitHub Security Advisories across all org repos
- Filter by severity: ${{ github.event.inputs.severity_threshold }}+
- Identify affected repositories and dependencies
- Calculate total count, breakdown by severity and repo

**Store baseline** in `memory/campaigns/security-compliance-${{ github.run_id }}/baseline.json`:
```json
{
  "campaign_id": "security-compliance-${{ github.run_id }}",
  "started": "[current date]",
  "audit_deadline": "${{ github.event.inputs.audit_date }}",
  "vulnerabilities_total": [count],
  "breakdown": {
    "critical": [count],
    "high": [count],
    "medium": [count]
  },
  "repos_affected": [count],
  "estimated_effort_hours": [estimate],
  "budget_approved": "$X",
  "executive_sponsor": "CISO"
}
```

### 2. Create Epic Tracking Issue

**Title**: "🚨 Security Compliance Campaign - Audit ${{ github.event.inputs.audit_date }}"

**Labels**: `campaign-tracker`, `security`, `compliance`, `campaign:security-compliance-${{ github.run_id }}`

**Body**:
```markdown
# Security Compliance Campaign

**Campaign ID**: `security-compliance-${{ github.run_id }}`
**Owner**: Security Team
**Executive Sponsor**: CISO
**Audit Deadline**: ${{ github.event.inputs.audit_date }}

## 🎯 Mission
Fix all critical/high vulnerabilities before audit to maintain compliance certification and customer trust.

## 📊 Baseline (Scan Results)
- **Critical**: [count] vulnerabilities
- **High**: [count] vulnerabilities  
- **Medium**: [count] vulnerabilities
- **Repositories Affected**: [count]
- **Estimated Effort**: [X] engineering hours

## ✅ Success Criteria
- [x] 100% critical vulnerabilities remediated
- [x] 95%+ high vulnerabilities remediated
- [x] All fixes documented with CVE references
- [x] Audit trail preserved in repo-memory
- [x] Final compliance report delivered

## 📈 Progress Tracking
Weekly updates posted here by campaign monitor.

**Query all campaign work**:
```bash
# All vulnerability tasks
gh issue list --label "campaign:security-compliance-${{ github.run_id }}"

# All fix PRs
gh pr list --label "campaign:security-compliance-${{ github.run_id }}"

# Campaign memory
gh repo view --json defaultBranchRef | \
  jq -r '.defaultBranchRef.target.tree.entries[] | select(.name=="memory")'
```

## 🚀 Workflow
1. **Launcher** (this workflow): Scan, create epic, generate vulnerability tasks
2. **Workers** (separate workflows): Create fix PRs for each vulnerability
3. **Monitor** (scheduled): Daily progress reports, escalate blockers
4. **Completion**: Final report to CISO with compliance documentation

## 💰 Budget & Cost Tracking
- **Approved Budget**: $X
- **AI Costs**: Tracked daily
- **Engineering Hours**: Tracked per fix
- **ROI**: Cost of campaign vs audit failure risk

## 📞 Escalation
- **Blockers**: Tag @security-leads
- **Budget overrun**: Notify @finance
- **Timeline risk**: Escalate to @ciso
```

### 3. Generate Vulnerability Task Issues

For each vulnerability (up to ${{ github.event.inputs.max_issues }}):

**Title**: "🔒 [CVE-XXXX] Fix [vulnerability name] in [repo]/[package]"

**Labels**: 
- `security`
- `campaign:security-compliance-${{ github.run_id }}`
- `severity:[critical|high|medium]`
- `repo:[repo-name]`
- `type:vulnerability`

**Body**:
```markdown
# Vulnerability: [Name]

**CVE**: CVE-XXXX-XXXXX
**Severity**: [Critical/High/Medium]
**CVSS Score**: X.X
**Repository**: [org]/[repo]
**Package**: [package-name]@[version]

## 🎯 Campaign Context
Part of Security Compliance Campaign for ${{ github.event.inputs.audit_date }} audit.
**Epic**: #[epic-issue-number]

## 🔍 Description
[Vulnerability description from advisory]

## 💥 Impact
[What this vulnerability allows attackers to do]

## ✅ Remediation
**Fix**: Update [package] from [old-version] to [new-version]+
**Breaking Changes**: [List any breaking changes]
**Testing Required**: [What to test after update]

## 📋 Fix Checklist
- [ ] Update dependency to fixed version
- [ ] Run tests to verify no regressions
- [ ] Create PR with fix
- [ ] Link PR to this issue
- [ ] Document fix in PR description
- [ ] Get security team approval

## 🤖 Automated Fix
A worker workflow will attempt to automatically create a fix PR.
If automatic fix fails, manual intervention required - tag @security-team.

## 📚 References
- Advisory: [link]
- CVE Details: https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-XXXX-XXXXX
- Fix PR: [will be linked by worker]

---
**Campaign**: security-compliance-${{ github.run_id }}
**Audit Deadline**: ${{ github.event.inputs.audit_date }}
```

### 4. Store Campaign Metadata

Create `memory/campaigns/security-compliance-${{ github.run_id }}/metadata.json`:
```json
{
  "campaign_id": "security-compliance-${{ github.run_id }}",
  "type": "security-compliance",
  "owner": "security-team",
  "executive_sponsor": "ciso@company.com",
  "audit_deadline": "${{ github.event.inputs.audit_date }}",
  "budget_approved": true,
  "epic_issue": [epic-issue-number],
  "created_at": "[timestamp]",
  "vulnerability_tasks": [
    [list of issue numbers]
  ],
  "governance": {
    "approval_status": "approved",
    "change_control_ticket": "CHG-XXXXX",
    "compliance_requirement": "SOC2",
    "review_checkpoints": ["weekly", "2-weeks-before-audit"]
  }
}
```

## Next Steps (Automated)

1. **Worker workflows** will trigger on vulnerability task creation
   - Each worker reads vulnerability issue
   - Creates fix PR with dependency update
   - Links PR back to issue
   - Updates fix status

2. **Monitor workflow** runs daily
   - Counts completed vs total
   - Calculates days remaining until audit
   - Identifies blockers (>3 days stalled)
   - Posts progress report to epic
   - Escalates if timeline at risk

3. **Completion workflow** triggers when all critical/high fixed
   - Generates final compliance report
   - Documents all CVEs fixed
   - Calculates ROI (cost vs audit failure risk)
   - Delivers to CISO

## Output

Campaign launched successfully:
- **Campaign ID**: `security-compliance-${{ github.run_id }}`
- **Epic Issue**: #[number]
- **Vulnerability Tasks**: [count] created
- **Baseline Stored**: `memory/campaigns/security-compliance-${{ github.run_id }}/baseline.json`
- **Workers**: Ready to process tasks
- **Monitor**: Will run daily at 9 AM
- **Audit Deadline**: ${{ github.event.inputs.audit_date }} ([X] days remaining)

**For CISO Dashboard**:
```bash
# Campaign overview
gh issue view [epic-issue-number]

# Real-time progress
gh issue list --label "campaign:security-compliance-${{ github.run_id }}" --json number,title,state,labels

# Daily metrics
cat memory/campaigns/security-compliance-${{ github.run_id }}/metrics/$(date +%Y-%m-%d).json
```
