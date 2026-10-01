const G = '/gh-aw/gallery/';

export type Frag = {
  kind: 'issue' | 'pr' | 'run' | 'alert' | 'comment';
  title: string;
  num?: string;
  meta?: string;
  chips?: { t: string; tone?: 'new' | 'danger' }[];
  add?: string;
  del?: string;
  note?: string;
};

export type Workflow = {
  id: string;
  title: string;
  href: string;
  text: string;
  trigger: string;
  frag: Frag;
};

export const workflows: Workflow[] = [
  { id: 'triage', title: 'Issue Triage', href: `${G}ai-issue-triage/`, trigger: 'issues: opened',
    text: 'Classifies new issues, spots duplicates, applies labels and asks for missing details.',
    frag: { kind: 'issue', title: 'Crash when saving an empty file', chips: [{ t: 'bug' }, { t: 'area: editor' }, { t: 'needs repro', tone: 'new' }] } },
  { id: 'review', title: 'Pull Request Review', href: `${G}automated-pr-review/`, trigger: 'pull_request',
    text: 'Inspects diffs for concrete defects and posts review feedback through safe outputs.',
    frag: { kind: 'comment', title: 'github-actions', note: 'Errors from save are dropped in this loop.', meta: 'on src/save.ts:42' } },
  { id: 'ci', title: 'CI Failure Investigation', href: `${G}ci-failure-investigation/`, trigger: 'workflow_run: failed',
    text: 'Reads failed runs, correlates logs and opens an issue with the likely cause.',
    frag: { kind: 'run', title: 'build / test (ubuntu)', meta: 'failed', note: 'Diagnosis: flaky timeout in db setup' } },
  { id: 'docs', title: 'Documentation Maintenance', href: `${G}docs-automation/`, trigger: 'schedule: weekly',
    text: 'Detects drift between code and docs and proposes updates you can review.',
    frag: { kind: 'pr', title: 'docs: sync CLI flags with --help', num: '#214', add: '+18', del: '−6', chips: [{ t: 'documentation' }] } },
  { id: 'code-improvement', title: 'Code Improvement', href: `${G}code-improvement/`, trigger: 'schedule: weekly',
    text: 'Finds duplicated logic and proposes focused changes.',
    frag: { kind: 'pr', title: 'refactor: extract shared retry helper', num: '#231', add: '+24', del: '−61', chips: [{ t: 'refactor' }] } },
  { id: 'repo-maintenance', title: 'Repository Maintenance', href: 'https://github.com/githubnext/agentics/blob/main/docs/repo-assist.md', trigger: 'schedule: daily',
    text: 'Works through a backlog with bounded tasks on a schedule.',
    frag: { kind: 'comment', title: 'repo-assist', note: 'Closed 3 stale issues and opened a fix for the flaky test.', meta: 'daily run' } },
  { id: 'dependency', title: 'Dependency Analysis', href: '/gh-aw/patterns/research-plan-assign-ops/', trigger: 'schedule: weekly',
    text: 'Researches upstream changes, then files prioritised follow-ups.',
    frag: { kind: 'issue', title: 'Upgrade plan: octokit 20 to 21', meta: '4 follow-up issues filed', chips: [{ t: 'dependencies' }] } },
  { id: 'report', title: 'Repository Reporting', href: `${G}ai-release-notes/`, trigger: 'release: published',
    text: 'Summarises repository or release activity on an event or a schedule.',
    frag: { kind: 'issue', title: 'Release notes: v2.3.1', meta: '14 merged pull requests · 3 fixes' } },
  { id: 'metrics', title: 'Metrics and Analytics', href: `${G}metrics-analytics/`, trigger: 'schedule: daily',
    text: 'Stores structured snapshots of workflow activity.',
    frag: { kind: 'issue', title: 'Weekly snapshot: 128 runs', meta: '94% success · 2 slower than last week' } },
  { id: 'quality', title: 'Code Quality Monitoring', href: `${G}multi-repo/code-quality-monitoring/`, trigger: 'schedule: weekly',
    text: 'Analyses quality across repositories and files actionable issues.',
    frag: { kind: 'issue', title: 'Quality report: 6 hotspots', meta: '3 repositories · 6 issues filed', chips: [{ t: 'quality' }] } },
  { id: 'tracking', title: 'Cross-Repository Issue Tracking', href: `${G}multi-repo/issue-tracking/`, trigger: 'schedule: daily',
    text: 'Aggregates issue status in one central repository.',
    frag: { kind: 'issue', title: 'Status: 23 open across 5 repos', meta: 'Updated 9:00 UTC · 4 blocked' } },
  { id: 'security', title: 'Security Review', href: `${G}security-review/`, trigger: 'schedule: daily',
    text: 'Combines repository evidence with AI judgement and reports through code scanning.',
    frag: { kind: 'alert', title: 'Unpinned action in release.yml', meta: 'Code scanning · Open', chips: [{ t: 'High', tone: 'danger' }] } },
  { id: 'side-repo', title: 'Triage from Side Repo', href: `${G}multi-repo/triage-from-side-repo/`, trigger: 'issues: opened',
    text: 'Triages a main repository from an isolated side repository.',
    frag: { kind: 'issue', title: 'acme/app #912 labelled', meta: 'from acme/triage-hub', chips: [{ t: 'bug' }, { t: 'p2' }] } },
  { id: 'feature-sync', title: 'Feature Synchronization', href: `${G}multi-repo/feature-sync/`, trigger: 'push: main',
    text: 'Syncs code and configuration through reviewable pull requests.',
    frag: { kind: 'pr', title: 'sync: port auth config from acme/app', num: '#88', add: '+42', del: '−3', chips: [{ t: 'sync' }] } },
  { id: 'dependabot', title: 'Dependabot Rollout', href: `${G}multi-repo/dependabot-rollout/`, trigger: 'workflow_dispatch',
    text: 'Rolls out tailored Dependabot configuration everywhere.',
    frag: { kind: 'pr', title: 'chore: add dependabot.yml', num: '#17', add: '+21', del: '−0', meta: '12 repositories' } },
];
