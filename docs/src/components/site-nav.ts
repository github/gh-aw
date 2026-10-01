/** Primary site sections, shared by the desktop header nav and the mobile menu sheet. */
export type SiteSection = 'home' | 'docs' | 'gallery' | 'blog';

export function getSection(id: string): SiteSection {
	if (id === '') return 'home';
	if (id === 'blog' || id.startsWith('blog/')) return 'blog';
	if (id === 'gallery' || id.startsWith('gallery/')) return 'gallery';
	return 'docs';
}

export function getNavLinks(base: string) {
	return [
		{ href: `${base}introduction/overview/`, label: 'Docs', section: 'docs' },
		{ href: `${base}gallery/`, label: 'Workflow examples', section: 'gallery' },
		{ href: `${base}blog/`, label: 'Blog', section: 'blog' },
	] satisfies { href: string; label: string; section: SiteSection }[];
}

export type FooterLink = { href: string; label: string };
export type FooterGroup = { label: string; links: FooterLink[] };

/** Link columns for the site footer. Product reuses the header nav so the two stay in sync. */
export function getFooterGroups(base: string): FooterGroup[] {
	return [
		{
			label: 'Product',
			links: [
				...getNavLinks(base).map(({ href, label }) => ({ href, label })),
				{ href: `${base}setup/quick-start/`, label: 'Get started' },
			],
		},
		{
			label: 'Docs',
			links: [
				{ href: `${base}setup/quick-start/`, label: 'Quick start' },
				{ href: `${base}setup/creating-workflows/`, label: 'Creating workflows' },
				{ href: `${base}reference/frontmatter/`, label: 'Reference' },
				{ href: `${base}reference/faq/`, label: 'FAQ' },
			],
		},
		{
			label: 'Resources',
			links: [
				{ href: `${base}llms.txt`, label: 'llms.txt' },
				{ href: `${base}llms-full.txt`, label: 'llms-full.txt' },
				{ href: `${base}blog/2026-01-12-welcome-to-pelis-agent-factory/`, label: "Peli's Agent Factory" },
				{ href: `${base}blog/rss.xml`, label: 'RSS' },
			],
		},
		{
			label: 'Community',
			links: [
				{ href: 'https://github.com/github/gh-aw', label: 'GitHub' },
				{ href: 'https://github.com/orgs/community/discussions/186451', label: 'Community feedback' },
			],
		},
	];
}
