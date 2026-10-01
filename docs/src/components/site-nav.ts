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
