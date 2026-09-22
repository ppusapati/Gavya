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

	// Links whose path is a prefix of another link's. Without this, /herd reads as
	// the current page while somebody is on /herd/pregnancies, and two entries in
	// the rail are highlighted at once.
	const EXACT = new Set(['/herd']);

	function current(href: string): 'page' | undefined {
		const path = page.url.pathname;
		if (href === '/' || EXACT.has(href)) return path === href ? 'page' : undefined;
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

	let signingOut = $state(false);
	let signOutNote = $state('');

	/**
	 * End the session, and say which kind of ending it was.
	 *
	 * This used to clear the browser and nothing else, leaving a live session on
	 * the platform. It now calls SignOut; when that call cannot be made the
	 * local state is still cleared — a person who asked to sign out must not
	 * stay signed in because the network was down — and the note below says so,
	 * because on a shared machine the difference matters.
	 */
	async function signOut() {
		if (signingOut) return;
		signingOut = true;
		signOutNote = '';
		try {
			const outcome = await settings.signOut();
			signOutNote =
				outcome === 'ended'
					? ''
					: 'Signed out of this browser. The platform could not be reached, so the session is still valid there until it expires — revoke it from another machine if this one is shared.';
		} finally {
			signingOut = false;
			panelOpen = true;
		}
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

			<span class="group">The herd</span>
			<a href="/herd" aria-current={current('/herd')}>Cattle</a>
			<a href="/milk" aria-current={current('/milk')}>Collection</a>
			<a href="/herd/pregnancies" aria-current={current('/herd/pregnancies')}>Pregnancies</a>
			<a href="/herd/vaccinations" aria-current={current('/herd/vaccinations')}>Vaccinations due</a>
			<a href="/farms" aria-current={current('/farms')}>Farms &amp; feed</a>

			<span class="group">Money</span>
			<a href="/money/cycles" aria-current={current('/money/cycles')}>Payment cycles</a>
			<a href="/money/collections" aria-current={current('/money/collections')}>Priced collections</a>
			<a href="/money/rate-cards" aria-current={current('/money/rate-cards')}>Rate cards</a>
			<a href="/money/pools" aria-current={current('/money/pools')}>Pools</a>
			<a href="/money/recoveries" aria-current={current('/money/recoveries')}>Recoveries</a>
			<a href="/money/billing" aria-current={current('/money/billing')}>Billing</a>

			<span class="group">Administration</span>
			<a href="/admin/people" aria-current={current('/admin/people')}>People &amp; access</a>
			<a href="/admin/tenants" aria-current={current('/admin/tenants')}>Tenants</a>
			<a href="/admin/audit" aria-current={current('/admin/audit')}>Audit</a>
			<a href="/admin/inbox" aria-current={current('/admin/inbox')}>Inbox</a>
			<a href="/admin/reports" aria-current={current('/admin/reports')}>Reports</a>
			<a href="/admin/files" aria-current={current('/admin/files')}>Files</a>

			<span class="group">Plant</span>
			<a href="/plant/movements" aria-current={current('/plant/movements')}>Movements</a>
			<a href="/plant/batches" aria-current={current('/plant/batches')}>Batches</a>
			<a href="/plant/recipes" aria-current={current('/plant/recipes')}>Recipes</a>
			<a href="/plant/laboratory" aria-current={current('/plant/laboratory')}>Laboratory</a>
			<a href="/plant/meters" aria-current={current('/plant/meters')}>Meters</a>

			<span class="group">Commerce</span>
			<a href="/commerce/catalogue" aria-current={current('/commerce/catalogue')}>Catalogue</a>
			<a href="/commerce/stock" aria-current={current('/commerce/stock')}>Stock</a>
			<a href="/commerce/orders" aria-current={current('/commerce/orders')}>Orders</a>
			<a href="/commerce/market" aria-current={current('/commerce/market')}>Cattle market</a>

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
					<button class="ghost" onclick={signOut} disabled={signingOut}>
						{signingOut ? 'Signing out…' : 'Sign out'}
					</button>
				{/if}
				<dl class="who">
					<dt>Dates in</dt>
					<dd>
						<span class="mono">{settings.timezone}</span>
						{#if settings.zoneSource === 'browser'}
							<span class="zonewarn">this browser's, not the tenant's</span>
						{/if}
					</dd>
				</dl>
			{/if}
		</div>
	</nav>

	<main class="main">
		{#if signOutNote}
			<p class="signoutnote">{signOutNote}</p>
		{/if}
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

	.zonewarn {
		display: block;
		font-size: 0.72rem;
		color: var(--bad, #b3261e);
	}

	.signoutnote {
		max-width: var(--measure);
		margin: 0 0 1rem;
		padding: 0.7rem 1rem;
		font-size: 0.85rem;
		background: color-mix(in srgb, var(--bad, #b3261e) 8%, transparent);
		border-left: 3px solid var(--bad, #b3261e);
	}
</style>
