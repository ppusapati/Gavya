<script lang="ts">
	import { ApiError, type Invoice, type ListInvoicesResponse } from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListInvoicesResponse>();

	function load() {
		task.run((signal) => settings.api().money.getOutstandingInvoices({ signal }));
	}
	$effect(load);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let creating = $state(false);
	let form = $state({ customer_id: '', reference_id: '', reference_type: '', currency: 'INR', tax_inclusive: false, notes: '' });
	let adding = $state<string | undefined>(undefined);
	let item = $state({ description: '', quantity: '', unit_price: '', tax_rate: '0' });
	let paying = $state<string | undefined>(undefined);
	let pay = $state({ amount: '', payment_method: 'upi', reference_no: '', notes: '' });

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError ? cause : new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true; saveError = undefined;
		try { await fn(); after(); load(); }
		catch (cause) { saveError = asApiError(cause); }
		finally { saving = false; }
	}

	function tone(status: string) {
		switch (status) {
			case 'paid': return 'calm';
			case 'void': return 'neutral';
			case 'issued': return 'attention';
			default: return 'neutral';
		}
	}

	function overdue(i: Invoice): boolean {
		if (i.paid_at) return false;
		const at = Date.parse(i.due_at);
		return Number.isFinite(at) && at < Date.now();
	}
</script>

<div class="page-head">
	<h1>Billing</h1>
	<p>
		What the society invoices and what has been paid against it — feed sold to a member, a delivery
		to a shop. Separate from settlement, which is what the society pays out.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>{creating ? 'Cancel' : 'Raise an invoice'}</button>
</div>

<ErrorNote error={saveError} />

{#if creating}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.createInvoice({
		customer_id: form.customer_id.trim(), reference_id: form.reference_id.trim(),
		reference_type: form.reference_type.trim(), currency: form.currency.trim().toUpperCase(),
		tax_inclusive: form.tax_inclusive, issued_at: new Date().toISOString(),
		notes: form.notes.trim(), created_by: settings.actorOrUnknown
	}), () => (creating = false)); }}>
		<div class="controls">
			<div class="field"><label for="ic">Customer</label><input id="ic" bind:value={form.customer_id} size="24" /></div>
			<div class="field"><label for="ir">Reference</label><input id="ir" bind:value={form.reference_id} size="18" /></div>
			<div class="field"><label for="it">Reference kind</label><input id="it" bind:value={form.reference_type} size="12" /></div>
			<div class="field"><label for="iu">Currency</label><input id="iu" bind:value={form.currency} size="4" /></div>
			<label class="check"><input type="checkbox" bind:checked={form.tax_inclusive} /> Prices include tax</label>
			<div class="field grow"><label for="in">Notes</label><input id="in" bind:value={form.notes} /></div>
			<button type="submit" disabled={saving || !form.customer_id.trim()}>Raise</button>
		</div>
	</form>
{/if}

<Await {task} retry={load} isEmpty={(d) => (d.invoices ?? []).length === 0} empty="Nothing outstanding.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Invoice</th><th>Customer</th><th class="num">Sub-total</th><th class="num">Tax</th><th class="num">Total</th><th>Due</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.invoices as i (i.id)}
						<tr>
							<td class="mono">{i.invoice_number}</td>
							<td class="mono">{i.customer_id}</td>
							<td class="num">{formatExact(i.sub_total, i.currency)}</td>
							<td class="num">{formatExact(i.tax_amount, i.currency)}</td>
							<td class="num"><strong>{formatExact(i.total_amount, i.currency)}</strong></td>
							<td>
								{#if overdue(i)}<Chip tone="critical" title="Past its due date">{instant(i.due_at)}</Chip>
								{:else}{instant(i.due_at)}{/if}
							</td>
							<td><Chip tone={tone(i.status)}>{label(i.status)}</Chip></td>
							<td class="actions">
								<button class="ghost" onclick={() => { adding = adding === i.id ? undefined : i.id; item = { description: '', quantity: '', unit_price: '', tax_rate: '0' }; }}>
									{adding === i.id ? 'Cancel' : 'Add line'}
								</button>
								{#if i.status === 'draft'}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.sendInvoice(i.id))}>Send</button>
								{/if}
								{#if i.status !== 'paid' && i.status !== 'void'}
									<button class="ghost" onclick={() => { paying = paying === i.id ? undefined : i.id; pay = { amount: i.total_amount, payment_method: 'upi', reference_no: '', notes: '' }; }}>
										{paying === i.id ? 'Cancel' : 'Record payment'}
									</button>
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.voidInvoice(i.id, settings.actorOrUnknown))}>Void</button>
								{/if}
							</td>
						</tr>
						{#if adding === i.id}
							<tr class="detail">
								<td colspan="8">
									<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.addInvoiceItem({
										invoice_id: i.id, description: item.description.trim(),
										quantity: item.quantity.trim(), unit_price: item.unit_price.trim(),
										tax_rate: item.tax_rate.trim(), created_by: settings.actorOrUnknown
									}), () => (adding = undefined)); }}>
										<div class="controls">
											<div class="field grow"><label for="ld-{i.id}">Description</label><input id="ld-{i.id}" bind:value={item.description} /></div>
											<div class="field"><label for="lq-{i.id}">Quantity</label><input id="lq-{i.id}" bind:value={item.quantity} size="8" inputmode="decimal" /></div>
											<div class="field"><label for="lp-{i.id}">Unit price</label><input id="lp-{i.id}" bind:value={item.unit_price} size="10" inputmode="decimal" /></div>
											<div class="field"><label for="lt-{i.id}">Tax %</label><input id="lt-{i.id}" bind:value={item.tax_rate} size="6" inputmode="decimal" /></div>
											<button type="submit" disabled={saving || !item.description.trim() || !item.quantity.trim()}>Add</button>
										</div>
									</form>
								</td>
							</tr>
						{/if}
						{#if paying === i.id}
							<tr class="detail">
								<td colspan="8">
									<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().money.recordPayment({
										invoice_id: i.id, amount: pay.amount.trim(), payment_method: pay.payment_method.trim(),
										reference_no: pay.reference_no.trim(), paid_at: new Date().toISOString(),
										notes: pay.notes.trim(), created_by: settings.actorOrUnknown
									}), () => (paying = undefined)); }}>
										<div class="controls">
											<div class="field"><label for="pa-{i.id}">Amount</label><input id="pa-{i.id}" bind:value={pay.amount} size="10" inputmode="decimal" /></div>
											<div class="field"><label for="pm-{i.id}">Method</label><input id="pm-{i.id}" bind:value={pay.payment_method} size="10" /></div>
											<div class="field"><label for="pn-{i.id}">Reference</label><input id="pn-{i.id}" bind:value={pay.reference_no} size="20" /></div>
											<button type="submit" disabled={saving || !pay.amount.trim()}>Record</button>
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

<style>
	.detail td { background: var(--surface-2); }
	.field.grow { flex: 1 1 16rem; }
	.actions { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.82rem; }
</style>
