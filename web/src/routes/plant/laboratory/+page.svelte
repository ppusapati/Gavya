<script lang="ts">
	import {
		ANALYTES,
		ApiError,
		SAMPLE_PURPOSES,
		SAMPLE_SOURCE_KINDS,
		type GetSampleReportResponse,
		type ListSamplesResponse,
		type Sample,
		type SampleResponse
	} from '$lib/api';
	import { instant, label, today, toLocalInput } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const samples = new Task<ListSamplesResponse>();
	const report = new Task<GetSampleReportResponse>();
	const one = new Task<SampleResponse>();

	let from = $state(weekAgo());
	let to = $state(today());
	let lookup = $state('');

	function weekAgo(): string {
		const d = new Date();
		d.setDate(d.getDate() - 7);
		return d.toISOString().slice(0, 10);
	}

	function load() {
		samples.run((s) =>
			settings.api().plant.listSamples({ from, to: to || undefined, limit: 200 }, { signal: s })
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

	let drawing = $state(false);
	let draw = $state({
		code: '',
		source_kind: 'COLLECTION',
		source_ref: '',
		drawn_at: toLocalInput(new Date()),
		seal_number: '',
		purpose: 'PAYMENT',
		duplicates_sample_id: ''
	});

	let open = $state<string | undefined>(undefined);
	let breaking = $state<string | undefined>(undefined);
	let seal = $state({ reason: '', at: '' });

	let handover = $state({ at: toLocalInput(new Date()), from: '', to: '', note: '' });

	let result = $state({
		analyte: 'FAT',
		value: '',
		scale: 2,
		method: '',
		instrument_ref: '',
		instrument_valid_until: '',
		instrument_certificate: '',
		analysed_at: toLocalInput(new Date())
	});

	/**
	 * The service refuses a value whose decimal places and stated scale
	 * disagree, and it is right to: 4.1 and 4.10 are the same number and not the
	 * same claim about how precisely it was read. The form says so before the
	 * bench sends it rather than after.
	 */
	const writtenScale = $derived.by(() => {
		const t = result.value.trim().replace(/^-/, '');
		const dot = t.indexOf('.');
		return dot < 0 ? 0 : t.length - dot - 1;
	});
	const scaleAgrees = $derived(result.value.trim() === '' || writtenScale === result.scale);

	function openSample(s: Sample) {
		if (open === s.id) {
			open = undefined;
			return;
		}
		open = s.id;
		saveError = undefined;
		report.run((sig) => settings.api().plant.getSampleReport(s.id, { signal: sig }));
	}

	function refreshReport(id: string) {
		report.run((sig) => settings.api().plant.getSampleReport(id, { signal: sig }));
	}

	function eligibilityTone(v: string) {
		switch (v) {
			case 'ELIGIBLE':
				return 'calm';
			case 'NOT_ELIGIBLE':
				return 'critical';
			default:
				return 'attention';
		}
	}
</script>

<div class="page-head">
	<h1>Laboratory</h1>
	<p>
		Samples, who held them, what they read, and which of those readings a payment may rest on. A
		reading never appears here without its verdict beside it: somebody shown a figure with no
		verdict will use it.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<div class="field"><label for="f">From</label><input id="f" type="date" bind:value={from} /></div>
	<div class="field"><label for="t">To</label><input id="t" type="date" bind:value={to} /></div>
	<button class="ghost" onclick={load} disabled={samples.pending}>Refresh</button>
	<button onclick={() => { drawing = !drawing; saveError = undefined; }}>
		{drawing ? 'Cancel' : 'Draw a sample'}
	</button>
</div>

<div class="controls">
	<div class="field"><label for="lk">Find a sample by id</label><input id="lk" bind:value={lookup} size="24" /></div>
	<button
		class="ghost"
		disabled={!lookup.trim() || one.pending}
		onclick={() => one.run((s) => settings.api().plant.getSample(lookup.trim(), { signal: s }))}
	>
		Find
	</button>
	{#if one.settled}
		<button class="ghost" onclick={() => { one.reset(); lookup = ''; }}>Clear</button>
	{/if}
</div>

{#if one.settled}
	<Await task={one} isEmpty={(d) => !d.sample} empty="No such sample.">
		{#snippet children(d)}
			<div class="panel">
				<dl class="kv">
					<dt>Code</dt><dd class="mono">{d.sample.code}</dd>
					<dt>Drawn</dt><dd>{instant(d.sample.drawn_at)} by {d.sample.drawn_by}</dd>
					<dt>From</dt><dd>{label(d.sample.source_kind)} <span class="mono">{d.sample.source_ref}</span></dd>
					<dt>Purpose</dt><dd>{label(d.sample.purpose)}</dd>
					<dt>Seal</dt>
					<dd>
						{#if d.sample.sealed}
							<Chip tone="calm">intact — {d.sample.seal_number}</Chip>
						{:else if d.sample.seal_number}
							<Chip tone="critical" title={d.sample.seal_broken_reason ?? ''}>broken</Chip>
							<span class="muted">{d.sample.seal_broken_reason}</span>
						{:else}
							<Chip tone="neutral">never sealed</Chip>
						{/if}
					</dd>
				</dl>
			</div>
		{/snippet}
	</Await>
{/if}

{#if drawing}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().plant.drawSample({
						code: draw.code.trim(),
						source_kind: draw.source_kind,
						source_ref: draw.source_ref.trim(),
						drawn_at: iso(draw.drawn_at),
						drawn_by: settings.actorOrUnknown,
						seal_number: draw.seal_number.trim() || undefined,
						purpose: draw.purpose,
						duplicates_sample_id: draw.duplicates_sample_id.trim() || undefined,
						actor: settings.actorOrUnknown
					}),
				() => {
					drawing = false;
					draw = { ...draw, code: '', source_ref: '', seal_number: '', duplicates_sample_id: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="sc">Code</label><input id="sc" bind:value={draw.code} size="14" /></div>
			<div class="field">
				<label for="sk">Drawn from</label>
				<select id="sk" bind:value={draw.source_kind}>
					{#each SAMPLE_SOURCE_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
				</select>
			</div>
			<div class="field grow"><label for="sr">Reference</label><input id="sr" bind:value={draw.source_ref} /></div>
			<div class="field"><label for="sd">Drawn at</label><input id="sd" type="datetime-local" bind:value={draw.drawn_at} /></div>
			<div class="field">
				<label for="sp">Purpose</label>
				<select id="sp" bind:value={draw.purpose}>
					{#each SAMPLE_PURPOSES as p (p)}<option value={p}>{label(p)}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="ss">Seal</label><input id="ss" bind:value={draw.seal_number} size="12" placeholder="optional" /></div>
			{#if draw.purpose === 'DUPLICATE'}
				<div class="field"><label for="sdup">Duplicates</label><input id="sdup" bind:value={draw.duplicates_sample_id} size="24" /></div>
			{/if}
			<button type="submit" disabled={saving || !draw.code.trim() || !draw.source_ref.trim()}>Draw</button>
		</div>
		{#if draw.purpose === 'PAYMENT' && !draw.seal_number.trim()}
			<p class="warn">
				A payment sample without a seal is recorded, and its results are not eligible to price
				milk. That is the platform's rule, not a form validation — it will accept this and the
				verdict will say so.
			</p>
		{/if}
		<p class="muted note">
			A process check is not sealed, and demanding a seal for one would make the platform tiresome
			about a reading that decides nothing.
		</p>
	</form>
{/if}

<Await task={samples} retry={load} isEmpty={(d) => (d.samples ?? []).length === 0} empty="No samples were drawn in this period.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Code</th><th>Drawn</th><th>From</th><th>Purpose</th><th>Seal</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.samples as s (s.id)}
						<tr>
							<td class="mono">{s.code}</td>
							<td>{instant(s.drawn_at)}</td>
							<td>{label(s.source_kind)} <span class="mono muted">{s.source_ref}</span></td>
							<td>{label(s.purpose)}</td>
							<td>
								{#if s.sealed}
									<Chip tone="calm">{s.seal_number}</Chip>
								{:else if s.seal_number}
									<Chip tone="critical" title={s.seal_broken_reason ?? ''}>broken</Chip>
								{:else}
									<Chip tone="neutral">unsealed</Chip>
								{/if}
							</td>
							<td class="actions">
								<button class="ghost" onclick={() => openSample(s)}>{open === s.id ? 'Close' : 'Open'}</button>
								{#if s.sealed}
									<button class="ghost" onclick={() => { breaking = breaking === s.id ? undefined : s.id; seal = { reason: '', at: '' }; }}>
										{breaking === s.id ? 'Cancel' : 'Break seal'}
									</button>
								{/if}
							</td>
						</tr>

						{#if breaking === s.id}
							<tr class="detail">
								<td colspan="6">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.breakSeal({
														id: s.id,
														reason: seal.reason.trim(),
														at: seal.at ? iso(seal.at) : undefined,
														actor: settings.actorOrUnknown
													}),
												() => {
													breaking = undefined;
													load();
												}
											);
										}}
									>
										<div class="controls">
											<div class="field grow">
												<label for="br-{s.id}">Why the seal was broken</label>
												<input id="br-{s.id}" bind:value={seal.reason} />
											</div>
											<div class="field"><label for="ba-{s.id}">When</label><input id="ba-{s.id}" type="datetime-local" bind:value={seal.at} /></div>
											<button type="submit" disabled={saving || !seal.reason.trim()}>Break</button>
										</div>
										<p class="muted note">
											The seal is what rules out the sample having been changed, so breaking it
											silently defeats it. Leave the time empty and the platform stamps now — right
											for a bottle being opened at the bench as somebody types, wrong for a
											laboratory book written up at the end of a shift.
										</p>
									</form>
								</td>
							</tr>
						{/if}

						{#if open === s.id}
							<tr class="detail">
								<td colspan="6">
									<Await task={report} isEmpty={(r) => !r.sample} empty="Nothing is recorded for this sample.">
										{#snippet children(r)}
											<p class="banner">
												{#if r.custody_intact}
													<Chip tone="calm">custody intact</Chip>
												{:else}
													<Chip tone="critical" title={r.custody_reason ?? ''}>custody broken</Chip>
													<span class="muted">{r.custody_reason}</span>
												{/if}
												{#if r.custody_holder}
													<span class="muted">held by {r.custody_holder}</span>
												{/if}
												<Chip tone={r.eligible === r.total && r.total > 0 ? 'calm' : 'attention'}>
													{r.eligible} of {r.total} readings fit to price milk
												</Chip>
											</p>

											<h3>Chain of custody</h3>
											{#if r.chain.length === 0}
												<p class="muted">Nobody has recorded handing this sample on.</p>
											{:else}
												<div class="tablewrap">
													<table>
														<thead><tr><th class="num">#</th><th>When</th><th>From</th><th>To</th><th>Note</th></tr></thead>
														<tbody>
															{#each r.chain as h (h.sequence)}
																<tr>
																	<td class="num">{h.sequence}</td>
																	<td>{instant(h.at)}</td>
																	<td>{h.from}</td>
																	<td>{h.to}</td>
																	<td class="muted">{h.note || '—'}</td>
																</tr>
															{/each}
														</tbody>
													</table>
												</div>
											{/if}

											<form
												onsubmit={(e) => {
													e.preventDefault();
													run(
														() =>
															settings.api().plant.recordHandover({
																sample_id: s.id,
																at: iso(handover.at),
																from: handover.from.trim(),
																to: handover.to.trim(),
																note: handover.note.trim() || undefined,
																actor: settings.actorOrUnknown
															}),
														() => {
															handover = { ...handover, from: '', to: '', note: '' };
															refreshReport(s.id);
														}
													);
												}}
											>
												<div class="controls">
													<div class="field"><label for="hat-{s.id}">When</label><input id="hat-{s.id}" type="datetime-local" bind:value={handover.at} /></div>
													<div class="field"><label for="hf-{s.id}">From</label><input id="hf-{s.id}" bind:value={handover.from} size="16" /></div>
													<div class="field"><label for="ht-{s.id}">To</label><input id="ht-{s.id}" bind:value={handover.to} size="16" /></div>
													<div class="field grow"><label for="hn-{s.id}">Note</label><input id="hn-{s.id}" bind:value={handover.note} /></div>
													<button type="submit" disabled={saving || !handover.from.trim() || !handover.to.trim()}>Record handover</button>
												</div>
											</form>

											<h3>Readings</h3>
											{#if r.analytes.length === 0}
												<p class="muted">Nothing has been analysed yet.</p>
											{/if}
											{#each r.analytes as g (g.analyte)}
												<h4>
													{label(g.analyte)}
													{#if g.spread}
														<Chip tone="attention" title="The difference between the highest and lowest reading of this analyte.">
															spread {g.spread.value}
														</Chip>
													{:else if g.spread_unavailable_reason}
														<Chip tone="neutral" title={g.spread_unavailable_reason}>no spread</Chip>
													{/if}
												</h4>
												<div class="tablewrap">
													<table>
														<thead><tr><th class="num">Reading</th><th>Method</th><th>Instrument</th><th>Analysed</th><th>Fit to pay on</th></tr></thead>
														<tbody>
															{#each g.results as res (res.id)}
																<tr>
																	<td class="num">{res.reading.value}</td>
																	<td>{res.method}</td>
																	<td>
																		<span class="mono">{res.instrument_ref}</span>
																		{#if res.instrument_valid_until}
																			<span class="muted">until {res.instrument_valid_until}</span>
																		{/if}
																	</td>
																	<td>{instant(res.analysed_at)}</td>
																	<td>
																		<Chip tone={eligibilityTone(res.eligibility)} title={res.eligibility_reason}>
																			{label(res.eligibility)}
																		</Chip>
																		<span class="muted">{res.eligibility_reason}</span>
																	</td>
																</tr>
															{/each}
														</tbody>
													</table>
												</div>
											{/each}
											{#if r.analytes.some((g) => g.results.length > 1)}
												<p class="muted note">
													Where one analyte was read more than once, both readings stand. A
													laboratory that ran a sample twice did so to find out whether the two
													agree, and picking one throws away the answer.
												</p>
											{/if}

											<form
												onsubmit={(e) => {
													e.preventDefault();
													run(
														() =>
															settings.api().plant.recordResult({
																sample_id: s.id,
																analyte: result.analyte,
																reading: { value: result.value.trim(), scale: result.scale },
																method: result.method.trim(),
																instrument_ref: result.instrument_ref.trim(),
																instrument_valid_until: result.instrument_valid_until || undefined,
																instrument_certificate: result.instrument_certificate.trim() || undefined,
																analysed_at: iso(result.analysed_at),
																analysed_by: settings.actorOrUnknown,
																actor: settings.actorOrUnknown
															}),
														() => {
															result = { ...result, value: '' };
															refreshReport(s.id);
														}
													);
												}}
											>
												<div class="controls">
													<div class="field">
														<label for="ra-{s.id}">Analyte</label>
														<select id="ra-{s.id}" bind:value={result.analyte}>
															{#each ANALYTES as a (a)}<option value={a}>{label(a)}</option>{/each}
														</select>
													</div>
													<div class="field"><label for="rv-{s.id}">Reading</label><input id="rv-{s.id}" bind:value={result.value} size="9" inputmode="decimal" /></div>
													<div class="field"><label for="rs-{s.id}">Decimal places</label><input id="rs-{s.id}" type="number" min="0" max="6" bind:value={result.scale} size="3" /></div>
													<div class="field"><label for="rm-{s.id}">Method</label><input id="rm-{s.id}" bind:value={result.method} size="14" /></div>
													<div class="field"><label for="ri-{s.id}">Instrument</label><input id="ri-{s.id}" bind:value={result.instrument_ref} size="14" /></div>
													<div class="field"><label for="rvu-{s.id}">Calibrated until</label><input id="rvu-{s.id}" type="date" bind:value={result.instrument_valid_until} /></div>
													<div class="field"><label for="rc-{s.id}">Certificate</label><input id="rc-{s.id}" bind:value={result.instrument_certificate} size="14" /></div>
													<div class="field"><label for="rat-{s.id}">Analysed</label><input id="rat-{s.id}" type="datetime-local" bind:value={result.analysed_at} /></div>
													<button
														type="submit"
														disabled={saving || !result.value.trim() || !result.method.trim() || !result.instrument_ref.trim() || !scaleAgrees}
													>
														Record
													</button>
												</div>
												{#if !scaleAgrees}
													<p class="warn">
														This is written to {writtenScale} decimal place{writtenScale === 1 ? '' : 's'}
														and the resolution says {result.scale}. The two have to agree: 4.1 and 4.10
														are the same number and not the same claim about how precisely it was read.
													</p>
												{/if}
												{#if !result.instrument_valid_until}
													<p class="muted note">
														With no calibration date the verdict reads UNKNOWN rather than raising a
														finding — this service does not hold the certificate register, so an
														absent date means nobody recorded one, not that the instrument is out.
													</p>
												{/if}
											</form>
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
	h3 { margin: 1.4rem 0 0.5rem; font-size: 0.88rem; font-weight: 600; }
	h4 { margin: 1.1rem 0 0.4rem; font-size: 0.82rem; font-weight: 500; display: flex; gap: 0.5rem; align-items: baseline; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.4rem 0 1rem; font-size: 0.85rem; }
	.warn { max-width: var(--measure); margin: 0.6rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
</style>
