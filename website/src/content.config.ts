import { defineCollection } from 'astro:content';
import { glob } from 'astro/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

// The pages are the plain Markdown files of the repository docs/ directory:
// agents read them directly, and this site renders them for humans.
export const collections = {
	docs: defineCollection({
		loader: glob({ base: '../docs', pattern: '**/[^_]*.md' }),
		schema: docsSchema(),
	}),
};
