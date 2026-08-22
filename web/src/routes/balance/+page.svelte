<script lang="ts">
	import type { BalanceWindow, ListWindowsResponse, WindowStatus } from '$lib/api';
	import { instant, label, shortId } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';

	const STATUSES: WindowStatus[] = ['OPEN', 'RECONCILED', 'ACCEPTED'];
	const PAGE = 50;

	let status = $state('');
	let offset = $state(0);
	const task = new Task<ListWindowsResponse>();

	function load() {
		task.run((signal) => settings.api().listWindows({ status, limit: PAGE, offset }, { signal }));
	}

	$effect(() => {
		void [status, offset];
		load();
	});

	const windows = $derived(task.data?.windows ?? []);

	function tone(w: BalanceWindow) {
		return w.status === 'ACCEPTED' ? 'calm' : w.status === 'OPEN' ? 'attention' : 'neutral';
	}
</script>

<div class="page-head">
	<h1>Mass balance</h1>
	<p>
		Milk that goes into a route has to come out of it. A balance window is one route over one
		period, and reconciling it distributes the difference across the flows in proportion to how
		well each is measured — so a gap that a badly measured leg could account for is not charged to
		a well measured one.
	</p>
</div>

<div class="controls">
	<div class="field">
		<label for="st">Status</label>
		<select
			id="st"
			value={status}
			onchange={(e) => {
				status = e.currentTarget.value;
				offset = 0;
			}}
		>
			<option value="">Any</option>
			{#each STATUSES as s (s)}<option value={s}>{label(s)}</option>{/each}
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
</div>

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.windows ?? []).length === 0}
	empty="No balance window matches this filter."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Route</th>
						<th>Period</th>
						<th>Unit</th>
						<th>Status</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each windows as w (w.id)}
						<tr>
							<td class="mono">{w.route_ref}</td>
							<td>{instant(w.period_start)} → {instant(w.period_end)}</td>
							<td class="mono">{w.unit}</td>
							<td><Chip tone={tone(w)}>{label(w.status)}</Chip></td>
							<td><a class="rowlink" href="/balance/{w.id}" title={w.id}>{shortId(w.id)} →</a></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<div class="pager">
	<button
		class="ghost"
		disabled={offset === 0 || task.pending}
		onclick={() => (offset = Math.max(0, offset - PAGE))}>← Previous</button
	>
	<span class="muted">Windows {offset + 1}–{offset + windows.length}</span>
	<button class="ghost" disabled={windows.length < PAGE || task.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
