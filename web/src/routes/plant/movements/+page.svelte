<script lang="ts">
	import {
		ApiError,
		DENSITY_SOURCES,
		MEASURE_METHODS,
		NODE_KINDS,
		ROUNDING_MODES,
		UNITS,
		type ListInstrumentsResponse,
		type ListMovementsResponse,
		type ListNodesResponse,
		type Movement,
		type NodeResponse,
		type ProposeFlowsResponse
	} from '$lib/api';
	import { instant, label, quantity, today, toLocalInput } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const nodes = new Task<ListNodesResponse>();
	const chosen = new Task<NodeResponse>();
	const movements = new Task<ListMovementsResponse>();
	const instruments = new Task<ListInstrumentsResponse>();
	const flows = new Task<ProposeFlowsResponse>();

	let from = $state(weekAgo());
	let to = $state(today());
	let nodeFilter = $state('');

	function weekAgo(): string {
		const d = new Date();
		d.setDate(d.getDate() - 7);
		return d.toISOString().slice(0, 10);
	}

	function loadNodes() {
		nodes.run((s) => settings.api().plant.listNodes('', { signal: s }));
	}
	function loadMovements() {
		movements.run((s) =>
			settings.api().plant.listMovements(
				{ node_id: nodeFilter || undefined, from, to: to || undefined, limit: 200 },
				{ signal: s }
			)
		);
	}
	function loadInstruments() {
		instruments.run((s) => settings.api().plant.listInstruments({ signal: s }));
	}

	$effect(loadNodes);
	$effect(loadInstruments);
	$effect(() => {
		void from;
		void to;
		void nodeFilter;
		loadMovements();
	});

	// The vessel itself, not the row from the list.
	//
	// Its capacity is the figure somebody wants in front of them while deciding
	// what to send to it, and the list is filtered — by kind, and by a limit —
	// so the row for a node may not be on the screen at all.
	$effect(() => {
		if (nodeFilter) chosen.run((s) => settings.api().plant.getNode(nodeFilter, { signal: s }));
	});

	const nodeName = $derived.by(() => {
		const m = new Map<string, string>();
		for (const n of nodes.data?.nodes ?? []) m.set(n.id, `${n.code} — ${n.name}`);
		return m;
	});
	function named(id: string): string {
		return nodeName.get(id) ?? id;
	}

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

	let addingNode = $state(false);
	let node = $state({ code: '', name: '', kind: 'TANKER', capacity: '', capacity_unit: '' });

	let dispatching = $state(false);
	let dsp = $state({
		from_node_id: '',
		to_node_id: '',
		at: toLocalInput(new Date()),
		value: '',
		unit: '',
		method: '',
		holdup: '',
		holdup_unit: ''
	});

	let receiving = $state<string | undefined>(undefined);
	let rcv = $state({
		at: toLocalInput(new Date()),
		value: '',
		unit: '',
		method: '',
		use_density: false,
		kg_per_litre: '',
		scale: 4,
		at_celsius: 40,
		source: '',
		rounding: ''
	});

	let abandoning = $state<string | undefined>(undefined);
	let abandonReason = $state('');

	let addingInstrument = $state(false);
	let inst = $state({
		node_id: '',
		method: '',
		label: '',
		kind: 'relative' as 'relative' | 'absolute',
		relative_ppm: '',
		absolute: '',
		absolute_unit: '',
		certificate_ref: '',
		calibrated_on: today(),
		valid_until: ''
	});

	let rounding = $state('');

	function iso(local: string): string {
		const d = new Date(local);
		return Number.isNaN(d.getTime()) ? '' : d.toISOString().replace(/\.\d{3}Z$/, 'Z');
	}

	function statusTone(s: string) {
		switch (s) {
			case 'RECEIVED':
				return 'calm';
			case 'IN_TRANSIT':
				return 'neutral';
			case 'ABANDONED':
				return 'critical';
			default:
				return 'neutral';
		}
	}

	function expired(validUntil: string, asOf: string): boolean {
		return Date.parse(validUntil) < Date.parse(asOf);
	}

	function startReceive(m: Movement) {
		receiving = receiving === m.id ? undefined : m.id;
		rcv = {
			at: toLocalInput(new Date()),
			value: '',
			unit: '',
			method: '',
			use_density: false,
			kg_per_litre: '',
			scale: 4,
			at_celsius: 40,
			source: '',
			rounding: ''
		};
		saveError = undefined;
	}
