<script lang="ts">
	import { page } from '$app/state';
	import favicon from '$lib/assets/favicon.svg';
	import { settings } from '$lib/settings.svelte';
	import '$lib/styles/app.css';

	let { children } = $props();

	let panelOpen = $state(false);
	let email = $state('');
	let password = $state('');
	let signingIn = $state(false);
	let failure = $state('');

	// The panel opens by itself, because no screen can ask the gateway anything
	// until there is a session. A session that has run out is the same case, and
	// says so rather than letting every screen answer 401.
	$effect(() => {
		if (!settings.ready) panelOpen = true;
	});

	function current(href: string): 'page' | undefined {
		const path = page.url.pathname;
		if (href === '/') return path === '/' ? 'page' : undefined;
		return path === href || path.startsWith(href + '/') ? 'page' : undefined;
	}

	async function signIn(event: SubmitEvent) {
		event.preventDefault();
		failure = '';
		signingIn = true;
		try {
			settings.save(); // keep the gateway even if the password is wrong
			await settings.signIn(email.trim(), password);
			password = '';
			panelOpen = false;
		} catch (cause) {
			// The message the platform gave, not a guess at it. Sign-in failures
			// are deliberately indistinguishable between a wrong password and an
			// address nobody has, and inventing a friendlier wording here would
			// undo that.
			failure = cause instanceof Error ? cause.message : String(cause);
		} finally {
			signingIn = false;
		}
	}

	function signOut() {
		settings.forget();
		panelOpen = true;
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
					<dd class="mono">{settings.tenantId || '— not signed in —'}</dd>
					<dt>Signed in as</dt>
					<dd class="mono">{settings.userId || '— not signed in —'}</dd>
					{#if settings.roleName}
						<dt>Role</dt>
						<dd>{settings.roleName}</dd>
					{/if}
				</dl>
				{#if settings.session}
					<button class="ghost" onclick={signOut}>Forget this session</button>
				{/if}
			{/if}
		</div>
	</nav>

	<main class="main">
		{#if panelOpen}
			<form class="panel context-form" onsubmit={signIn}>
				<h2>Sign in</h2>
				<p class="muted">
					The gateway decides which tenant you act for and what you may do, from the session this
					makes. Neither is something the browser can claim: a tenant sent from here is stripped
					before the request reaches a service.
				</p>
				{#if settings.expired}
					<p class="muted">Your previous session has expired.</p>
				{/if}
				<div class="controls">
					<div class="field">
						<label for="gw">Gateway</label>
						<input id="gw" bind:value={settings.gatewayUrl} size="28" placeholder="http://localhost:8000" />
					</div>
					<div class="field">
						<label for="email">Email</label>
						<input id="email" type="email" bind:value={email} size="26" autocomplete="username" />
					</div>
					<div class="field">
						<label for="pw">Password</label>
						<input id="pw" type="password" bind:value={password} size="20" autocomplete="current-password" />
					</div>
					<button type="submit" disabled={signingIn || !email.trim() || !password}>
						{signingIn ? 'Signing in…' : 'Sign in'}
					</button>
				</div>
				{#if failure}
					<p class="failure" role="alert">{failure}</p>
				{/if}
			</form>
		{/if}

		{#if settings.ready}
			{@render children()}
		{:else if !panelOpen}
			<div class="panel empty">Sign in to begin.</div>
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

	.failure {
		margin: 0.9rem 0 0;
		color: var(--bad, #b3261e);
		font-size: 0.85rem;
	}
</style>
