<script lang="ts">
	import {
		ApiError,
		UNITS,
		type CheckRecipeResponse,
		type Formulation,
		type FormulationInput,
		type FormulationResponse,
		type GetObservedYieldResponse,
		type ListFormulationsResponse
	} from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const formulations = new Task<ListFormulationsResponse>();
	const detail = new Task<FormulationResponse>();
	const observed = new Task<GetObservedYieldResponse>();
	const check = new Task<CheckRecipeResponse>();

	function load() {
		formulations.run((s) => settings.api().plant.listFormulations({ signal: s }));
	}
	$effect(load);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function asApiError(c: unknown): ApiError {
		return c instanceof ApiError
			? c
			: new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
		} catch (c) {
			saveError = asApiError(c);
		} finally {
			saving = false;
		}
	}

	let creating = $state(false);
	let nf = $state({
		code: '',
		name: '',
		output_product_ref: '',
		output_unit: '',
		expected_yield_percent: '',
		expectation_basis: '',
		valid_from: today(),
		valid_to: ''
	});
	let ingredients = $state<
		{ product_ref: string; share_percent: string; tolerance_percent: string; required: boolean }[]
	>([]);

	/**
	 * Per cent to parts per million, without floating point.
	 *
	 * 94.5% is 945000 ppm. Multiplying by ten thousand in a double is right for
	 * that figure and wrong by one for others, and a yield target one ppm off
	 * what somebody typed is a target they cannot recognise. So the digits are
	 * shifted as text.
	 *
	 * More than four decimal places is finer than a ppm can hold, and this
	 * refuses it rather than truncating. Silently dropping the fifth digit would
	 * store a target nobody entered and show it back as though they had.
	 */
	function toPPM(percent: string): number | undefined {
		const t = percent.trim();
		if (t === '' || !/^\d+(\.\d{1,4})?$/.test(t)) return undefined;
		const [whole, frac = ''] = t.split('.');
		return Number(whole + (frac + '0000').slice(0, 4));
	}

	function ppmOK(percent: string): boolean {
		return percent.trim() === '' || toPPM(percent) !== undefined;
	}

	const figuresOK = $derived(
		ppmOK(nf.expected_yield_percent) &&
			ingredients.every((i) => ppmOK(i.share_percent) && ppmOK(i.tolerance_percent))
	);

	let open = $state<string | undefined>(undefined);
	let approving = $state<string | undefined>(undefined);
	let approval = $state({ at: '', note: '' });
	let withdrawing = $state<string | undefined>(undefined);
	let withdrawReason = $state('');
	let batchCode = $state('');

	function openRecipe(f: Formulation) {
		if (open === f.id) {
			open = undefined;
			return;
		}
		open = f.id;
		saveError = undefined;
		detail.run((s) => settings.api().plant.getFormulation({ id: f.id }, { signal: s }));
		observed.run((s) => settings.api().plant.getObservedYield({ id: f.id }, { signal: s }));
		check.reset();
		batchCode = '';
	}

	function statusTone(f: Formulation) {
		if (f.status === 'APPROVED') return 'calm';
		if (f.status === 'WITHDRAWN') return 'neutral';
		return 'attention';
	}
</script>

