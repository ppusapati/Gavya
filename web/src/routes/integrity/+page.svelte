<script lang="ts">
	import { page } from '$app/state';
	import {
		ApiError,
		type AdjudicateResponse,
		type Classification,
		type DivergenceStatus,
		type IngestAssertionResponse,
		type ListDivergencesResponse,
		type RecordComputationResponse,
		type SettlementComponent
	} from '$lib/api';
	import { classificationTone, instant, label, shortId, statusTone, today } from '$lib/display';
	import { formatMinorUnits } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const CLASSIFICATIONS: Classification[] = [
		'MATCH',
		'ROUNDING_DIFFERENCE',
		'INPUT_DIFFERENCE',
		'POLICY_DIFFERENCE',
		'RECOVERY_DIFFERENCE',
		'UNEXPLAINED',
		'INSUFFICIENT_EVIDENCE'
	];

	const STATUSES: DivergenceStatus[] = [
		'OPEN',
		'UNDER_REVIEW',
		'ACCEPTED',
		'EXTERNAL_CONFIRMED',
		'SHADOW_CONFIRMED',
		'RESOLVED'
	];

	const PAGE = 50;

	// The overview links here already filtered, so a reviewer arrives at the rows
	// they clicked rather than at everything.
	let classification = $state(page.url.searchParams.get('classification') ?? '');
	let status = $state(page.url.searchParams.get('status') ?? 'OPEN');
	let producer = $state('');
	let minAbsDelta = $state(0);
	let offset = $state(0);

	const task = new Task<ListDivergencesResponse>();

	/* ---- putting a pair in front of the classifier ---- */

	const ingested = new Task<IngestAssertionResponse>();
	const computed = new Task<RecordComputationResponse>();
	const adjudicated = new Task<AdjudicateResponse>();

	let feeding = $state(false);
	let feedError = $state<ApiError | undefined>(undefined);

	let assertion = $state({
		source_system_id: '',
		external_settlement_id: '',
		producer_ref: '',
		period_start: today(),
		period_end: today(),
		currency: 'INR',
		amount_scale: 2,
		total: '',
		import_batch_id: '',
		source_record_id: '',
		raw_payload: '{}'
	});

	let computation = $state({
		total: '',
		policy_version: '',
		rate_card_id: '',
		input_digest: ''
	});

	/**
	 * A total and nothing else is one component of kind TOTAL.
	 *
	 * This console does not break a settlement into lines — the AMCU export
	 * that would say how is one of the things nobody has handed over yet — and
	 * inventing a breakdown here would put lines into the evidence that nobody
	 * measured. The classifier reports what it could and could not compare, and
	 * a pair with one component each is one it will call INSUFFICIENT_EVIDENCE
	 * rather than MATCH, which is the correct answer to what was supplied.
	 */
	function oneTotal(amount: string): SettlementComponent[] {
		return [{ kind: 'TOTAL', label: 'as supplied', amount: amount.trim() }];
	}

	function parsePayload(text: string): unknown {
		try {
			return JSON.parse(text);
		} catch {
			return { unparsed: text };
		}
	}

	const payloadIsJSON = $derived.by(() => {
		try {
			JSON.parse(assertion.raw_payload);
			return true;
		} catch {
			return false;
		}
	});

	async function feed(event: SubmitEvent) {
		event.preventDefault();
		if (feeding) return;
		feeding = true;
		feedError = undefined;
		adjudicated.reset();
		try {
			const a = await ingested.run((s) =>
				settings.api().ingestAssertion(
					{
						source_system_id: assertion.source_system_id.trim(),
						external_settlement_id: assertion.external_settlement_id.trim(),
						producer_ref: assertion.producer_ref.trim(),
						period_start: assertion.period_start,
						period_end: assertion.period_end,
						currency: assertion.currency.trim(),
						amount_scale: assertion.amount_scale,
						total: assertion.total.trim(),
						components: oneTotal(assertion.total),
						asserted_at: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
						import_batch_id: assertion.import_batch_id.trim(),
						source_record_id: assertion.source_record_id.trim(),
						raw_payload: parsePayload(assertion.raw_payload),
						created_by: settings.actorOrUnknown
					},
					{ signal: s }
				)
			);
			if (!a) return;

			const c = await computed.run((s) =>
				settings.api().recordComputation(
					{
						assertion_id: a.assertion.id,
						producer_ref: assertion.producer_ref.trim(),
						period_start: assertion.period_start,
						period_end: assertion.period_end,
						currency: assertion.currency.trim(),
						amount_scale: assertion.amount_scale,
						total: computation.total.trim(),
						components: oneTotal(computation.total),
						policy_version: computation.policy_version.trim(),
						rate_card_id: computation.rate_card_id.trim(),
						input_digest: computation.input_digest.trim(),
						as_of: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
						created_by: settings.actorOrUnknown
					},
					{ signal: s }
				)
			);
			if (!c) return;

			await adjudicated.run((s) =>
				settings.api().adjudicate(
					{
						assertion_id: a.assertion.id,
						computation_id: c.computation.id,
						actor: settings.actorOrUnknown
					},
					{ signal: s }
				)
			);
			load();
		} catch (cause) {
			feedError =
				cause instanceof ApiError
					? cause
					: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
		} finally {
			feeding = false;
		}
	}

	function load() {
		task.run((signal) =>
			settings.api().listDivergences(
				{
					status,
					classification,
					producer_ref: producer.trim(),
					min_abs_delta: minAbsDelta,
					limit: PAGE,
					offset
				},
				{ signal }
			)
		);
	}

	$effect(() => {
		// Re-runs whenever any filter changes. Task discards a reply that a newer
		// query has already superseded.
		void [classification, status, producer, minAbsDelta, offset];
		load();
	});

	function refilter(fn: () => void) {
		fn();
		offset = 0;
	}

	const rows = $derived(task.data?.divergences ?? []);
	const atEnd = $derived(rows.length < PAGE);
