<script lang="ts">
	import { ApiError, type Farm, type ListFarmsResponse } from '$lib/api';
	import { label, quantity, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const farms = new Task<ListFarmsResponse>();
	const sections = new Task<{ sections: { id: string; name: string; section_type: string; capacity: number; current_occupancy: number }[] }>();
	const feeds = new Task<{ feed_types: { id: string; name: string; category: string; unit: string; nutritional_info: string }[] }>();
	const report = new Task<{ reports: { cattle_id: string; feed_type_id: string; total_kg: string }[] }>();

	function loadFarms() {
		farms.run((signal) => settings.api().herd.listFarms({ signal }));
	}
	function loadFeeds() {
		feeds.run((signal) => settings.api().herd.listFeedTypes({ signal }));
	}

	$effect(loadFarms);
	$effect(loadFeeds);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(event: SubmitEvent | undefined, fn: () => Promise<unknown>, after: () => void) {
		event?.preventDefault();
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	/* ---- farms ---- */

	let openFarm = $state<string | undefined>(undefined);
	let addingFarm = $state(false);
	let farmForm = $state({
		name: '', code: '', address: '', city: '', state: '', country: 'India',
		capacity: 0, manager_id: '', status: 'active'
	});
	let capacity = $state(0);
	let sectionForm = $state({ name: '', section_type: '', capacity: 0, current_occupancy: 0 });

	function selectFarm(f: Farm) {
		openFarm = openFarm === f.id ? undefined : f.id;
		capacity = f.capacity;
		sectionForm = { name: '', section_type: '', capacity: 0, current_occupancy: 0 };
		saveError = undefined;
		if (openFarm) {
			sections.run((signal) => settings.api().herd.listFarmSections(f.id, { signal }));
		}
	}

	/* ---- feed ---- */

	let addingFeed = $state(false);
	let feedForm = $state({ name: '', category: '', unit: 'kg', nutritional_info: '' });

	let planForm = $state({ cattle_id: '', feed_type_id: '', daily_quantity_kg: '', notes: '' });
	let showPlan = $state(false);

	let reportForm = $state({ cattle_id: '', from: today(), to: today() });
</script>

<div class="page-head">
	<h1>Farms &amp; feed</h1>
	<p>
		Where the herd is kept, how much room there is, and what it is fed. Capacity and occupancy are
		whole animals; rations are exact decimals in whatever unit the feed is measured in.
	</p>
</div>

<ErrorNote error={saveError} />

<section>
	<h2>Farms</h2>
	<div class="controls">
		<button class="ghost" onclick={loadFarms} disabled={farms.pending}>Refresh</button>
		<button onclick={() => { addingFarm = !addingFarm; saveError = undefined; }}>
			{addingFarm ? 'Cancel' : 'Add a farm'}
		</button>
	</div>

	{#if addingFarm}
		<form
			class="panel"
			onsubmit={(e) =>
				run(e, () => settings.api().herd.createFarm({ ...farmForm, created_by: settings.actorOrUnknown }), () => {
					addingFarm = false;
					farmForm = { name: '', code: '', address: '', city: '', state: '', country: 'India', capacity: 0, manager_id: '', status: 'active' };
					loadFarms();
				})}
		>
			<div class="controls">
				<div class="field"><label for="fn">Name</label><input id="fn" bind:value={farmForm.name} /></div>
				<div class="field"><label for="fc">Code</label><input id="fc" bind:value={farmForm.code} size="10" /></div>
				<div class="field grow"><label for="fa">Address</label><input id="fa" bind:value={farmForm.address} /></div>
				<div class="field"><label for="fi">City</label><input id="fi" bind:value={farmForm.city} size="14" /></div>
				<div class="field"><label for="fs">State</label><input id="fs" bind:value={farmForm.state} size="14" /></div>
				<div class="field"><label for="fk">Capacity</label><input id="fk" type="number" min="0" bind:value={farmForm.capacity} size="6" /></div>
				<button type="submit" disabled={saving || !farmForm.name.trim()}>{saving ? 'Adding…' : 'Add'}</button>
			</div>
		</form>
	{/if}

	<Await task={farms} retry={loadFarms} isEmpty={(d) => (d.farms ?? []).length === 0} empty="No farms recorded.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Code</th><th>Name</th><th>Where</th><th class="num">Capacity</th><th>Status</th><th></th></tr></thead>
					<tbody>
						{#each d.farms as f (f.id)}
							<tr>
								<td class="mono">{f.code}</td>
								<td>{f.name}</td>
								<td>{[f.city, f.state].filter(Boolean).join(', ') || '—'}</td>
								<td class="num">{f.capacity}</td>
								<td><Chip tone={f.status === 'active' ? 'calm' : 'neutral'}>{label(f.status)}</Chip></td>
								<td><button class="ghost" onclick={() => selectFarm(f)}>{openFarm === f.id ? 'Close' : 'Sections'}</button></td>
							</tr>
							{#if openFarm === f.id}
								<tr class="detail">
									<td colspan="6">
										<form
											onsubmit={(e) =>
												run(e, () => settings.api().herd.updateFarmCapacity({ id: f.id, capacity, updated_by: settings.actorOrUnknown }), loadFarms)}
										>
											<div class="controls">
												<div class="field"><label for="cap-{f.id}">Capacity</label><input id="cap-{f.id}" type="number" min="0" bind:value={capacity} size="6" /></div>
												<button type="submit" disabled={saving}>Set capacity</button>
											</div>
										</form>

										<form
											onsubmit={(e) =>
												run(e, () => settings.api().herd.createFarmSection({ farm_id: f.id, ...sectionForm, created_by: settings.actorOrUnknown }), () => {
													sectionForm = { name: '', section_type: '', capacity: 0, current_occupancy: 0 };
													sections.run((s) => settings.api().herd.listFarmSections(f.id, { signal: s }));
												})}
										>
											<div class="controls">
												<div class="field"><label for="sn-{f.id}">Section</label><input id="sn-{f.id}" bind:value={sectionForm.name} size="18" /></div>
												<div class="field"><label for="st-{f.id}">Kind</label><input id="st-{f.id}" bind:value={sectionForm.section_type} size="12" placeholder="milking, dry, calf" /></div>
												<div class="field"><label for="sc-{f.id}">Capacity</label><input id="sc-{f.id}" type="number" min="0" bind:value={sectionForm.capacity} size="6" /></div>
												<div class="field"><label for="so-{f.id}">Occupied</label><input id="so-{f.id}" type="number" min="0" bind:value={sectionForm.current_occupancy} size="6" /></div>
												<button type="submit" disabled={saving || !sectionForm.name.trim()}>Add section</button>
											</div>
										</form>

										<Await task={sections} isEmpty={(s) => (s.sections ?? []).length === 0} empty="No sections in this farm.">
											{#snippet children(s)}
												<div class="tablewrap">
													<table>
														<thead><tr><th>Section</th><th>Kind</th><th class="num">Capacity</th><th class="num">Occupied</th></tr></thead>
														<tbody>
															{#each s.sections as sec (sec.id)}
																<tr>
																	<td>{sec.name}</td>
																	<td>{label(sec.section_type)}</td>
																	<td class="num">{sec.capacity}</td>
																	<td class="num">
																		<Chip tone={sec.current_occupancy > sec.capacity ? 'critical' : 'neutral'}>
																			{sec.current_occupancy}
																		</Chip>
																	</td>
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
</section>

<section>
	<h2>Feed</h2>
	<div class="controls">
		<button class="ghost" onclick={loadFeeds} disabled={feeds.pending}>Refresh</button>
		<button onclick={() => { addingFeed = !addingFeed; saveError = undefined; }}>
			{addingFeed ? 'Cancel' : 'Add a feed'}
		</button>
		<button class="ghost" onclick={() => (showPlan = !showPlan)}>
			{showPlan ? 'Hide ration' : 'Set a ration'}
		</button>
	</div>

	{#if addingFeed}
		<form
			class="panel"
			onsubmit={(e) =>
				run(e, () => settings.api().herd.createFeedType({ ...feedForm, created_by: settings.actorOrUnknown }), () => {
					addingFeed = false;
					feedForm = { name: '', category: '', unit: 'kg', nutritional_info: '' };
					loadFeeds();
				})}
		>
			<div class="controls">
				<div class="field"><label for="dn">Name</label><input id="dn" bind:value={feedForm.name} /></div>
				<div class="field"><label for="dc">Category</label><input id="dc" bind:value={feedForm.category} size="14" /></div>
				<div class="field"><label for="du">Unit</label><input id="du" bind:value={feedForm.unit} size="6" /></div>
				<div class="field grow"><label for="di">Nutrition</label><input id="di" bind:value={feedForm.nutritional_info} placeholder="CP 22%, TDN 72%" /></div>
				<button type="submit" disabled={saving || !feedForm.name.trim()}>{saving ? 'Adding…' : 'Add'}</button>
			</div>
		</form>
	{/if}

	{#if showPlan}
		<form
			class="panel"
			onsubmit={(e) =>
				run(e, () =>
					settings.api().herd.createNutritionPlan({
						cattle_id: planForm.cattle_id.trim(),
						feed_type_id: planForm.feed_type_id,
						daily_quantity_kg: planForm.daily_quantity_kg.trim(),
						start_date: new Date().toISOString(),
						notes: planForm.notes.trim(),
						created_by: settings.actorOrUnknown
					}), () => {
						planForm = { cattle_id: '', feed_type_id: '', daily_quantity_kg: '', notes: '' };
					})}
		>
			<h3>A standing ration</h3>
			<div class="controls">
				<div class="field"><label for="pa">Animal</label><input id="pa" bind:value={planForm.cattle_id} size="26" /></div>
				<div class="field">
					<label for="pf">Feed</label>
					<select id="pf" bind:value={planForm.feed_type_id}>
						<option value="">—</option>
						{#each feeds.data?.feed_types ?? [] as f (f.id)}<option value={f.id}>{f.name}</option>{/each}
					</select>
				</div>
				<div class="field"><label for="pq">Per day (kg)</label><input id="pq" bind:value={planForm.daily_quantity_kg} size="8" inputmode="decimal" /></div>
				<div class="field grow"><label for="pn">Notes</label><input id="pn" bind:value={planForm.notes} /></div>
				<button type="submit" disabled={saving || !planForm.cattle_id.trim() || !planForm.feed_type_id}>
					{saving ? 'Setting…' : 'Set'}
				</button>
			</div>
		</form>
	{/if}

	<Await task={feeds} retry={loadFeeds} isEmpty={(d) => (d.feed_types ?? []).length === 0} empty="No feeds recorded.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Feed</th><th>Category</th><th>Unit</th><th>Nutrition</th></tr></thead>
					<tbody>
						{#each d.feed_types as f (f.id)}
							<tr>
								<td>{f.name}</td>
								<td>{label(f.category)}</td>
								<td>{f.unit}</td>
								<td class="muted">{f.nutritional_info || '—'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>What was eaten</h2>
	<form
		class="controls"
		onsubmit={(e) => {
			e.preventDefault();
			report.run((signal) =>
				settings.api().herd.getFeedConsumptionReport(
					{
						cattle_id: reportForm.cattle_id.trim(),
						from: new Date(reportForm.from).toISOString(),
						to: new Date(reportForm.to).toISOString()
					},
					{ signal }
				)
			);
		}}
	>
		<div class="field"><label for="ra">Animal</label><input id="ra" bind:value={reportForm.cattle_id} size="26" /></div>
		<div class="field"><label for="rf">From</label><input id="rf" type="date" bind:value={reportForm.from} /></div>
		<div class="field"><label for="rt">To</label><input id="rt" type="date" bind:value={reportForm.to} /></div>
		<button type="submit" disabled={report.pending || !reportForm.cattle_id.trim()}>Report</button>
	</form>

	{#if report.settled}
		<Await task={report} isEmpty={(d) => (d.reports ?? []).length === 0} empty="Nothing was recorded as fed in that window.">
			{#snippet children(d)}
				<div class="tablewrap">
					<table>
						<thead><tr><th>Feed</th><th class="num">Total</th></tr></thead>
						<tbody>
							{#each d.reports as r (r.feed_type_id)}
								<tr>
									<td>{(feeds.data?.feed_types ?? []).find((f) => f.id === r.feed_type_id)?.name ?? r.feed_type_id}</td>
									<td class="num">{quantity(r.total_kg, 'kg')}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/snippet}
		</Await>
	{/if}
</section>

<style>
	section {
		margin-top: 2rem;
	}

	h3 {
		margin: 0 0 0.6rem;
		font-size: 0.9rem;
	}

	.detail td {
		background: var(--surface-2);
	}

	.field.grow {
		flex: 1 1 14rem;
	}
</style>
