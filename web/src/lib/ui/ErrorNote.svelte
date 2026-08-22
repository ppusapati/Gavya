<script lang="ts">
	import type { ApiError } from '$lib/api';

	let {
		error,
		retry
	}: {
		error: ApiError | undefined;
		retry?: () => void;
	} = $props();
</script>

{#if error}
	<div class="error" role="alert">
		<span class="code">{error.code.replace(/_/g, ' ')}</span>
		<p>{error.humane}</p>
		{#if error.retryable && retry}
			<!-- Offered only when repeating the call could plausibly succeed. An
			     invalid argument does not become valid by asking again, and a call
			     that already had an effect must not be repeated on a guess. -->
			<button class="ghost" onclick={retry}>Try again</button>
		{/if}
	</div>
{/if}

<style>
	p {
		margin: 0;
	}

	button {
		margin-top: 0.6rem;
	}
</style>
