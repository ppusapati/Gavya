<script lang="ts">
	import { ApiError, type ListProductsResponse, type Product, type SKU } from '$lib/api';
	import { label, quantity } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const products = new Task<ListProductsResponse>();
	const cats = new Task<{ categories: { id: string; name: string; parent_id?: string }[] }>();
	const brands = new Task<{ brands: { id: string; name: string }[] }>();
	const skus = new Task<{ skus: SKU[] }>();

	let status = $state('');

	function load() {
		products.run((s) => settings.api().commerce.listProducts({ status }, { signal: s }));
	}
	$effect(() => { void status; load(); });
	$effect(() => {
		cats.run((s) => settings.api().commerce.listCategories({ signal: s }));
		brands.run((s) => settings.api().commerce.listBrands({ signal: s }));
	});

	const catName = $derived(new Map((cats.data?.categories ?? []).map((c) => [c.id, c.name])));
	const brandName = $derived(new Map((brands.data?.brands ?? []).map((b) => [b.id, b.name])));

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let sheet = $state<'product' | 'category' | 'brand' | undefined>(undefined);
	let open = $state<string | undefined>(undefined);
	let repricing = $state<string | undefined>(undefined);
	let price = $state('');

	let prod = $state({ name: '', slug: '', category_id: '', brand_id: '', description: '', product_type: 'simple', status: 'active' });
	let cat = $state({ name: '', slug: '', parent_id: '', description: '', sort_order: 0 });
	let brand = $state({ name: '', slug: '', logo_url: '' });
	let sku = $state({ code: '', name: '', price: '', currency: 'INR', unit: 'ml', unit_size: '', status: 'active' });

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

	function selectProduct(p: Product) {
		open = open === p.id ? undefined : p.id;
		sku = { code: '', name: '', price: '', currency: 'INR', unit: 'ml', unit_size: '', status: 'active' };
		saveError = undefined;
		if (open) skus.run((s) => settings.api().commerce.listProductSKUs(p.id, { signal: s }));
	}
</script>

<div class="page-head">
	<h1>Catalogue</h1>
	<p>
		What the society sells, and at what price. A SKU's price is a decimal string with its currency
		beside it — never a number this screen reconstituted.
	</p>
</div>

<div class="controls">
	<div class="field">
		<label for="st">Status</label>
		<select id="st" bind:value={status}>
			<option value="">Any</option><option value="active">Active</option><option value="discontinued">Discontinued</option>
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={products.pending}>Refresh</button>
	<button onclick={() => { sheet = sheet === 'product' ? undefined : 'product'; saveError = undefined; }}>Add a product</button>
	<button class="ghost" onclick={() => { sheet = sheet === 'category' ? undefined : 'category'; saveError = undefined; }}>Add a category</button>
	<button class="ghost" onclick={() => { sheet = sheet === 'brand' ? undefined : 'brand'; saveError = undefined; }}>Add a brand</button>
</div>

<ErrorNote error={saveError} />

