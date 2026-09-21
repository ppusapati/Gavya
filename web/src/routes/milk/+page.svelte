<script lang="ts">
	import { ApiError, type ListSessionsResponse, type MilkSession } from '$lib/api';
	import { label, quantity } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const PAGE = 50;

	let offset = $state(0);
	const task = new Task<ListSessionsResponse>();
	const records = new Task<{ records: { id: string; cattle_id: string; quantity_liters: string }[] }>();

	function load() {
		task.run((signal) => settings.api().herd.listSessions({ limit: PAGE, offset }, { signal }));
	}

	$effect(() => {
		void offset;
		load();
	});

	const sessions = $derived(task.data?.sessions ?? []);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	/* ---- opening a booth ---- */

	let opening = $state(false);
	let open = $state({ cattle_id: '', shift_type: 'morning' });

	/* ---- the open session ---- */

	let current = $state<string | undefined>(undefined);
	let entry = $state({ cattle_id: '', quantity_liters: '' });
	let qual = $state({ record_id: '', fat_percent: '', snf_percent: '', lactose: '' });
	let showQuality = $state(false);

	function select(s: MilkSession) {
		current = current === s.id ? undefined : s.id;
		entry = { cattle_id: s.cattle_id, quantity_liters: '' };
		showQuality = false;
		saveError = undefined;
		if (current) loadRecords(current);
	}

	function loadRecords(sessionId: string) {
		records.run((signal) => settings.api().herd.listSessionRecords(sessionId, { signal }));
	}

	const total = $derived(
		// Summed as a string would be wrong and summed as a float would be worse.
		// The count is honest; the total belongs to the service that holds the
		// exact figures, and GetDailyYield is where it is asked for.
		(records.data?.records ?? []).length
	);
</script>

<div class="page-head">
	<h1>Collection</h1>
	<p>
		A booth opens a session for an animal and records against it all morning. Quantities are exact
		decimals and are sent exactly as typed — nothing here turns "6.250" into a number on the way,
		because the settlement drawn from it has to be a figure a member can check.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { opening = !opening; saveError = undefined; }}>
		{opening ? 'Cancel' : 'Open a session'}
	</button>
</div>

<ErrorNote error={saveError} />

