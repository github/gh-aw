import { defineCollection, z } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';
import { blogSchema } from 'starlight-blog/schema';
// import { changelogsLoader } from 'starlight-changelogs/loader';

export const collections = {
	docs: defineCollection({ 
		loader: docsLoader(), 
		schema: docsSchema({
			extend: (ctx) => {
				const blogExtension = blogSchema(ctx);
				return blogExtension.extend({
					// Agent protection flag: when set to true, instructs AI agents
					// to treat this documentation page as read-only and skip any
					// automated editing, generation, or modification operations.
					// This is useful for auto-generated content, release notes,
					// or documentation pages that should only be manually curated.
					'disable-agentic-editing': z.boolean().optional().describe(
						'Prevents AI agents from making automated edits to this page'
					),
				});
			}
		})
	}),
	// changelogs: defineCollection({
	// 	loader: changelogsLoader([
	// 		{
	// 			provider: 'github',       // use GitHub releases as changelog source
	// 			base: 'changelog',        // base path for changelog pages
	// 			owner: 'githubnext',      // GitHub org/user
	// 			repo: 'gh-aw',            // GitHub repo
	// 			// Use GitHub token if available in environment, otherwise rely on public API
	// 			...(process.env.GITHUB_TOKEN && { token: process.env.GITHUB_TOKEN }),
	// 			// No process filter: include all releases
	// 		},
	// 	]),
	// }),
};
