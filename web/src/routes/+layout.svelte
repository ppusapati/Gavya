<script lang="ts">
	import { page } from '$app/state';
	import favicon from '$lib/assets/favicon.svg';
	import { settings } from '$lib/settings.svelte';
	import '$lib/styles/app.css';

	let { children } = $props();

	let panelOpen = $state(false);

	// The panel opens by itself the first time, because no screen can ask the
	// gateway anything until it knows which tenant is being reviewed.
	$effect(() => {
		if (!settings.ready) panelOpen = true;
	});

	function current(href: string): 'page' | undefined {
		const path = page.url.pathname;
		if (href === '/') return path === '/' ? 'page' : undefined;
		return path === href || path.startsWith(href + '/') ? 'page' : undefined;
	}

	function save(event: SubmitEvent) {
		event.preventDefault();
		settings.save();
		panelOpen = false;
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>Gavya — integrity workspaces</title>
</svelte:head>

<div class="shell">
	<nav class="rail">
		<div class="brand">
			Gavya
			<small>Integrity workspaces</small>
		</div>

		<div class="nav">
			<a href="/" aria-current={current('/')}>Overview</a>

			<span class="group">Integrity &amp; audit</span>
			<a href="/integrity" aria-current={current('/integrity')}>Divergence queue</a>

			<span class="group">Mass balance</span>
			<a href="/balance" aria-current={current('/balance')}>Balance windows</a>

			<span class="group">Data mapping</span>
			<a href="/mapping" aria-current={current('/mapping')}>External identities</a>
			<a href="/mapping/conflicts" aria-current={current('/mapping/conflicts')}>Slot conflicts</a>
			<a href="/quarantine" aria-current={current('/quarantine')}>Quarantine</a>
		</div>

		<div class="context">
			<button class="ghost" onclick={() => (panelOpen = !panelOpen)} aria-expanded={panelOpen}>
				{panelOpen ? 'Close' : 'Change'} context
			</button>
			{#if !panelOpen}
				<dl class="who">
					<dt>Tenant</dt>
					<dd class="mono">{settings.tenantId || '— not set —'}</dd>
					<dt>Reviewer</dt>
					<dd>{settings.actor || '— not set —'}</dd>
				</dl>
			{/if}
		</div>
	</nav>

	<main class="main">
		{#if panelOpen}
			<form class="panel context-form" onsubmit={save}>
				<h2>Review context</h2>
				<p class="muted">
					Phase-1 has no sign-in. The tenant decides which records are visible, and the reviewer's
					name is what gets written into the audit trail of every resolution made here.
				</p>
				<div class="controls">
					<div class="field">
						<label for="gw">Gateway</label>
						<input id="gw" bind:value={settings.gatewayUrl} size="28" placeholder="http://localhost:8000" />
					</div>
					<div class="field">
						<label for="tid">Tenant id</label>
						<input id="tid" bind:value={settings.tenantId} size="26" placeholder="required" />
					</div>
					<div class="field">
						<label for="who">Your name</label>
						<input id="who" bind:value={settings.actor} size="20" placeholder="recorded on resolutions" />
					</div>
					<button type="submit" disabled={!settings.ready}>Use this context</button>
				</div>
			</form>
		{/if}

		{#if settings.ready}
			{@render children()}
		{:else if !panelOpen}
			<div class="panel empty">Set a gateway and a tenant to begin.</div>
		{/if}
	</main>
</div>

<style>
	.context {
		margin-top: auto;
		border-top: 1px solid var(--rule);
		padding-top: 1rem;
	}

	.who {
		display: grid;
		gap: 0.1rem;
		margin: 0.85rem 0 0;
	}

	.who dt {
		font-family: var(--mono);
		font-size: 0.62rem;
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: var(--muted);
	}

	.who dd {
		margin: 0 0 0.5rem;
		font-size: 0.82rem;
		overflow-wrap: anywhere;
	}

	.context-form {
		margin-bottom: 1.75rem;
	}

	.context-form .controls {
		margin-bottom: 0;
		margin-top: 1rem;
	}
</style>
