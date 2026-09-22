<script lang="ts">
	import { ApiError, type Bid, type ListActiveResponse, type Ownership, type Sale } from '$lib/api';
	import { label } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const listings = new Task<ListActiveResponse>();
	const one = new Task<{ listing: ListActiveResponse['listings'][number] }>();
	const history = new Task<{ history: Ownership[] }>();

	let cattleId = $state('');

	function load() {
		listings.run((s) => settings.api().commerce.listActiveListings({}, { signal: s }));
	}
	$effect(load);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let creating = $state(false);
	let form = $state({ cattle_id: '', seller_id: '', title: '', description: '', asking_price: '', currency: 'INR', listing_type: 'negotiable' });
	let bidding = $state<string | undefined>(undefined);
	let bid = $state({ bidder_id: '', bid_amount: '', message: '' });
	let lastBid = $state<Bid | undefined>(undefined);
	let lastSale = $state<Sale | undefined>(undefined);
	let selling = $state<string | undefined>(undefined);
	let sale = $state({ seller_id: '', buyer_id: '', cattle_id: '', sale_price: '' });

	function asApiError(c: unknown): ApiError {
		return c instanceof ApiError ? c : new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true; saveError = undefined;
		try { await fn(); after(); load(); }
		catch (c) { saveError = asApiError(c); }
		finally { saving = false; }
	}
</script>

<div class="page-head">
	<h1>The cattle market</h1>
	<p>
		Animals offered, bid on and sold between members. A sale moves ownership, and the ownership
		history is what answers "whose animal was this in March" after it has changed hands twice.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<button class="ghost" onclick={load} disabled={listings.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>{creating ? 'Cancel' : 'List an animal'}</button>
</div>

{#if creating}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createListing({
		...form, currency: form.currency.trim().toUpperCase(), created_by: settings.actorOrUnknown
	}), () => (creating = false)); }}>
		<div class="controls">
			<div class="field"><label for="la">Animal</label><input id="la" bind:value={form.cattle_id} size="24" /></div>
			<div class="field"><label for="ls">Seller</label><input id="ls" bind:value={form.seller_id} size="22" /></div>
			<div class="field grow"><label for="lt">Title</label><input id="lt" bind:value={form.title} /></div>
			<div class="field"><label for="lp">Asking</label><input id="lp" bind:value={form.asking_price} size="10" inputmode="decimal" /></div>
			<div class="field"><label for="lc">Currency</label><input id="lc" bind:value={form.currency} size="4" /></div>
			<div class="field">
				<label for="lk">Kind</label>
				<select id="lk" bind:value={form.listing_type}>
					<option value="negotiable">Negotiable</option><option value="fixed">Fixed</option><option value="auction">Auction</option>
				</select>
			</div>
			<button type="submit" disabled={saving || !form.cattle_id.trim() || !form.title.trim()}>List</button>
		</div>
	</form>
{/if}

