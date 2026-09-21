<script lang="ts">
	import { page } from '$app/state';
	import { ApiError, type Allocation, type PoolResponse, type Valuation } from '$lib/api';
	import { instant, label } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const id = $derived(page.params.id ?? '');

	const pool = new Task<PoolResponse>();
	const milk = new Task<{ producer_milk: { id: string; producer_ref: string; quantity: string; origin_kind: string }[] }>();
	const uses = new Task<{ utilisations: { id: string; class: string; quantity: string; price: string; price_scale: number }[] }>();
	const valuation = new Task<{ valuation: Valuation }>();
	const allocations = new Task<{ allocations: Allocation[] }>();
	const correction = new Task<{ outcome: string; reason: string; event_kind?: string; adjustment: string }>();

	function load() {
		const api = settings.api().money;
		pool.run((s) => api.getPool(id, { signal: s }));
		milk.run((s) => api.listProducerMilk(id, { signal: s }));
		uses.run((s) => api.listUtilisations(id, { signal: s }));
		valuation.run((s) => api.getValuation(id, { signal: s }));
		allocations.run((s) => api.listAllocations({ pool_id: id }, { signal: s }));
	}

	$effect(() => { void id; if (id) load(); });

	const p = $derived(pool.data?.pool);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let addMilk = $state({ producer_ref: '', quantity: '' });
	let addUse = $state({ class: 'CLASS_I', quantity: '', price: '', price_scale: 2 });
	let prices = $state('FAT 46000 4\nSNF 21000 4');

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError ? cause : new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	function componentPrices() {
		return prices.split('\n').map((l) => l.trim()).filter(Boolean).map((l) => {
			const [component = '', price = '', scale = '0'] = l.split(/\s+/);
			return { component, price, scale: Number(scale) };
		});
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true; saveError = undefined;
		try { await fn(); after(); load(); }
		catch (cause) { saveError = asApiError(cause); }
		finally { saving = false; }
	}
</script>

<p><a href="/money/pools">← Pools</a></p>

