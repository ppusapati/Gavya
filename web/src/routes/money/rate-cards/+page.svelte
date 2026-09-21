<script lang="ts">
	import { ApiError, type ListRateCardsResponse, type RateCard, type RateCardResponse } from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListRateCardsResponse>();
	const inForce = new Task<RateCardResponse>();
	const one = new Task<RateCardResponse>();

	let at = $state(today());

	function load() {
		task.run((signal) => settings.api().money.listRateCards({}, { signal }));
	}
	$effect(load);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let declaring = $state(false);
	let form = $state({
		name: '', kind: 'CHART', currency: 'INR', amount_scale: 2,
		basis: 'PER_LITRE', between_points: 'INTERPOLATE', outside_chart: 'CLAMP',
		rounding: 'HALF_UP', valid_from: today()
	});
	let cells = $state('4.00 8.50 34.50\n4.60 8.70 34.50');

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError ? cause : new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	/** Each line is "fat snf rate", at the scale the operator wrote them. */
	function parseCells() {
		return cells
			.split('\n')
			.map((l) => l.trim())
			.filter(Boolean)
			.map((l) => {
				const [fat = '', snf = '', rate = ''] = l.split(/\s+/);
				return {
					fat: { value: fat.replace('.', ''), scale: (fat.split('.')[1] ?? '').length },
					snf: { value: snf.replace('.', ''), scale: (snf.split('.')[1] ?? '').length },
					rate: rate.replace('.', ''),
					rate_scale: (rate.split('.')[1] ?? '').length
				};
			});
	}

	async function declare(event: SubmitEvent) {
		event.preventDefault();
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await settings.api().money.declareRateCard({
				name: form.name.trim(),
				kind: form.kind,
				currency: form.currency.trim().toUpperCase(),
				amount_scale: Number(form.amount_scale),
				basis: form.kind === 'CHART' ? form.basis : undefined,
				between_points: form.kind === 'CHART' ? form.between_points : undefined,
				outside_chart: form.kind === 'CHART' ? form.outside_chart : undefined,
				rounding: form.rounding,
				cells: form.kind === 'CHART' ? parseCells() : undefined,
				valid_from: new Date(form.valid_from).toISOString(),
				actor: settings.actorOrUnknown
			});
			declaring = false;
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	function complete(c: RateCard): boolean {
		return c.kind !== 'CHART' || (!!c.basis && !!c.between_points && !!c.outside_chart);
	}
</script>

<div class="page-head">
	<h1>Rate cards</h1>
	<p>
		What a society's milk is worth, and from when. A chart card has to say its basis, what happens
		between its points and what happens outside it — a card that leaves any of those out is one
		whose answer depends on whoever reads it, and the service refuses to store one.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { declaring = !declaring; saveError = undefined; }}>
		{declaring ? 'Cancel' : 'Declare a card'}
	</button>
	<div class="field">
		<label for="at">In force on</label>
		<input id="at" type="date" bind:value={at} />
	</div>
	<button class="ghost" onclick={() => inForce.run((s) => settings.api().money.getRateCardInForce(new Date(at).toISOString(), { signal: s }))}>
		Which card?
	</button>
</div>

<ErrorNote error={saveError} />

