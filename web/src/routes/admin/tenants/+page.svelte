<script lang="ts">
	import {
		ApiError,
		type ListTenantSettingsResponse,
		type ListTenantsResponse,
		type Tenant,
		type TenantResponse,
		type UpdateTenantRequest
	} from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const tenants = new Task<ListTenantsResponse>();
	const one = new Task<TenantResponse>();
	const tenantSettings = new Task<ListTenantSettingsResponse>();

	function load() {
		tenants.run((s) => settings.api().admin.listTenants({ signal: s }));
	}
	$effect(load);

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

	let creating = $state(false);
	let nt = $state({
		name: '',
		slug: '',
		plan: '',
		contact_email: '',
		contact_phone: '',
		address: '',
		country: '',
		timezone: browserZone(),
		currency: 'INR',
		max_users: 50,
		max_cattle: 5000
	});

	function browserZone(): string {
		try {
			return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
		} catch {
			return 'UTC';
		}
	}

	let open = $state<string | undefined>(undefined);

	/**
	 * The edit form holds every mutable field, pre-filled from the record.
	 *
	 * UpdateTenant builds a whole tenant out of what arrives and writes it, so
	 * an omitted field is not left alone — it is cleared. A form with only the
	 * fields somebody wanted to change would blank the contact details, the
	 * timezone that decides which day a collection falls on, and the currency
	 * every money view reads its code from.
	 */
	let edit = $state<UpdateTenantRequest | undefined>(undefined);

	let settingKey = $state('');
	let settingValue = $state('');
	let settingType = $state('string');

	function openTenant(t: Tenant) {
		if (open === t.id) {
			open = undefined;
			edit = undefined;
			one.reset();
			tenantSettings.reset();
			return;
		}
		open = t.id;
		saveError = undefined;
		one.run((s) => settings.api().admin.getTenant(t.id, { signal: s }));
		tenantSettings.run((s) => settings.api().admin.listTenantSettings(t.id, { signal: s }));
		edit = fromTenant(t);
	}

	function fromTenant(t: Tenant): UpdateTenantRequest {
		return {
			id: t.id,
			name: t.name,
			contact_email: t.contact_email,
			contact_phone: t.contact_phone,
			address: t.address,
			country: t.country,
			timezone: t.timezone,
			currency: t.currency,
			max_users: t.max_users,
			max_cattle: t.max_cattle,
			updated_by: settings.actorOrUnknown
		};
	}

	function statusTone(s: string) {
		if (s === 'active') return 'calm';
		if (s === 'suspended') return 'critical';
		return 'neutral';
	}
</script>

<div class="page-head">
	<h1>Tenants</h1>
	<p>
		Who the platform holds data for, and the two figures every other screen depends on: the currency
		its money is denominated in, and the timezone that decides which day a collection falls on.
	</p>
</div>

<ErrorNote error={saveError} />

