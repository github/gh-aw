import octicons from '@primer/octicons';

export const base = '/gh-aw';
export const TRIFECTA_URL = 'https://simonw.substack.com/p/the-lethal-trifecta-for-ai-agents';

export const icon = (n: keyof typeof octicons, size = 16) =>
  octicons[n].toSVG({ width: size, height: size });

export const risks = [
  { icon: 'lock', label: 'Private data', text: 'Code, secrets and issues it can read' },
  { icon: 'alert', label: 'Untrusted content', text: 'Issues and comments anyone can write' },
  { icon: 'globe', label: 'Outbound access', text: 'A way to send things out or change state' },
] as const;

export const layers = [
  {
    id: 'compile',
    icon: 'checklist',
    name: 'Compile-time validation',
    short: 'Compile-time validation',
    text: 'Schema validation, expression allowlisting and pinned actions are checked before a workflow can ever run.',
    href: `${base}/introduction/architecture/#compilation-time-security`,
  },
  {
    id: 'sandbox',
    icon: 'container',
    name: 'Sandboxed agent',
    short: 'Sandboxed agent',
    text: 'The agent runs read-only by default, with optional Cloud Hypervisor microVMs for stronger isolation.',
    href: `${base}/reference/glossary/#cloud-hypervisor`,
  },
  {
    id: 'credentials',
    icon: 'key',
    name: 'Credential isolation',
    short: 'Credential isolation',
    text: 'An API proxy holds the tokens, so the agent never sees them and cannot leak them.',
    href: `${base}/introduction/architecture/#agent-workflow-firewall-awf`,
  },
  {
    id: 'integrity',
    icon: 'filter',
    name: 'Integrity filtering',
    short: 'Integrity filtering',
    text: 'Untrusted GitHub content is filtered before the agent sees it.',
    href: `${base}/reference/integrity/`,
  },
  {
    id: 'threat',
    icon: 'shield',
    name: 'Threat detection',
    short: 'Threat detection',
    text: 'A separate job scans proposed outputs and blocks suspicious ones.',
    href: `${base}/reference/threat-detection/`,
  },
  {
    id: 'outputs',
    icon: 'check-circle',
    name: 'Safe outputs',
    short: 'Safe outputs',
    text: 'Only the writes you declared are applied, by a separate job with its own permissions.',
    href: `${base}/reference/safe-outputs/`,
  },
] as const;

export type Layer = (typeof layers)[number];
export const layer = (id: Layer['id']) => layers.find((l) => l.id === id)!;