{#if inForce.settled}
	<Await task={inForce} isEmpty={(d) => !d.rate_card} empty="No card was in force on that day.">
		{#snippet children(d)}
			<p class="panel">
				On {at}: <strong>{d.rate_card.name}</strong>
				<span class="muted">({label(d.rate_card.kind)}, {d.rate_card.currency}, rounding {label(d.rate_card.rounding)})</span>
			</p>
		{/snippet}
	</Await>
{/if}

{#if declaring}
	<form class="panel" onsubmit={declare}>
		<div class="controls">
			<div class="field grow"><label for="rn">Name</label><input id="rn" bind:value={form.name} /></div>
			<div class="field">
				<label for="rk">Kind</label>
				<select id="rk" bind:value={form.kind}><option value="CHART">Chart</option><option value="FORMULA">Formula</option></select>
			</div>
			<div class="field"><label for="rc">Currency</label><input id="rc" bind:value={form.currency} size="4" /></div>
			<div class="field"><label for="rs">Scale</label><input id="rs" type="number" min="0" max="9" bind:value={form.amount_scale} size="3" /></div>
			<div class="field">
				<label for="rr">Rounding</label>
				<select id="rr" bind:value={form.rounding}>
					<option value="HALF_UP">Half up</option><option value="HALF_EVEN">Half even</option>
					<option value="HALF_DOWN">Half down</option><option value="UP">Up</option><option value="DOWN">Down</option>
				</select>
			</div>
			<div class="field"><label for="rv">From</label><input id="rv" type="date" bind:value={form.valid_from} /></div>
		</div>
		{#if form.kind === 'CHART'}
			<div class="controls">
				<div class="field">
					<label for="rb">Basis</label>
					<select id="rb" bind:value={form.basis}><option value="PER_LITRE">Per litre</option><option value="PER_KG">Per kg</option></select>
				</div>
				<div class="field">
					<label for="rp">Between points</label>
					<select id="rp" bind:value={form.between_points}>
						<option value="INTERPOLATE">Interpolate</option><option value="BAND">Band</option><option value="EXACT_ONLY">Exact only</option>
					</select>
				</div>
				<div class="field">
					<label for="ro">Outside the chart</label>
					<select id="ro" bind:value={form.outside_chart}><option value="CLAMP">Clamp</option><option value="REFUSE">Refuse</option></select>
				</div>
			</div>
			<div class="field wide">
				<label for="rcells">Cells — one per line, "fat snf rate"</label>
				<textarea id="rcells" rows="5" bind:value={cells}></textarea>
			</div>
		{/if}
		<button type="submit" disabled={saving || !form.name.trim()}>{saving ? 'Declaring…' : 'Declare'}</button>
	</form>
{/if}

<Await {task} retry={load} isEmpty={(d) => (d.rate_cards ?? []).length === 0} empty="No rate cards declared.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>Card</th><th>Kind</th><th>Basis</th><th>Rounding</th><th>From</th><th>To</th><th></th></tr></thead>
				<tbody>
					{#each d.rate_cards as c (c.id)}
						<tr>
							<td>{c.name}</td>
							<td>{label(c.kind)}</td>
							<td>{c.basis ? label(c.basis) : '—'}</td>
							<td>{label(c.rounding)}</td>
							<td>{instant(c.valid_from)}</td>
							<td>
								{#if c.valid_to}{instant(c.valid_to)}{:else}<Chip tone="calm">open</Chip>{/if}
							</td>
							<td>
								{#if !complete(c)}<Chip tone="critical">incomplete</Chip>{/if}
								<button class="ghost" onclick={() => one.run((s) => settings.api().money.getRateCard(c.id, { signal: s }))}>Cells</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

{#if one.settled}
	<Await task={one} isEmpty={(d) => !d.rate_card} empty="">
		{#snippet children(d)}
			<h2>{d.rate_card.name}</h2>
			{#if (d.rate_card.cells ?? []).length}
				<div class="tablewrap">
					<table>
						<thead><tr><th class="num">Fat</th><th class="num">SNF</th><th class="num">Rate</th></tr></thead>
						<tbody>
							{#each d.rate_card.cells ?? [] as cell, i (i)}
								<tr>
									<td class="num">{cell.fat.value}<span class="muted">/{cell.fat.scale}</span></td>
									<td class="num">{cell.snf.value}<span class="muted">/{cell.snf.scale}</span></td>
									<td class="num">{cell.rate}<span class="muted">/{cell.rate_scale}</span></td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<p class="muted note">
					Each figure is its digits and the scale they are written at, which is how the platform
					holds a decimal exactly. 3450 at scale 2 is 34.50.
				</p>
			{/if}
			{#if (d.rate_card.terms ?? []).length}
				<div class="tablewrap">
					<table>
						<thead><tr><th>Component</th><th class="num">Rate</th></tr></thead>
						<tbody>
							{#each d.rate_card.terms ?? [] as t, i (i)}
								<tr><td>{label(t.component)}</td><td class="num">{t.rate}<span class="muted">/{t.rate_scale}</span></td></tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		{/snippet}
	</Await>
{/if}

<style>
	.note { max-width: var(--measure); font-size: 0.82rem; }
	.field.grow { flex: 1 1 16rem; }
	.field.wide { max-width: var(--measure); margin: 0.9rem 0; }
	.field.wide textarea { width: 100%; font-family: var(--mono); }
</style>
