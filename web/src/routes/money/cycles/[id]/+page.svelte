<script lang="ts">
	import { page } from '$app/state';
	import { ApiError, type CycleResponse, type ListPayablesResponse, type Payable } from '$lib/api';
	import { instant, label } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const id = $derived(page.params.id ?? '');

	const cycle = new Task<CycleResponse>();
	const payables = new Task<ListPayablesResponse>();
	const printed = new Task<{ statements: { producer_ref: string; page: string }[]; width: number }>();

	function load() {
		cycle.run((signal) => settings.api().money.getCycle(id, { signal }));
		payables.run((signal) => settings.api().money.listPayables(id, { signal }));
	}

	$effect(() => {
		void id;
		if (id) load();
	});

	const c = $derived(cycle.data?.cycle);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let holding = $state<string | undefined>(undefined);
	let holdReason = $state('');
	let paying = $state<string | undefined>(undefined);
	let reference = $state('');
	let adjusting = $state(false);
	let adj = $state({ producer_ref: '', amount: '', reason: '', adjusts_payable_id: '' });

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	function tone(status: string) {
		switch (status) {
			case 'PAID':
				return 'calm';
			case 'APPROVED':
				return 'attention';
			case 'HELD':
				return 'critical';
			default:
				return 'neutral';
		}
	}

	const currency = $derived(payables.data?.currency ?? c?.currency ?? '');
</script>

<p><a href="/money/cycles">← Payment cycles</a></p>

<div class="page-head">
	<h1>{c?.name ?? 'Cycle'}</h1>
	<p>
		What each producer is owed for this fortnight. Every figure here came from settlement-service as
		an exact decimal and is shown as it arrived — nothing on this page turns a payment into a
		floating-point number on the way to a screen.
	</p>
</div>

<ErrorNote error={saveError} />