<Await task={listings} retry={load} isEmpty={(d) => (d.listings ?? []).length === 0} empty="Nothing is on offer.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>Animal</th><th>Title</th><th>Kind</th><th class="num">Asking</th><th>Status</th><th></th></tr></thead>
				<tbody>
					{#each d.listings as l (l.id)}
						<tr>
							<td><a href="/herd/{l.cattle_id}" class="mono">{l.cattle_id}</a></td>
							<td>{l.title}</td>
							<td>{label(l.listing_type)}</td>
							<td class="num">{formatExact(l.asking_price, l.currency)}</td>
							<td><Chip tone={l.status === 'active' ? 'calm' : 'neutral'}>{label(l.status)}</Chip></td>
							<td class="actions">
								<button class="ghost" onclick={() => one.run((s) => settings.api().commerce.getListing(l.id, { signal: s }))}>Detail</button>
								<button class="ghost" onclick={() => { bidding = bidding === l.id ? undefined : l.id; bid = { bidder_id: '', bid_amount: '', message: '' }; }}>
									{bidding === l.id ? 'Cancel' : 'Bid'}
								</button>
								<button class="ghost" onclick={() => { selling = selling === l.id ? undefined : l.id; sale = { seller_id: '', buyer_id: '', cattle_id: l.cattle_id, sale_price: l.asking_price }; }}>
									{selling === l.id ? 'Cancel' : 'Record sale'}
								</button>
							</td>
						</tr>
						{#if bidding === l.id}
							<tr class="detail">
								<td colspan="6">
									<form onsubmit={(e) => { e.preventDefault(); run(async () => {
										const r = await settings.api().commerce.placeBid({
											listing_id: l.id, bidder_id: bid.bidder_id.trim(),
											bid_amount: bid.bid_amount.trim(), message: bid.message.trim(),
											created_by: settings.actorOrUnknown
										});
										lastBid = r.bid;
									}, () => (bidding = undefined)); }}>
										<div class="controls">
											<div class="field"><label for="bb-{l.id}">Bidder</label><input id="bb-{l.id}" bind:value={bid.bidder_id} size="22" /></div>
											<div class="field"><label for="ba-{l.id}">Amount</label><input id="ba-{l.id}" bind:value={bid.bid_amount} size="10" inputmode="decimal" /></div>
											<div class="field grow"><label for="bm-{l.id}">Message</label><input id="bm-{l.id}" bind:value={bid.message} /></div>
											<button type="submit" disabled={saving || !bid.bidder_id.trim() || !bid.bid_amount.trim()}>Place</button>
										</div>
									</form>
								</td>
							</tr>
						{/if}
						{#if selling === l.id}
							<tr class="detail">
								<td colspan="6">
									<form onsubmit={(e) => { e.preventDefault(); run(async () => {
										const r = await settings.api().commerce.recordSale({
											listing_id: l.id, seller_id: sale.seller_id.trim(), buyer_id: sale.buyer_id.trim(),
											cattle_id: sale.cattle_id.trim(), sale_price: sale.sale_price.trim(),
											created_by: settings.actorOrUnknown
										});
										lastSale = r.sale;
									}, () => (selling = undefined)); }}>
										<div class="controls">
											<div class="field"><label for="ss-{l.id}">Seller</label><input id="ss-{l.id}" bind:value={sale.seller_id} size="22" /></div>
											<div class="field"><label for="sb-{l.id}">Buyer</label><input id="sb-{l.id}" bind:value={sale.buyer_id} size="22" /></div>
											<div class="field"><label for="sp-{l.id}">Price</label><input id="sp-{l.id}" bind:value={sale.sale_price} size="10" inputmode="decimal" /></div>
											<button type="submit" disabled={saving || !sale.buyer_id.trim()}>Record</button>
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

{#if lastBid}
	<p class="panel">
		Bid <span class="mono">{lastBid.id}</span> — {formatExact(lastBid.bid_amount, lastBid.currency)},
		<Chip tone={lastBid.status === 'accepted' ? 'calm' : lastBid.status === 'rejected' ? 'neutral' : 'attention'}>{label(lastBid.status)}</Chip>
		{#if lastBid.status === 'pending'}
			<button class="ghost" disabled={saving} onclick={() => run(async () => { const r = await settings.api().commerce.acceptBid(lastBid!.id, settings.actorOrUnknown); lastBid = r.bid; })}>Accept</button>
			<button class="ghost" disabled={saving} onclick={() => run(async () => { const r = await settings.api().commerce.rejectBid(lastBid!.id, settings.actorOrUnknown); lastBid = r.bid; })}>Reject</button>
		{/if}
	</p>
{/if}

{#if lastSale}
	<p class="panel">
		Sale <span class="mono">{lastSale.id}</span> — {formatExact(lastSale.sale_price, lastSale.currency)},
		{label(lastSale.status)}
	</p>
{/if}

{#if one.settled && one.data?.listing}
	<div class="panel">
		<dl class="kv">
			<dt>Listing</dt><dd class="mono">{one.data.listing.id}</dd>
			<dt>Title</dt><dd>{one.data.listing.title}</dd>
			<dt>Asking</dt><dd>{formatExact(one.data.listing.asking_price, one.data.listing.currency)}</dd>
			<dt>Status</dt><dd>{label(one.data.listing.status)}</dd>
		</dl>
	</div>
{/if}

<h2>Who has owned an animal</h2>
<form class="controls" onsubmit={(e) => { e.preventDefault(); history.run((s) => settings.api().commerce.getOwnershipHistory(cattleId.trim(), { signal: s })); }}>
	<div class="field"><label for="oh">Animal</label><input id="oh" bind:value={cattleId} size="24" /></div>
	<button type="submit" disabled={!cattleId.trim()}>Show</button>
</form>

{#if history.settled}
	<Await task={history} isEmpty={(d) => (d.history ?? []).length === 0} empty="No ownership recorded for that animal.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Owner</th><th>How</th></tr></thead>
					<tbody>
						{#each d.history as h (h.id)}
							<tr><td class="mono">{h.owner_id}</td><td>{label(h.acquisition_type)}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
{/if}

<style>
	.detail td { background: var(--surface-2); }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
</style>
