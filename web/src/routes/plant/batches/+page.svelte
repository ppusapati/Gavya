<script lang="ts">
	import {
		ApiError,
		BATCH_KINDS,
		BATCH_SOURCE_KINDS,
		BATCH_STATUSES,
		DENSITY_SOURCES,
		ROUNDING_MODES,
		TRACE_DIRECTIONS,
		UNITS,
		type GetBatchGenealogyResponse,
		type GetBatchYieldResponse,
		type ListProductionBatchesResponse,
		type ProductionBatch,
		type ProductionBatchResponse,
		type TraceBatchResponse
	} from '$lib/api';
	import { instant, label, quantity, today, toLocalInput } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const batches = new Task<ListProductionBatchesResponse>();
	const genealogy = new Task<GetBatchGenealogyResponse>();
	const trace = new Task<TraceBatchResponse>();
	const batchYield = new Task<GetBatchYieldResponse>();

	// Looked up by the code on the vessel, which is what somebody standing in a
	// plant actually has. The list is a period and a limit; a lot from six weeks
	// ago is not on it.
	const found = new Task<ProductionBatchResponse>();
	let lookup = $state('');

	let from = $state(monthAgo());
	let to = $state(today());

	function monthAgo(): string {
		const d = new Date();
		d.setMonth(d.getMonth() - 1);
		return d.toISOString().slice(0, 10);
	}

	function load() {
		batches.run((s) =>
			settings.api().plant.listBatches({ from, to: to || undefined, limit: 200 }, { signal: s })
		);
	}
	$effect(() => {
		void from;
		void to;
		load();
	});

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

	function iso(local: string): string {
		const d = new Date(local);
		return Number.isNaN(d.getTime()) ? '' : d.toISOString().replace(/\.\d{3}Z$/, 'Z');
	}

	let creating = $state(false);
	let nb = $state({
		code: '',
		kind: 'RAW',
		product_ref: '',
		value: '',
		unit: '',
		produced_at: toLocalInput(new Date()),
		source_kind: 'MOVEMENT',
		source_ref: '',
		formulation_code: ''
	});

	let open = $state<string | undefined>(undefined);
	let direction = $state('BACKWARD');
	let bounded = $state(false);
	let maxDepth = $state(10);
	let maxBatches = $state(500);

	let input = $state({ input_code: '', value: '', unit: '' });

	let useDensity = $state(false);
	let dens = $state({ kg_per_litre: '', scale: 4, at_celsius: 40, source: '', rounding: '' });

	let statusFor = $state<string | undefined>(undefined);
	let newStatus = $state('QUARANTINED');
	let statusReason = $state('');

	function openBatch(b: ProductionBatch) {
		if (open === b.id) {
			open = undefined;
			return;
		}
		open = b.id;
		saveError = undefined;
		genealogy.run((s) => settings.api().plant.getBatchGenealogy({ id: b.id }, { signal: s }));
		trace.reset();
		batchYield.reset();
	}

	function runTrace(id: string) {
		trace.run((s) =>
			settings.api().plant.traceBatch(
				{
					id,
					direction,
					max_depth: bounded ? maxDepth : undefined,
					max_batches: bounded ? maxBatches : undefined
				},
				{ signal: s }
			)
		);
	}

	function runYield(id: string) {
		batchYield.run((s) =>
			settings.api().plant.getBatchYield(
				{
					id,
					density: useDensity
						? {
								kg_per_litre: dens.kg_per_litre.trim(),
								scale: dens.scale,
								at_celsius: dens.at_celsius,
								source: dens.source
							}
						: undefined,
					rounding_mode: useDensity ? dens.rounding : undefined
				},
				{ signal: s }
			)
		);
	}

	function tone(status: string) {
		switch (status) {
			case 'RELEASED':
				return 'calm';
			case 'QUARANTINED':
			case 'RECALLED':
				return 'critical';
			case 'DISPOSED':
				return 'neutral';
			default:
				return 'neutral';
		}
	}
</script>

