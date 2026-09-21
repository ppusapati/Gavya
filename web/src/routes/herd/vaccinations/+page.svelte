<script lang="ts">
	import { type ListVaccinationsResponse } from '$lib/api';
	import { instant } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';

	const task = new Task<ListVaccinationsResponse>();

	function load() {
		task.run((signal) => settings.api().herd.listUpcomingVaccinations({ signal }));
	}

	$effect(load);

	/** Overdue is not a state the service reports; it is a comparison with now. */
	function overdue(due: string | undefined): boolean {
		if (!due) return false;
		const at = Date.parse(due);
		return Number.isFinite(at) && at < Date.now();
	}
</script>

<div class="page-head">
	<h1>Vaccinations due</h1>
	<p>
		What health-service says is coming up, across the herd. A vaccination with no next due date is
		not listed here — the service reports what it was told to expect, and a schedule nobody entered
		is not a schedule that was missed.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
</div>

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.vaccinations ?? []).length === 0}
	empty="Nothing is due. Either the herd is up to date or no next dates were recorded — those are different things, and this screen cannot tell them apart."
>
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Due</th><th>Animal</th><th>Vaccine</th><th>Last given</th><th>Batch</th></tr>
				</thead>
				<tbody>
					{#each d.vaccinations as v (v.id)}
						<tr>
							<td>
								<Chip tone={overdue(v.next_due_date) ? 'critical' : 'attention'}>
									{v.next_due_date ? instant(v.next_due_date) : '—'}
								</Chip>
							</td>
							<td><a href="/herd/{v.cattle_id}" class="mono">{v.cattle_id}</a></td>
							<td>{v.vaccine_name}</td>
							<td>{instant(v.administered_at)}</td>
							<td class="mono">{v.batch_number || '—'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>