{#if sheet === 'category'}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createCategory({ ...cat, parent_id: cat.parent_id || undefined, sort_order: Number(cat.sort_order), created_by: settings.actorOrUnknown }), () => { sheet = undefined; cats.run((s) => settings.api().commerce.listCategories({ signal: s })); }); }}>
		<div class="controls">
			<div class="field"><label for="cn">Name</label><input id="cn" bind:value={cat.name} /></div>
			<div class="field"><label for="cs">Slug</label><input id="cs" bind:value={cat.slug} size="16" /></div>
			<div class="field">
				<label for="cp">Under</label>
				<select id="cp" bind:value={cat.parent_id}>
					<option value="">— top level —</option>
					{#each cats.data?.categories ?? [] as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="co">Order</label><input id="co" type="number" bind:value={cat.sort_order} size="4" /></div>
			<button type="submit" disabled={saving || !cat.name.trim()}>Add</button>
		</div>
	</form>
{/if}

{#if sheet === 'brand'}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createBrand({ ...brand, created_by: settings.actorOrUnknown }), () => { sheet = undefined; brands.run((s) => settings.api().commerce.listBrands({ signal: s })); }); }}>
		<div class="controls">
			<div class="field"><label for="bn">Name</label><input id="bn" bind:value={brand.name} /></div>
			<div class="field"><label for="bs">Slug</label><input id="bs" bind:value={brand.slug} size="16" /></div>
			<div class="field grow"><label for="bl">Logo URL</label><input id="bl" bind:value={brand.logo_url} /></div>
			<button type="submit" disabled={saving || !brand.name.trim()}>Add</button>
		</div>
	</form>
{/if}

{#if sheet === 'product'}
	<form class="panel" onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createProduct({ ...prod, created_by: settings.actorOrUnknown }), () => (sheet = undefined)); }}>
		<div class="controls">
			<div class="field grow"><label for="pn">Name</label><input id="pn" bind:value={prod.name} /></div>
			<div class="field"><label for="ps">Slug</label><input id="ps" bind:value={prod.slug} size="16" /></div>
			<div class="field">
				<label for="pc">Category</label>
				<select id="pc" bind:value={prod.category_id}>
					<option value="">—</option>
					{#each cats.data?.categories ?? [] as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
				</select>
			</div>
			<div class="field">
				<label for="pb">Brand</label>
				<select id="pb" bind:value={prod.brand_id}>
					<option value="">—</option>
					{#each brands.data?.brands ?? [] as b (b.id)}<option value={b.id}>{b.name}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="pt">Type</label><input id="pt" bind:value={prod.product_type} size="10" /></div>
			<button type="submit" disabled={saving || !prod.name.trim()}>Add</button>
		</div>
	</form>
{/if}

<Await task={products} retry={load} isEmpty={(d) => (d.products ?? []).length === 0} empty="No products.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead><tr><th>Product</th><th>Category</th><th>Brand</th><th>Type</th><th>Status</th><th></th></tr></thead>
				<tbody>
					{#each d.products as p (p.id)}
						<tr>
							<td>{p.name}</td>
							<td>{catName.get(p.category_id) ?? '—'}</td>
							<td>{brandName.get(p.brand_id) ?? '—'}</td>
							<td>{label(p.product_type)}</td>
							<td><Chip tone={p.status === 'active' ? 'calm' : 'neutral'}>{label(p.status)}</Chip></td>
							<td><button class="ghost" onclick={() => selectProduct(p)}>{open === p.id ? 'Close' : 'SKUs'}</button></td>
						</tr>
						{#if open === p.id}
							<tr class="detail">
								<td colspan="6">
									<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.createSKU({ product_id: p.id, ...sku, currency: sku.currency.trim().toUpperCase(), created_by: settings.actorOrUnknown }), () => skus.run((s) => settings.api().commerce.listProductSKUs(p.id, { signal: s }))); }}>
										<div class="controls">
											<div class="field"><label for="kc-{p.id}">Code</label><input id="kc-{p.id}" bind:value={sku.code} size="12" /></div>
											<div class="field grow"><label for="kn-{p.id}">Name</label><input id="kn-{p.id}" bind:value={sku.name} /></div>
											<div class="field"><label for="kp-{p.id}">Price</label><input id="kp-{p.id}" bind:value={sku.price} size="9" inputmode="decimal" /></div>
											<div class="field"><label for="ku-{p.id}">Currency</label><input id="ku-{p.id}" bind:value={sku.currency} size="4" /></div>
											<div class="field"><label for="kt-{p.id}">Unit</label><input id="kt-{p.id}" bind:value={sku.unit} size="6" /></div>
											<div class="field"><label for="kz-{p.id}">Size</label><input id="kz-{p.id}" bind:value={sku.unit_size} size="8" inputmode="decimal" /></div>
											<button type="submit" disabled={saving || !sku.code.trim()}>Add SKU</button>
										</div>
									</form>
									<Await task={skus} isEmpty={(s) => (s.skus ?? []).length === 0} empty="No SKUs for this product.">
										{#snippet children(s)}
											<div class="tablewrap">
												<table>
													<thead><tr><th>Code</th><th>Name</th><th class="num">Price</th><th>Size</th><th>Status</th><th></th></tr></thead>
													<tbody>
														{#each s.skus as k (k.id)}
															<tr>
																<td class="mono">{k.code}</td>
																<td>{k.name}</td>
																<td class="num">{formatExact(k.price, k.currency)}</td>
																<td>{quantity(k.unit_size, k.unit)}</td>
																<td><Chip tone={k.status === 'active' ? 'calm' : 'neutral'}>{label(k.status)}</Chip></td>
																<td>
																	<button class="ghost" onclick={() => { repricing = repricing === k.id ? undefined : k.id; price = k.price; }}>
																		{repricing === k.id ? 'Cancel' : 'Reprice'}
																	</button>
																</td>
															</tr>
															{#if repricing === k.id}
																<tr class="detail">
																	<td colspan="6">
																		<form onsubmit={(e) => { e.preventDefault(); run(() => settings.api().commerce.updateSKUPrice({ id: k.id, price: price.trim(), updated_by: settings.actorOrUnknown }), () => { repricing = undefined; skus.run((sg) => settings.api().commerce.listProductSKUs(p.id, { signal: sg })); }); }}>
																			<div class="controls">
																				<div class="field"><label for="np-{k.id}">New price</label><input id="np-{k.id}" bind:value={price} size="10" inputmode="decimal" /></div>
																				<button type="submit" disabled={saving}>Set</button>
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
	.field.grow { flex: 1 1 14rem; }
</style>
