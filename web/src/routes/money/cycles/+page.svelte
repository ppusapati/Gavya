<script lang="ts">
	import { ApiError, type Cycle, type ListCyclesResponse } from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListCyclesResponse>();

	function load() {
		task.run((signal) => settings.api().money.listCycles({}, { signal }));
	}
	$effect(load);

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let opening = $state(false);
	let form = $state({
		society_code: '',
		name: '',
		period_start: '',
		period_end: '',
		currency: 'INR',
		amount_scale: 2,
		deduction_policy: 'CAP_AT_EARNINGS'
	});

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(fn: () => Promise<unknown>) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	/** The order a cycle moves in, which the service enforces and this reflects. */
	function tone(status: string) {
		switch (status) {
			case 'PAID':
				return 'calm';
			case 'APPROVED':
			case 'GATHERED':
				return 'attention';
			case 'ABANDONED':
				return 'neutral';
			default:
				return 'neutral';
		}
	}

	function canGather(c: Cycle) {
		return c.status === 'OPEN';
	}
	function canApprove(c: Cycle) {
		return c.status === 'GATHERED';
	}
</script>

<div class="page-head">
	<h1>Payment cycles</h1>
	<p>
		A fortnight, gathered into what each producer is owed, approved, and paid. The order is enforced
		by settlement-service and not by this screen: a cycle cannot be approved before it was gathered,
		and cannot be paid before it was approved.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { opening = !opening; saveError = undefined; }}>
		{opening ? 'Cancel' : 'Open a cycle'}
	</button>
</div>

<ErrorNote error={saveError} />

{#if opening}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(async () => {
				await settings.api().money.openCycle({
					society_code: form.society_code.trim(),
					name: form.name.trim(),
					period_start: form.period_start,
					period_end: form.period_end,
					currency: form.currency.trim().toUpperCase(),
					amount_scale: Number(form.amount_scale),
					deduction_policy: form.deduction_policy,
					actor: settings.actorOrUnknown
				});
				opening = false;
			});
		}}
	>
		<div class="controls">
			<div class="field"><label for="sc">Society</label><input id="sc" bind:value={form.society_code} size="12" /></div>
			<div class="field grow"><label for="nm">Name</label><input id="nm" bind:value={form.name} placeholder="September, first fortnight" /></div>
			<div class="field"><label for="ps">From</label><input id="ps" type="date" bind:value={form.period_start} /></div>
			<div class="field"><label for="pe">To</label><input id="pe" type="date" bind:value={form.period_end} /></div>
			<div class="field"><label for="cu">Currency</label><input id="cu" bind:value={form.currency} size="4" /></div>
			<div class="field"><label for="as">Scale</label><input id="as" type="number" min="0" max="9" bind:value={form.amount_scale} size="3" /></div>
			<div class="field">
				<label for="dp">If deductions exceed earnings</label>
				<select id="dp" bind:value={form.deduction_policy}>
					<option value="CAP_AT_EARNINGS">Cap at earnings</option>
					<option value="ALLOW_NEGATIVE">Allow negative</option>
				</select>
			</div>
			<button type="submit" disabled={saving || !form.name.trim() || !form.period_start || !form.period_end}>
				{saving ? 'Opening…' : 'Open'}
			</button>
		</div>
		<p class="muted note">
			The scale is how many decimals this currency is recorded to and is not a display preference.
			A rupee has two and a dinar has three; an amount without its scale is not an amount.
		</p>
	</form>
{/if}

<Await {task} retry={load} isEmpty={(d) => (d.cycles ?? []).length === 0} empty="No cycles yet.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Cycle</th><th>Society</th><th>Period</th><th>Status</th><th>Approved</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.cycles as c (c.id)}
						<tr>
							<td><a href="/money/cycles/{c.id}">{c.name}</a></td>
							<td class="mono">{c.society_code}</td>
							<td>{instant(c.period_start)} — {instant(c.period_end)}</td>
							<td><Chip tone={tone(c.status)}>{label(c.status)}</Chip></td>
							<td class="muted">{c.approved_by ?? '—'}</td>
							<td>
								{#if canGather(c)}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.gatherCycle(c.id, settings.actorOrUnknown))}>
										Gather
									</button>
								{/if}
								{#if canApprove(c)}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.approveCycle(c.id, settings.actorOrUnknown))}>
										Approve
									</button>
								{/if}
								{#if c.status !== 'PAID' && c.status !== 'ABANDONED'}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().money.abandonCycle(c.id, settings.actorOrUnknown))}>
										Abandon
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<style>
	.note { max-width: var(--measure); margin: 0.8rem 0 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 16rem; }
</style>
