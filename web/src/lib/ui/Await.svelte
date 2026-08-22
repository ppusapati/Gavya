<script lang="ts" generics="T">
	import type { Task } from '$lib/task.svelte';
	import ErrorNote from './ErrorNote.svelte';

	let {
		task,
		retry,
		empty = 'Nothing to show.',
		isEmpty,
		children
	}: {
		task: Task<T>;
		retry?: () => void;
		empty?: string;
		/** An empty result is a real answer, so each screen says what empty means for it. */
		isEmpty?: (data: T) => boolean;
		children: import('svelte').Snippet<[T]>;
	} = $props();
</script>

<ErrorNote error={task.error} {retry} />

{#if task.pending && task.data === undefined}
	<div class="panel empty">Loading…</div>
{:else if task.data !== undefined}
	{#if isEmpty?.(task.data)}
		<div class="panel empty">{empty}</div>
	{:else}
		<div class:stale={task.pending}>{@render children(task.data)}</div>
	{/if}
{/if}

<style>
	/* While a newer query is in flight the old answer stays legible but is
	   visibly no longer current, rather than blanking the screen. */
	.stale {
		opacity: 0.55;
	}
</style>
