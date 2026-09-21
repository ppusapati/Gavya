<script lang="ts">
	import { ApiError, type ListActivePregnanciesResponse, type Pregnancy } from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListActivePregnanciesResponse>();

	function load() {
		task.run((signal) => settings.api().herd.listActivePregnancies({ signal }));
	}

	$effect(load);

	/* ---- the three steps that follow a confirmed pregnancy ---- */

	let open = $state<string | undefined>(undefined);
	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let calf = $state({ calf_id: '', calf_gender: 'F', calf_weight: '', complications: '', status: 'normal' });

	/* ---- starting one: an insemination against an existing cycle ---- */

	let starting = $state(false);
	let ins = $state({ cycle_id: '', cattle_id: '', bull_id: '', semen_batch_id: '', method: 'AI' });
	let conf = $state({ cattle_id: '', insemination_id: '', expected: '' });

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function run(event: SubmitEvent, fn: () => Promise<unknown>) {
		event.preventDefault();
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			open = undefined;
			starting = false;
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	const nowISO = () => new Date().toISOString();

	function expected(p: Pregnancy): string {
		return instant(p.expected_calving_date);
	}
</script>

<div class="page-head">
	<h1>Pregnancies</h1>
	<p>
		Every pregnancy the society is currently carrying, and the two things that end one: a calving,
		or a cycle that goes round again. A calf recorded here is an animal in the herd, which is one of
		the twenty-two references in this platform that cross a service boundary.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { starting = !starting; saveError = undefined; }}>
		{starting ? 'Cancel' : 'Record an insemination'}
	</button>
</div>

{#if starting}
	<form
		class="panel"
		onsubmit={(e) =>
			run(e, () =>
				settings.api().herd.recordInsemination({
					cycle_id: ins.cycle_id.trim(),
					cattle_id: ins.cattle_id.trim(),
					bull_id: ins.bull_id.trim() || undefined,
					semen_batch_id: ins.semen_batch_id.trim() || undefined,
					inseminated_at: nowISO(),
					method: ins.method.trim(),
					created_by: settings.actorOrUnknown
				})
			)}
	>
		<h2>Record an insemination</h2>
		<ErrorNote error={saveError} />
		<div class="controls">
			<div class="field">
				<label for="ic">Cycle</label>
				<input id="ic" bind:value={ins.cycle_id} size="26" placeholder="the heat it follows" />
			</div>
			<div class="field">
				<label for="ia">Animal</label>
				<input id="ia" bind:value={ins.cattle_id} size="26" />
			</div>
			<div class="field">
				<label for="ib">Bull</label>
				<input id="ib" bind:value={ins.bull_id} size="20" placeholder="optional" />
			</div>
			<div class="field">
				<label for="is">Semen batch</label>
				<input id="is" bind:value={ins.semen_batch_id} size="20" placeholder="optional" />
			</div>
			<div class="field">
				<label for="im">Method</label>
				<input id="im" bind:value={ins.method} size="6" />
			</div>
			<button type="submit" disabled={saving || !ins.cycle_id.trim() || !ins.cattle_id.trim()}>
				{saving ? 'Recording…' : 'Record'}
			</button>
		</div>
	</form>

	<form
		class="panel"
		onsubmit={(e) =>
			run(e, () =>
				settings.api().herd.confirmPregnancy({
					cattle_id: conf.cattle_id.trim(),
					insemination_id: conf.insemination_id.trim(),
					confirmed_at: nowISO(),
					expected_calving_date: conf.expected
						? new Date(conf.expected).toISOString()
						: nowISO(),
					created_by: settings.actorOrUnknown
				})
			)}
	>
		<h2>Confirm a pregnancy</h2>
		<ErrorNote error={saveError} />
		<div class="controls">
			<div class="field">
				<label for="ca">Animal</label>
				<input id="ca" bind:value={conf.cattle_id} size="26" />
			</div>
			<div class="field">
				<label for="ci">Insemination</label>
				<input id="ci" bind:value={conf.insemination_id} size="26" />
			</div>
			<div class="field">
				<label for="ce">Expected calving</label>
				<input id="ce" type="date" bind:value={conf.expected} />
			</div>
			<button type="submit" disabled={saving || !conf.cattle_id.trim() || !conf.insemination_id.trim()}>
				{saving ? 'Confirming…' : 'Confirm'}
			</button>
		</div>
	</form>
{/if}

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.pregnancies ?? []).length === 0}
	empty="No pregnancies are being carried. That is a season, not an error."
>
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Animal</th><th>Confirmed</th><th>Expected</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.pregnancies as p (p.id)}
						<tr>
							<td><a href="/herd/{p.cattle_id}" class="mono">{p.cattle_id}</a></td>
							<td>{instant(p.confirmed_at)}</td>
							<td>{expected(p)}</td>
							<td><Chip tone="calm">{label(p.status)}</Chip></td>
							<td>
								<button
									class="ghost"
									onclick={() => {
										open = open === p.id ? undefined : p.id;
										calf = { calf_id: '', calf_gender: 'F', calf_weight: '', complications: '', status: 'normal' };
										saveError = undefined;
									}}
								>
									{open === p.id ? 'Cancel' : 'Record calving'}
								</button>
							</td>
						</tr>
						{#if open === p.id}
							<tr class="detail">
								<td colspan="5">
									<form
										onsubmit={(e) =>
											run(e, () =>
												settings.api().herd.recordCalving({
													pregnancy_id: p.id,
													cattle_id: p.cattle_id,
													calf_id: calf.calf_id.trim() || undefined,
													calf_gender: calf.calf_gender,
													calf_weight: calf.calf_weight.trim(),
													complications: calf.complications.trim(),
													status: calf.status.trim(),
													created_by: settings.actorOrUnknown
												})
											)}
									>
										<ErrorNote error={saveError} />
										<div class="controls">
											<div class="field">
												<label for="kc-{p.id}">Calf</label>
												<input id="kc-{p.id}" bind:value={calf.calf_id} size="26" placeholder="once it is in the herd" />
											</div>
											<div class="field">
												<label for="kg-{p.id}">Sex</label>
												<select id="kg-{p.id}" bind:value={calf.calf_gender}>
													<option value="F">Female</option>
													<option value="M">Male</option>
												</select>
											</div>
											<div class="field">
												<label for="kw-{p.id}">Weight (kg)</label>
												<input id="kw-{p.id}" bind:value={calf.calf_weight} size="8" inputmode="decimal" />
											</div>
											<div class="field grow">
												<label for="kx-{p.id}">Complications</label>
												<input id="kx-{p.id}" bind:value={calf.complications} placeholder="none, if none" />
											</div>
											<button type="submit" disabled={saving}>{saving ? 'Recording…' : 'Record'}</button>
										</div>
										<p class="muted note">
											The calf identifier points at an animal in cattle-service. Leaving it empty
											records the calving without one, which is the honest state until the calf is
											entered in the herd.
										</p>
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

<style>
	.detail td {
		background: var(--surface-2);
	}

	.note {
		max-width: var(--measure);
		margin: 0.8rem 0 0;
		font-size: 0.82rem;
	}

	.field.grow {
		flex: 1 1 14rem;
	}
</style>
