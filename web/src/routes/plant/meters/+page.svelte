<script lang="ts">
	import {
		ApiError,
		INSTRUMENT_KINDS,
		ORIGIN_KINDS,
		QUANTITY_KINDS,
		SUBJECT_KINDS,
		type GetActiveCertificateResponse,
		type GetMeterResponse,
		type GetObservationResponse,
		type ListFlaggedObservationsResponse,
		type ListObservationsForSubjectResponse,
		type Observation
	} from '$lib/api';
	import { instant, label, today, toLocalInput } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const flagged = new Task<ListFlaggedObservationsResponse>();
	const history = new Task<ListObservationsForSubjectResponse>();
	const meter = new Task<GetMeterResponse>();
	const certificate = new Task<GetActiveCertificateResponse>();

	// The whole reading, not the columns a table has room for: where it came
	// from, which session it belongs to, and which uncertainty model produced
	// the estimate beside it.
	const reading = new Task<GetObservationResponse>();
	let openReading = $state<string | undefined>(undefined);

	function toggleReading(id: string) {
		if (openReading === id) {
			openReading = undefined;
			reading.reset();
			return;
		}
		openReading = id;
		reading.run((s) => settings.api().plant.getObservation(id, { signal: s }));
	}

	function loadFlagged() {
		flagged.run((s) => settings.api().plant.listFlaggedObservations({ limit: 100 }, { signal: s }));
	}
	$effect(loadFlagged);

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

	/* ---- the subject's history ---- */

	let subject = $state({ kind: 'PRODUCER', id: '' });
	let quantityKind = $state('');
	let validAt = $state('');
	let asOf = $state('');

	function loadHistory() {
		history.run((s) =>
			settings.api().plant.listObservationsForSubject(
				{
					subject: { kind: subject.kind, id: subject.id.trim() },
					quantity_kind: quantityKind || undefined,
					valid_at: validAt ? iso(validAt) : undefined,
					as_of: asOf ? iso(asOf) : undefined,
					limit: 100,
					offset: 0
				},
				{ signal: s }
			)
		);
	}

	/* ---- recording ---- */

	let recording = $state(false);
	let obs = $state({
		quantity_kind: 'VOLUME_LITRES',
		value: '',
		instrument_id: '',
		session_ref: '',
		observed_by: '',
		origin_kind: 'NATIVE',
		source_system_id: '',
		source_record_id: '',
		valid_from: toLocalInput(new Date()),
		valid_to: '',
		corrects: ''
	});

	/* ---- meters and certificates ---- */

	let registering = $state(false);
	let newMeter = $state({ serial: '', kind: 'MILK_ANALYSER', label: '', make: '', model: '' });

	let meterId = $state('');
	let certifying = $state(false);
	let cert = $state({
		certificate_number: '',
		verifying_authority: '',
		issued_at: today(),
		expires_at: '',
		origin_kind: 'NATIVE',
		source_system_id: ''
	});
	let verdictFor = $state('');

	function lookupMeter() {
		const id = meterId.trim();
		if (!id) return;
		meter.run((s) => settings.api().plant.getMeter(id, { signal: s }));
		certificate.run((s) =>
			settings
				.api()
				.plant.getActiveCertificate(
					{ instrument_id: id, quantity_kind: verdictFor || undefined },
					{ signal: s }
				)
		);
	}

	function verdictTone(v: string) {
		switch (v) {
			case 'ELIGIBLE':
				return 'calm';
			case 'NOT_ELIGIBLE':
				return 'critical';
			default:
				return 'attention';
		}
	}

	function anomalyBand(o: Observation): string {
		const a = o.anomaly;
		if (!a) return '';
		const lo = a.lower_bound ?? undefined;
		const hi = a.upper_bound ?? undefined;
		if (lo === undefined && hi === undefined) return 'unbounded';
		return `${lo ?? '−∞'} … ${hi ?? '∞'}`;
	}
</script>

<div class="page-head">
	<h1>Meters and readings</h1>
	<p>
		What a meter measured, what certifies the meter, and which readings the platform will let a
		settlement rest on. A reading with no uncertainty estimate says so outright — an absent estimate
		is not a perfect one, and a consumer left to infer it from a missing field infers zero.
	</p>
