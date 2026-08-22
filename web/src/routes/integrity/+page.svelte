<script lang="ts">
	import { page } from '$app/state';
	import type { Classification, DivergenceStatus, ListDivergencesResponse } from '$lib/api';
	import { classificationTone, instant, label, shortId, statusTone } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';

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
								{d.delta}
								<span class="cur">{d.currency}</span>
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
	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}

	.cur {
		font-size: 0.72rem;
		color: var(--muted);
		margin-left: 0.3rem;
	}

	/* A negative delta means the incumbent paid less than the recomputation says
	   it should have, which is the direction a producer would dispute. */
	.against {
		color: var(--critical);
	}
</style>