<div class="page-head">
	<h1>{p?.name ?? 'Pool'}</h1>
	<p>
		{#if p}{instant(p.period_start)} — {instant(p.period_end)}, in {label(p.unit)}, {p.currency}.{/if}
	</p>
</div>

<ErrorNote error={saveError} />

<Await task={pool} retry={load} isEmpty={(d) => !d.pool} empty="No such pool.">
	{#snippet children()}
		{#if p}
			<p><Chip tone={p.status === 'SETTLED' ? 'calm' : p.status === 'VALUED' ? 'attention' : 'neutral'}>{label(p.status)}</Chip></p>
		{/if}
	{/snippet}
</Await>

<section>
	<h2>Whose milk is in it</h2>
	<form class="controls" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.addProducerMilk({ pool_id: id, producer_ref: addMilk.producer_ref.trim(), quantity: addMilk.quantity.trim(), actor: settings.actorOrUnknown }), () => (addMilk = { producer_ref: '', quantity: '' })); }}>
		<div class="field"><label for="mp">Producer</label><input id="mp" bind:value={addMilk.producer_ref} size="22" /></div>
		<div class="field"><label for="mq">Quantity</label><input id="mq" bind:value={addMilk.quantity} size="10" inputmode="decimal" /></div>
		<button type="submit" disabled={saving || !addMilk.producer_ref.trim() || !addMilk.quantity.trim()}>Add</button>
	</form>
	<Await task={milk} isEmpty={(d) => (d.producer_milk ?? []).length === 0} empty="Nothing gathered into this pool yet.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Producer</th><th class="num">Quantity</th><th>Origin</th></tr></thead>
					<tbody>
						{#each d.producer_milk as m (m.id)}
							<tr><td class="mono">{m.producer_ref}</td><td class="num">{m.quantity}</td><td>{label(m.origin_kind)}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>What it was sold as</h2>
	<form class="controls" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.recordUtilisation({ pool_id: id, class: addUse.class, quantity: addUse.quantity.trim(), price: addUse.price.trim(), price_scale: Number(addUse.price_scale), actor: settings.actorOrUnknown }), () => (addUse.quantity = '')); }}>
		<div class="field">
			<label for="uc">Class</label>
			<select id="uc" bind:value={addUse.class}>
				<option value="CLASS_I">Class I</option><option value="CLASS_II">Class II</option>
				<option value="CLASS_III">Class III</option><option value="CLASS_IV">Class IV</option>
			</select>
		</div>
		<div class="field"><label for="uq">Quantity</label><input id="uq" bind:value={addUse.quantity} size="10" inputmode="decimal" /></div>
		<div class="field"><label for="up">Price (digits)</label><input id="up" bind:value={addUse.price} size="8" /></div>
		<div class="field"><label for="us">Scale</label><input id="us" type="number" min="0" max="9" bind:value={addUse.price_scale} size="3" /></div>
		<button type="submit" disabled={saving || !addUse.quantity.trim() || !addUse.price.trim()}>Record</button>
	</form>
	<Await task={uses} isEmpty={(d) => (d.utilisations ?? []).length === 0} empty="No utilisations recorded.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Class</th><th class="num">Quantity</th><th class="num">Price</th></tr></thead>
					<tbody>
						{#each d.utilisations as u (u.id)}
							<tr><td>{label(u.class)}</td><td class="num">{u.quantity}</td><td class="num">{u.price}<span class="muted">/{u.price_scale}</span></td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Valuing it</h2>
	<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.valuePool({ pool_id: id, component_prices: componentPrices(), actor: settings.actorOrUnknown })); }}>
		<div class="field wide">
			<label for="cp">Component prices — one per line, "component digits scale"</label>
			<textarea id="cp" rows="3" bind:value={prices}></textarea>
		</div>
		<div class="controls">
			<button type="submit" disabled={saving}>{saving ? 'Valuing…' : 'Value the pool'}</button>
			{#if p?.status === 'VALUED'}
				<button type="button" class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.settlePool(id, settings.actorOrUnknown))}>
					Settle
				</button>
			{/if}
			<button type="button" class="ghost" disabled={saving}
				onclick={() => correction.run((s) => settings.api().money.applyCorrection({ pool_id: id, component_prices: componentPrices(), actor: settings.actorOrUnknown }, { signal: s }))}>
				Apply a correction
			</button>
		</div>
	</form>

	{#if correction.settled && correction.data}
		<p class="panel">
			<Chip tone={correction.data.outcome === 'APPLIED' ? 'calm' : 'neutral'}>{label(correction.data.outcome)}</Chip>
			{correction.data.reason}
			{#if correction.data.event_kind}<span class="muted">— {label(correction.data.event_kind)}, {correction.data.adjustment}</span>{/if}
		</p>
		<p class="muted note">
			The retroactivity policy decides this, not the operator. An outcome that is not "applied" is
			the policy working rather than a failure.
		</p>
	{/if}

	<Await task={valuation} isEmpty={(d) => !d.valuation} empty="Not valued yet.">
		{#snippet children(d)}
			<dl class="kv">
				<dt>Classified value</dt><dd>{formatExact(d.valuation.classified_value, d.valuation.currency)}</dd>
				<dt>Component value</dt><dd>{formatExact(d.valuation.component_value, d.valuation.currency)}</dd>
				<dt>Producer settlement fund</dt>
				<dd><strong>{formatExact(d.valuation.producer_settlement_fund, d.valuation.currency)}</strong> <span class="muted">the residual</span></dd>
				<dt>Total quantity</dt><dd>{d.valuation.total_quantity}</dd>
				<dt>Blend price</dt><dd>{d.valuation.blend_price}<span class="muted">/{d.valuation.blend_price_scale}</span></dd>
				<dt>Computed</dt><dd>{instant(d.valuation.computed_at)}</dd>
			</dl>
			{#if (d.valuation.rounding_trail ?? []).length}
				<h3>Where precision was lost</h3>
				<div class="tablewrap">
					<table>
						<thead><tr><th>Step</th><th>Mode</th><th class="num">From</th><th class="num">To</th><th class="num">Discarded</th><th class="num">Result</th></tr></thead>
						<tbody>
							{#each d.valuation.rounding_trail ?? [] as r, i (i)}
								<tr>
									<td>{label(r.operation)}</td><td>{label(r.mode)}</td>
									<td class="num">{r.from_scale}</td><td class="num">{r.to_scale}</td>
									<td class="num">{r.discarded}</td><td class="num">{r.result}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<p class="muted note">
					Every precision-losing operation, recorded as it happened. A settlement that carries an
					unexplainable residual is one nobody can defend.
				</p>
			{/if}
		{/snippet}
	</Await>
</section>

<section>
	<h2>What each producer gets</h2>
	<Await task={allocations} isEmpty={(d) => (d.allocations ?? []).length === 0} empty="No allocations; the pool has not been valued.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Producer</th><th class="num">Component</th><th class="num">Fund share</th><th class="num">Total</th></tr></thead>
					<tbody>
						{#each d.allocations as a (a.id)}
							<tr>
								<td class="mono">{a.producer_ref}</td>
								<td class="num">{formatExact(a.component_value, a.currency)}</td>
								<td class="num">{formatExact(a.fund_share, a.currency)}</td>
								<td class="num"><strong>{formatExact(a.total, a.currency)}</strong></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<style>
	section { margin-top: 2rem; }
	h3 { margin: 1.2rem 0 0.5rem; font-size: 0.86rem; }
	.note { max-width: var(--measure); font-size: 0.82rem; }
	.field.wide { max-width: var(--measure); margin: 0.9rem 0; }
	.field.wide textarea { width: 100%; font-family: var(--mono); }
</style>
