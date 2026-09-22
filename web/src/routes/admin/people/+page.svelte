<script lang="ts">
	import {
		ApiError,
		type IssueServiceIdentityResponse,
		type ListMembersResponse,
		type ListRolesResponse,
		type ListServiceIdentitiesResponse,
		type VerifySessionResponse
	} from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const members = new Task<ListMembersResponse>();
	const roles = new Task<ListRolesResponse>();
	const credentials = new Task<ListServiceIdentitiesResponse>();
	const mine = new Task<VerifySessionResponse>();

	function loadMembers() {
		members.run((s) => settings.api().identity.listMembers({ signal: s }));
	}
	function loadCredentials() {
		credentials.run((s) => settings.api().identity.listServiceIdentities({ signal: s }));
	}
	$effect(loadMembers);
	$effect(loadCredentials);
	$effect(() => {
		roles.run((s) => settings.api().identity.listRoles({ signal: s }));
	});
	$effect(() => {
		mine.run((s) => settings.api().identity.verifySession({ signal: s }));
	});

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function asApiError(c: unknown): ApiError {
		return c instanceof ApiError
			? c
			: new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
		} catch (c) {
			saveError = asApiError(c);
		} finally {
			saving = false;
		}
	}

	const roleNames = $derived((roles.data?.roles ?? []).map((r) => r.name));

	let adding = $state(false);
	let nm = $state({ email: '', full_name: '', role: '', password: '' });

	let openRole = $state<string | undefined>(undefined);

	let settingPassword = $state<string | undefined>(undefined);
	let newPassword = $state('');

	let changingOwn = $state(false);
	let own = $state({ current: '', next: '', again: '' });

	let issuing = $state(false);
	let cred = $state({ name: '', expires_at: '' });

	/**
	 * The one secret this console is ever shown.
	 *
	 * Held in memory and never written to storage: the platform keeps a hash and
	 * cannot show it again, so this panel is the only place it exists outside
	 * whoever copies it. It is cleared when the page is left.
	 */
	let issued = $state<IssueServiceIdentityResponse | undefined>(undefined);

	function statusTone(s: string) {
		if (s === 'active') return 'calm';
		if (s === 'suspended' || s === 'revoked') return 'critical';
		return 'neutral';
	}

	function expired(iso: string): boolean {
		const at = Date.parse(iso);
		return Number.isFinite(at) && at < Date.now();
	}
</script>

<div class="page-head">
	<h1>People</h1>
	<p>
		Who may use this co-operative's platform, what each of them may do, and which machines hold
		credentials. Until these screens existed, authorisation was enforced against roles nobody could
		be given — every one of them was a row somebody typed into a database console.
	</p>
</div>

<ErrorNote error={saveError} />

