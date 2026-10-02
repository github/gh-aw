/**
 * Heading link enhancement
 *
 * Starlight wraps each markdown heading as `.sl-heading-wrapper > h2 + a.sl-anchor-link`.
 * Without JS that link is a plain `#id` anchor. This script turns it into a
 * "copy link to section" button: clicking copies the full URL with the hash,
 * updates the address bar without jumping, and briefly shows a "Copied" state
 * (styles in custom.css, "Heading links").
 */

const COPIED_MS = 1500;
const timers = new WeakMap<HTMLElement, number>();

let liveRegion: HTMLElement | null = null;

function announce(message: string): void {
	if (!liveRegion || !liveRegion.isConnected) {
		liveRegion = document.createElement('div');
		liveRegion.className = 'sr-only aw-anchor-live';
		liveRegion.setAttribute('aria-live', 'polite');
		document.body.append(liveRegion);
	}
	// Clear first so repeated copies of the same section are announced again
	liveRegion.textContent = '';
	window.setTimeout(() => {
		if (liveRegion) liveRegion.textContent = message;
	}, 50);
}

function headingText(link: HTMLAnchorElement): string {
	const heading = link.parentElement?.firstElementChild;
	return heading?.textContent?.trim() ?? '';
}

function enhance(): void {
	document
		.querySelectorAll<HTMLAnchorElement>('.sl-markdown-content .sl-anchor-link:not([data-aw-enhanced])')
		.forEach((link) => {
			link.dataset.awEnhanced = '';
			link.setAttribute('aria-label', `Copy link to section: ${headingText(link)}`);
			const tip = document.createElement('span');
			tip.className = 'aw-anchor-tip';
			tip.setAttribute('aria-hidden', 'true');
			tip.textContent = 'Copied';
			link.append(tip);
		});
}

async function onClick(event: MouseEvent): Promise<void> {
	if (event.defaultPrevented || event.button !== 0) return;
	if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
	const link = (event.target as Element | null)?.closest<HTMLAnchorElement>(
		'.sl-markdown-content .sl-anchor-link[data-aw-enhanced]',
	);
	if (!link || !navigator.clipboard?.writeText) return;

	event.preventDefault();
	const url = new URL(link.getAttribute('href') ?? '', window.location.href).toString();
	history.replaceState(history.state, '', url);

	try {
		await navigator.clipboard.writeText(url);
	} catch {
		// Clipboard blocked: the address bar already holds the link
		return;
	}

	link.setAttribute('data-copied', '');
	announce('Link copied');
	window.clearTimeout(timers.get(link));
	timers.set(
		link,
		window.setTimeout(() => link.removeAttribute('data-copied'), COPIED_MS),
	);
}

if (document.readyState === 'loading') {
	document.addEventListener('DOMContentLoaded', enhance);
} else {
	enhance();
}
document.addEventListener('astro:page-load', enhance);
document.addEventListener('click', onClick);