</script>

<div class="page-head">
	<h1>Divergence queue</h1>
	<p>
		Each row is one settlement where the incumbent's figure and the platform's independent
		recomputation did not agree. The classification is deterministic: it is what the evidence
		supports, decided before any model is consulted.
	</p>
</div>

<details class="feeder">
	<summary>Put a pair in front of the classifier</summary>
	<p class="muted note">
		Three calls in order: record what the incumbent asserts, record what this platform computes for
		the same producer and period, then compare them. Every row in the queue below arrived this way,
		and until now nothing in the console could make one.
	</p>

	<ErrorNote error={feedError} />

	<form onsubmit={feed}>
		<h3>What the incumbent asserts</h3>
		<div class="controls">
			<div class="field"><label for="ass">Source system</label><input id="ass" bind:value={assertion.source_system_id} size="16" /></div>
			<div class="field"><label for="aid">Their settlement id</label><input id="aid" bind:value={assertion.external_settlement_id} size="20" /></div>
			<div class="field"><label for="apr">Producer</label><input id="apr" bind:value={assertion.producer_ref} size="18" /></div>
			<div class="field"><label for="aps">From</label><input id="aps" type="date" bind:value={assertion.period_start} /></div>
			<div class="field"><label for="ape">To</label><input id="ape" type="date" bind:value={assertion.period_end} /></div>
			<div class="field"><label for="acu">Currency</label><input id="acu" bind:value={assertion.currency} size="5" /></div>
			<div class="field"><label for="asc">Decimals</label><input id="asc" type="number" min="0" max="6" bind:value={assertion.amount_scale} size="3" /></div>
			<div class="field"><label for="ato">Their total</label><input id="ato" bind:value={assertion.total} size="12" inputmode="decimal" /></div>
		</div>
		<div class="controls">
			<div class="field"><label for="aib">Import batch</label><input id="aib" bind:value={assertion.import_batch_id} size="20" /></div>
			<div class="field"><label for="asr">Source record</label><input id="asr" bind:value={assertion.source_record_id} size="20" /></div>
		</div>
		<div class="field wide">
			<label for="arp">The source record verbatim</label>
			<textarea id="arp" rows="3" bind:value={assertion.raw_payload}></textarea>
		</div>
		{#if !payloadIsJSON}
			<p class="warn">
				Not valid JSON. It will be sent as an object wrapping the text, so the replay hash still
				means something — but the classifier reads nothing from inside it either way.
			</p>
		{/if}

		<h3>What this platform computes</h3>
		<div class="controls">
			<div class="field"><label for="cto">Our total</label><input id="cto" bind:value={computation.total} size="12" inputmode="decimal" /></div>
			<div class="field"><label for="cpv">Policy version</label><input id="cpv" bind:value={computation.policy_version} size="14" /></div>
			<div class="field"><label for="crc">Rate card</label><input id="crc" bind:value={computation.rate_card_id} size="20" /></div>
			<div class="field"><label for="cid">Input digest</label><input id="cid" bind:value={computation.input_digest} size="20" /></div>
			<button type="submit" disabled={feeding || !assertion.producer_ref.trim() || !assertion.total.trim() || !computation.total.trim()}>
				{feeding ? 'Comparing…' : 'Compare'}
			</button>
		</div>

		<p class="muted note">
			Each side is sent as a single component of kind TOTAL. This console does not break a
			settlement into lines, and inventing a breakdown would put lines into the evidence that
			nobody measured — so a pair supplied this way is one the classifier will usually call
			insufficient evidence rather than a match, which is the honest verdict on what it was given.
		</p>
	</form>

	{#if ingested.settled && ingested.data}
		<p class="banner">
			<Chip tone={ingested.data.created ? 'calm' : 'neutral'}>
				{ingested.data.created ? 'assertion recorded' : 'already ingested'}
			</Chip>
			{#if !ingested.data.created}
				<span class="muted">
					The same payload had been ingested before, which makes a replayed import batch
					observably idempotent rather than a duplicate settlement.
				</span>
			{/if}
		</p>
	{/if}

	{#if adjudicated.settled}
		<Await task={adjudicated} isEmpty={(d) => !d.divergence} empty="Nothing came back.">
			{#snippet children(d)}
				<p class="banner">
					<Chip tone={classificationTone(d.divergence.classification)}>
						{label(d.divergence.classification)}
					</Chip>
					{d.divergence.rationale}
					<a href="/integrity/{d.divergence.id}">Open it →</a>
				</p>
			{/snippet}
		</Await>
	{/if}
</details>

<div class="controls">
	<div class="field">
		<label for="st">Status</label>
		<select id="st" value={status} onchange={(e) => refilter(() => (status = e.currentTarget.value))}>
			<option value="">Any</option>
			{#each STATUSES as s (s)}<option value={s}>{label(s)}</option>{/each}
		</select>
	</div>

	<div class="field">
		<label for="cl">Classification</label>
		<select
			id="cl"
			value={classification}
			onchange={(e) => refilter(() => (classification = e.currentTarget.value))}
		>
			<option value="">Any</option>
			{#each CLASSIFICATIONS as c (c)}<option value={c}>{label(c)}</option>{/each}
		</select>
	</div>

	<div class="field">
		<label for="pr">Producer</label>
		<input
			id="pr"
			value={producer}
			placeholder="exact reference"
			onchange={(e) => refilter(() => (producer = e.currentTarget.value))}
		/>
	</div>

	<div class="field">
		<label for="md">Minimum difference</label>
		<input
			id="md"
			type="number"
			min="0"
			step="1"
			size="10"
			value={minAbsDelta}
			title="In minor units — the smallest unit the amount is expressed in"
			onchange={(e) => refilter(() => (minAbsDelta = Number(e.currentTarget.value) || 0))}
		/>
	</div>

	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
</div>

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.divergences ?? []).length === 0}
	empty="No divergence matches these filters."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Producer</th>
						<th>Difference</th>
						<th>Classification</th>
						<th>Status</th>
						<th>Adjudicated</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each rows as d (d.id)}
						<tr>
							<td class="mono">{d.producer_ref}</td>
							<td class="num" class:against={d.delta_minor_units < 0}>
								{formatMinorUnits(d.delta_minor_units, d.amount_scale, d.currency)}
							</td>
							<td>
								<Chip tone={classificationTone(d.classification)} title={d.rationale}>
									{label(d.classification)}
								</Chip>
								{#if d.ml_hypotheses?.length}
									<Chip advisory title="A model offered an explanation. It is advisory and did not decide this classification.">
										advisory note
									</Chip>
								{/if}
							</td>
							<td>
								<Chip tone={statusTone(d.status)}>{label(d.status)}</Chip>
								{#if d.needs_review}
									<Chip tone="attention">needs review</Chip>
								{/if}
							</td>
							<td>{instant(d.created_at)}</td>
							<td><a class="rowlink" href="/integrity/{d.id}" title={d.id}>{shortId(d.id)} →</a></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<div class="pager">
	<button class="ghost" disabled={offset === 0 || task.pending} onclick={() => (offset = Math.max(0, offset - PAGE))}>
		← Previous
	</button>
	<span class="muted">Rows {offset + 1}–{offset + rows.length}</span>
	<button class="ghost" disabled={atEnd || task.pending} onclick={() => (offset += PAGE)}>Next →</button>
</div>

<style>
	.feeder { margin: 1rem 0 1.4rem; }
	.feeder summary { cursor: pointer; font-size: 0.9rem; font-weight: 600; margin-bottom: 0.6rem; }
	.feeder h3 { margin: 1rem 0 0.4rem; font-size: 0.85rem; font-weight: 600; }
	.note { max-width: var(--measure); margin: 0.5rem 0; font-size: 0.82rem; }
	.warn { max-width: var(--measure); margin: 0.5rem 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.6rem 0; font-size: 0.85rem; }
	.field.wide { display: block; max-width: var(--measure); }
	.field.wide textarea { width: 100%; font: inherit; }

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}


	/* A negative delta means the incumbent paid less than the recomputation says
	   it should have, which is the direction a producer would dispute. */
	.against {
		color: var(--critical);
	}
</style>