</div>

<ErrorNote error={saveError} />

<section>
	<h2>Flagged</h2>
	<p class="muted note">
		Readings the anomaly model scored outside its band. A score is an opinion about a number, not a
		verdict on it: nothing here is rejected, and the band it was judged against is shown beside the
		score so somebody can disagree with the model rather than only with the reading.
	</p>
	<div class="controls">
		<button class="ghost" onclick={loadFlagged} disabled={flagged.pending}>Refresh</button>
		<button onclick={() => { recording = !recording; saveError = undefined; }}>
			{recording ? 'Cancel' : 'Record a reading'}
		</button>
	</div>

	{#if recording}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().plant.recordObservation({
							subject: { kind: subject.kind, id: subject.id.trim() },
							quantity_kind: obs.quantity_kind,
							value: obs.value.trim(),
							instrument_id: obs.instrument_id.trim() || undefined,
							session_ref: obs.session_ref.trim() || undefined,
							observed_by: obs.observed_by.trim() || settings.actorOrUnknown,
							origin: {
								kind: obs.origin_kind,
								source_system_id: obs.source_system_id.trim() || undefined,
								source_record_id: obs.source_record_id.trim() || undefined
							},
							valid_from: iso(obs.valid_from),
							valid_to: obs.valid_to ? iso(obs.valid_to) : undefined,
							corrects: obs.corrects.trim() || undefined,
							created_by: settings.actorOrUnknown
						}),
					() => {
						recording = false;
						obs = { ...obs, value: '', corrects: '' };
						loadFlagged();
						if (subject.id.trim()) loadHistory();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field">
					<label for="osk">Subject</label>
					<select id="osk" bind:value={subject.kind}>
						{#each SUBJECT_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
					</select>
				</div>
				<div class="field grow"><label for="osi">Identifier</label><input id="osi" bind:value={subject.id} /></div>
				<div class="field">
					<label for="oqk">Quantity</label>
					<select id="oqk" bind:value={obs.quantity_kind}>
						{#each QUANTITY_KINDS as q (q)}<option value={q}>{label(q)}</option>{/each}
					</select>
				</div>
				<div class="field"><label for="ov">Value</label><input id="ov" bind:value={obs.value} size="10" inputmode="decimal" /></div>
				<div class="field"><label for="oi">Meter</label><input id="oi" bind:value={obs.instrument_id} size="20" placeholder="optional" /></div>
			</div>
			<div class="controls">
				<div class="field">
					<label for="ook">Origin</label>
					<select id="ook" bind:value={obs.origin_kind}>
						{#each ORIGIN_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
					</select>
				</div>
				{#if obs.origin_kind !== 'NATIVE'}
					<div class="field"><label for="oss">Source system</label><input id="oss" bind:value={obs.source_system_id} size="16" /></div>
					<div class="field"><label for="osr">Source record</label><input id="osr" bind:value={obs.source_record_id} size="16" /></div>
				{/if}
				<div class="field"><label for="ovf">True from</label><input id="ovf" type="datetime-local" bind:value={obs.valid_from} /></div>
				<div class="field"><label for="ovt">Until</label><input id="ovt" type="datetime-local" bind:value={obs.valid_to} /></div>
				<div class="field"><label for="oc">Corrects</label><input id="oc" bind:value={obs.corrects} size="20" placeholder="optional" /></div>
				<button type="submit" disabled={saving || !subject.id.trim() || !obs.value.trim()}>Record</button>
			</div>
			<p class="muted note">
				A correction supersedes rather than edits. The reading it replaces stays readable, so a
				settlement run in January stays explainable after somebody corrects the figure in March.
			</p>
		</form>
	{/if}

	<Await task={flagged} retry={loadFlagged} isEmpty={(d) => (d.observations ?? []).length === 0} empty="Nothing is flagged.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>Subject</th><th>Quantity</th><th class="num">Value</th><th class="num">Score</th><th>Band</th><th>Why</th><th>Fit to pay on</th></tr>
					</thead>
					<tbody>
						{#each d.observations as o (o.id)}
							<tr>
								<td>{label(o.subject.kind)} <span class="mono">{o.subject.id}</span></td>
								<td>{label(o.quantity_kind)}</td>
								<td class="num">{o.value} <span class="muted">{o.unit}</span></td>
								<td class="num">{o.anomaly?.score ?? '—'}</td>
								<td class="muted">{anomalyBand(o)}</td>
								<td class="muted">{o.anomaly?.explanation || '—'}</td>
								<td>
									<Chip tone={verdictTone(o.eligibility_verdict)} title={o.eligibility_reason}>
										{label(o.eligibility_verdict)}
									</Chip>
									{#if o.uncertainty_missing}
										<Chip tone="attention" title="No estimate was obtained. That is not a perfect one.">no uncertainty</Chip>
									{/if}
									<button class="ghost" onclick={() => toggleReading(o.id)}>
										{openReading === o.id ? 'Close' : 'Open'}
									</button>
								</td>
							</tr>
							{#if openReading === o.id}
								<tr class="detail">
									<td colspan="7">
										<Await task={reading} isEmpty={(r) => !r.observation} empty="No such reading.">
											{#snippet children(r)}
												<dl class="kv">
													<dt>Recorded</dt><dd>{instant(r.observation.recorded_at)} by {r.observation.created_by}</dd>
													<dt>True from</dt>
													<dd>{instant(r.observation.valid_from)} — {instant(r.observation.valid_to)}</dd>
													<dt>Origin</dt>
													<dd>
														{label(r.observation.origin.kind)}
														{#if r.observation.origin.source_system_id}
															<span class="mono muted">{r.observation.origin.source_system_id}</span>
														{/if}
														{#if r.observation.origin.source_record_id}
															<span class="mono muted">/{r.observation.origin.source_record_id}</span>
														{/if}
													</dd>
													{#if r.observation.session_ref}
														<dt>Session</dt><dd class="mono">{r.observation.session_ref}</dd>
													{/if}
													{#if r.observation.instrument_id}
														<dt>Meter</dt><dd class="mono">{r.observation.instrument_id}</dd>
													{/if}
													<dt>Uncertainty</dt>
													<dd>
														{#if r.observation.uncertainty}
															± {r.observation.uncertainty.expanded_uncertainty}
															<span class="muted">
																from {r.observation.uncertainty.uncertainty_model_id}
																{r.observation.uncertainty.model_version ?? ''}
															</span>
														{:else}
															<span class="muted">
																None was obtained. The value carries no stated confidence, which is
																not the same as carrying a perfect one.
															</span>
														{/if}
													</dd>
													<dt>Fit to pay on</dt>
													<dd>
														<Chip tone={verdictTone(r.observation.eligibility_verdict)}>
															{label(r.observation.eligibility_verdict)}
														</Chip>
														<span class="muted">{r.observation.eligibility_reason}</span>
													</dd>
													{#if r.observation.supersedes}
														<dt>Replaces</dt><dd class="mono">{r.observation.supersedes}</dd>
													{/if}
													{#if r.observation.superseded_by}
														<dt>Replaced by</dt><dd class="mono">{r.observation.superseded_by}</dd>
													{/if}
												</dl>
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
	<h2>One subject's readings</h2>
	<p class="muted note">
		Two different questions, asked separately. "True at" selects the fact that was true of the world
		at that instant; "known at" selects what the platform knew at that instant. A correction entered
		last week changes the second answer for a date in March and leaves the first alone.
	</p>
	<div class="controls">
		<div class="field">
			<label for="hk">Subject</label>
			<select id="hk" bind:value={subject.kind}>
				{#each SUBJECT_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
			</select>
		</div>
		<div class="field grow"><label for="hi">Identifier</label><input id="hi" bind:value={subject.id} /></div>
		<div class="field">
			<label for="hq">Quantity</label>
			<select id="hq" bind:value={quantityKind}>
				<option value="">Every quantity</option>
				{#each QUANTITY_KINDS as q (q)}<option value={q}>{label(q)}</option>{/each}
			</select>
		</div>
		<div class="field"><label for="hv">True at</label><input id="hv" type="datetime-local" bind:value={validAt} /></div>
		<div class="field"><label for="ha">Known at</label><input id="ha" type="datetime-local" bind:value={asOf} /></div>
		<button class="ghost" disabled={!subject.id.trim() || history.pending} onclick={loadHistory}>Look up</button>
	</div>

	{#if history.settled}
		<Await task={history} isEmpty={(d) => (d.observations ?? []).length === 0} empty="Nothing is recorded for that subject.">
			{#snippet children(d)}
				<div class="tablewrap">
					<table>
						<thead>
							<tr><th>Quantity</th><th class="num">Value</th><th>True from</th><th>Recorded</th><th>Uncertainty</th><th>Fit to pay on</th><th></th></tr>
						</thead>
						<tbody>
							{#each d.observations as o (o.id)}
								<tr class:superseded={!o.current}>
									<td>{label(o.quantity_kind)}</td>
									<td class="num">{o.value} <span class="muted">{o.unit}</span></td>
									<td>{instant(o.valid_from)}</td>
									<td>{instant(o.recorded_at)}</td>
									<td>
										{#if o.uncertainty}
											± {o.uncertainty.expanded_uncertainty}
											<span class="muted">
												at k={o.uncertainty.coverage_factor}, p={o.uncertainty.coverage_probability}
											</span>
										{:else}
											<Chip tone="attention" title="No estimate was obtained. The value carries no stated confidence — which is not the same as carrying a perfect one.">none stated</Chip>
										{/if}
									</td>
									<td>
										<Chip tone={verdictTone(o.eligibility_verdict)} title={o.eligibility_reason}>
											{label(o.eligibility_verdict)}
										</Chip>
									</td>
									<td>
										{#if !o.current}
											<Chip tone="neutral" title={o.superseded_at ? `Superseded ${instant(o.superseded_at)}` : ''}>superseded</Chip>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/snippet}
		</Await>
	{/if}
</section>

<section>
	<h2>Meters</h2>
	<div class="controls">
		<div class="field"><label for="mi">Meter id</label><input id="mi" bind:value={meterId} size="24" /></div>
		<div class="field">
			<label for="mv">Verdict for</label>
			<select id="mv" bind:value={verdictFor}>
				<option value="">No quantity — certificate only</option>
				{#each QUANTITY_KINDS as q (q)}<option value={q}>{label(q)}</option>{/each}
			</select>
		</div>
		<button class="ghost" disabled={!meterId.trim() || meter.pending} onclick={lookupMeter}>Look up</button>
		<button class="ghost" onclick={() => { registering = !registering; saveError = undefined; }}>
			{registering ? 'Cancel' : 'Register a meter'}
		</button>
	</div>

	{#if registering}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().plant.registerMeter({
							serial: newMeter.serial.trim(),
							kind: newMeter.kind,
							label: newMeter.label.trim() || undefined,
							make: newMeter.make.trim() || undefined,
							model: newMeter.model.trim() || undefined,
							actor: settings.actorOrUnknown
						}),
					() => {
						registering = false;
						newMeter = { serial: '', kind: 'MILK_ANALYSER', label: '', make: '', model: '' };
					}
				);
			}}
		>
			<div class="controls">
				<div class="field"><label for="ms">Serial</label><input id="ms" bind:value={newMeter.serial} size="18" /></div>
				<div class="field">
					<label for="mk">Kind</label>
					<select id="mk" bind:value={newMeter.kind}>
						{#each INSTRUMENT_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
					</select>
				</div>
				<div class="field grow"><label for="ml">Label</label><input id="ml" bind:value={newMeter.label} /></div>
				<div class="field"><label for="mm">Make</label><input id="mm" bind:value={newMeter.make} size="14" /></div>
				<div class="field"><label for="mo">Model</label><input id="mo" bind:value={newMeter.model} size="14" /></div>
				<button type="submit" disabled={saving || !newMeter.serial.trim()}>Register</button>
			</div>
		</form>
	{/if}

	{#if meter.settled}
		<Await task={meter} isEmpty={(d) => !d.instrument} empty="No such meter.">
			{#snippet children(d)}
				<div class="panel">
					<dl class="kv">
						<dt>Serial</dt><dd class="mono">{d.instrument.serial}</dd>
						<dt>Kind</dt><dd>{label(d.instrument.kind)}</dd>
						<dt>Label</dt><dd>{d.instrument.label || '—'}</dd>
						<dt>Make and model</dt>
						<dd>{[d.instrument.make, d.instrument.model].filter(Boolean).join(' ') || '—'}</dd>
						<dt>Registered</dt><dd>{instant(d.instrument.created_at)}</dd>
					</dl>
				</div>
			{/snippet}
		</Await>
	{/if}

	{#if certificate.settled}
		<Await task={certificate} isEmpty={(d) => !d.certificate} empty="No certificate is in force for that meter.">
			{#snippet children(d)}
				<div class="panel">
					<dl class="kv">
						<dt>Certificate</dt><dd class="mono">{d.certificate.certificate_number}</dd>
						<dt>Issued by</dt><dd>{d.certificate.verifying_authority}</dd>
						<dt>In force</dt><dd>{instant(d.certificate.issued_at)} — {instant(d.certificate.expires_at)}</dd>
						<dt>Recorded as</dt><dd>{label(d.certificate.origin.kind)}</dd>
						{#if d.eligibility}
							<dt>For {label(verdictFor)}</dt>
							<dd>
								<Chip tone={verdictTone(d.eligibility.verdict)}>{label(d.eligibility.verdict)}</Chip>
								<span class="muted">{d.eligibility.reason}</span>
							</dd>
						{:else}
							<dt>Verdict</dt>
							<dd class="muted">
								Name a quantity above to see what a settlement would record. The verdict depends on
								it: a quantity outside legal metrology is eligible whatever the certificate says.
							</dd>
						{/if}
					</dl>
				</div>
			{/snippet}
		</Await>
	{/if}

	<div class="controls">
		<button
			class="ghost"
			disabled={!meterId.trim()}
			onclick={() => { certifying = !certifying; saveError = undefined; }}
		>
			{certifying ? 'Cancel' : 'Record a certificate'}
		</button>
	</div>

	{#if certifying}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().plant.recordCertificate({
							instrument_id: meterId.trim(),
							certificate_number: cert.certificate_number.trim(),
							verifying_authority: cert.verifying_authority.trim(),
							issued_at: new Date(cert.issued_at).toISOString().replace(/\.\d{3}Z$/, 'Z'),
							expires_at: new Date(cert.expires_at).toISOString().replace(/\.\d{3}Z$/, 'Z'),
							origin: {
								kind: cert.origin_kind,
								source_system_id: cert.source_system_id.trim() || undefined
							},
							created_by: settings.actorOrUnknown
						}),
					() => {
						certifying = false;
						lookupMeter();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field"><label for="cn">Number</label><input id="cn" bind:value={cert.certificate_number} size="18" /></div>
				<div class="field grow"><label for="ca">Verifying authority</label><input id="ca" bind:value={cert.verifying_authority} /></div>
				<div class="field"><label for="ci">Issued</label><input id="ci" type="date" bind:value={cert.issued_at} /></div>
				<div class="field"><label for="ce">Expires</label><input id="ce" type="date" bind:value={cert.expires_at} /></div>
				<div class="field">
					<label for="co">Origin</label>
					<select id="co" bind:value={cert.origin_kind}>
						{#each ORIGIN_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
					</select>
				</div>
				{#if cert.origin_kind !== 'NATIVE'}
					<div class="field"><label for="cs">Source system</label><input id="cs" bind:value={cert.source_system_id} size="16" /></div>
				{/if}
				<button
					type="submit"
					disabled={saving || !cert.certificate_number.trim() || !cert.verifying_authority.trim() || !cert.expires_at}
				>
					Record
				</button>
			</div>
		</form>
	{/if}
</section>

<style>
	section { margin-top: 2.2rem; }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.superseded { opacity: 0.6; }
	.detail td { background: var(--surface-2); }
</style>
