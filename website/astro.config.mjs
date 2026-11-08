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
					items: [{ label: 'Overview', slug: 'index' }, 'getting-started', 'agents'],
				},
				{
					label: 'Demo',
					items: [
						{ label: 'Overview', slug: 'demo/overview' },
						'demo/entrypoints',
						'demo/apis',
						'demo/telemetry',
						'demo/testing',
					],
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
								'httpserver/configuration',
								'httpserver/routing',
								'httpserver/observability',
								'httpserver/testing',
							],
						},
						{
							label: 'HTTP client',
							items: [
								{ label: 'Overview', slug: 'httpclient/overview' },
								'httpclient/configuration',
								'httpclient/observability',
								'httpclient/testing',
							],
						},
						{
							label: 'gRPC server',
							items: [
								{ label: 'Overview', slug: 'grpcserver/overview' },
								'grpcserver/configuration',
								'grpcserver/observability',
								'grpcserver/testing',
							],
						},
						{
							label: 'gRPC client',
							items: [
								{ label: 'Overview', slug: 'grpcclient/overview' },
								'grpcclient/configuration',
								'grpcclient/observability',
								'grpcclient/testing',
							],
						},
						{
							label: 'MCP server',
							items: [
								{ label: 'Overview', slug: 'mcpserver/overview' },
								'mcpserver/configuration',
								'mcpserver/observability',
								'mcpserver/testing',
							],
						},
						{
							label: 'MCP client',
							items: [
								{ label: 'Overview', slug: 'mcpclient/overview' },
								'mcpclient/configuration',
								'mcpclient/observability',
								'mcpclient/testing',
							],
						},
						{
							label: 'SQL',
							items: [
								{ label: 'Overview', slug: 'sql/overview' },
								'sql/configuration',
								'sql/migrations',
								'sql/observability',
								'sql/testing',
							],
						},
						{
							label: 'Validator',
							items: [
								{ label: 'Overview', slug: 'validator/overview' },
								'validator/configuration',
								'validator/observability',
								'validator/testing',
							],
						},
						{
							label: 'Worker',
							items: [
								{ label: 'Overview', slug: 'worker/overview' },
								'worker/configuration',
								'worker/observability',
								'worker/testing',
							],
						},
						{
							label: 'Cron',
							items: [
								{ label: 'Overview', slug: 'cron/overview' },
								'cron/configuration',
								'cron/observability',
								'cron/testing',
							],
						},
					],
				},
			],
			plugins: [
				starlightLlmsTxt({
					projectName: 'oryn',
					details: [
						'- oryn is a Go framework: applications are assembled from modules (core, httpserver, httpclient, grpcserver, grpcclient, mcpserver, mcpclient, sql, validator, worker, cron) with uber/fx dependency injection.',
						'- A complete example application, serving one domain module over HTTP, gRPC and MCP: https://github.com/go-oryn/demo',
						'- Extension points are `AsXxx` helpers (e.g. `httpserver.AsHandler`, `health.AsProbe`) taking values or fx constructors.',
						'- Every module is instrumented with OpenTelemetry; standard `OTEL_*` environment variables win over the `otel.*` config keys.',
						'- Tests run applications with `coretest.Start` and assert telemetry in memory with the `oteltest` packages.',
						'- Go API reference: https://pkg.go.dev/github.com/go-oryn/oryn',
					].join('\n'),
					customSets: [
						{
							label: 'Start here',
							description: 'overview, getting started guide, and the rules for agents writing oryn code',
							paths: ['index', 'getting-started', 'agents'],
						},
						{
							label: 'Demo',
							description: 'the demo application: one tasks module served over HTTP, gRPC and MCP, entrypoints, telemetry and tests',
							paths: ['demo/**'],
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
							label: 'gRPC server',
							description: 'the grpcserver module: services, interceptors, health, reflection, observability and testing',
							paths: ['grpcserver/**'],
						},
						{
							label: 'MCP server',
							description: 'the mcpserver module: MCP tools, resources and prompts, streamable HTTP, observability and testing',
							paths: ['mcpserver/**'],
						},
						{
							label: 'HTTP client',
							description: 'the httpclient module: instrumented HTTP client, transport middlewares, configuration and in-memory testing',
							paths: ['httpclient/**'],
						},
						{
							label: 'gRPC client',
							description: 'the grpcclient module: named gRPC connections, interceptors, configuration and in-memory testing',
							paths: ['grpcclient/**'],
						},
						{
							label: 'MCP client',
							description: 'the mcpclient module: sessions to MCP servers, configuration, observability and in-memory testing',
							paths: ['mcpclient/**'],
						},
						{
							label: 'Validator',
							description: 'the validator module: struct validation, custom validations, observability and testing',
							paths: ['validator/**'],
						},
						{
							label: 'Worker',
							description: 'the worker module: background workers, restarts, health, observability and testing',
							paths: ['worker/**'],
						},
						{
							label: 'Cron',
							description: 'the cron module: scheduled jobs, schedules per environment, observability and testing',
							paths: ['cron/**'],
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
