// @ts-check

/**
 * Strip decorative emojis from rendered markdown for a more professional look.
 *
 * - Applies to regular text nodes.
 * - Applies to code blocks and inline code so rendered pages contain no emojis.
 * - Also applies to image/link metadata and raw HTML/MDX JSX attributes.
 */
export default function remarkStripEmojis() {
	return function transform(tree) {
		visit(tree);
	};
}

/**
 * @param {any} node
 */
function visit(node) {
	if (!node || typeof node !== 'object') return;

	if (node.type === 'text' && typeof node.value === 'string') {
		node.value = stripEmojis(node.value);
	}

	if (node.type === 'inlineCode' && typeof node.value === 'string') {
		node.value = stripEmojis(node.value);
	}

	if (node.type === 'code' && typeof node.value === 'string') {
		node.value = stripEmojis(node.value, { preserveWhitespace: true });
	}

	if (node.type === 'html' && typeof node.value === 'string') {
		node.value = stripEmojis(node.value);
	}

	if (node.type === 'image') {
		if (typeof node.alt === 'string') node.alt = stripEmojis(node.alt);
		if (typeof node.title === 'string') node.title = stripEmojis(node.title);
	}

	if (node.type === 'link' && typeof node.title === 'string') {
		node.title = stripEmojis(node.title);
	}

	if (node.type === 'definition' && typeof node.title === 'string') {
		node.title = stripEmojis(node.title);
	}

	// MDX JSX elements can carry emoji in string-valued attributes (e.g., alt/title).
	// We keep this conservative and only touch string values.
	if (
		(node.type === 'mdxJsxFlowElement' || node.type === 'mdxJsxTextElement') &&
		Array.isArray(node.attributes)
	) {
		for (const attr of node.attributes) {
			if (!attr || typeof attr !== 'object') continue;
			if (typeof attr.value === 'string') {
				attr.value = stripEmojis(attr.value);
				continue;
			}
			// Some MDX parsers represent attribute values as objects.
			if (attr.value && typeof attr.value === 'object' && typeof attr.value.value === 'string') {
				attr.value.value = stripEmojis(attr.value.value);
			}
		}
	}

	const { children } = node;
	if (Array.isArray(children)) {
		for (const child of children) visit(child);
	}
}

const replacements = new Map([
	// Prefer text symbols over emoji glyphs.
	['✅', '✓'],
	['❌', '✗'],
	['⚠️', '!'],
	['⚠', '!'],
	// Common decorative prefixes.
	['🚀', ''],
	['🔍', ''],
	['🤖', ''],
	['🛡️', ''],
	['🛡', ''],
	['🔒', ''],
	['🔐', ''],
	['🔓', ''],
	['📥', ''],
	['📤', ''],
	['🌐', ''],
	['🚫', ''],
	['🐳', ''],
	['💰', ''],
	['⚡', ''],
	['🔗', ''],
	['🏷️', ''],
	['🏷', ''],
	['📊', ''],
	['🔬', ''],
	['🏗️', ''],
	['🏗', ''],
	['🧪', ''],
	['📋', ''],
	['🧩', ''],
	['🎯', ''],
	['🎭', ''],
]);

/**
 * @param {string} input
 * @param {{ preserveWhitespace?: boolean }} [options]
 */
function stripEmojis(input, options) {
	let output = input;

	for (const [from, to] of replacements.entries()) {
		if (output.includes(from)) output = output.split(from).join(to);
	}

	// Remove leftover emoji presentation selectors.
	output = output.replace(/\uFE0F/gu, '');

	// Strip any remaining pictographic emoji characters.
	// Node 20+ supports Unicode property escapes.
	output = output.replace(/\p{Extended_Pictographic}+/gu, '');

	// Collapse double spaces introduced by removals, but not inside code blocks
	// where whitespace (indentation) is significant.
	if (!options?.preserveWhitespace) {
		output = output.replace(/[ \t]{2,}/g, ' ');
	}
	return output;
}
