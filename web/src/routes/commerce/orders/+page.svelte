<script lang="ts">
	import { ApiError, type Order, type OrderInvoice, type Return } from '$lib/api';
	import { instant, label } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const order = new Task<{ order: Order }>();
	const returns = new Task<{ returns: Return[] }>();
	const invoice = new Task<{ invoice: OrderInvoice }>();
	const oneReturn = new Task<{ return: Return }>();

	let lookup = $state('');

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let creating = $state(false);
	let form = $state({ customer_id: '', currency: 'INR', tax_inclusive: false, shipping_address: '', notes: '' });
	let item = $state({ sku_id: '', product_id: '', quantity: '', unit_price: '', tax_rate: '0' });
	let ret = $state({ reason: '', refund_amount: '' });
	let showReturn = $state(false);

	function asApiError(c: unknown): ApiError {
		return c instanceof ApiError ? c : new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
	}

	function show(id: string) {
		lookup = id;
		order.run((s) => settings.api().commerce.getOrder(id, { signal: s }));
		returns.run((s) => settings.api().commerce.listOrderReturns(id, { signal: s }));
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true; saveError = undefined;
		try { await fn(); after(); if (lookup) show(lookup); }
		catch (c) { saveError = asApiError(c); }
		finally { saving = false; }
	}

	const o = $derived(order.data?.order);

	function tone(status: string) {
		switch (status) {
			case 'delivered': case 'confirmed': return 'calm';
			case 'cancelled': return 'neutral';
			case 'draft': return 'attention';
			default: return 'neutral';
		}
	}
</script>

<div class="page-head">
	<h1>Orders</h1>
	<p>
		The order book: what a shop asked for, what it was charged, and what came back. An order is
		looked up by its identifier — order-service lists by order rather than by tenant, and a screen
		that invented a list would be inventing a procedure.
	</p>
</div>

<ErrorNote error={saveError} />

<form class="controls" onsubmit={(e) => { e.preventDefault(); if (lookup.trim()) show(lookup.trim()); }}>
	<div class="field"><label for="lk">Order</label><input id="lk" bind:value={lookup} size="26" /></div>
	<button type="submit" disabled={!lookup.trim()}>Open</button>
	<button type="button" onclick={() => { creating = !creating; saveError = undefined; }}>{creating ? 'Cancel' : 'New order'}</button>
</form>

{#if creating}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(async () => {
		const res = await settings.api().commerce.createOrder({
			customer_id: form.customer_id.trim(), currency: form.currency.trim().toUpperCase(),
			tax_inclusive: form.tax_inclusive, shipping_address: form.shipping_address.trim(),
			notes: form.notes.trim(), ordered_at: new Date().toISOString(), created_by: settings.actorOrUnknown
		});
		creating = false;
		show(res.order.id);
	}); }}>
		<div class="controls">
			<div class="field"><label for="oc">Customer</label><input id="oc" bind:value={form.customer_id} size="24" /></div>
			<div class="field"><label for="ou">Currency</label><input id="ou" bind:value={form.currency} size="4" /></div>
			<label class="check"><input type="checkbox" bind:checked={form.tax_inclusive} /> Prices include tax</label>
			<div class="field grow"><label for="oa">Ship to</label><input id="oa" bind:value={form.shipping_address} /></div>
			<button type="submit" disabled={saving || !form.customer_id.trim()}>Create</button>
		</div>
	</form>
{/if}