{#if opening}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().herd.createSession({
						cattle_id: open.cattle_id.trim(),
						shift_type: open.shift_type,
						// The tenant's own timezone decides which day a collection falls
						// on. The service refuses to guess it, and the browser's is not
						// the right answer — a booth in Baramati and a reviewer in London
						// must agree about the fourteenth.
						timezone: settings.timezone,
						created_by: settings.actorOrUnknown
					}),
				() => {
					opening = false;
					open = { cattle_id: '', shift_type: 'morning' };
					load();
				}
			);
		}}
	>
		<h2>Open a session</h2>
		<div class="controls">
			<div class="field">
				<label for="oc">Animal</label>
				<input id="oc" bind:value={open.cattle_id} size="26" />
			</div>
			<div class="field">
				<label for="os">Shift</label>
				<select id="os" bind:value={open.shift_type}>
					<option value="morning">Morning</option>
					<option value="afternoon">Afternoon</option>
					<option value="evening">Evening</option>
				</select>
			</div>
			<div class="field">
				<label for="oz">Timezone</label>
				<input id="oz" value={settings.timezone} size="16" readonly />
			</div>
			<button type="submit" disabled={saving || !open.cattle_id.trim()}>
				{saving ? 'Opening…' : 'Open'}
			</button>
		</div>
	</form>
{/if}

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.sessions ?? []).length === 0}
	empty="No sessions. Nothing has been collected, or nothing has been opened."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Session</th><th>Animal</th><th>Shift</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each sessions as s (s.id)}
						<tr>
							<td class="mono">{s.id}</td>
							<td><a href="/herd/{s.cattle_id}" class="mono">{s.cattle_id}</a></td>
							<td>{label(s.shift_type)}</td>
							<td>
								<Chip tone={s.status === 'closed' ? 'neutral' : 'calm'}>{label(s.status)}</Chip>
							</td>
							<td>
								<button class="ghost" onclick={() => select(s)}>
									{current === s.id ? 'Close' : 'Open'}
								</button>
								{#if s.status !== 'closed'}
									<button
										class="ghost"
										onclick={() =>
											run(
												() =>
													settings.api().herd.updateSessionStatus({
														id: s.id,
														status: 'closed',
														updated_by: settings.actorOrUnknown
													}),
												load
											)}
										disabled={saving}
									>
										End
									</button>
								{/if}
							</td>
						</tr>
						{#if current === s.id}
							<tr class="detail">
								<td colspan="5">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().herd.recordMilk({
														session_id: s.id,
														cattle_id: entry.cattle_id.trim() || s.cattle_id,
														quantity_liters: entry.quantity_liters.trim(),
														created_by: settings.actorOrUnknown
													}),
												() => {
													entry.quantity_liters = '';
													loadRecords(s.id);
												}
											);
										}}
									>
										<div class="controls">
											<div class="field">
												<label for="rq-{s.id}">Litres</label>
												<input
													id="rq-{s.id}"
													bind:value={entry.quantity_liters}
													size="10"
													inputmode="decimal"
													placeholder="6.250"
												/>
											</div>
											<button type="submit" disabled={saving || !entry.quantity_liters.trim()}>
												{saving ? 'Recording…' : 'Record'}
											</button>
											<button type="button" class="ghost" onclick={() => (showQuality = !showQuality)}>
												{showQuality ? 'Hide quality' : 'Record quality'}
											</button>
										</div>
									</form>

									{#if showQuality}
										<form
											onsubmit={(e) => {
												e.preventDefault();
												run(
													() =>
														settings.api().herd.recordQuality({
															record_id: qual.record_id.trim(),
															fat_percent: qual.fat_percent.trim(),
															snf_percent: qual.snf_percent.trim(),
															lactose: qual.lactose.trim()
														}),
													() => {
														qual = { record_id: '', fat_percent: '', snf_percent: '', lactose: '' };
														loadRecords(s.id);
													}
												);
											}}
										>
											<div class="controls">
												<div class="field">
													<label for="qr-{s.id}">Record</label>
													<input id="qr-{s.id}" bind:value={qual.record_id} size="26" />
												</div>
												<div class="field">
													<label for="qf-{s.id}">Fat %</label>
													<input id="qf-{s.id}" bind:value={qual.fat_percent} size="6" inputmode="decimal" />
												</div>
												<div class="field">
													<label for="qs-{s.id}">SNF %</label>
													<input id="qs-{s.id}" bind:value={qual.snf_percent} size="6" inputmode="decimal" />
												</div>
												<div class="field">
													<label for="ql-{s.id}">Lactose</label>
													<input id="ql-{s.id}" bind:value={qual.lactose} size="6" inputmode="decimal" placeholder="if measured" />
												</div>
												<button type="submit" disabled={saving || !qual.record_id.trim()}>
													{saving ? 'Recording…' : 'Record'}
												</button>
											</div>
										</form>
									{/if}

									<Await
										task={records}
										isEmpty={(d) => (d.records ?? []).length === 0}
										empty="Nothing recorded against this session yet."
									>
										{#snippet children(d)}
											<div class="tablewrap">
												<table>
													<thead><tr><th>Record</th><th>Animal</th><th class="num">Litres</th></tr></thead>
													<tbody>
														{#each d.records as r (r.id)}
															<tr>
																<td class="mono">
																	<button class="link" onclick={() => (qual.record_id = r.id)}>{r.id}</button>
																</td>
																<td class="mono">{r.cattle_id}</td>
																<td class="num">{quantity(r.quantity_liters)}</td>
															</tr>
														{/each}
													</tbody>
												</table>
											</div>
											<p class="muted note">
												{total} collection{total === 1 ? '' : 's'} on this session. The day's total for
												an animal is on its own page, because the exact sum belongs to the service
												that holds the exact figures rather than to a column added up in a browser.
											</p>
										{/snippet}
									</Await>
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<div class="pager">
	<button class="ghost" disabled={offset === 0 || task.pending} onclick={() => (offset = Math.max(0, offset - PAGE))}>
		← Previous
	</button>
	<span class="muted">Sessions {offset + 1}–{offset + sessions.length}</span>
	<button class="ghost" disabled={sessions.length < PAGE || task.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	.detail td {
		background: var(--surface-2);
	}

	.note {
		max-width: var(--measure);
		margin: 0.8rem 0 0;
		font-size: 0.82rem;
	}

	.link {
		background: none;
		border: 0;
		padding: 0;
		font: inherit;
		color: var(--link, inherit);
		cursor: pointer;
		text-decoration: underline;
	}

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
