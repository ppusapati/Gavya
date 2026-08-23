<script lang="ts">
	import { page } from '$app/state';
	import {
		ApiError,
		type DivergenceStatus,
		type Evidence,
		type GetDivergenceResponse
	} from '$lib/api';
	import { formatMinorUnits, formatMinorUnitsPlain } from '$lib/money';
	import { classificationTone, instant, label, statusTone } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const id = $derived(page.params.id ?? '');

	const task = new Task<GetDivergenceResponse>();

	function load() {
		task.run((signal) => settings.api().getDivergence(id, { signal }));
	}

	$effect(() => {
		void id;
		load();
	});

	const d = $derived(task.data?.divergence);

	// A reviewer decides which of the two figures stands, or defers. Every option
	// says plainly what it asserts, because the choice is written into the audit
	// trail under their name.
	const OUTCOMES: { value: DivergenceStatus; means: string }[] = [
		{ value: 'UNDER_REVIEW', means: 'I am looking into this; do not treat it as settled.' },
		{ value: 'EXTERNAL_CONFIRMED', means: "The incumbent's figure is right; the recomputation is wrong." },
		{ value: 'SHADOW_CONFIRMED', means: 'The recomputation is right; the incumbent underpaid or overpaid.' },
		{ value: 'ACCEPTED', means: 'The difference is real and understood, and no correction is needed.' },
		{ value: 'RESOLVED', means: 'Whatever this needed has been done elsewhere.' }
	];

	let outcome = $state<DivergenceStatus>('UNDER_REVIEW');
	let note = $state('');
	let submitting = $state(false);
	let submitError = $state<ApiError | undefined>(undefined);

	const chosen = $derived(OUTCOMES.find((o) => o.value === outcome));
	// Nothing can be recorded without a reason: a status with no explanation is
	// an audit trail that says a decision happened but not why.
	const canSubmit = $derived(note.trim().length > 0 && !submitting && !!d);

	async function resolve(event: SubmitEvent) {
		event.preventDefault();
		if (!canSubmit) return;
		submitting = true;
		submitError = undefined;
		try {
			const res = await settings.api().resolveDivergence({
				id,
				status: outcome,
				resolution: note.trim(),
				actor: settings.actorOrUnknown
			});
			task.data = { divergence: res.divergence };
			note = '';
		} catch (cause) {
			submitError =
				cause instanceof ApiError
					? cause
					: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
		} finally {
			submitting = false;
		}
	}

	function side(e: Evidence): string {
		if (e.only_external) return 'only the incumbent has this line';
		if (e.only_shadow) return 'only the recomputation has this line';
		const differs = [e.quantity_differs && 'quantity', e.rate_differs && 'rate'].filter(Boolean);
		return differs.length ? `${differs.join(' and ')} differ` : '';
	}
</script>

<div class="page-head">
	<a class="rowlink" href="/integrity">← Back to the queue</a>
	<h1>Divergence</h1>
</div>

