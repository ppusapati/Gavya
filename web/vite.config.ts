import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// The workspaces are a browser client for the gateway: every screen is a
// Connect call the browser makes itself, and there is no server-rendered data
// to fetch. So the build emits static files with an index.html fallback, which
// can be served from anywhere — including the gateway itself, which is the
// deployment that needs no CORS configuration at all.
export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			adapter: adapter({ fallback: 'index.html' })
		})
	]
});