<div class="page-head">
	<h1>Batches</h1>
	<p>
		What was made, what went into it, and what can be reached from it. A batch says outright whether
		it is held rather than leaving a reader to work it out from the status, because a reader who
		works it out wrongly ships quarantined milk.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<div class="field"><label for="f">From</label><input id="f" type="date" bind:value={from} /></div>
	<div class="field"><label for="t">To</label><input id="t" type="date" bind:value={to} /></div>
	<button class="ghost" onclick={load} disabled={batches.pending}>Refresh</button>
	<button onclick={() => { creating = !creating; saveError = undefined; }}>
		{creating ? 'Cancel' : 'Record a batch'}
	</button>
</div>

<div class="controls">
	<div class="field">
		<label for="lk">Find by the code on the vessel</label>
		<input id="lk" bind:value={lookup} size="16" />
	</div>
	<button
		class="ghost"
		disabled={!lookup.trim() || found.pending}
		onclick={() => found.run((s) => settings.api().plant.getBatch({ code: lookup.trim() }, { signal: s }))}
	>
		Find
	</button>
	{#if found.settled}
		<button class="ghost" onclick={() => { found.reset(); lookup = ''; }}>Clear</button>
	{/if}
</div>

{#if found.settled}
	<Await task={found} isEmpty={(d) => !d.batch} empty="No batch carries that code.">
		{#snippet children(d)}
			<div class="panel">
				<dl class="kv">
					<dt>Code</dt><dd class="mono">{d.batch.code}</dd>
					<dt>Product</dt><dd>{d.batch.product_ref}</dd>
					<dt>Kind</dt><dd>{label(d.batch.kind)}</dd>
					<dt>Produced</dt><dd>{quantity(d.batch.produced.value, d.batch.produced.unit)} on {instant(d.batch.produced_at)}</dd>
					<dt>Status</dt>
					<dd>
						<Chip tone={tone(d.batch.status)} title={d.batch.status_reason ?? ''}>{label(d.batch.status)}</Chip>
						{#if d.batch.held}<Chip tone="critical">held</Chip>{/if}
					</dd>
					{#if d.batch.status_reason}
						<dt>Because</dt><dd>{d.batch.status_reason}</dd>
					{/if}
				</dl>
			</div>
		{/snippet}
	</Await>
{/if}

{#if creating}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().plant.createBatch({
						code: nb.code.trim(),
						kind: nb.kind,
						product_ref: nb.product_ref.trim(),
						produced: { value: nb.value.trim(), unit: nb.unit },
						produced_at: iso(nb.produced_at),
						produced_by: settings.actorOrUnknown,
						source_kind: nb.kind === 'RAW' ? nb.source_kind : undefined,
						source_ref: nb.kind === 'RAW' ? nb.source_ref.trim() : undefined,
						formulation_code: nb.formulation_code.trim() || undefined,
						actor: settings.actorOrUnknown
					}),
				() => {
					creating = false;
					nb = { ...nb, code: '', value: '', source_ref: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="bc">Code</label><input id="bc" bind:value={nb.code} size="14" /></div>
			<div class="field">
				<label for="bk">Kind</label>
				<select id="bk" bind:value={nb.kind}>
					{#each BATCH_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
				</select>
			</div>
			<div class="field grow"><label for="bp">Product</label><input id="bp" bind:value={nb.product_ref} /></div>
			<div class="field"><label for="bq">Produced</label><input id="bq" bind:value={nb.value} size="10" inputmode="decimal" /></div>
			<div class="field">
				<label for="bu">Unit</label>
				<select id="bu" bind:value={nb.unit}>
					<option value="">—</option>
					{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="ba">At</label><input id="ba" type="datetime-local" bind:value={nb.produced_at} /></div>
		</div>

		{#if nb.kind === 'RAW'}
			<div class="controls">
				<div class="field">
					<label for="bsk">Came from</label>
					<select id="bsk" bind:value={nb.source_kind}>
						{#each BATCH_SOURCE_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
					</select>
				</div>
				<div class="field grow"><label for="bsr">Reference</label><input id="bsr" bind:value={nb.source_ref} /></div>
			</div>
			<p class="muted note">
				Where raw milk came from is required on a raw batch and refused on any other: a batch made
				from other batches already says where it came from, and two accounts of that would not
				reconcile.
			</p>
		{:else}
			<div class="controls">
				<div class="field grow">
					<label for="bf">Recipe code</label>
					<input id="bf" bind:value={nb.formulation_code} placeholder="optional" />
				</div>
			</div>
			<p class="muted note">
				A recipe code is resolved to the version in force at the moment above, which is why that
				moment is required beside it. A plant that has not written its recipes down still records
				what it made.
			</p>
		{/if}

		<div class="controls">
			<button type="submit" disabled={saving || !nb.code.trim() || !nb.value.trim() || !nb.unit || !nb.product_ref.trim()}>
				Record
			</button>
		</div>
	</form>
{/if}

<Await task={batches} retry={load} isEmpty={(d) => (d.batches ?? []).length === 0} empty="Nothing was made in this period.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Code</th><th>Kind</th><th>Product</th><th class="num">Produced</th><th>When</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.batches as b (b.id)}
						<tr class:held={b.held}>
							<td class="mono">{b.code}</td>
							<td>{label(b.kind)}</td>
							<td>{b.product_ref}</td>
							<td class="num">{quantity(b.produced.value, b.produced.unit)}</td>
							<td>{instant(b.produced_at)}</td>
							<td>
								<Chip tone={tone(b.status)} title={b.status_reason ?? ''}>{label(b.status)}</Chip>
								{#if b.held}<Chip tone="critical" title="This lot may not be fed into anything.">held</Chip>{/if}
							</td>
							<td class="actions">
								<button class="ghost" onclick={() => openBatch(b)}>{open === b.id ? 'Close' : 'Open'}</button>
								<button class="ghost" onclick={() => { statusFor = statusFor === b.id ? undefined : b.id; statusReason = ''; }}>
									{statusFor === b.id ? 'Cancel' : 'Set status'}
								</button>
							</td>
						</tr>

						{#if b.status_reason}
							<tr class="detail"><td colspan="7"><strong>{label(b.status)}:</strong> {b.status_reason}</td></tr>
						{/if}

						{#if statusFor === b.id}
							<tr class="detail">
								<td colspan="7">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.setBatchStatus({
														id: b.id,
														status: newStatus,
														reason: statusReason.trim() || undefined,
														actor: settings.actorOrUnknown
													}),
												() => {
													statusFor = undefined;
													load();
												}
											);
										}}
									>
										<div class="controls">
											<div class="field">
												<label for="ss-{b.id}">Status</label>
												<select id="ss-{b.id}" bind:value={newStatus}>
													{#each BATCH_STATUSES as s (s)}<option value={s}>{label(s)}</option>{/each}
												</select>
											</div>
											<div class="field grow">
												<label for="sr-{b.id}">Reason</label>
												<input id="sr-{b.id}" bind:value={statusReason} placeholder="What the plant will be told." />
											</div>
											<button type="submit" disabled={saving}>Set</button>
										</div>
										<p class="muted note">
											A reason is required to put a batch under hold and required to lift one. A
											quarantine nobody can explain gets lifted by whoever is on shift; a quarantine
											lifted with no explanation is one nobody can defend afterwards.
										</p>
									</form>
								</td>
							</tr>
						{/if}

						{#if open === b.id}
							<tr class="detail">
								<td colspan="7">
									<h3>What went into it</h3>
									<Await task={genealogy} isEmpty={(g) => !g.batch} empty="No such batch.">
										{#snippet children(g)}
											<p class="figure">
												{quantity(g.remaining.value, g.remaining.unit)}
												<span class="muted">left after everything drawn from it</span>
											</p>
											{#if (g.inputs ?? []).length === 0}
												<p class="muted">Nothing is recorded as going into this batch.</p>
											{:else}
												<div class="tablewrap">
													<table>
														<thead><tr><th>Lot</th><th class="num">Consumed</th></tr></thead>
														<tbody>
															{#each g.inputs as i (i.id)}
																<tr>
																	<td class="mono">{i.input_code || i.input_batch_id}</td>
																	<td class="num">{quantity(i.consumed.value, i.consumed.unit)}</td>
																</tr>
															{/each}
														</tbody>
													</table>
												</div>
											{/if}
										{/snippet}
									</Await>

									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.recordInput({
														output_batch_id: b.id,
														input_code: input.input_code.trim(),
														consumed: { value: input.value.trim(), unit: input.unit },
														actor: settings.actorOrUnknown
													}),
												() => {
													input = { input_code: '', value: '', unit: '' };
													genealogy.run((s) =>
														settings.api().plant.getBatchGenealogy({ id: b.id }, { signal: s })
													);
												}
											);
										}}
									>
										<div class="controls">
											<div class="field"><label for="ic-{b.id}">Lot going in</label><input id="ic-{b.id}" bind:value={input.input_code} size="14" /></div>
											<div class="field"><label for="iq-{b.id}">Consumed</label><input id="iq-{b.id}" bind:value={input.value} size="10" inputmode="decimal" /></div>
											<div class="field">
												<label for="iu-{b.id}">Unit</label>
												<select id="iu-{b.id}" bind:value={input.unit}>
													<option value="">—</option>
													{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
												</select>
											</div>
											<button type="submit" disabled={saving || !input.input_code.trim() || !input.value.trim() || !input.unit}>
												Record input
											</button>
										</div>
									</form>

									<h3>Yield</h3>
									<div class="controls">
										<label class="check">
											<input type="checkbox" bind:checked={useDensity} />
											Give a density
										</label>
										{#if useDensity}
											<div class="field"><label for="yk-{b.id}">kg per litre</label><input id="yk-{b.id}" bind:value={dens.kg_per_litre} size="8" inputmode="decimal" /></div>
											<div class="field"><label for="ys-{b.id}">Scale</label><input id="ys-{b.id}" type="number" min="0" max="9" bind:value={dens.scale} size="3" /></div>
											<div class="field"><label for="yc-{b.id}">Tenths of °C</label><input id="yc-{b.id}" type="number" bind:value={dens.at_celsius} size="5" /></div>
											<div class="field">
												<label for="yd-{b.id}">Read by</label>
												<select id="yd-{b.id}" bind:value={dens.source}>
													<option value="">—</option>
													{#each DENSITY_SOURCES as s (s)}<option value={s}>{label(s)}</option>{/each}
												</select>
											</div>
											<div class="field">
												<label for="yr-{b.id}">Rounding</label>
												<select id="yr-{b.id}" bind:value={dens.rounding}>
													<option value="">—</option>
													{#each ROUNDING_MODES as r (r)}<option value={r}>{label(r)}</option>{/each}
												</select>
											</div>
										{/if}
										<button class="ghost" disabled={batchYield.pending} onclick={() => runYield(b.id)}>Work it out</button>
									</div>
									<p class="muted note">
										A batch weighed in kilograms and made from milk measured in litres has no yield
										without a density, and the 1.03 everybody quotes is a figure nobody measured for
										this milk.
									</p>

									{#if batchYield.settled}
										<Await task={batchYield} isEmpty={(y) => !y.batch} empty="No yield.">
											{#snippet children(y)}
												<dl class="kv">
													<dt>Produced</dt><dd>{quantity(y.produced.value, y.produced.unit)}</dd>
													<dt>Consumed</dt><dd>{quantity(y.consumed.value, y.consumed.unit)}</dd>
													<dt>Observed</dt>
													<dd>
														{#if y.observed_yield_percent}
															{y.observed_yield_percent}%
														{:else}
															<Chip tone="attention" title={y.unavailable_reason ?? ''}>not available</Chip>
															<span class="muted">{y.unavailable_reason}</span>
														{/if}
													</dd>
													<dt>Against the recipe</dt>
													<dd>
														{#if y.no_expectation_declared}
															<span class="muted">
																No target was declared for this process, which is not the same as
																having met one.
															</span>
														{:else if y.variance_percent}
															{y.variance_percent}%
														{:else}
															—
														{/if}
													</dd>
												</dl>
											{/snippet}
										</Await>
									{/if}

									<h3>Trace</h3>
									<div class="controls">
										<div class="field">
											<label for="td-{b.id}">Direction</label>
											<select id="td-{b.id}" bind:value={direction}>
												{#each TRACE_DIRECTIONS as x (x)}
													<option value={x}>
														{x === 'BACKWARD' ? 'Backward — what it was made from' : 'Forward — what was made from it'}
													</option>
												{/each}
											</select>
										</div>
										<label class="check">
											<input type="checkbox" bind:checked={bounded} />
											Bound the walk
										</label>
										{#if bounded}
											<div class="field"><label for="tmd-{b.id}">Max depth</label><input id="tmd-{b.id}" type="number" min="1" bind:value={maxDepth} size="4" /></div>
											<div class="field"><label for="tmb-{b.id}">Max batches</label><input id="tmb-{b.id}" type="number" min="1" bind:value={maxBatches} size="5" /></div>
										{/if}
										<button class="ghost" disabled={trace.pending} onclick={() => runTrace(b.id)}>Walk</button>
									</div>

									{#if trace.settled}
										<Await task={trace} isEmpty={(r) => !r.batch} empty="Nothing to trace.">
											{#snippet children(r)}
												{#if !r.complete}
													<p class="banner">
														<Chip tone="critical">incomplete</Chip>
														{r.warning || 'The walk stopped before it finished.'}
														{#if (r.frontier ?? []).length}
															<span class="muted">
																Continue from: {(r.frontier ?? []).join(', ')}
															</span>
														{/if}
													</p>
													<p class="warn">
														Acting on this list recalls some of what it should. It is not the answer
														to the question until it says it is complete.
													</p>
												{/if}
												{#if (r.unreadable ?? []).length}
													<p class="banner">
														<Chip tone="critical">{(r.unreadable ?? []).length} unreadable</Chip>
														<span class="mono">{(r.unreadable ?? []).join(', ')}</span>
													</p>
												{/if}

												<h4>Left the plant ({r.finished.length})</h4>
												{#if r.finished.length === 0}
													<p class="muted">Nothing traced here has shipped.</p>
												{:else}
													<div class="tablewrap">
														<table>
															<thead><tr><th>Code</th><th>Product</th><th class="num">Depth</th><th>Via</th><th>Status</th></tr></thead>
															<tbody>
																{#each r.finished as a (a.batch.id)}
																	<tr>
																		<td class="mono">{a.batch.code}</td>
																		<td>{a.batch.product_ref}</td>
																		<td class="num">{a.depth}</td>
																		<td class="mono muted">{a.via || '—'}</td>
																		<td><Chip tone={tone(a.batch.status)}>{label(a.batch.status)}</Chip></td>
																	</tr>
																{/each}
															</tbody>
														</table>
													</div>
												{/if}

												<h4>Everything reached ({r.affected.length})</h4>
												<div class="tablewrap">
													<table>
														<thead><tr><th>Code</th><th>Kind</th><th class="num">Depth</th><th>Via</th><th>Status</th></tr></thead>
														<tbody>
															{#each r.affected as a (a.batch.id)}
																<tr>
																	<td class="mono">{a.batch.code}</td>
																	<td>{label(a.batch.kind)}</td>
																	<td class="num">{a.depth}</td>
																	<td class="mono muted">{a.via || '—'}</td>
																	<td><Chip tone={tone(a.batch.status)}>{label(a.batch.status)}</Chip></td>
																</tr>
															{/each}
														</tbody>
													</table>
												</div>
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
	h4 { margin: 1rem 0 0.4rem; font-size: 0.82rem; font-weight: 500; }
	.detail td { background: var(--surface-2); }
	.held td:first-child { box-shadow: inset 3px 0 0 var(--bad, #b3261e); }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
	.figure { font-size: 1.15rem; margin: 0.3rem 0 0.8rem; }
	.figure .muted { font-size: 0.82rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.8rem 0; font-size: 0.85rem; }
	.warn { max-width: var(--measure); margin: 0.4rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
</style>
