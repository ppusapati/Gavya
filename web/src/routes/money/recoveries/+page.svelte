<script lang="ts">
	import { ApiError, type ListRecoveriesResponse, type Statement } from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListRecoveriesResponse>();
	const statement = new Task<{ statement: Statement }>();
	const printed = new Task<{ page: string; width: number }>();

	let producer = $state('');
	let outstandingOnly = $state(true);
	let cycleId = $state('');

	function load() {
		task.run((signal) =>
			settings.api().money.listRecoveries(
				{ producer_ref: producer.trim() || undefined, outstanding_only: outstandingOnly },
				{ signal }
			)
		);
	}
	$effect(() => { void outstandingOnly; load(); });

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let opening = $state(false);
	let form = $state({
		producer_ref: '', kind: 'FEED_CREDIT', reference: '', currency: 'INR', amount_scale: 2,
		principal: '', instalment: '', priority: 1, opened_on: today()
	});

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError ? cause : new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void) {
		if (saving) return;
		saving = true; saveError = undefined;
		try { await fn(); after(); load(); }
		catch (cause) { saveError = asApiError(cause); }
		finally { saving = false; }
	}
</script>

<div class="page-head">
	<h1>Recoveries &amp; statements</h1>
	<p>
		What a member owes the society and is paying back out of their milk, and the statement that
		shows them exactly how a fortnight came to what it came to.
	</p>
</div>

<div class="controls">
	<div class="field"><label for="rp">Producer</label><input id="rp" bind:value={producer} size="22" onchange={load} /></div>
	<label class="check"><input type="checkbox" bind:checked={outstandingOnly} /> Outstanding only</label>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { opening = !opening; saveError = undefined; }}>{opening ? 'Cancel' : 'Open a recovery'}</button>
</div>

<ErrorNote error={saveError} />

{#if opening}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.openRecovery({
		producer_ref: form.producer_ref.trim(), kind: form.kind,
		reference: form.reference.trim() || undefined,
		currency: form.currency.trim().toUpperCase(), amount_scale: Number(form.amount_scale),
		principal: form.principal.trim(), instalment: form.instalment.trim() || undefined,
		priority: Number(form.priority), opened_on: form.opened_on, actor: settings.actorOrUnknown
	}), () => (opening = false)); }}>
		<div class="controls">
			<div class="field"><label for="op">Producer</label><input id="op" bind:value={form.producer_ref} size="22" /></div>
			<div class="field">
				<label for="ok">Kind</label>
				<select id="ok" bind:value={form.kind}>
					<option value="ADVANCE">Advance</option><option value="FEED_CREDIT">Feed credit</option>
					<option value="SOCIETY_DUES">Society dues</option><option value="LOAN">Loan</option><option value="OTHER">Other</option>
				</select>
			</div>
			<div class="field"><label for="or">Reference</label><input id="or" bind:value={form.reference} size="18" /></div>
			<div class="field"><label for="oa">Principal</label><input id="oa" bind:value={form.principal} size="10" inputmode="decimal" /></div>
			<div class="field"><label for="oi">Instalment</label><input id="oi" bind:value={form.instalment} size="10" inputmode="decimal" /></div>
			<div class="field"><label for="oy">Priority</label><input id="oy" type="number" min="1" bind:value={form.priority} size="4" /></div>
			<div class="field"><label for="od">Opened</label><input id="od" type="date" bind:value={form.opened_on} /></div>
			<button type="submit" disabled={saving || !form.producer_ref.trim() || !form.principal.trim()}>Open</button>
		</div>
		<p class="muted note">
			Priority decides which recovery is taken first when a fortnight cannot cover them all. What
			happens when deductions exceed earnings is the cycle's policy, not this one's.
		</p>
	</form>
{/if}

