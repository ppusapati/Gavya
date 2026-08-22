<script lang="ts">
	import { formatMinorUnits, type ClassSummary, type SummariseResponse } from '$lib/api';
	import { classificationTone, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';

	function daysAgo(n: number): string {
		const d = new Date();
		d.setDate(d.getDate() - n);
		return d.toISOString().replace(/\.\d{3}Z$/, 'Z');
	}

	let days = $state(30);
	const task = new Task<SummariseResponse>();

	function load() {
		const from = daysAgo(days);
		const to = new Date(Date.now() + 60_000).toISOString().replace(/\.\d{3}Z$/, 'Z');
		task.run((signal) => settings.api().summarise(from, to, { signal }));
	}

	$effect(() => {
		void days;
		load();
	});

	const rows = $derived(task.data?.summaries ?? []);

	// Counts are additive across every row; money is not, so the headline numbers
	// are counts and the amounts stay beside the currency and scale they are in.
	const needsReview = $derived(
		rows
			.filter((r) => r.classification === 'UNEXPLAINED' || r.classification === 'INSUFFICIENT_EVIDENCE')
			.reduce((n, r) => n + r.count, 0)
	);
	const explained = $derived(
		rows
			.filter(
				(r) =>
					r.classification !== 'MATCH' &&
					r.classification !== 'UNEXPLAINED' &&
					r.classification !== 'INSUFFICIENT_EVIDENCE'
			)
			.reduce((n, r) => n + r.count, 0)
	);
	const matched = $derived(
		rows.filter((r) => r.classification === 'MATCH').reduce((n, r) => n + r.count, 0)
	);
	const adjudicated = $derived(rows.reduce((n, r) => n + r.count, 0));

	function amount(r: ClassSummary): string {
		return `${formatMinorUnits(r.total_abs_minor_units, r.amount_scale)} ${r.currency}`;
	}
</script>

<div class="page-head">
	<h1>Shadow settlement, at a glance</h1>
	<p>
		Every settlement the incumbent asserted has been recomputed independently and the two compared.
		This is the mix of what came back: how much the two systems disagree about, and how much of that
		disagreement the platform can already account for.
	</p>
</div>

<div class="controls">
	<div class="field">
		<label for="win">Period</label>
		<select id="win" bind:value={days}>
			<option value={7}>Last 7 days</option>
			<option value={30}>Last 30 days</option>
			<option value={90}>Last 90 days</option>
			<option value={365}>Last year</option>
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
</div>

<Await {task} retry={load} isEmpty={(d) => (d.summaries ?? []).length === 0}
	empty="No settlement was adjudicated in this period.">
	{#snippet children()}
		<div class="stats">
			<div class="stat" class:attention={needsReview > 0}>
				<span class="v">{needsReview.toLocaleString()}</span>
				<span class="l">need a person — unexplained, or not enough evidence to judge</span>
			</div>
			<div class="stat">
				<span class="v">{explained.toLocaleString()}</span>
				<span class="l">differ for a known reason: rounding, inputs, policy or recovery</span>
			</div>
			<div class="stat calm">
				<span class="v">{matched.toLocaleString()}</span>
				<span class="l">agreed to the minor unit</span>
			</div>
			<div class="stat">
				<span class="v">{adjudicated.toLocaleString()}</span>
				<span class="l">settlements adjudicated in this period</span>
			</div>
		</div>

		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Classification</th>
						<th>Currency</th>
						<th>Count</th>
						<th>Absolute difference</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each rows as r (r.classification + r.currency + r.amount_scale)}
						<tr>
							<td>
								<Chip tone={classificationTone(r.classification)}>{label(r.classification)}</Chip>
							</td>
							<td class="mono">{r.currency}</td>
							<td class="num">{r.count.toLocaleString()}</td>
							<td class="num">{amount(r)}</td>
							<td>
								{#if r.count > 0 && r.classification !== 'MATCH'}
									<a class="rowlink" href="/integrity?classification={r.classification}">Review →</a>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		<p class="muted note">
			Rows are grouped by currency and by scale. Minor units at different scales are not the same
			unit, so they are never added together into one figure.
		</p>
	{/snippet}
</Await>

<style>
	.note {
		font-size: 0.82rem;
		margin-top: 0.9rem;
		max-width: var(--measure);
	}
</style>