<Await {task} retry={load}>
	{#snippet children()}
		{#if d}
			<div class="panel">
				<div class="headline">
					<div>
						<span class="delta" class:against={d.delta_minor_units < 0}>
							{formatMinorUnits(d.delta_minor_units, d.amount_scale, d.currency)}
						</span>
						<p class="muted direction">
							{#if d.delta_minor_units < 0}
								The incumbent settled for less than the platform's recomputation.
							{:else if d.delta_minor_units > 0}
								The incumbent settled for more than the platform's recomputation.
							{:else}
								The two figures agree exactly.
							{/if}
						</p>
					</div>
					<div class="chips">
						<Chip tone={classificationTone(d.classification)}>{label(d.classification)}</Chip>
						<Chip tone={statusTone(d.status)}>{label(d.status)}</Chip>
						{#if d.needs_review}<Chip tone="attention">needs review</Chip>{/if}
					</div>
				</div>

				<p class="rationale">{d.rationale}</p>

				<dl class="kv">
					<dt>Producer</dt>
					<dd class="mono">{d.producer_ref}</dd>
					<dt>Assertion</dt>
					<dd class="mono">{d.assertion_id}</dd>
					<dt>Recomputation</dt>
					<dd class="mono">{d.computation_id}</dd>
					<dt>Adjudicated</dt>
					<dd>{instant(d.created_at)}</dd>
					{#if d.resolved_at}
						<dt>Resolved</dt>
						<dd>{instant(d.resolved_at)} by {d.resolved_by || 'unattributed'}</dd>
						<dt>Resolution</dt>
						<dd>{d.resolution}</dd>
					{/if}
				</dl>
			</div>

			<div class="panel">
				<h2>Where the two disagree</h2>
				{#if d.evidence?.length}
					<div class="tablewrap">
						<table>
							<thead>
								<tr>
									<th>Component</th>
									<th>Incumbent</th>
									<th>Recomputation</th>
									<th>Difference</th>
									<th>Note</th>
								</tr>
							</thead>
							<tbody>
								{#each d.evidence as e, i (e.kind + i)}
									<tr>
										<td>{label(e.kind)}</td>
										<td class="num">{formatMinorUnitsPlain(e.external_minor_units, d.amount_scale)}</td>
										<td class="num">{formatMinorUnitsPlain(e.shadow_minor_units, d.amount_scale)}</td>
										<td class="num" class:nonzero={e.delta_minor_units !== 0}>
											{formatMinorUnitsPlain(e.delta_minor_units, d.amount_scale)}
										</td>
										<td class="muted">{side(e)}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{:else}
					<p class="muted">
						Neither side supplied line detail, so the difference could only be judged on the totals.
					</p>
				{/if}
			</div>

			{#if d.ml_hypotheses?.length}
				<div class="panel advisory">
					<h2>Advisory — a model's suggestion</h2>
					<p class="muted">
						These are hypotheses, not findings. The classification above was decided by the
						deterministic rules from the evidence alone; nothing here changed it, and if the model
						had been unavailable the classification would be identical.
					</p>
					{#each d.ml_hypotheses as h, i (h.model_version + i)}
						<div class="hypo">
							<h3>
								{label(h.classification)}
								<Chip advisory>{(h.confidence * 100).toFixed(0)}% confident</Chip>
								<Chip advisory>{h.model_version}</Chip>
							</h3>
							<p>{h.rationale}</p>
							{#if h.supporting_fields?.length}
								<p class="muted fields">Looked at: {h.supporting_fields.join(', ')}</p>
							{/if}
						</div>
					{/each}
				</div>
			{/if}

			<form class="panel" onsubmit={resolve}>
				<h2>Record a decision</h2>
				<ErrorNote error={submitError} />

				<div class="field wide">
					<label for="outcome">Outcome</label>
					<select id="outcome" bind:value={outcome}>
						{#each OUTCOMES as o (o.value)}<option value={o.value}>{label(o.value)}</option>{/each}
					</select>
					{#if chosen}<p class="muted means">{chosen.means}</p>{/if}
				</div>

				<div class="field wide">
					<label for="note">Why</label>
					<textarea
						id="note"
						rows="3"
						bind:value={note}
						placeholder="What you checked, and what convinced you."
					></textarea>
				</div>

				<div class="submit">
					<button type="submit" disabled={!canSubmit}>
						{submitting ? 'Recording…' : 'Record'}
					</button>
					<span class="muted">
						Recorded against <strong>{settings.actorOrUnknown}</strong>. The divergence is kept
						either way — a decision is added to its history, it does not replace it.
					</span>
				</div>
			</form>
		{/if}
	{/snippet}
</Await>

<style>
	.headline {
		display: flex;
		flex-wrap: wrap;
		gap: 1rem;
		justify-content: space-between;
		align-items: flex-start;
	}

	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
	}

	.delta {
		font-family: var(--mono);
		font-size: 2rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}

	.against {
		color: var(--critical);
	}


	.direction {
		margin: 0.35rem 0 0;
		font-size: 0.86rem;
	}

	.rationale {
		max-width: var(--measure);
		border-left: 2px solid var(--rule-firm);
		padding-left: 0.9rem;
		margin: 1.1rem 0;
	}

	.nonzero {
		font-weight: 700;
	}

	.hypo + .hypo {
		border-top: 1px solid var(--rule);
		margin-top: 0.9rem;
		padding-top: 0.9rem;
	}

	.hypo h3 {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		flex-wrap: wrap;
		color: var(--signal);
	}

	.hypo p {
		margin: 0.4rem 0 0;
		max-width: var(--measure);
	}

	.fields {
		font-size: 0.8rem;
	}

	.field.wide {
		max-width: var(--measure);
		margin-bottom: 1rem;
	}

	.field.wide textarea,
	.field.wide select {
		width: 100%;
	}

	.means {
		font-size: 0.82rem;
		margin: 0.35rem 0 0;
	}

	.submit {
		display: flex;
		align-items: center;
		gap: 0.9rem;
		flex-wrap: wrap;
		font-size: 0.82rem;
	}
</style>