<Await {task} retry={load} isEmpty={(d) => (d.recoveries ?? []).length === 0} empty="Nothing outstanding.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Producer</th><th>Kind</th><th>Reference</th><th class="num">Principal</th><th class="num">Recovered</th><th class="num">Outstanding</th><th class="num">Instalment</th><th class="num">Priority</th><th>Status</th></tr>
				</thead>
				<tbody>
					{#each d.recoveries as r (r.id)}
						<tr>
							<td class="mono">{r.producer_ref}</td>
							<td>{label(r.kind)}</td>
							<td class="mono">{r.reference ?? '—'}</td>
							<td class="num">{formatExact(r.principal, r.currency)}</td>
							<td class="num">{formatExact(r.recovered, r.currency)}</td>
							<td class="num"><strong>{formatExact(r.outstanding, r.currency)}</strong></td>
							<td class="num">{formatExact(r.instalment, r.currency)}</td>
							<td class="num">{r.priority}</td>
							<td><Chip tone={r.status === 'SETTLED' ? 'calm' : r.status === 'WAIVED' ? 'neutral' : 'attention'}>{label(r.status)}</Chip></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<h2>A producer's statement</h2>
<form class="controls" onsubmit={(e) => {
	e.preventDefault();
	statement.run((s) => settings.api().money.getProducerStatement({ cycle_id: cycleId.trim(), producer_ref: producer.trim() }, { signal: s }));
}}>
	<div class="field"><label for="sc">Cycle</label><input id="sc" bind:value={cycleId} size="24" /></div>
	<div class="field"><label for="sp">Producer</label><input id="sp" bind:value={producer} size="22" /></div>
	<button type="submit" disabled={!cycleId.trim() || !producer.trim()}>Show</button>
	<button type="button" class="ghost" disabled={!cycleId.trim() || !producer.trim()}
		onclick={() => printed.run((s) => settings.api().money.printProducerStatement({ cycle_id: cycleId.trim(), producer_ref: producer.trim() }, { signal: s }))}>
		Print
	</button>
</form>

{#if statement.settled}
	<Await task={statement} isEmpty={(d) => !d.statement} empty="No statement for that producer in that cycle.">
		{#snippet children(d)}
			<div class="panel">
				<dl class="kv">
					<dt>Producer</dt><dd class="mono">{d.statement.producer_ref}</dd>
					<dt>Cycle</dt><dd>{d.statement.cycle_name} — {instant(d.statement.period_start)} to {instant(d.statement.period_end)}</dd>
					<dt>Gross</dt><dd>{formatExact(d.statement.gross, d.statement.currency)}</dd>
					<dt>Deducted</dt><dd>{formatExact(d.statement.deducted, d.statement.currency)}</dd>
					<dt>Net</dt><dd><strong>{formatExact(d.statement.net, d.statement.currency)}</strong></dd>
					{#if d.statement.held_reason}<dt>Held because</dt><dd>{d.statement.held_reason}</dd>{/if}
				</dl>
			</div>
			<div class="tablewrap">
				<table>
					<thead><tr><th>Day</th><th>Shift</th><th class="num">Quantity</th><th class="num">Rate</th><th class="num">Amount</th></tr></thead>
					<tbody>
						{#each d.statement.lines as l (l.collection_id)}
							<tr>
								<td>{instant(l.collected_on)}</td><td>{label(l.shift)}</td>
								<td class="num">{l.quantity} {l.quantity_unit}</td>
								<td class="num">{l.rate ?? '—'}</td>
								<td class="num">{formatExact(l.amount, d.statement.currency)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			{#if (d.statement.adjustments ?? []).length}
				<h3>Adjustments</h3>
				<div class="tablewrap">
					<table>
						<thead><tr><th class="num">Amount</th><th>Why</th><th>Status</th></tr></thead>
						<tbody>
							{#each d.statement.adjustments ?? [] as a (a.id)}
								<tr><td class="num">{formatExact(a.amount, d.statement.currency)}</td><td>{a.reason}</td><td>{label(a.status)}</td></tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		{/snippet}
	</Await>
{/if}

{#if printed.settled && printed.data}
	<h3>As it prints</h3>
	<pre class="statement">{printed.data.page}</pre>
{/if}

<style>
	h3 { margin: 1.4rem 0 0.6rem; font-size: 0.88rem; }
	.note { max-width: var(--measure); margin: 0.8rem 0 0; font-size: 0.82rem; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.82rem; }
	.statement { background: var(--surface-2); padding: 1rem; overflow-x: auto; font-family: var(--mono); font-size: 0.75rem; line-height: 1.35; }
</style>