<div class="page-head">
	<h1>Recipes</h1>
	<p>
		What a process is meant to yield, who signed off on the figure, and what this plant's own vats
		actually did. The platform holds no table of standard yields and will not invent one — a target
		is either something a plant derived from its own history or something somebody put their name
		to, and it says which.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<button class="ghost" onclick={load} disabled={formulations.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>
		{creating ? 'Cancel' : 'Draft a recipe'}
	</button>
</div>

{#if creating}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			const inputs: FormulationInput[] = ingredients
				.filter((i) => i.product_ref.trim())
				.map((i) => ({
					product_ref: i.product_ref.trim(),
					expected_share_ppm: toPPM(i.share_percent),
					share_tolerance_ppm: toPPM(i.tolerance_percent),
					required: i.required
				}));
			run(
				() =>
					settings.api().plant.createFormulation({
						code: nf.code.trim(),
						name: nf.name.trim(),
						output_product_ref: nf.output_product_ref.trim(),
						output_unit: nf.output_unit,
						expected_yield_ppm: toPPM(nf.expected_yield_percent),
						expectation_basis: nf.expectation_basis.trim() || undefined,
						valid_from: nf.valid_from,
						valid_to: nf.valid_to || undefined,
						inputs,
						actor: settings.actorOrUnknown
					}),
				() => {
					creating = false;
					ingredients = [];
					nf = { ...nf, code: '', name: '', expected_yield_percent: '', expectation_basis: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="rc">Code</label><input id="rc" bind:value={nf.code} size="14" /></div>
			<div class="field grow"><label for="rn">Name</label><input id="rn" bind:value={nf.name} /></div>
			<div class="field grow"><label for="ro">Makes</label><input id="ro" bind:value={nf.output_product_ref} /></div>
			<div class="field">
				<label for="ru">Unit</label>
				<select id="ru" bind:value={nf.output_unit}>
					<option value="">—</option>
					{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="rvf">In force from</label><input id="rvf" type="date" bind:value={nf.valid_from} /></div>
			<div class="field"><label for="rvt">Until</label><input id="rvt" type="date" bind:value={nf.valid_to} /></div>
		</div>

		<div class="controls">
			<div class="field">
				<label for="ry">Expected yield %</label>
				<input id="ry" bind:value={nf.expected_yield_percent} size="8" inputmode="decimal" placeholder="optional" />
			</div>
			<div class="field grow">
				<label for="rb">Where that figure came from</label>
				<input id="rb" bind:value={nf.expectation_basis} placeholder="Required beside a target." />
			</div>
		</div>
		{#if !ppmOK(nf.expected_yield_percent)}
			<p class="warn">A yield is a plain decimal percentage — 94.5, not 94.5% and not a fraction.</p>
		{/if}
		<p class="muted note">
			A target a plant derived from two hundred of its own vats and one read off a supplier's
			leaflet are different claims, so the service requires the basis beside the figure. A recipe
			with no period would silently apply to every batch ever made, including the ones made before
			it existed — which is why the date above is required.
		</p>

		<h3>Ingredients</h3>
		{#each ingredients as ing, i (i)}
			<div class="controls">
				<div class="field grow"><label for="ip-{i}">Product</label><input id="ip-{i}" bind:value={ing.product_ref} /></div>
				<div class="field"><label for="is-{i}">Share %</label><input id="is-{i}" bind:value={ing.share_percent} size="7" inputmode="decimal" placeholder="optional" /></div>
				<div class="field"><label for="it-{i}">Tolerance %</label><input id="it-{i}" bind:value={ing.tolerance_percent} size="7" inputmode="decimal" placeholder="optional" /></div>
				<label class="check"><input type="checkbox" bind:checked={ing.required} /> Required</label>
				<button type="button" class="ghost" onclick={() => (ingredients = ingredients.filter((_, j) => j !== i))}>Remove</button>
			</div>
			{#if !ppmOK(ing.share_percent) || !ppmOK(ing.tolerance_percent)}
				<p class="warn">
					A share is a plain decimal percentage with at most four decimal places — finer than that
					is finer than a part per million, and this refuses it rather than quietly dropping a
					digit.
				</p>
			{/if}
		{/each}
		<div class="controls">
			<button
				type="button"
				class="ghost"
				onclick={() => (ingredients = [...ingredients, { product_ref: '', share_percent: '', tolerance_percent: '', required: true }])}
			>
				Add an ingredient
			</button>
			<button type="submit" disabled={saving || !nf.code.trim() || !nf.output_product_ref.trim() || !nf.output_unit || !figuresOK}>
				Save as draft
			</button>
		</div>
		<p class="muted note">
			Shares need not sum to a hundred: a recipe naming its two main ingredients and leaving the
			salt undeclared is an ordinary recipe, and demanding the rest would invent a figure. Without a
			tolerance no share finding is raised at all — how close is close enough is a question about
			this plant's process and its scales, and a number chosen here would be the platform's opinion
			in a report with the plant's name on it.
		</p>
	</form>
{/if}

<Await task={formulations} retry={load} isEmpty={(d) => (d.formulations ?? []).length === 0} empty="No recipes are recorded.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Code</th><th>Name</th><th>Makes</th><th class="num">Expected</th><th>In force</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.formulations as f (f.id)}
						<tr>
							<td class="mono">{f.code}</td>
							<td>{f.name}</td>
							<td>{f.output_product_ref}</td>
							<td class="num">{f.expected_yield_percent ? `${f.expected_yield_percent}%` : '—'}</td>
							<td>
								{instant(f.valid_from)}{f.valid_to ? ` — ${instant(f.valid_to)}` : ''}
							</td>
							<td>
								<Chip tone={statusTone(f)} title={f.withdrawn_reason ?? ''}>{label(f.status)}</Chip>
								{#if !f.usable_for_production}
									<Chip tone="neutral" title="A batch may not be made against this version.">not usable</Chip>
								{/if}
							</td>
							<td class="actions">
								<button class="ghost" onclick={() => openRecipe(f)}>{open === f.id ? 'Close' : 'Open'}</button>
								{#if f.status === 'DRAFT'}
									<button class="ghost" onclick={() => { approving = approving === f.id ? undefined : f.id; approval = { at: '', note: '' }; }}>
										{approving === f.id ? 'Cancel' : 'Approve'}
									</button>
								{/if}
								{#if f.status !== 'WITHDRAWN'}
									<button class="ghost" onclick={() => { withdrawing = withdrawing === f.id ? undefined : f.id; withdrawReason = ''; }}>
										{withdrawing === f.id ? 'Cancel' : 'Withdraw'}
									</button>
								{/if}
							</td>
						</tr>

						{#if approving === f.id}
							<tr class="detail">
								<td colspan="7">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.approveFormulation({
														id: f.id,
														approver: settings.actorOrUnknown,
														at: approval.at || undefined,
														note: approval.note.trim() || undefined
													}),
												() => {
													approving = undefined;
													load();
												}
											);
										}}
									>
										<div class="controls">
											<div class="field"><label for="aa-{f.id}">Agreed on</label><input id="aa-{f.id}" type="date" bind:value={approval.at} /></div>
											<div class="field grow"><label for="an-{f.id}">Note</label><input id="an-{f.id}" bind:value={approval.note} /></div>
											<button type="submit" disabled={saving}>Approve as {settings.actorOrUnknown}</button>
										</div>
										<p class="muted note">
											Approving is an act with a name against it, not a field somebody filled in while
											typing the rest. Leave the date empty and the platform stamps now — right for
											somebody approving as they type, wrong for a recipe agreed at a Tuesday meeting
											and entered on Thursday.
										</p>
									</form>
								</td>
							</tr>
						{/if}

						{#if withdrawing === f.id}
							<tr class="detail">
								<td colspan="7">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.withdrawFormulation({
														id: f.id,
														reason: withdrawReason.trim(),
														actor: settings.actorOrUnknown
													}),
												() => {
													withdrawing = undefined;
													load();
												}
											);
										}}
									>
										<div class="controls">
											<div class="field grow">
												<label for="wr-{f.id}">Why it was withdrawn</label>
												<input id="wr-{f.id}" bind:value={withdrawReason} placeholder="A recipe withdrawn with no reason is one nobody can explain reintroducing." />
											</div>
											<button type="submit" disabled={saving || !withdrawReason.trim()}>Withdraw</button>
										</div>
									</form>
								</td>
							</tr>
						{/if}

						{#if open === f.id}
							<tr class="detail">
								<td colspan="7">
									<Await task={detail} isEmpty={(x) => !x.formulation} empty="No such recipe.">
										{#snippet children(x)}
											<dl class="kv">
												<dt>Status</dt>
												<dd>
													<Chip tone={statusTone(x.formulation)}>{label(x.formulation.status)}</Chip>
													{#if x.formulation.approved_by}
														approved by {x.formulation.approved_by}
														{x.formulation.approved_at ? `on ${instant(x.formulation.approved_at)}` : ''}
													{/if}
												</dd>
												{#if x.formulation.approval_note}
													<dt>Approval note</dt><dd>{x.formulation.approval_note}</dd>
												{/if}
												{#if x.formulation.expectation_basis}
													<dt>Target's basis</dt><dd>{x.formulation.expectation_basis}</dd>
												{/if}
												{#if x.formulation.withdrawn_reason}
													<dt>Withdrawn because</dt><dd>{x.formulation.withdrawn_reason}</dd>
												{/if}
											</dl>

											{#if (x.formulation.inputs ?? []).length}
												<h4>Ingredients</h4>
												<div class="tablewrap">
													<table>
														<thead><tr><th>Product</th><th class="num">Expected share</th><th class="num">Tolerance</th><th>Required</th></tr></thead>
														<tbody>
															{#each x.formulation.inputs ?? [] as i (i.product_ref)}
																<tr>
																	<td>{i.product_ref}</td>
																	<td class="num">{i.expected_share_ppm ? `${i.expected_share_ppm} ppm` : '—'}</td>
																	<td class="num">
																		{#if i.share_tolerance_ppm}
																			{i.share_tolerance_ppm} ppm
																		{:else}
																			<Chip tone="neutral" title="Observed and declared shares are reported; nothing is judged.">none declared</Chip>
																		{/if}
																	</td>
																	<td>{i.required ? 'Yes' : 'No'}</td>
																</tr>
															{/each}
														</tbody>
													</table>
												</div>
											{/if}
										{/snippet}
									</Await>

									<h4>What this plant's vats actually did</h4>
									<Await task={observed} isEmpty={(o) => !o.formulation_id} empty="No history.">
										{#snippet children(o)}
											<p class="muted note">{o.note}</p>
											{#if o.batches_counted > 0}
												<dl class="kv">
													<dt>Lowest</dt><dd>{o.lowest_percent ? `${o.lowest_percent}%` : '—'}</dd>
													<dt>Median</dt><dd>{o.median_percent ? `${o.median_percent}%` : '—'}</dd>
													<dt>Highest</dt><dd>{o.highest_percent ? `${o.highest_percent}%` : '—'}</dd>
													<dt>Batches counted</dt><dd>{o.batches_counted}</dd>
													<dt>Left out for want of a density</dt>
													<dd>
														{o.batches_needing_a_density}
														{#if o.batches_needing_a_density > 0}
															<Chip tone="attention">not in the figures above</Chip>
														{/if}
													</dd>
												</dl>
											{/if}
										{/snippet}
									</Await>

									<h4>Hold a batch up against it</h4>
									<div class="controls">
										<div class="field"><label for="cb-{f.id}">Batch code</label><input id="cb-{f.id}" bind:value={batchCode} size="14" /></div>
										<button
											class="ghost"
											disabled={!batchCode.trim() || check.pending}
											onclick={() => check.run((s) => settings.api().plant.checkRecipe({ code: batchCode.trim() }, { signal: s }))}
										>
											Check
										</button>
									</div>
									<p class="muted note">
										Nothing here refuses anything. A plant substitutes, and a vat recorded with a note
										beside it beats a vat not recorded at all.
									</p>

									{#if check.settled}
										<Await task={check} isEmpty={(c) => !c.batch} empty="No such batch.">
											{#snippet children(c)}
												<p class="banner">
													<span class="mono">{c.batch.code}</span>
													{#if c.serious_count > 0}
														<Chip tone="critical">{c.serious_count} somebody has to answer for</Chip>
													{:else}
														<Chip tone="calm">nothing serious</Chip>
													{/if}
												</p>

												{#if c.findings.length}
													<ul class="findings">
														{#each c.findings as fd, i (i)}
															<li>
																<Chip tone={fd.serious ? 'critical' : 'neutral'}>{label(fd.kind)}</Chip>
																<span>{fd.explanation}</span>
															</li>
														{/each}
													</ul>
												{/if}

												{#if c.shares_unavailable_reason}
													<p class="banner">
														<Chip tone="attention">no shares</Chip>
														{c.shares_unavailable_reason}
													</p>
												{:else if c.shares.length}
													<div class="tablewrap">
														<table>
															<thead><tr><th>Product</th><th class="num">Declared</th><th class="num">Observed</th><th class="num">Difference</th><th>Tolerance</th></tr></thead>
															<tbody>
																{#each c.shares as sh (sh.product_ref)}
																	<tr>
																		<td>{sh.product_ref}</td>
																		<td class="num">{sh.expected_percent}%</td>
																		<td class="num">{sh.observed_percent}%</td>
																		<td class="num">{sh.difference_ppm} ppm</td>
																		<td>
																			{#if sh.tolerance_ppm}
																				{sh.tolerance_ppm} ppm
																			{:else}
																				<span class="muted">none declared — reported, not judged</span>
																			{/if}
																		</td>
																	</tr>
																{/each}
															</tbody>
														</table>
													</div>
												{/if}
											{/snippet}
										</Await>
									{/if}
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
	h3 { margin: 1.4rem 0 0.5rem; font-size: 0.88rem; font-weight: 600; }
	h4 { margin: 1.2rem 0 0.4rem; font-size: 0.82rem; font-weight: 500; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.8rem 0; font-size: 0.85rem; }
	.findings { list-style: none; padding: 0; margin: 0.6rem 0; display: grid; gap: 0.5rem; max-width: var(--measure); }
	.findings li { display: flex; gap: 0.6rem; align-items: baseline; }
	.warn { max-width: var(--measure); margin: 0.4rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
</style>