{#if mine.settled}
	<Await task={mine} isEmpty={(d) => !d.tenant_id} empty="This session could not be verified.">
		{#snippet children(v)}
			<div class="panel">
				<dl class="kv">
					<dt>Signed in as</dt><dd class="mono">{v.user_id || v.service_identity_id || '—'}</dd>
					<dt>Role</dt><dd>{v.role_name ? label(v.role_name) : 'None.'}</dd>
					<dt>Permissions</dt>
					<dd>
						{#if v.permissions.length === 0}
							<Chip tone="attention">holds none</Chip>
							<span class="muted">
								Every screen in this console will refuse. That is a real state, not a failure to
								load — the reply said so.
							</span>
						{:else}
							<div class="perms">
								{#each v.permissions as p (p)}<Chip tone="neutral">{p}</Chip>{/each}
							</div>
						{/if}
					</dd>
				</dl>
				<div class="controls">
					<button class="ghost" onclick={() => { changingOwn = !changingOwn; saveError = undefined; own = { current: '', next: '', again: '' }; }}>
						{changingOwn ? 'Cancel' : 'Change my password'}
					</button>
				</div>
				{#if changingOwn}
					<form
						onsubmit={(e) => {
							e.preventDefault();
							run(
								() => settings.api().identity.changePassword(own.current, own.next),
								() => {
									changingOwn = false;
									own = { current: '', next: '', again: '' };
								}
							);
						}}
					>
						<div class="controls">
							<div class="field"><label for="pc">Current</label><input id="pc" type="password" bind:value={own.current} autocomplete="current-password" /></div>
							<div class="field"><label for="pn">New</label><input id="pn" type="password" bind:value={own.next} autocomplete="new-password" /></div>
							<div class="field"><label for="pa">Again</label><input id="pa" type="password" bind:value={own.again} autocomplete="new-password" /></div>
							<button type="submit" disabled={saving || !own.current || !own.next || own.next !== own.again}>
								Change
							</button>
						</div>
						{#if own.again && own.next !== own.again}
							<p class="warn">The two new passwords are not the same.</p>
						{/if}
						<p class="muted note">
							Whose password is read from the session by the service, never from this request.
							There is nothing here that names a subject, and that is the point: a route needing
							no permission that named its own subject would let anybody signed in change anybody
							else's.
						</p>
					</form>
				{/if}
			</div>
		{/snippet}
	</Await>
{/if}

<section>
	<h2>Members</h2>
	<div class="controls">
		<button class="ghost" onclick={loadMembers} disabled={members.pending}>Refresh</button>
		<button onclick={() => { adding = !adding; saveError = undefined; }}>
			{adding ? 'Cancel' : 'Add a person'}
		</button>
	</div>

	{#if adding}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().identity.addMember({
							email: nm.email.trim(),
							full_name: nm.full_name.trim(),
							role: nm.role,
							password: nm.password || undefined,
							actor: settings.actorOrUnknown
						}),
					() => {
						adding = false;
						nm = { email: '', full_name: '', role: '', password: '' };
						loadMembers();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field grow"><label for="me">Email</label><input id="me" type="email" bind:value={nm.email} /></div>
				<div class="field grow"><label for="mn">Name</label><input id="mn" bind:value={nm.full_name} /></div>
				<div class="field">
					<label for="mr">Role</label>
					<select id="mr" bind:value={nm.role}>
						<option value="">—</option>
						{#each roleNames as r (r)}<option value={r}>{label(r)}</option>{/each}
					</select>
				</div>
				<div class="field"><label for="mp">Password</label><input id="mp" type="password" bind:value={nm.password} autocomplete="new-password" placeholder="optional" /></div>
				<button type="submit" disabled={saving || !nm.email.trim() || !nm.role}>Add</button>
			</div>
			<p class="muted note">
				The password may be left empty. The person then exists and cannot sign in until somebody
				sets one — a real state, not an error. If the address already has an account this attaches
				it to this co-operative rather than making a second one.
			</p>
		</form>
	{/if}

	<Await task={members} retry={loadMembers} isEmpty={(d) => (d.members ?? []).length === 0} empty="Nobody is a member of this tenant.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>Name</th><th>Email</th><th>Role</th><th>Status</th><th>Can sign in</th><th>Last seen</th><th></th></tr>
					</thead>
					<tbody>
						{#each d.members as m (m.user_id)}
							<tr>
								<td>{m.full_name || '—'}</td>
								<td class="mono">{m.email}</td>
								<td>
									<select
										value={m.role}
										disabled={saving}
										onchange={(e) => run(() => settings.api().identity.setMemberRole(m.user_id, e.currentTarget.value, settings.actorOrUnknown), loadMembers)}
									>
										{#each roleNames as r (r)}<option value={r}>{label(r)}</option>{/each}
									</select>
								</td>
								<td><Chip tone={statusTone(m.status)}>{label(m.status)}</Chip></td>
								<td>
									{#if m.has_password}
										<Chip tone="calm">Yes</Chip>
									{:else}
										<Chip tone="attention" title="They exist and have no credential.">No password</Chip>
									{/if}
								</td>
								<td>{m.last_login_at ? instant(m.last_login_at) : 'Never.'}</td>
								<td class="actions">
									<button class="ghost" onclick={() => { settingPassword = settingPassword === m.user_id ? undefined : m.user_id; newPassword = ''; }}>
										{settingPassword === m.user_id ? 'Cancel' : 'Set password'}
									</button>
									{#if m.status === 'active'}
										<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().identity.setMemberStatus(m.user_id, 'suspended', settings.actorOrUnknown), loadMembers)}>
											Suspend
										</button>
									{:else}
										<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().identity.setMemberStatus(m.user_id, 'active', settings.actorOrUnknown), loadMembers)}>
											Reinstate
										</button>
									{/if}
									<button
										class="ghost"
										disabled={saving}
										onclick={() => run(() => settings.api().identity.signOutEverywhere(m.user_id, 'ended by an administrator'), loadMembers)}
									>
										Sign out everywhere
									</button>
								</td>
							</tr>
							{#if settingPassword === m.user_id}
								<tr class="detail">
									<td colspan="7">
										<form
											onsubmit={(e) => {
												e.preventDefault();
												run(
													() =>
														settings.api().identity.setMemberPassword(m.user_id, newPassword, settings.actorOrUnknown),
													() => {
														settingPassword = undefined;
														newPassword = '';
														loadMembers();
													}
												);
											}}
										>
											<div class="controls">
												<div class="field">
													<label for="sp-{m.user_id}">New password for {m.email}</label>
													<input id="sp-{m.user_id}" type="password" bind:value={newPassword} autocomplete="new-password" />
												</div>
												<button type="submit" disabled={saving || !newPassword}>Set</button>
											</div>
											<p class="muted note">
												Setting somebody else's takes a permission that changing your own does
												not. Their existing sessions are not ended by this — use "sign out
												everywhere" beside it if that is what was meant.
											</p>
										</form>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Roles</h2>
	<Await task={roles} isEmpty={(d) => (d.roles ?? []).length === 0} empty="No roles are defined.">
		{#snippet children(d)}
			<p class="muted note">
				The same for every co-operative. The permissions are listed with each role rather than
				behind another lookup, because "what does a supervisor actually get" is the question
				somebody assigning one is really asking.
			</p>
			<div class="tablewrap">
				<table>
					<thead><tr><th>Role</th><th>What it is for</th><th class="num">Permissions</th><th></th></tr></thead>
					<tbody>
						{#each d.roles as r (r.name)}
							<tr>
								<td>{label(r.name)}</td>
								<td class="muted">{r.description}</td>
								<td class="num">{r.permissions.length}</td>
								<td>
									<button class="ghost" onclick={() => (openRole = openRole === r.name ? undefined : r.name)}>
										{openRole === r.name ? 'Hide' : 'Show'}
									</button>
								</td>
							</tr>
							{#if openRole === r.name}
								<tr class="detail">
									<td colspan="4">
										<div class="perms">
											{#each r.permissions as p (p)}<Chip tone="neutral">{p}</Chip>{/each}
										</div>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Machine credentials</h2>
	<div class="controls">
		<button class="ghost" onclick={loadCredentials} disabled={credentials.pending}>Refresh</button>
		<button onclick={() => { issuing = !issuing; saveError = undefined; }}>
			{issuing ? 'Cancel' : 'Issue one'}
		</button>
	</div>

	{#if issuing}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					async () => {
						issued = await settings.api().identity.issueServiceIdentity({
							name: cred.name.trim(),
							expires_at: new Date(cred.expires_at).toISOString().replace(/\.\d{3}Z$/, 'Z'),
							actor: settings.actorOrUnknown
						});
					},
					() => {
						issuing = false;
						cred = { name: '', expires_at: '' };
						loadCredentials();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field grow"><label for="cn">Name</label><input id="cn" bind:value={cred.name} /></div>
				<div class="field"><label for="ce">Expires</label><input id="ce" type="date" bind:value={cred.expires_at} /></div>
				<button type="submit" disabled={saving || !cred.name.trim() || !cred.expires_at}>Issue</button>
			</div>
			<p class="muted note">
				An expiry is required and this console does not choose one. A credential with no end date
				is one nobody rotates, and a default here would be the platform setting a co-operative's
				rotation policy without being asked.
			</p>
		</form>
	{/if}

	{#if issued}
		<!--
			Shown once, and held only in memory.

			The platform keeps a hash and cannot show this again, so this panel is
			the only place the secret exists outside whoever copies it. Writing it to
			localStorage would put a live credential in a place that survives the
			session and belongs to the browser rather than the person.
		-->
		<div class="secret">
			<p>
				<Chip tone="critical">shown once</Chip>
				Copy this now. The platform keeps a hash and cannot show it again — if it is lost, issue
				another and revoke this one.
			</p>
			<dl class="kv">
				<dt>Name</dt><dd class="mono">{issued.name}</dd>
				<dt>Identifier</dt><dd class="mono">{issued.id}</dd>
				<dt>Secret</dt><dd class="mono break">{issued.secret}</dd>
				<dt>Expires</dt><dd>{instant(issued.expires_at)}</dd>
			</dl>
			<button class="ghost" onclick={() => (issued = undefined)}>I have copied it</button>
		</div>
	{/if}

	<Await task={credentials} retry={loadCredentials} isEmpty={(d) => (d.service_identities ?? []).length === 0} empty="No machine credentials have been issued.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Name</th><th>Status</th><th>Expires</th><th>Last used</th><th></th></tr></thead>
					<tbody>
						{#each d.service_identities as s (s.id)}
							<tr>
								<td>{s.name}</td>
								<td><Chip tone={statusTone(s.status)}>{label(s.status)}</Chip></td>
								<td>
									<Chip tone={expired(s.expires_at) ? 'critical' : 'calm'}>{instant(s.expires_at)}</Chip>
								</td>
								<td>{s.last_used_at ? instant(s.last_used_at) : 'Never.'}</td>
								<td>
									{#if s.status !== 'revoked'}
										<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().identity.revokeServiceIdentity(s.id, settings.actorOrUnknown), loadCredentials)}>
											Revoke
										</button>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<style>
	section { margin-top: 2.2rem; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
	.perms { display: flex; gap: 0.4rem; flex-wrap: wrap; }
	.warn { max-width: var(--measure); margin: 0.6rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
	.break { word-break: break-all; }
	.secret {
		max-width: var(--measure);
		margin: 1rem 0;
		padding: 1rem;
		background: color-mix(in srgb, var(--bad, #b3261e) 8%, transparent);
		border-left: 3px solid var(--bad, #b3261e);
		font-size: 0.85rem;
	}
</style>
