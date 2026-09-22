<script lang="ts">
	import { ApiError, type Batch, type ListWarehousesResponse, type StockMovement } from '$lib/api';
	import { instant, label, quantity, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const warehouses = new Task<ListWarehousesResponse>();
	const movements = new Task<{ movements: StockMovement[] }>();
	const expiring = new Task<{ batches: Batch[] }>();

	let selected = $state('');

	function loadWarehouses() {
		warehouses.run((s) => settings.api().commerce.listWarehouses({ signal: s }));
	}
	$effect(loadWarehouses);
	$effect(() => {
		expiring.run((s) => settings.api().commerce.listExpiringBatches({ signal: s }));
	});
	$effect(() => {
		if (selected) movements.run((s) => settings.api().commerce.listStockMovements({ warehouse_id: selected }, { signal: s }));
	});

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let adding = $state(false);
	let wh = $state({ name: '', code: '', address: '', manager_id: '', status: 'active' });
	let move = $state({ sku_id: '', movement_type: 'in', quantity: '', reference_id: '', reference_type: '', notes: '' });
	let batch = $state({ sku_id: '', batch_number: '', quantity: '', manufactured_at: today(), expires_at: '' });
	let showBatch = $state(false);

	function asApiError(c: unknown): ApiError {
		return c instanceof ApiError ? c : new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true; saveError = undefined;
		try { await fn(); after(); }
		catch (c) { saveError = asApiError(c); }
		finally { saving = false; }
	}

	function expired(b: Batch): boolean {
		if (!b.expires_at) return false;
		const at = Date.parse(b.expires_at);
		return Number.isFinite(at) && at < Date.now();
	}
</script>

<div class="page-head">
	<h1>Stock</h1>
	<p>
		Warehouses, what moved, and what is about to go off. Direction is carried by the movement type
		and never by a negative quantity — and an adjustment is not a delta: it states the count after a
		stocktake and replaces the running total.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<button class="ghost" onclick={loadWarehouses} disabled={warehouses.pending}>Refresh</button>
	<button onclick={() => { adding = !adding; saveError = undefined; }}>{adding ? 'Cancel' : 'Add a warehouse'}</button>
</div>

{#if adding}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createWarehouse({ ...wh, created_by: settings.actorOrUnknown }), () => { adding = false; loadWarehouses(); }); }}>
		<div class="controls">
			<div class="field"><label for="wn">Name</label><input id="wn" bind:value={wh.name} /></div>
			<div class="field"><label for="wc">Code</label><input id="wc" bind:value={wh.code} size="10" /></div>
			<div class="field grow"><label for="wa">Address</label><input id="wa" bind:value={wh.address} /></div>
			<div class="field"><label for="wm">Manager</label><input id="wm" bind:value={wh.manager_id} size="20" /></div>
			<button type="submit" disabled={saving || !wh.name.trim()}>Add</button>
		</div>
	</form>
{/if}

<Await task={warehouses} retry={loadWarehouses} isEmpty={(d) => (d.warehouses ?? []).length === 0} empty="No warehouses.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>Code</th><th>Name</th><th>Where</th><th>Status</th><th></th></tr></thead>
				<tbody>
					{#each d.warehouses as w (w.id)}
						<tr>
							<td class="mono">{w.code}</td>
							<td>{w.name}</td>
							<td class="muted">{w.address || '—'}</td>
							<td><Chip tone={w.status === 'active' ? 'calm' : 'neutral'}>{label(w.status)}</Chip></td>
							<td><button class="ghost" onclick={() => (selected = selected === w.id ? '' : w.id)}>{selected === w.id ? 'Close' : 'Movements'}</button></td>
						</tr>
						{#if selected === w.id}
							<tr class="detail">
								<td colspan="5">
									<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.adjustStock({
										warehouse_id: w.id, sku_id: move.sku_id.trim(), movement_type: move.movement_type,
										quantity: move.quantity.trim(), reference_id: move.reference_id.trim(),
										reference_type: move.reference_type.trim(), notes: move.notes.trim(),
										moved_at: new Date().toISOString(), moved_by: settings.actorOrUnknown,
										created_by: settings.actorOrUnknown
									}), () => { move.quantity = ''; movements.run((s) => settings.api().commerce.listStockMovements({ warehouse_id: w.id }, { signal: s })); }); }}>
										<div class="controls">
											<div class="field"><label for="ms-{w.id}">SKU</label><input id="ms-{w.id}" bind:value={move.sku_id} size="22" /></div>
											<div class="field">
												<label for="mt-{w.id}">Movement</label>
												<select id="mt-{w.id}" bind:value={move.movement_type}>
													<option value="in">In</option><option value="out">Out</option>
													<option value="transfer">Transfer</option><option value="adjustment">Adjustment (counted total)</option>
												</select>
											</div>
											<div class="field"><label for="mq-{w.id}">Quantity</label><input id="mq-{w.id}" bind:value={move.quantity} size="9" inputmode="decimal" /></div>
											<div class="field grow"><label for="mn-{w.id}">Notes</label><input id="mn-{w.id}" bind:value={move.notes} /></div>
											<button type="submit" disabled={saving || !move.sku_id.trim() || !move.quantity.trim()}>Record</button>
											<button type="button" class="ghost" onclick={() => (showBatch = !showBatch)}>{showBatch ? 'Hide batch' : 'Add a batch'}</button>
										</div>
										{#if move.movement_type === 'adjustment'}
											<p class="muted note">
												An adjustment replaces the running total. Enter what was counted on the
												shelf, not the difference — the service sets stock to this number.
											</p>
										{/if}
									</form>

									{#if showBatch}
										<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createBatch({
											warehouse_id: w.id, sku_id: batch.sku_id.trim(), batch_number: batch.batch_number.trim(),
											quantity: batch.quantity.trim(),
											manufactured_at: batch.manufactured_at ? new Date(batch.manufactured_at).toISOString() : undefined,
											expires_at: batch.expires_at ? new Date(batch.expires_at).toISOString() : undefined,
											status: 'available', created_by: settings.actorOrUnknown
										}), () => { showBatch = false; expiring.run((s) => settings.api().commerce.listExpiringBatches({ signal: s })); }); }}>
											<div class="controls">
												<div class="field"><label for="bs-{w.id}">SKU</label><input id="bs-{w.id}" bind:value={batch.sku_id} size="22" /></div>
												<div class="field"><label for="bn-{w.id}">Batch</label><input id="bn-{w.id}" bind:value={batch.batch_number} size="16" /></div>
												<div class="field"><label for="bq-{w.id}">Quantity</label><input id="bq-{w.id}" bind:value={batch.quantity} size="9" inputmode="decimal" /></div>
												<div class="field"><label for="bm-{w.id}">Made</label><input id="bm-{w.id}" type="date" bind:value={batch.manufactured_at} /></div>
												<div class="field"><label for="be-{w.id}">Expires</label><input id="be-{w.id}" type="date" bind:value={batch.expires_at} /></div>
												<button type="submit" disabled={saving || !batch.batch_number.trim()}>Add batch</button>
											</div>
										</form>
									{/if}

									<Await task={movements} isEmpty={(m) => (m.movements ?? []).length === 0} empty="Nothing has moved here.">
										{#snippet children(m)}
											<div class="tablewrap">
												<table>
													<thead><tr><th>When</th><th>SKU</th><th>Movement</th><th class="num">Quantity</th><th>Notes</th></tr></thead>
													<tbody>
														{#each m.movements as mv (mv.id)}
															<tr>
																<td>{instant(mv.moved_at)}</td>
																<td class="mono">{mv.sku_id}</td>
																<td><Chip tone={mv.movement_type === 'adjustment' ? 'attention' : 'neutral'}>{label(mv.movement_type)}</Chip></td>
																<td class="num">{quantity(mv.quantity)}</td>
																<td class="muted">{mv.notes || '—'}</td>
															</tr>
														{/each}
													</tbody>
												</table>
											</div>
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

<h2>Expiring</h2>
<Await task={expiring} isEmpty={(d) => (d.batches ?? []).length === 0} empty="Nothing is close to its date.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>Batch</th><th>SKU</th><th class="num">Quantity</th><th>Expires</th><th>Status</th></tr></thead>
				<tbody>
					{#each d.batches as b (b.id)}
						<tr>
							<td class="mono">{b.batch_number}</td>
							<td class="mono">{b.sku_id}</td>
							<td class="num">{quantity(b.quantity)}</td>
							<td>
								<Chip tone={expired(b) ? 'critical' : 'attention'}>{b.expires_at ? instant(b.expires_at) : '—'}</Chip>
							</td>
							<td>{label(b.status)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<style>
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
</style>
