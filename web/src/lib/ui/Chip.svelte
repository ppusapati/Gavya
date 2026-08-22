<script lang="ts">
	import type { Tone } from '$lib/display';

	let {
		tone = 'neutral',
		advisory = false,
		title = '',
		children
	}: {
		tone?: Tone;
		/** Dashes the border, so a model's opinion never looks like a finding. */
		advisory?: boolean;
		title?: string;
		children: import('svelte').Snippet;
	} = $props();
</script>

<span class="chip {tone === 'neutral' ? '' : tone}" class:advisory {title}>{@render children()}</span>
