// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';
import starlightLinksValidator from 'starlight-links-validator';
import starlightGitHubAlerts from 'starlight-github-alerts';
import starlightBlog from 'starlight-blog';
import mermaid from 'astro-mermaid';
import { fileURLToPath } from 'node:url';
import remarkStripEmojis from './src/lib/remark/stripEmojis.js';

/**
 * Creates blog authors config with GitHub profile pictures
 * @param {Record<string, {name: string, url: string, picture?: string}>} authors
 */
function createAuthors(authors) {
	return Object.fromEntries(
		Object.entries(authors).map(([key, author]) => [
			key,
			{ ...author, picture: author.picture ?? `https://github.com/${key}.png?size=200` }
		])
	);
}

// NOTE: A previous attempt defined a custom Shiki grammar for `aw` (agentic workflow) but
// Shiki did not register it and builds produced a warning: language "aw" not found.
// For now we alias `aw` -> `markdown` which removes the warning and still gives
// reasonable highlighting for examples that combine frontmatter + markdown.
// If richer highlighting is needed later, implement a proper TextMate grammar
// in a separate JSON file and load it here (ensure required embedded scopes exist).

// https://astro.build/config
export default defineConfig({
	site: 'https://github.github.io',
	base: '/gh-aw/',
	markdown: {
		remarkPlugins: [remarkStripEmojis],
	},
	vite: {
		server: {
			fs: {
				allow: [
					fileURLToPath(new URL('../', import.meta.url)),
				],
			},
		},
	},
	devToolbar: {
		enabled: false
	},
	experimental: {
		clientPrerender: false
	},
	integrations: [
		mermaid(),
		starlight({
			title: 'GitHub Agentic Workflows',
			logo: {
				src: './src/assets/agentic-workflow.svg',
				replacesTitle: false,
			},
		components: {
				Head: './src/components/CustomHead.astro',
				SocialIcons: './src/components/CustomHeader.astro',
				ThemeSelect: './src/components/ThemeToggle.astro',
				Footer: './src/components/CustomFooter.astro',
				SiteTitle: './src/components/CustomLogo.astro',
			},
			customCss: [
				'./src/styles/custom.css',
			],
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/github/gh-aw' },
			],
			tableOfContents: { 
			minHeadingLevel: 2, 
			maxHeadingLevel: 4 
		},
			pagination: true,
			expressiveCode: {
				frames: {
					showCopyToClipboardButton: true,
				},
				shiki: {
						langs: /** @type {any[]} */ ([
							"markdown",
							"yaml"
						]),
						langAlias: { aw: "markdown" }
				},
			},
			plugins: [
				starlightBlog({
					recentPostCount: 12,
					authors: createAuthors({
						'githubnext': {
							name: 'GitHub Next',
							url: 'https://githubnext.com/',
						},
						'dsyme': {
							name: 'Don Syme',
							url: 'https://dsyme.net/',
						},
						'pelikhan': {
							name: 'Peli de Halleux',
							url: 'https://www.microsoft.com/research/people/jhalleux/',
						},
						'mnkiefer': {
							name: 'Mara Kiefer',
							url: 'https://github.com/mnkiefer',
						},
						'claude': {
							name: 'Claude',
							url: 'https://claude.ai',
						},
						'codex': {
							name: 'Codex',
							url: 'https://openai.com/index/openai-codex/',
						},
						'copilot': {
							name: 'Copilot',
							url: 'https://github.com/features/copilot',
							picture: 'https://avatars.githubusercontent.com/in/1143301?s=64&amp;v=4',
						},
					}),
				}),
				starlightGitHubAlerts(),
				starlightLinksValidator({
					errorOnRelativeLinks: true,
					errorOnLocalLinks: true,
				}),
				starlightLlmsTxt({
					description: 'GitHub Agentic Workflows (gh-aw) is a Go-based GitHub CLI extension that enables writing agentic workflows in natural language using markdown files, and running them as GitHub Actions workflows.',
					optionalLinks: [
						{
							label: 'GitHub Repository',
							url: 'https://github.com/github/gh-aw',
							description: 'Source code and development resources for gh-aw'
						},
						{
							label: 'GitHub CLI Documentation',
							url: 'https://cli.github.com/manual/',
							description: 'Documentation for the GitHub CLI tool'
						}
					],
					customSets: [
						{
							label: "agentic-workflows",
							paths: ['blog/*meet-the-workflows*'],
							description: "A comprehensive blog series documenting workflow patterns, best practices, and real-world examples of agentic workflows created at Peli's Agent Factory"
						}
					]
				})
			],
			sidebar: [
				{
					label: 'Introduction',
					autogenerate: { directory: 'introduction' },
				},
				{
					label: 'Setup',
					items: [
						{ label: 'Quick Start', link: '/setup/quick-start/' },
						{ label: 'Creating Workflows', link: '/setup/creating-workflows/' },
						{ label: 'CLI Commands', link: '/setup/cli/' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'GitHub Actions Primer', link: '/guides/github-actions-primer/' },
						{ label: 'Packaging & Distribution', link: '/guides/packaging-imports/' },
						{ label: 'Using MCPs', link: '/guides/mcps/' },
						{ label: 'Web Search', link: '/guides/web-search/' },
						{ label: 'Ephemerals', link: '/guides/ephemerals/' },
					],
				},
				{
					label: 'Design Patterns',
					items: [
						{ label: 'ChatOps', link: '/patterns/chatops/' },
						{ label: 'DailyOps', link: '/patterns/dailyops/' },
						{ label: 'IssueOps', link: '/patterns/issueops/' },
						{ label: 'LabelOps', link: '/patterns/labelops/' },
						{ label: 'ProjectOps', link: '/patterns/projectops/' },
						{ label: 'DataOps', link: '/patterns/dataops/' },
						{ label: 'TaskOps', link: '/patterns/taskops/' },
						{ label: 'MultiRepoOps', link: '/patterns/multirepoops/' },
						{ label: 'SideRepoOps', link: '/patterns/siderepoops/' },
						{ label: 'SpecOps', link: '/patterns/specops/' },
						{ label: 'TrialOps', link: '/patterns/trialops/' },
						{ label: 'Monitoring', link: '/patterns/monitoring/' },
						{ label: 'Orchestration', link: '/patterns/orchestration/' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'AI Engines', link: '/reference/engines/' },
						{ label: 'Cache & Memory', link: '/reference/memory/' },
						{ label: 'Concurrency', link: '/reference/concurrency/' },
						{ label: 'Custom Agents', link: '/reference/copilot-custom-agents/' },
						{ label: 'Custom Safe Outputs', link: '/reference/custom-safe-outputs/' },
						{ label: 'FAQ', link: '/reference/faq/' },
						{ label: 'Frontmatter', link: '/reference/frontmatter/' },
						{ label: 'Frontmatter (Full)', link: '/reference/frontmatter-full/' },
						{ label: 'GH-AW as MCP Server', link: '/setup/mcp-server/' },
						{ label: 'Glossary', link: '/reference/glossary/' },
						{ label: 'Imports', link: '/reference/imports/' },
						{ label: 'Markdown', link: '/reference/markdown/' },
						{ label: 'MCP Gateway', link: '/reference/mcp-gateway/' },
						{ label: 'Network Access', link: '/reference/network/' },
						{ label: 'Permissions', link: '/reference/permissions/' },
						{ label: 'Rate Limiting Controls', link: '/reference/rate-limiting-controls/' },
						{ label: 'Safe Inputs', link: '/reference/safe-inputs/' },
						{ label: 'Safe Outputs', link: '/reference/safe-outputs/' },
						{ label: 'Schedule Syntax', link: '/reference/schedule-syntax/' },
						{ label: 'Templating', link: '/reference/templating/' },
						{ label: 'Threat Detection', link: '/reference/threat-detection/' },
						{ label: 'Tokens', link: '/reference/tokens/' },
						{ label: 'Tools', link: '/reference/tools/' },
						{ label: 'Triggers', link: '/reference/triggers/' },
						{ label: 'Workflow Structure', link: '/reference/workflow-structure/' },
					],
				},
				{
					label: 'Troubleshooting',
					autogenerate: { directory: 'troubleshooting' },
				},
			],
		}),
	],
});
