// @ts-check
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';
import { remarkMarkdownLinks } from './src/remark-markdown-links.mjs';

const base = '/oryn';
const docsDir = fileURLToPath(new URL('../docs', import.meta.url));

export default defineConfig({
	site: 'https://go-oryn.github.io',
	base,
	trailingSlash: 'always',
	markdown: {
		remarkPlugins: [[remarkMarkdownLinks, { docsDir, base }]],
	},
	integrations: [
		starlight({
			title: 'oryn',
			description: 'A modular Go framework built on fx, OpenTelemetry native, documented for humans and agents.',
			logo: { src: '../docs/assets/oryn-mark.png', alt: 'oryn' },
			customCss: ['./src/styles/custom.css'],
			favicon: '/favicon.png',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/go-oryn/oryn' }],
			lastUpdated: true,
			sidebar: [
				{
					label: 'Start here',
					items: [{ label: 'Overview', slug: 'index' }, 'getting-started', 'demo', 'agents'],
				},
				{
					label: 'Modules',
					items: [
						{
							label: 'Core',
							items: ['core/application', 'core/configuration', 'core/opentelemetry', 'core/health', 'core/testing'],
						},
						{
							label: 'HTTP server',
							items: [
								{ label: 'Overview', slug: 'httpserver/overview' },
								'httpserver/routing',
								'httpserver/observability',
								'httpserver/testing',
							],
						},
						{
							label: 'SQL',
							items: [{ label: 'Overview', slug: 'sql/overview' }, 'sql/migrations', 'sql/testing'],
						},
					],
				},
			],
			plugins: [
				starlightLlmsTxt({
					projectName: 'oryn',
					details: [
						'- oryn is a Go framework: applications are assembled from modules (core, httpserver, sql...) with uber/fx dependency injection.',
						'- A complete example application: https://github.com/go-oryn/demo',
						'- Extension points are `AsXxx` helpers (e.g. `httpserver.AsHandler`, `health.AsProbe`) taking values or fx constructors.',
						'- Every module is instrumented with OpenTelemetry; standard `OTEL_*` environment variables win over the `otel.*` config keys.',
						'- Tests run applications with `coretest.Start` and assert telemetry in memory with the `oteltest` packages.',
						'- Go API reference: https://pkg.go.dev/github.com/go-oryn/oryn',
					].join('\n'),
					customSets: [
						{
							label: 'Start here',
							description: 'overview, getting started guide, demo application, and the rules for agents writing oryn code',
							paths: ['index', 'getting-started', 'demo', 'agents'],
						},
						{
							label: 'Core',
							description: 'application, configuration, OpenTelemetry, logging, health checks and testing',
							paths: ['core/**'],
						},
						{
							label: 'HTTP server',
							description: 'the httpserver module: handlers, middlewares, handler groups, observability and testing',
							paths: ['httpserver/**'],
						},
						{
							label: 'SQL',
							description: 'the sql module: PostgreSQL, MySQL and SQLite connection pool, migrations, observability and testing',
							paths: ['sql/**'],
						},
					],
				}),
			],
		}),
	],
});