<p class="banner">
	{#if settings.zoneSource === 'tenant'}
		<Chip tone="calm">reading dates in {settings.timezone}</Chip>
		taken from this tenant's own record.
	{:else}
		<Chip tone="attention">reading dates in {settings.timezone}</Chip>
		which is this browser's zone, not the tenant's — the console could not read the tenant record at
		sign-in.
		<button class="linklike" onclick={() => settings.adoptTenantZone()}>Try again</button>
	{/if}
</p>

<div class="controls">
	<button class="ghost" onclick={load} disabled={tenants.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>
		{creating ? 'Cancel' : 'Create a tenant'}
	</button>
</div>

{#if creating}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().admin.createTenant({ ...nt, created_by: settings.actorOrUnknown }),
				() => {
					creating = false;
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field grow"><label for="tn">Name</label><input id="tn" bind:value={nt.name} /></div>
			<div class="field"><label for="ts">Slug</label><input id="ts" bind:value={nt.slug} size="14" /></div>
			<div class="field"><label for="tp">Plan</label><input id="tp" bind:value={nt.plan} size="12" /></div>
			<div class="field"><label for="tc">Currency</label><input id="tc" bind:value={nt.currency} size="5" /></div>
			<div class="field"><label for="tz">Timezone</label><input id="tz" bind:value={nt.timezone} size="18" /></div>
		</div>
		<div class="controls">
			<div class="field grow"><label for="te">Contact email</label><input id="te" bind:value={nt.contact_email} type="email" /></div>
			<div class="field"><label for="tph">Phone</label><input id="tph" bind:value={nt.contact_phone} size="16" /></div>
			<div class="field grow"><label for="ta">Address</label><input id="ta" bind:value={nt.address} /></div>
			<div class="field"><label for="tco">Country</label><input id="tco" bind:value={nt.country} size="6" /></div>
			<div class="field"><label for="tmu">Max users</label><input id="tmu" type="number" min="0" bind:value={nt.max_users} size="6" /></div>
			<div class="field"><label for="tmc">Max cattle</label><input id="tmc" type="number" min="0" bind:value={nt.max_cattle} size="7" /></div>
			<button type="submit" disabled={saving || !nt.name.trim() || !nt.slug.trim()}>Create</button>
		</div>
		<p class="muted note">
			The currency's scale is not set here. The service stores how many digits after the point this
			tenant's currency has beside the code itself, so an amount already recorded cannot change
			meaning if a currency table is later corrected.
		</p>
	</form>
{/if}

<Await task={tenants} retry={load} isEmpty={(d) => (d.tenants ?? []).length === 0} empty="No tenants.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Name</th><th>Slug</th><th>Plan</th><th>Currency</th><th>Timezone</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.tenants as t (t.id)}
						<tr class:mine={t.id === settings.tenantId}>
							<td>
								{t.name}
								{#if t.id === settings.tenantId}<Chip tone="neutral">signed in</Chip>{/if}
							</td>
							<td class="mono">{t.slug}</td>
							<td>{t.plan || '—'}</td>
							<td>{t.currency} <span class="muted">{t.currency_scale} dp</span></td>
							<td class="mono">{t.timezone || '—'}</td>
							<td><Chip tone={statusTone(t.status)}>{label(t.status)}</Chip></td>
							<td class="actions">
								<button class="ghost" onclick={() => openTenant(t)}>{open === t.id ? 'Close' : 'Open'}</button>
								{#if t.status === 'active'}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.suspendTenant(t.id, settings.actorOrUnknown), load)}>
										Suspend
									</button>
								{:else}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.activateTenant(t.id, settings.actorOrUnknown), load)}>
										Activate
									</button>
								{/if}
							</td>
						</tr>

						{#if open === t.id}
							<tr class="detail">
								<td colspan="7">
									<Await task={one} isEmpty={(x) => !x.tenant} empty="No such tenant.">
										{#snippet children(x)}
											<dl class="kv">
												<dt>Identifier</dt><dd class="mono">{x.tenant.id}</dd>
												<dt>Created</dt><dd>{instant(x.tenant.created_at)} by {x.tenant.created_by}</dd>
												<dt>Last changed</dt><dd>{instant(x.tenant.updated_at)} by {x.tenant.updated_by || '—'}</dd>
												<dt>Limits</dt><dd>{x.tenant.max_users} users, {x.tenant.max_cattle} cattle</dd>
											</dl>
										{/snippet}
									</Await>

									{#if edit && edit.id === t.id}
										<h3>Edit</h3>
										<form
											onsubmit={(e) => {
												e.preventDefault();
												const body = edit;
												if (!body) return;
												run(
													() =>
														settings.api().admin.updateTenant({
															...body,
															updated_by: settings.actorOrUnknown
														}),
													() => {
														load();
														one.run((s) => settings.api().admin.getTenant(t.id, { signal: s }));
														if (t.id === settings.tenantId) settings.adoptTenantZone();
													}
												);
											}}
										>
											<div class="controls">
												<div class="field grow"><label for="en-{t.id}">Name</label><input id="en-{t.id}" bind:value={edit.name} /></div>
												<div class="field grow"><label for="ee-{t.id}">Contact email</label><input id="ee-{t.id}" bind:value={edit.contact_email} type="email" /></div>
												<div class="field"><label for="ep-{t.id}">Phone</label><input id="ep-{t.id}" bind:value={edit.contact_phone} size="16" /></div>
											</div>
											<div class="controls">
												<div class="field grow"><label for="ea-{t.id}">Address</label><input id="ea-{t.id}" bind:value={edit.address} /></div>
												<div class="field"><label for="ec-{t.id}">Country</label><input id="ec-{t.id}" bind:value={edit.country} size="6" /></div>
												<div class="field"><label for="ez-{t.id}">Timezone</label><input id="ez-{t.id}" bind:value={edit.timezone} size="18" /></div>
												<div class="field"><label for="ecu-{t.id}">Currency</label><input id="ecu-{t.id}" bind:value={edit.currency} size="5" /></div>
												<div class="field"><label for="eu-{t.id}">Max users</label><input id="eu-{t.id}" type="number" min="0" bind:value={edit.max_users} size="6" /></div>
												<div class="field"><label for="ect-{t.id}">Max cattle</label><input id="ect-{t.id}" type="number" min="0" bind:value={edit.max_cattle} size="7" /></div>
												<button type="submit" disabled={saving}>Save</button>
												<button type="button" class="ghost" onclick={() => (edit = fromTenant(t))}>Revert</button>
											</div>
											<p class="muted note">
												Every field above is sent on every save, pre-filled from the record. The
												service writes a whole tenant out of what arrives, so a form that sent only
												what somebody typed would clear the rest — including the timezone that
												decides which day a collection falls on.
											</p>
										</form>
									{/if}

									<h3>Settings</h3>
									<Await task={tenantSettings} isEmpty={(x) => (x.settings ?? []).length === 0} empty="No settings are recorded for this tenant.">
										{#snippet children(x)}
											<div class="tablewrap">
												<table>
													<thead><tr><th>Key</th><th>Value</th><th>Type</th><th>Last changed</th></tr></thead>
													<tbody>
														{#each x.settings as st (st.id)}
															<tr>
																<td class="mono">{st.key}</td>
																<td class="mono">{st.value}</td>
																<td>{st.data_type}</td>
																<td>{instant(st.updated_at)} {st.updated_by ? `by ${st.updated_by}` : ''}</td>
															</tr>
														{/each}
													</tbody>
												</table>
											</div>
										{/snippet}
									</Await>

									{#if t.id === settings.tenantId}
										<form
											onsubmit={(e) => {
												e.preventDefault();
												run(
													() =>
														settings.api().admin.upsertTenantSetting({
															key: settingKey.trim(),
															value: settingValue,
															data_type: settingType,
															created_by: settings.actorOrUnknown
														}),
													() => {
														settingKey = '';
														settingValue = '';
														tenantSettings.run((s) =>
															settings.api().admin.listTenantSettings(t.id, { signal: s })
														);
													}
												);
											}}
										>
											<div class="controls">
												<div class="field"><label for="sk-{t.id}">Key</label><input id="sk-{t.id}" bind:value={settingKey} size="20" /></div>
												<div class="field grow"><label for="sv-{t.id}">Value</label><input id="sv-{t.id}" bind:value={settingValue} /></div>
												<div class="field">
													<label for="st-{t.id}">Type</label>
													<select id="st-{t.id}" bind:value={settingType}>
														<option value="string">string</option>
														<option value="number">number</option>
														<option value="boolean">boolean</option>
														<option value="json">json</option>
													</select>
												</div>
												<button type="submit" disabled={saving || !settingKey.trim()}>Set</button>
											</div>
										</form>
									{:else}
										<p class="muted note">
											Settings are written for the signed-in tenant only. UpsertTenantSetting takes
											the tenant from the session, so a value typed here would land on the wrong
											one.
										</p>
									{/if}
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<style>
	h3 { margin: 1.4rem 0 0.5rem; font-size: 0.88rem; font-weight: 600; }
	.detail td { background: var(--surface-2); }
	.mine td:first-child { box-shadow: inset 3px 0 0 var(--accent, #3d6b4a); }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.8rem 0; font-size: 0.85rem; }
	.linklike {
		background: none;
		border: 0;
		padding: 0;
		color: inherit;
		font: inherit;
		cursor: pointer;
		text-decoration: underline;
	}
</style>