</script>

<div class="page-head">
	<h1>Movements</h1>
	<p>
		Milk between vessels: what was dispatched, what arrived, and the difference. Every quantity
		carries its unit because the two this platform uses differ by about three per cent — which is
		larger than most of the margins in this business, and looks exactly like a plausible transit
		loss when it is really a units mistake.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<div class="field"><label for="f">From</label><input id="f" type="date" bind:value={from} /></div>
	<div class="field"><label for="t">To</label><input id="t" type="date" bind:value={to} /></div>
	<div class="field">
		<label for="nf">Node</label>
		<select id="nf" bind:value={nodeFilter}>
			<option value="">Every node</option>
			{#each nodes.data?.nodes ?? [] as n (n.id)}
				<option value={n.id}>{n.code} — {n.name}</option>
			{/each}
		</select>
	</div>
	<button class="ghost" onclick={() => { addingNode = !addingNode; saveError = undefined; }}>
		{addingNode ? 'Cancel' : 'Register a node'}
	</button>
	<button onclick={() => { dispatching = !dispatching; saveError = undefined; }}>
		{dispatching ? 'Cancel' : 'Dispatch'}
	</button>
</div>

{#if addingNode}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().plant.registerNode({
						code: node.code.trim(),
						name: node.name.trim(),
						kind: node.kind,
						capacity:
							node.capacity.trim() && node.capacity_unit
								? { value: node.capacity.trim(), unit: node.capacity_unit }
								: undefined,
						actor: settings.actorOrUnknown
					}),
				() => {
					addingNode = false;
					node = { code: '', name: '', kind: 'TANKER', capacity: '', capacity_unit: '' };
					loadNodes();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="nc">Code</label><input id="nc" bind:value={node.code} size="12" /></div>
			<div class="field grow"><label for="nn">Name</label><input id="nn" bind:value={node.name} /></div>
			<div class="field">
				<label for="nk">Kind</label>
				<select id="nk" bind:value={node.kind}>
					{#each NODE_KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="ncap">Capacity</label><input id="ncap" bind:value={node.capacity} size="9" inputmode="decimal" /></div>
			<div class="field">
				<label for="ncu">Unit</label>
				<select id="ncu" bind:value={node.capacity_unit}>
					<option value="">—</option>
					{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
				</select>
			</div>
			<button type="submit" disabled={saving || !node.code.trim() || !node.name.trim()}>Register</button>
		</div>
		<p class="muted note">
			A capacity needs a unit beside it or it is not recorded at all. There is no default here
			because there is none in the service: litres is what most of this milk is measured in, which
			makes it the assumption that would be wrong least often and hardest to find.
		</p>
	</form>
{/if}

{#if dispatching}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().plant.dispatch({
						from_node_id: dsp.from_node_id,
						to_node_id: dsp.to_node_id,
						at: iso(dsp.at),
						quantity: { value: dsp.value.trim(), unit: dsp.unit },
						method: dsp.method,
						holdup:
							dsp.holdup.trim() && dsp.holdup_unit
								? { value: dsp.holdup.trim(), unit: dsp.holdup_unit }
								: undefined,
						actor: settings.actorOrUnknown
					}),
				() => {
					dispatching = false;
					dsp = { ...dsp, value: '', holdup: '' };
					loadMovements();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field">
				<label for="dfr">From</label>
				<select id="dfr" bind:value={dsp.from_node_id}>
					<option value="">—</option>
					{#each nodes.data?.nodes ?? [] as n (n.id)}<option value={n.id}>{n.code} — {n.name}</option>{/each}
				</select>
			</div>
			<div class="field">
				<label for="dto">To</label>
				<select id="dto" bind:value={dsp.to_node_id}>
					<option value="">—</option>
					{#each nodes.data?.nodes ?? [] as n (n.id)}<option value={n.id}>{n.code} — {n.name}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="dat">At</label><input id="dat" type="datetime-local" bind:value={dsp.at} /></div>
			<div class="field"><label for="dq">Quantity</label><input id="dq" bind:value={dsp.value} size="10" inputmode="decimal" /></div>
			<div class="field">
				<label for="du">Unit</label>
				<select id="du" bind:value={dsp.unit}>
					<option value="">—</option>
					{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
				</select>
			</div>
			<div class="field">
				<label for="dm">Measured by</label>
				<select id="dm" bind:value={dsp.method}>
					<option value="">—</option>
					{#each MEASURE_METHODS as m (m)}<option value={m}>{label(m)}</option>{/each}
				</select>
			</div>
			<div class="field"><label for="dh">Holdup</label><input id="dh" bind:value={dsp.holdup} size="8" inputmode="decimal" placeholder="optional" /></div>
			<div class="field">
				<label for="dhu">Unit</label>
				<select id="dhu" bind:value={dsp.holdup_unit}>
					<option value="">—</option>
					{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
				</select>
			</div>
			<button
				type="submit"
				disabled={saving || !dsp.from_node_id || !dsp.to_node_id || !dsp.value.trim() || !dsp.unit || !dsp.method}
			>
				Dispatch
			</button>
		</div>
		<p class="muted note">
			Holdup is what stayed in the sending vessel. It is recorded beside the variance rather than
			folded into it: the holdup is in a vessel somebody can look inside, and the variance is not
			anywhere.
		</p>
	</form>
{/if}

{#if nodeFilter}
	<Await task={chosen} isEmpty={(d) => !d.node} empty="No such node.">
		{#snippet children(d)}
			<div class="panel">
				<dl class="kv">
					<dt>Node</dt><dd class="mono">{d.node.code}</dd>
					<dt>Name</dt><dd>{d.node.name}</dd>
					<dt>Kind</dt><dd>{label(d.node.kind)}</dd>
					<dt>Capacity</dt>
					<dd>
						{#if d.node.capacity}
							{quantity(d.node.capacity.value, d.node.capacity.unit)}
						{:else}
							<span class="muted">none recorded</span>
						{/if}
					</dd>
					<dt>In service</dt>
					<dd><Chip tone={d.node.active ? 'calm' : 'neutral'}>{d.node.active ? 'Yes' : 'No'}</Chip></dd>
				</dl>
			</div>
		{/snippet}
	</Await>
{/if}

<Await
	task={movements}
	retry={loadMovements}
	isEmpty={(d) => (d.movements ?? []).length === 0}
	empty="Nothing moved in this period."
>
	{#snippet children(d)}
		{#if d.unreconciled > 0}
			<p class="banner">
				<Chip tone="attention">{d.unreconciled} unreconciled</Chip>
				That many of these arrived without a variance being computable. Any total of transit loss
				over this period is missing them, and would read as complete.
			</p>
		{/if}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Dispatched</th><th>From</th><th>To</th><th class="num">Sent</th>
						<th class="num">Received</th><th class="num">Variance</th><th>Status</th><th></th>
					</tr>
				</thead>
				<tbody>
					{#each d.movements as m (m.id)}
						<tr>
							<td>{instant(m.dispatched_at)}</td>
							<td>{named(m.from_node_id)}</td>
							<td>{named(m.to_node_id)}</td>
							<td class="num">{quantity(m.dispatched.value, m.dispatched.unit)}</td>
							<td class="num">{m.received ? quantity(m.received.value, m.received.unit) : '—'}</td>
							<td class="num">
								{#if m.variance}
									{quantity(m.variance.value, m.variance.unit)}
								{:else if m.variance_unavailable_reason}
									<Chip tone="attention" title={m.variance_unavailable_reason}>not computable</Chip>
								{:else}
									—
								{/if}
							</td>
							<td><Chip tone={statusTone(m.status)} title={m.abandoned_reason ?? ''}>{label(m.status)}</Chip></td>
							<td class="actions">
								{#if m.status === 'IN_TRANSIT'}
									<button class="ghost" onclick={() => startReceive(m)}>
										{receiving === m.id ? 'Cancel' : 'Receive'}
									</button>
									<button class="ghost" onclick={() => { abandoning = abandoning === m.id ? undefined : m.id; abandonReason = ''; }}>
										{abandoning === m.id ? 'Cancel' : 'Abandon'}
									</button>
								{/if}
							</td>
						</tr>

						{#if m.variance_unavailable_reason}
							<tr class="detail"><td colspan="8"><strong>No variance:</strong> {m.variance_unavailable_reason}</td></tr>
						{/if}

						{#if receiving === m.id}
							<tr class="detail">
								<td colspan="8">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.receive({
														movement_id: m.id,
														at: iso(rcv.at),
														quantity: { value: rcv.value.trim(), unit: rcv.unit },
														method: rcv.method,
														density: rcv.use_density
															? {
																	kg_per_litre: rcv.kg_per_litre.trim(),
																	scale: rcv.scale,
																	at_celsius: rcv.at_celsius,
																	source: rcv.source
																}
															: undefined,
														rounding: rcv.use_density ? rcv.rounding : undefined,
														actor: settings.actorOrUnknown
													}),
												() => {
													receiving = undefined;
													loadMovements();
												}
											);
										}}
									>
										<div class="controls">
											<div class="field"><label for="ra-{m.id}">At</label><input id="ra-{m.id}" type="datetime-local" bind:value={rcv.at} /></div>
											<div class="field"><label for="rq-{m.id}">Quantity</label><input id="rq-{m.id}" bind:value={rcv.value} size="10" inputmode="decimal" /></div>
											<div class="field">
												<label for="ru-{m.id}">Unit</label>
												<select id="ru-{m.id}" bind:value={rcv.unit}>
													<option value="">—</option>
													{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
												</select>
											</div>
											<div class="field">
												<label for="rm-{m.id}">Measured by</label>
												<select id="rm-{m.id}" bind:value={rcv.method}>
													<option value="">—</option>
													{#each MEASURE_METHODS as x (x)}<option value={x}>{label(x)}</option>{/each}
												</select>
											</div>
											<label class="check">
												<input type="checkbox" bind:checked={rcv.use_density} />
												Give a density
											</label>
										</div>

										{#if rcv.unit && rcv.unit !== m.dispatched.unit && !rcv.use_density}
											<p class="warn">
												This was sent in {label(m.dispatched.unit)} and is being received in
												{label(rcv.unit)}. The two ends cannot be compared without a density read
												off this milk, and the service will refuse the receipt rather than guess
												one.
											</p>
										{/if}

										{#if rcv.use_density}
											<div class="controls">
												<div class="field"><label for="rk-{m.id}">kg per litre</label><input id="rk-{m.id}" bind:value={rcv.kg_per_litre} size="8" inputmode="decimal" /></div>
												<div class="field"><label for="rs-{m.id}">Scale</label><input id="rs-{m.id}" type="number" min="0" max="9" bind:value={rcv.scale} size="3" /></div>
												<div class="field"><label for="rc-{m.id}">Tenths of °C</label><input id="rc-{m.id}" type="number" bind:value={rcv.at_celsius} size="5" /></div>
												<div class="field">
													<label for="rsrc-{m.id}">Read by</label>
													<select id="rsrc-{m.id}" bind:value={rcv.source}>
														<option value="">—</option>
														{#each DENSITY_SOURCES as s (s)}<option value={s}>{label(s)}</option>{/each}
													</select>
												</div>
												<div class="field">
													<label for="rr-{m.id}">Rounding</label>
													<select id="rr-{m.id}" bind:value={rcv.rounding}>
														<option value="">—</option>
														{#each ROUNDING_MODES as r (r)}<option value={r}>{label(r)}</option>{/each}
													</select>
												</div>
											</div>
											<p class="muted note">
												Temperature is in tenths of a degree — 4.0 degrees is 40 — because a
												temperature with a decimal point in a JSON number is the same float
												problem every quantity here avoids. A rounding mode is required beside a
												density, because converting rounds.
											</p>
										{/if}

										<div class="controls">
											<button
												type="submit"
												disabled={saving || !rcv.value.trim() || !rcv.unit || !rcv.method || (rcv.use_density && (!rcv.kg_per_litre.trim() || !rcv.source || !rcv.rounding))}
											>
												Receive
											</button>
										</div>
									</form>
								</td>
							</tr>
						{/if}

						{#if abandoning === m.id}
							<tr class="detail">
								<td colspan="8">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											run(
												() =>
													settings.api().plant.abandonMovement({
														movement_id: m.id,
														reason: abandonReason.trim(),
														actor: settings.actorOrUnknown
													}),
												() => {
													abandoning = undefined;
													loadMovements();
												}
											);
										}}
									>
										<div class="controls">
											<div class="field grow">
												<label for="ab-{m.id}">Why it was abandoned</label>
												<input id="ab-{m.id}" bind:value={abandonReason} placeholder="What happened to the consignment." />
											</div>
											<button type="submit" disabled={saving || !abandonReason.trim()}>Abandon</button>
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

<section>
	<h2>Instruments</h2>
	<div class="controls">
		<button class="ghost" onclick={() => { addingInstrument = !addingInstrument; saveError = undefined; }}>
			{addingInstrument ? 'Cancel' : 'Register an instrument'}
		</button>
	</div>

	{#if addingInstrument}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().plant.registerInstrument({
							node_id: inst.node_id,
							method: inst.method,
							label: inst.label.trim(),
							relative_ppm: inst.kind === 'relative' ? Number(inst.relative_ppm) : undefined,
							absolute:
								inst.kind === 'absolute'
									? { value: inst.absolute.trim(), unit: inst.absolute_unit }
									: undefined,
							certificate_ref: inst.certificate_ref.trim(),
							calibrated_on: inst.calibrated_on,
							valid_until: inst.valid_until,
							actor: settings.actorOrUnknown
						}),
					() => {
						addingInstrument = false;
						loadInstruments();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field">
					<label for="in">Node</label>
					<select id="in" bind:value={inst.node_id}>
						<option value="">—</option>
						{#each nodes.data?.nodes ?? [] as n (n.id)}<option value={n.id}>{n.code} — {n.name}</option>{/each}
					</select>
				</div>
				<div class="field">
					<label for="im">Method</label>
					<select id="im" bind:value={inst.method}>
						<option value="">—</option>
						{#each MEASURE_METHODS as m (m)}<option value={m}>{label(m)}</option>{/each}
					</select>
				</div>
				<div class="field grow"><label for="il">Label</label><input id="il" bind:value={inst.label} /></div>
				<div class="field">
					<label for="ik">Uncertainty</label>
					<select id="ik" bind:value={inst.kind}>
						<option value="relative">Relative (ppm of reading)</option>
						<option value="absolute">Absolute (fixed quantity)</option>
					</select>
				</div>
				{#if inst.kind === 'relative'}
					<div class="field"><label for="ip">Parts per million</label><input id="ip" bind:value={inst.relative_ppm} size="8" inputmode="numeric" /></div>
				{:else}
					<div class="field"><label for="ia">Amount</label><input id="ia" bind:value={inst.absolute} size="8" inputmode="decimal" /></div>
					<div class="field">
						<label for="iau">Unit</label>
						<select id="iau" bind:value={inst.absolute_unit}>
							<option value="">—</option>
							{#each UNITS as u (u)}<option value={u}>{label(u)}</option>{/each}
						</select>
					</div>
				{/if}
				<div class="field"><label for="ic">Certificate</label><input id="ic" bind:value={inst.certificate_ref} size="16" /></div>
				<div class="field"><label for="ical">Calibrated</label><input id="ical" type="date" bind:value={inst.calibrated_on} /></div>
				<div class="field"><label for="iv">Valid until</label><input id="iv" type="date" bind:value={inst.valid_until} /></div>
				<button type="submit" disabled={saving || !inst.node_id || !inst.method || !inst.valid_until}>Register</button>
			</div>
			<p class="muted note">
				Exactly one kind of uncertainty. Parts per million is how a flowmeter certificate reads and
				a fixed quantity is how a weighbridge certificate reads; an instrument specified the wrong
				way round is wrong at one end of its range.
			</p>
		</form>
	{/if}

	<Await
		task={instruments}
		retry={loadInstruments}
		isEmpty={(d) => (d.instruments ?? []).length === 0}
		empty="No instruments are registered."
	>
		{#snippet children(d)}
			{#if d.expired > 0}
				<p class="banner">
					<Chip tone="critical">{d.expired} out of calibration</Chip>
					as at {d.as_of}. A reconciliation weighted by these instruments rests on certificates
					nobody has renewed.
				</p>
			{/if}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Label</th><th>Node</th><th>Method</th><th class="num">Uncertainty</th><th>Certificate</th><th>Valid until</th></tr></thead>
					<tbody>
						{#each d.instruments as i (i.id)}
							<tr>
								<td>{i.label || '—'}</td>
								<td>{named(i.node_id)}</td>
								<td>{label(i.method)}</td>
								<td class="num">
									{#if i.absolute}
										{quantity(i.absolute.value, i.absolute.unit)}
									{:else if i.relative_ppm}
										{i.relative_ppm} ppm
									{:else}
										—
									{/if}
								</td>
								<td class="mono">{i.certificate_ref || '—'}</td>
								<td>
									<Chip tone={expired(i.valid_until, d.as_of) ? 'critical' : 'calm'}>{i.valid_until}</Chip>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Flows for the reconciler</h2>
	<p class="muted note">
		This period's movements, shaped as balance-service wants them. The rounding mode has no default:
		it is a small effect on one flow and it decides which of two nearly-equal legs gets blamed when a
		window does not close.
	</p>
	<div class="controls">
		<div class="field">
			<label for="fr">Rounding</label>
			<select id="fr" bind:value={rounding}>
				<option value="">—</option>
				{#each ROUNDING_MODES as r (r)}<option value={r}>{label(r)}</option>{/each}
			</select>
		</div>
		<button
			class="ghost"
			disabled={!rounding || flows.pending}
			onclick={() => flows.run((s) => settings.api().plant.proposeFlows({ from, to, rounding }, { signal: s }))}
		>
			Propose
		</button>
	</div>

	{#if flows.settled}
		<Await task={flows} isEmpty={(d) => (d.flows ?? []).length === 0} empty="No flows in this period.">
			{#snippet children(d)}
				{#if d.unmeasured > 0}
					<p class="banner">
						<Chip tone="attention">{d.unmeasured} unmeasured</Chip>
						of {d.flows.length}. The reconciler solves for these rather than weighing them, so a
						window mostly made of them is one whose answer is mostly inference.
					</p>
				{/if}
				<div class="tablewrap">
					<table>
						<thead><tr><th>From</th><th>To</th><th class="num">Measured</th><th class="num">Standard uncertainty</th><th></th></tr></thead>
						<tbody>
							{#each d.flows as f (f.flow_id)}
								<tr>
									<td>{named(f.from_node_id)}</td>
									<td>{named(f.to_node_id)}</td>
									<td class="num">{quantity(f.measured.value, f.measured.unit)}</td>
									<td class="num">
										{f.standard_uncertainty ? quantity(f.standard_uncertainty.value, f.standard_uncertainty.unit) : '—'}
									</td>
									<td>
										{#if f.unmeasured}
											<Chip tone="attention" title={f.unmeasured_reason ?? ''}>unmeasured</Chip>
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

<style>
	section { margin-top: 2.2rem; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
	.banner {
		display: flex;
		gap: 0.6rem;
		align-items: baseline;
		max-width: var(--measure);
		margin: 0.8rem 0;
		font-size: 0.85rem;
	}
	.warn {
		max-width: var(--measure);
		margin: 0.7rem 0 0;
		font-size: 0.82rem;
		color: var(--bad, #b3261e);
	}
</style>
