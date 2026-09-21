<script lang="ts">
	import { ApiError, type ListPoolsResponse, type RetroactivityPolicyResponse } from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListPoolsResponse>();
	const policy = new Task<RetroactivityPolicyResponse>();
	const events = new Task<{ events: { id: string; producer_ref: string; amount: string; currency: string; kind: string; created_at: string }[] }>();

	function load() {
		task.run((signal) => settings.api().money.listPools({}, { signal }));
	}
	$effect(load);
	$effect(() => {
		policy.run((signal) => settings.api().money.getEffectiveRetroactivityPolicy(undefined, { signal }));
		events.run((signal) => settings.api().money.listEconomicEvents({ limit: 25 }, { signal }));
	});

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let creating = $state(false);
	let declaring = $state(false);
	let form = $state({ name: '', period_start: today(), period_end: today(), unit: 'LITRES', currency: 'INR', amount_scale: 2, rate_card_id: '', policy_version: '' });
	let pol = $state({ name: '', mode: 'DO_NOT_REOPEN', max_lookback_days: 30, currency: 'INR', amount_scale: 2, minimum_adjustment: '', effective_from: today() });

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError ? cause : new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try { await fn(); after(); load(); }
		catch (cause) { saveError = asApiError(cause); }
		finally { saving = false; }
	}
</script>

<div class="page-head">
	<h1>Pools</h1>
	<p>
		A period's milk, valued as one. What the classes were sold for is the classified value; what the
		components were worth is the component value; the producer settlement fund is what is left, and
		is a residual rather than a number anybody chooses.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>{creating ? 'Cancel' : 'Open a pool'}</button>
	<button class="ghost" onclick={() => { declaring = !declaring; saveError = undefined; }}>
		{declaring ? 'Cancel' : 'Retroactivity policy'}
	</button>
</div>

<ErrorNote error={saveError} />

{#if policy.settled && policy.data?.policy}
	<p class="panel">
		Corrections to a settled pool: <strong>{label(policy.data.policy.mode)}</strong>
		<span class="muted">
			within {policy.data.policy.max_lookback_days} days, minimum adjustment {policy.data.policy.minimum_adjustment}
		</span>
	</p>
{/if}

{#if creating}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.createPool({
		name: form.name.trim(),
		period_start: new Date(form.period_start).toISOString(),
		period_end: new Date(form.period_end).toISOString(),
		unit: form.unit, currency: form.currency.trim().toUpperCase(),
		amount_scale: Number(form.amount_scale),
		rate_card_id: form.rate_card_id.trim() || undefined,
		policy_version: form.policy_version.trim() || undefined,
		actor: settings.actorOrUnknown
	}), () => (creating = false)); }}>
		<div class="controls">
			<div class="field grow"><label for="pn">Name</label><input id="pn" bind:value={form.name} /></div>
			<div class="field"><label for="pf">From</label><input id="pf" type="date" bind:value={form.period_start} /></div>
			<div class="field"><label for="pt">To</label><input id="pt" type="date" bind:value={form.period_end} /></div>
			<div class="field"><label for="pu">Unit</label><select id="pu" bind:value={form.unit}><option value="LITRES">Litres</option><option value="KG">Kilograms</option></select></div>
			<div class="field"><label for="pc">Currency</label><input id="pc" bind:value={form.currency} size="4" /></div>
			<div class="field"><label for="pz">Scale</label><input id="pz" type="number" min="0" max="9" bind:value={form.amount_scale} size="3" /></div>
			<div class="field"><label for="pr">Rate card</label><input id="pr" bind:value={form.rate_card_id} size="22" placeholder="optional" /></div>
			<button type="submit" disabled={saving || !form.name.trim()}>{saving ? 'Opening…' : 'Open'}</button>
		</div>
	</form>
{/if}

{#if declaring}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.declareRetroactivityPolicy({
		name: pol.name.trim(), mode: pol.mode, max_lookback_days: Number(pol.max_lookback_days),
		currency: pol.currency.trim().toUpperCase(), amount_scale: Number(pol.amount_scale),
		minimum_adjustment: pol.minimum_adjustment.trim() || undefined,
		effective_from: new Date(pol.effective_from).toISOString(), actor: settings.actorOrUnknown
	}), () => { declaring = false; policy.run((s) => settings.api().money.getEffectiveRetroactivityPolicy(undefined, { signal: s })); }); }}>
		<div class="controls">
			<div class="field grow"><label for="qn">Name</label><input id="qn" bind:value={pol.name} /></div>
			<div class="field">
				<label for="qm">When a settled pool is corrected</label>
				<select id="qm" bind:value={pol.mode}>
					<option value="DO_NOT_REOPEN">Do not reopen</option>
					<option value="RECALCULATE">Recalculate</option>
					<option value="APPLY_INCREMENTAL">Apply incremental</option>
					<option value="CUSTOM">Custom</option>
				</select>
			</div>
			<div class="field"><label for="ql">Lookback (days)</label><input id="ql" type="number" min="0" bind:value={pol.max_lookback_days} size="5" /></div>
			<div class="field"><label for="qa">Minimum adjustment</label><input id="qa" bind:value={pol.minimum_adjustment} size="9" inputmode="decimal" /></div>
			<div class="field"><label for="qe">From</label><input id="qe" type="date" bind:value={pol.effective_from} /></div>
			<button type="submit" disabled={saving || !pol.name.trim()}>Declare</button>
		</div>
	</form>
{/if}

<Await {task} retry={load} isEmpty={(d) => (d.pools ?? []).length === 0} empty="No pools.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>Pool</th><th>Period</th><th>Unit</th><th>Status</th></tr></thead>
				<tbody>
					{#each d.pools as p (p.id)}
						<tr>
							<td><a href="/money/pools/{p.id}">{p.name}</a></td>
							<td>{instant(p.period_start)} — {instant(p.period_end)}</td>
							<td>{label(p.unit)}</td>
							<td><Chip tone={p.status === 'SETTLED' ? 'calm' : p.status === 'VALUED' ? 'attention' : 'neutral'}>{label(p.status)}</Chip></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<h2>Recent economic events</h2>
<Await task={events} isEmpty={(d) => (d.events ?? []).length === 0} empty="No events yet; a pool produces them when it is settled.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>When</th><th>Producer</th><th>Kind</th><th class="num">Amount</th></tr></thead>
				<tbody>
					{#each d.events as e (e.id)}
						<tr>
							<td>{instant(e.created_at)}</td>
							<td class="mono">{e.producer_ref}</td>
							<td><Chip tone={e.kind === 'ORIGINAL' ? 'neutral' : 'attention'}>{label(e.kind)}</Chip></td>
							<td class="num">{e.amount} {e.currency}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<style>
	.field.grow { flex: 1 1 16rem; }
</style>
