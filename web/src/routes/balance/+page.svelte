<script lang="ts">
	import {
		ApiError,
		UNITS,
		type BalanceWindow,
		type ListWindowsResponse,
		type WindowStatus
	} from '$lib/api';
	import { instant, label, shortId, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const STATUSES: WindowStatus[] = ['OPEN', 'RECONCILED', 'ACCEPTED'];
	const PAGE = 50;

	let status = $state('');
	let offset = $state(0);
	const task = new Task<ListWindowsResponse>();

	function load() {
		task.run((signal) => settings.api().listWindows({ status, limit: PAGE, offset }, { signal }));
	}

	$effect(() => {
		void [status, offset];
		load();
	});

	const windows = $derived(task.data?.windows ?? []);

	function tone(w: BalanceWindow) {
		return w.status === 'ACCEPTED' ? 'calm' : w.status === 'OPEN' ? 'attention' : 'neutral';
	}

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
	let nw = $state({ route_ref: '', period_start: today(), period_end: today(), unit: '' });

	/**
	 * What a window's instruments can establish, asked before anything is
	 * reconciled.
	 *
	 * This is the useful time to find out that the only leg anybody can verify
	 * is the tanker. Afterwards, "the window reconciled" reads as a clean bill
	 * of health whether or not a single measurement in it was checkable.
	 */
	const answerable = new Task<import('$lib/api').ObservabilityResponse>();
	const flows = new Task<import('$lib/api').ListFlowsResponse>();
	let openWindow = $state<string | undefined>(undefined);

	let flow = $state({
		flow_id: '',
		from_node: '',
		to_node: '',
		measured: '',
		standard_uncertainty: '',
		unmeasured: false,
		observation_ref: ''
	});

	function inspect(w: BalanceWindow) {
		if (openWindow === w.id) {
			openWindow = undefined;
			answerable.reset();
			flows.reset();
			return;
		}
		openWindow = w.id;
		saveError = undefined;
		flows.run((s) => settings.api().listFlows(w.id, { signal: s }));
		answerable.run((s) => settings.api().observability(w.id, { signal: s }));
	}

	function refresh(id: string) {
		flows.run((s) => settings.api().listFlows(id, { signal: s }));
		answerable.run((s) => settings.api().observability(id, { signal: s }));
	}
</script>

<div class="page-head">
	<h1>Mass balance</h1>
	<p>
		Milk that goes into a route has to come out of it. A balance window is one route over one
		period, and reconciling it distributes the difference across the flows in proportion to how
		well each is measured — so a gap that a badly measured leg could account for is not charged to
		a well measured one.
	</p>
</div>

<div class="controls">
	<div class="field">
		<label for="st">Status</label>
		<select
			id="st"
			value={status}
			onchange={(e) => {
				status = e.currentTarget.value;
				offset = 0;
			}}
		>
			<option value="">Any</option>
			{#each STATUSES as s (s)}<option value={s}>{label(s)}</option>{/each}
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>
		{creating ? 'Cancel' : 'Open a window'}
	</button>
</div>

<ErrorNote error={saveError} />

{#if creating}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().createWindow({
						route_ref: nw.route_ref.trim(),
						period_start: nw.period_start,
						period_end: nw.period_end,
						unit: nw.unit,
						actor: settings.actorOrUnknown
					}),
				() => {
					creating = false;
					nw = { ...nw, route_ref: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field grow"><label for="wr">Route</label><input id="wr" bind:value={nw.route_ref} /></div>
			<div class="field"><label for="ws">From</label><input id="ws" type="date" bind:value={nw.period_start} /></div>
			<div class="field"><label for="we">To</label><input id="we" type="date" bind:value={nw.period_end} /></div>
			<div class="field">
				<label for="wu">Unit</label>
				<select id="wu" bind:value={nw.unit}>
					<option value="">—</option>
					{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
				</select>
			</div>
			<button type="submit" disabled={saving || !nw.route_ref.trim() || !nw.unit}>Open</button>
		</div>
		<p class="muted note">
			Every flow in a window is in the window's unit. There is no default: litres and kilograms
			differ by about three per cent, and a window that silently mixed them would reconcile to a
			difference nobody could explain.
		</p>
	</form>
{/if}

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.windows ?? []).length === 0}
	empty="No balance window matches this filter."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Route</th>
						<th>Period</th>
						<th>Unit</th>
						<th>Status</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each windows as w (w.id)}
						<tr>
							<td class="mono">{w.route_ref}</td>
							<td>{instant(w.period_start)} → {instant(w.period_end)}</td>
							<td class="mono">{w.unit}</td>
							<td><Chip tone={tone(w)}>{label(w.status)}</Chip></td>
							<td class="actions">
								<button class="ghost" onclick={() => inspect(w)}>{openWindow === w.id ? 'Close' : 'Flows'}</button>
								<a class="rowlink" href="/balance/{w.id}" title={w.id}>{shortId(w.id)} →</a>
							</td>
						</tr>
						{#if openWindow === w.id}
							<tr class="detail">
								<td colspan="5">
									<h3>What this window can establish</h3>
									<Await task={answerable} isEmpty={() => false} empty="">
										{#snippet children(o)}
											<p class="banner">
												{#if o.fully_observable}
													<Chip tone="calm">every unmeasured leg is determined</Chip>
												{:else}
													<Chip tone="critical">{o.unobservable.length} unmeasured legs are undetermined</Chip>
												{/if}
												{#if o.fully_redundant}
													<Chip tone="calm">every measurement is checkable</Chip>
												{:else}
													<Chip tone="attention">{o.just_determined.length} measurements nothing can check</Chip>
												{/if}
											</p>
											{#if !o.fully_observable}
												<p class="warn">
													The reconciler will still print a figure for
													{o.unobservable.join(', ')} — it is one of infinitely many that fit.
												</p>
											{/if}
											{#if !o.fully_redundant}
												<p class="warn">
													Nothing in this window disagrees with {o.just_determined.join(', ')}
													however wrong they are, which makes "the window reconciled" a much
													weaker statement than it sounds.
												</p>
											{/if}
											<dl class="kv">
												<dt>Determined, unmeasured</dt><dd class="mono">{o.observable.join(', ') || '—'}</dd>
												<dt>Checkable measurements</dt><dd class="mono">{o.redundant.join(', ') || '—'}</dd>
											</dl>
										{/snippet}
									</Await>

									<h3>Flows</h3>
									<Await task={flows} isEmpty={(d) => (d.flows ?? []).length === 0} empty="No flows yet.">
										{#snippet children(d)}
											<div class="tablewrap">
												<table>
													<thead><tr><th>Leg</th><th>From</th><th>To</th><th class="num">Measured</th><th class="num">Uncertainty</th></tr></thead>
													<tbody>
														{#each d.flows as f (f.id)}
															<tr>
																<td class="mono">{f.flow_id}</td>
																<td class="mono">{f.from_node || 'boundary'}</td>
																<td class="mono">{f.to_node || 'boundary'}</td>
																<td class="num">
																	{#if f.unmeasured}
																		<Chip tone="attention">unmeasured</Chip>
																	{:else}
																		{f.measured} {w.unit}
																	{/if}
																</td>
																<td class="num">{f.standard_uncertainty || '—'}</td>
															</tr>
														{/each}
													</tbody>
												</table>
											</div>
										{/snippet}
									</Await>

									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().addFlow({
														window_id: w.id,
														flow_id: flow.flow_id.trim(),
														from_node: flow.from_node.trim(),
														to_node: flow.to_node.trim(),
														measured: flow.unmeasured ? '0' : flow.measured.trim(),
														standard_uncertainty: flow.unmeasured
															? undefined
															: flow.standard_uncertainty.trim() || undefined,
														unmeasured: flow.unmeasured,
														observation_ref: flow.observation_ref.trim() || undefined,
														actor: settings.actorOrUnknown
													}),
												() => {
													flow = { ...flow, flow_id: '', measured: '', standard_uncertainty: '' };
													refresh(w.id);
												}
											);
										}}
									>
										<div class="controls">
											<div class="field"><label for="fl-{w.id}">Leg</label><input id="fl-{w.id}" bind:value={flow.flow_id} size="14" /></div>
											<div class="field"><label for="ff-{w.id}">From</label><input id="ff-{w.id}" bind:value={flow.from_node} size="18" placeholder="empty = boundary" /></div>
											<div class="field"><label for="ft-{w.id}">To</label><input id="ft-{w.id}" bind:value={flow.to_node} size="18" placeholder="empty = boundary" /></div>
											<label class="check"><input type="checkbox" bind:checked={flow.unmeasured} /> Unmeasured</label>
											{#if !flow.unmeasured}
												<div class="field"><label for="fm-{w.id}">Measured ({w.unit})</label><input id="fm-{w.id}" bind:value={flow.measured} size="10" inputmode="decimal" /></div>
												<div class="field"><label for="fu-{w.id}">Uncertainty</label><input id="fu-{w.id}" bind:value={flow.standard_uncertainty} size="9" inputmode="decimal" /></div>
											{/if}
											<button type="submit" disabled={saving || !flow.flow_id.trim() || (!flow.unmeasured && !flow.measured.trim())}>
												Add
											</button>
										</div>
										<p class="muted note">
											An empty node is the system boundary — milk entering or leaving the network.
											Quantities are sent as decimal strings, never as JSON numbers: a float would
											already have lost the third decimal by the time it arrived.
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

<div class="pager">
	<button
		class="ghost"
		disabled={offset === 0 || task.pending}
		onclick={() => (offset = Math.max(0, offset - PAGE))}>← Previous</button
	>
	<span class="muted">Windows {offset + 1}–{offset + windows.length}</span>
	<button class="ghost" disabled={windows.length < PAGE || task.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	h3 { margin: 1.3rem 0 0.5rem; font-size: 0.88rem; font-weight: 600; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.6rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.6rem; align-items: baseline; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.5rem 0; font-size: 0.85rem; }
	.warn { max-width: var(--measure); margin: 0.5rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