<Await task={cycle} retry={load} isEmpty={(d) => !d.cycle} empty="No such cycle.">
	{#snippet children()}
		{#if c}
			<div class="panel">
				<dl class="kv">
					<dt>Society</dt><dd class="mono">{c.society_code}</dd>
					<dt>Period</dt><dd>{instant(c.period_start)} — {instant(c.period_end)}</dd>
					<dt>Status</dt><dd><Chip tone={tone(c.status)}>{label(c.status)}</Chip></dd>
					<dt>If deductions exceed earnings</dt><dd>{label(c.deduction_policy)}</dd>
					{#if c.gathered_at}<dt>Gathered</dt><dd>{instant(c.gathered_at)}</dd>{/if}
					{#if c.approved_at}<dt>Approved</dt><dd>{instant(c.approved_at)} by {c.approved_by}</dd>{/if}
					{#if c.paid_at}<dt>Paid</dt><dd>{instant(c.paid_at)}</dd>{/if}
				</dl>
			</div>
		{/if}
	{/snippet}
</Await>

<div class="controls">
	<button class="ghost" onclick={load} disabled={payables.pending}>Refresh</button>
	<button class="ghost" onclick={() => { adjusting = !adjusting; saveError = undefined; }}>
		{adjusting ? 'Cancel' : 'Raise an adjustment'}
	</button>
	<button
		class="ghost"
		disabled={printed.pending}
		onclick={() => printed.run((signal) => settings.api().money.printCycleStatements({ cycle_id: id }, { signal }))}
	>
		Print every statement
	</button>
</div>

{#if adjusting}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().money.raiseAdjustment({
						cycle_id: id,
						producer_ref: adj.producer_ref.trim(),
						currency: currency || 'INR',
						amount_scale: c?.amount_scale ?? 2,
						amount: adj.amount.trim(),
						adjusts_payable_id: adj.adjusts_payable_id.trim() || undefined,
						reason: adj.reason.trim(),
						actor: settings.actorOrUnknown
					}),
				() => {
					adjusting = false;
					adj = { producer_ref: '', amount: '', reason: '', adjusts_payable_id: '' };
				}
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="ap">Producer</label><input id="ap" bind:value={adj.producer_ref} size="24" /></div>
			<div class="field"><label for="aa">Amount</label><input id="aa" bind:value={adj.amount} size="10" inputmode="decimal" /></div>
			<div class="field"><label for="ad">Adjusts</label><input id="ad" bind:value={adj.adjusts_payable_id} size="24" placeholder="optional" /></div>
			<div class="field grow"><label for="ar">Reason</label><input id="ar" bind:value={adj.reason} /></div>
			<button type="submit" disabled={saving || !adj.producer_ref.trim() || !adj.amount.trim() || !adj.reason.trim()}>
				{saving ? 'Raising…' : 'Raise'}
			</button>
		</div>
		<p class="muted note">
			An adjustment is a payable of its own, not an edit of one that was paid. It deducts nothing
			and it must say why — three separate rules the service enforces, because a figure that
			changed and cannot be shown to have changed is one nobody can defend to the member who asks.
		</p>
	</form>
{/if}

<Await
	task={payables}
	retry={load}
	isEmpty={(d) => (d.payables ?? []).length === 0}
	empty="Nothing is owed yet. A cycle has no payables until it is gathered."
>
	{#snippet children(d)}
		{#if d.total_net}
			<p class="figure">
				{formatExact(d.total_net, d.currency ?? '')}
				<span class="muted">net across {d.payables.length} producer{d.payables.length === 1 ? '' : 's'}</span>
			</p>
		{/if}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Producer</th><th>Kind</th><th class="num">Gross</th><th class="num">Deducted</th>
						<th class="num">Net</th><th>Status</th><th></th>
					</tr>
				</thead>
				<tbody>
					{#each d.payables as p (p.id)}
						<tr>
							<td class="mono">{p.producer_ref}</td>
							<td>{label(p.kind)}</td>
							<td class="num">{formatExact(p.gross, p.currency)}</td>
							<td class="num">{formatExact(p.deducted, p.currency)}</td>
							<td class="num"><strong>{formatExact(p.net, p.currency)}</strong></td>
							<td>
								<Chip tone={tone(p.status)} title={p.held_reason ?? ''}>{label(p.status)}</Chip>
							</td>
							<td class="actions">
								<a href="/money/payables/{p.id}">Explain</a>
								{#if p.status === 'PAYABLE'}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.approvePayable(p.id, settings.actorOrUnknown))}>
										Approve
									</button>
								{/if}
								{#if p.status === 'APPROVED'}
									<button class="ghost" onclick={() => { paying = paying === p.id ? undefined : p.id; reference = ''; }}>
										{paying === p.id ? 'Cancel' : 'Mark paid'}
									</button>
								{/if}
								{#if p.status !== 'PAID' && p.status !== 'HELD'}
									<button class="ghost" onclick={() => { holding = holding === p.id ? undefined : p.id; holdReason = ''; }}>
										{holding === p.id ? 'Cancel' : 'Hold'}
									</button>
								{/if}
							</td>
						</tr>
						{#if p.held_reason && p.status === 'HELD'}
							<tr class="detail"><td colspan="7"><strong>Held:</strong> {p.held_reason}</td></tr>
						{/if}
						{#if holding === p.id}
							<tr class="detail">
								<td colspan="7">
									<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.holdPayable({ id: p.id, reason: holdReason.trim(), actor: settings.actorOrUnknown }), () => (holding = undefined)); }}>
										<div class="controls">
											<div class="field grow">
												<label for="hr-{p.id}">Why it is held</label>
												<input id="hr-{p.id}" bind:value={holdReason} placeholder="What the member will be told at the office." />
											</div>
											<button type="submit" disabled={saving || !holdReason.trim()}>Hold</button>
										</div>
									</form>
								</td>
							</tr>
						{/if}
						{#if paying === p.id}
							<tr class="detail">
								<td colspan="7">
									<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.markPaid({ id: p.id, payment_reference: reference.trim() || undefined, actor: settings.actorOrUnknown }), () => (paying = undefined)); }}>
										<div class="controls">
											<div class="field grow">
												<label for="pr-{p.id}">Payment reference</label>
												<input id="pr-{p.id}" bind:value={reference} placeholder="NEFT/2026/0917/88213" />
											</div>
											<button type="submit" disabled={saving}>Mark paid</button>
										</div>
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

{#if printed.settled}
	<Await task={printed} isEmpty={(d) => (d.statements ?? []).length === 0} empty="Nothing to print.">
		{#snippet children(d)}
			<h2>Statements</h2>
			<p class="muted note">
				{d.width} columns, fixed width, as a society office's printer wants them.
			</p>
			{#each d.statements as s (s.producer_ref)}
				<pre class="statement">{s.page}</pre>
			{/each}
		{/snippet}
	</Await>
{/if}

<style>
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.8rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 16rem; }
	.figure { font-size: 1.4rem; margin: 0.4rem 0 1rem; }
	.figure .muted { font-size: 0.85rem; }
	.actions { display: flex; gap: 0.5rem; align-items: center; }
	.statement {
		background: var(--surface-2);
		padding: 1rem;
		overflow-x: auto;
		font-family: var(--mono);
		font-size: 0.75rem;
		line-height: 1.35;
	}
</style>
