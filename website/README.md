# website

The documentation website, built with [Starlight](https://starlight.astro.build) from the Markdown files of [docs/](../docs).

```shell
cd website
npm ci
npm run dev       # http://localhost:4321/oryn/, reloads on docs/ changes
npm run build     # static site in dist/, with llms.txt, llms-full.txt and llms-small.txt
npm run preview   # serves dist/
```

- Pages are the `docs/**/*.md` files: plain Markdown with a `title` and `description` frontmatter. No MDX or components, so agents can read them as is.
- Link pages with relative `.md` links (`[Testing](../core/testing.md)`): they work on GitHub, and are turned into website links at build time.
- Add new pages to the `sidebar` of [astro.config.mjs](astro.config.mjs), and to the `customSets` of the llms.txt plugin when adding a module.