{#if order.settled}
	<Await task={order} isEmpty={(d) => !d.order} empty="No such order.">
		{#snippet children()}
			{#if o}
				<div class="panel">
					<dl class="kv">
						<dt>Order</dt><dd class="mono">{o.order_number}</dd>
						<dt>Customer</dt><dd class="mono">{o.customer_id}</dd>
						<dt>Status</dt><dd><Chip tone={tone(o.status)}>{label(o.status)}</Chip></dd>
						<dt>Sub-total</dt><dd>{formatExact(o.sub_total, o.currency)}</dd>
						<dt>Tax</dt><dd>{formatExact(o.tax_amount, o.currency)}</dd>
						<dt>Total</dt><dd><strong>{formatExact(o.total_amount, o.currency)}</strong></dd>
						<dt>Ordered</dt><dd>{instant(o.ordered_at)}</dd>
						{#if o.delivered_at}<dt>Delivered</dt><dd>{instant(o.delivered_at)}</dd>{/if}
					</dl>
				</div>

				<div class="controls">
					{#if o.status === 'draft'}
						<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().commerce.confirmOrder(o.id, settings.actorOrUnknown))}>Confirm</button>
					{/if}
					{#if o.status !== 'cancelled' && o.status !== 'delivered'}
						<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().commerce.cancelOrder(o.id, settings.actorOrUnknown))}>Cancel order</button>
					{/if}
					<button class="ghost" disabled={saving} onclick={() => invoice.run((s) => settings.api().commerce.generateInvoice(o.id, settings.actorOrUnknown, { signal: s }))}>
						Generate invoice
					</button>
					<button class="ghost" onclick={() => (showReturn = !showReturn)}>{showReturn ? 'Cancel' : 'Request a return'}</button>
				</div>

				<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.addOrderItem({
					order_id: o.id, sku_id: item.sku_id.trim(), product_id: item.product_id.trim(),
					quantity: item.quantity.trim(), unit_price: item.unit_price.trim(),
					tax_rate: item.tax_rate.trim(), created_by: settings.actorOrUnknown
				}), () => (item = { sku_id: '', product_id: '', quantity: '', unit_price: '', tax_rate: '0' })); }}>
					<div class="controls">
						<div class="field"><label for="is">SKU</label><input id="is" bind:value={item.sku_id} size="22" /></div>
						<div class="field"><label for="ip">Product</label><input id="ip" bind:value={item.product_id} size="22" /></div>
						<div class="field"><label for="iq">Quantity</label><input id="iq" bind:value={item.quantity} size="8" inputmode="decimal" /></div>
						<div class="field"><label for="iu">Unit price</label><input id="iu" bind:value={item.unit_price} size="10" inputmode="decimal" /></div>
						<div class="field"><label for="it">Tax %</label><input id="it" bind:value={item.tax_rate} size="6" inputmode="decimal" /></div>
						<button type="submit" disabled={saving || !item.sku_id.trim() || !item.quantity.trim()}>Add line</button>
					</div>
				</form>

				{#if showReturn}
					<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.requestReturn({
						order_id: o.id, reason: ret.reason.trim(), refund_amount: ret.refund_amount.trim(),
						created_by: settings.actorOrUnknown
					}), () => { showReturn = false; ret = { reason: '', refund_amount: '' }; }); }}>
						<div class="controls">
							<div class="field grow"><label for="rr">Why</label><input id="rr" bind:value={ret.reason} /></div>
							<div class="field"><label for="ra">Refund</label><input id="ra" bind:value={ret.refund_amount} size="10" inputmode="decimal" /></div>
							<button type="submit" disabled={saving || !ret.reason.trim()}>Request</button>
						</div>
					</form>
				{/if}
			{/if}
		{/snippet}
	</Await>
{/if}

{#if invoice.settled && invoice.data}
	<h2>Invoice</h2>
	<div class="panel">
		<dl class="kv">
			<dt>Number</dt><dd class="mono">{invoice.data.invoice.invoice_number}</dd>
			<dt>Total</dt><dd><strong>{formatExact(invoice.data.invoice.total_amount, invoice.data.invoice.currency)}</strong></dd>
			<dt>Due</dt><dd>{instant(invoice.data.invoice.due_at)}</dd>
			<dt>Status</dt><dd>{label(invoice.data.invoice.status)}</dd>
		</dl>
	</div>
{/if}

{#if returns.settled}
	<Await task={returns} isEmpty={(d) => (d.returns ?? []).length === 0} empty="Nothing came back.">
		{#snippet children(d)}
			<h2>Returns</h2>
			<div class="tablewrap">
				<table>
					<thead><tr><th>Requested</th><th>Why</th><th class="num">Refund</th><th>Status</th><th></th></tr></thead>
					<tbody>
						{#each d.returns as r (r.id)}
							<tr>
								<td>{instant(r.requested_at)}</td>
								<td>{r.reason}</td>
								<td class="num">{formatExact(r.refund_amount, r.currency)}</td>
								<td><Chip tone={r.status === 'completed' ? 'calm' : r.status === 'rejected' ? 'neutral' : 'attention'}>{label(r.status)}</Chip></td>
								<td class="actions">
									{#if r.status === 'requested'}
										<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().commerce.decideReturn({ id: r.id, status: 'approved', updated_by: settings.actorOrUnknown }))}>Approve</button>
										<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().commerce.decideReturn({ id: r.id, status: 'rejected', updated_by: settings.actorOrUnknown }))}>Reject</button>
									{/if}
									<button class="ghost" onclick={() => oneReturn.run((s) => settings.api().commerce.getReturn(r.id, { signal: s }))}>
										Detail
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
{/if}

{#if oneReturn.settled && oneReturn.data}
	<div class="panel">
		<dl class="kv">
			<dt>Return</dt><dd class="mono">{oneReturn.data.return.id}</dd>
			<dt>Why</dt><dd>{oneReturn.data.return.reason}</dd>
			<dt>Refund</dt><dd>{formatExact(oneReturn.data.return.refund_amount, oneReturn.data.return.currency)}</dd>
			<dt>Status</dt><dd>{label(oneReturn.data.return.status)}</dd>
			{#if oneReturn.data.return.processed_at}
				<dt>Decided</dt><dd>{instant(oneReturn.data.return.processed_at)}</dd>
			{/if}
		</dl>
	</div>
{/if}

<style>
	.field.grow { flex: 1 1 14rem; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.82rem; }
	.actions { display: flex; gap: 0.5rem; }
</style>
