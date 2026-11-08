import path from 'node:path';
import { visit } from 'unist-util-visit';

/**
 * Turns the relative links between Markdown files of docs/ (e.g. `../core/testing.md#assert-telemetry`),
 * which work on GitHub and for agents, into links to the website pages (`/oryn/core/testing/#assert-telemetry`).
 */
export function remarkMarkdownLinks({ docsDir, base }) {
	return (tree, file) => {
		if (!file.path || !path.resolve(file.path).startsWith(docsDir)) {
			return;
		}

		visit(tree, 'link', (node) => {
			const match = node.url.match(/^([^:#?]+\.md)(#.*)?$/);
			if (!match || node.url.startsWith('/')) {
				return;
			}

			const target = path.relative(docsDir, path.resolve(path.dirname(file.path), match[1]));
			const slug = target.replace(/\.md$/, '').replace(/(^|\/)index$/, '');

			node.url = `${base}/${slug ? `${slug}/` : ''}${match[2] ?? ''}`;
		});
	};
}
