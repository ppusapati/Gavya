<script lang="ts">
	import { page } from '$app/state';
	import {
		ApiError,
		type CattleResponse,
		type GetBreedingHistoryResponse,
		type ListTreatmentsResponse,
		type ListVaccinationsResponse
	} from '$lib/api';
	import { instant, label, quantity, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const id = $derived(page.params.id ?? '');

	const animal = new Task<CattleResponse>();
	const cycles = new Task<GetBreedingHistoryResponse>();
	const shots = new Task<ListVaccinationsResponse>();
	const treatments = new Task<ListTreatmentsResponse>();
	const yieldTask = new Task<{ total_liters: string }>();

	let day = $state(today());

	function loadAll() {
		const api = settings.api().herd;
		animal.run((signal) => api.getCattle(id, { signal }));
		cycles.run((signal) => api.getBreedingHistory(id, { signal }));
		shots.run((signal) => api.getVaccinationHistory(id, { signal }));
		treatments.run((signal) => api.getTreatmentHistory(id, { signal }));
	}

	$effect(() => {
		void id;
		loadAll();
	});

	$effect(() => {
		void [id, day];
		if (id && day) {
			yieldTask.run((signal) => settings.api().herd.getDailyYield(id, day, { signal }));
		}
	});

	const c = $derived(animal.data?.cattle);

	/* ---- recording something against this animal ---- */

	type Sheet = 'vaccination' | 'treatment' | 'visit' | 'feed' | 'cycle' | undefined;
	let sheet = $state<Sheet>(undefined);
	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	let vac = $state({ vaccine_name: '', batch_number: '', dosage: '', next_due_date: '' });
	let tre = $state({ diagnosis: '', diagnosis_code: '', medicine_name: '', dosage: '', status: 'ongoing' });
	let vis = $state({ purpose: '', notes: '', cost: '', currency: 'INR' });
	let fed = $state({ feed_type_id: '', quantity_kg: '' });
	let cyc = $state({ notes: '', status: 'heat' });

	const feeds = new Task<{ feed_types: { id: string; name: string; unit: string }[] }>();
	$effect(() => {
		feeds.run((signal) => settings.api().herd.listFeedTypes({ signal }));
	});

	function open(which: Sheet) {
		sheet = sheet === which ? undefined : which;
		saveError = undefined;
	}

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	async function submit(event: SubmitEvent, run: () => Promise<unknown>, after: () => void) {
		event.preventDefault();
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await run();
			sheet = undefined;
			after();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	const nowISO = () => new Date().toISOString();
</script>

<div class="page-head">
	<h1>{c?.name || c?.tag_number || 'Animal'}</h1>
	<p>
		Everything four services hold about one animal: what it gave, what was done to it, what it was
		fed and what it has calved. A person thinking about a cow does not think in services.
	</p>
</div>

<p><a href="/herd">← The herd</a></p>

<Await task={animal} retry={loadAll} isEmpty={(d) => !d.cattle} empty="No such animal.">
	{#snippet children()}
		{#if c}
			<div class="panel">
				<dl class="kv wide">
					<dt>Tag</dt>
					<dd class="mono">{c.tag_number}</dd>
					<dt>Status</dt>
					<dd><Chip tone={c.status === 'active' ? 'calm' : 'neutral'}>{label(c.status)}</Chip></dd>
					<dt>Sex</dt>
					<dd>{c.gender === 'F' ? 'Female' : c.gender === 'M' ? 'Male' : c.gender}</dd>
					<dt>Weight</dt>
					<dd>{quantity(c.weight, 'kg')}</dd>
				</dl>
			</div>
		{/if}
	{/snippet}
</Await>

<section>
	<h2>What she gave</h2>
	<div class="controls">
		<div class="field">
			<label for="day">Day</label>
			<input id="day" type="date" bind:value={day} />
		</div>
	</div>
	<Await task={yieldTask} isEmpty={() => false}>
		{#snippet children(d)}
			<p class="figure">
				{quantity(d.total_liters, 'litres')}
				<span class="muted">on {day}</span>
			</p>
			<p class="muted note">
				Which day a collection falls on is a local fact. This is the figure milk-service reports
				for the tenant's own timezone, not for the browser's.
			</p>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Breeding</h2>
	<div class="controls">
		<button class="ghost" onclick={() => open('cycle')}>
			{sheet === 'cycle' ? 'Cancel' : 'Record a heat'}
		</button>
	</div>
	{#if sheet === 'cycle'}
		<form
			class="panel"
			onsubmit={(e) =>
				submit(
					e,
					() =>
						settings.api().herd.createBreedingCycle({
							cattle_id: id,
							heat_date: nowISO(),
							status: cyc.status,
							notes: cyc.notes.trim(),
							created_by: settings.actorOrUnknown
						}),
					() => cycles.run((s) => settings.api().herd.getBreedingHistory(id, { signal: s }))
				)}
		>
			<ErrorNote error={saveError} />
			<div class="controls">
				<div class="field">
					<label for="cs">Status</label>
					<input id="cs" bind:value={cyc.status} size="10" />
				</div>
				<div class="field grow">
					<label for="cn">Notes</label>
					<input id="cn" bind:value={cyc.notes} placeholder="What was observed, and when." />
				</div>
				<button type="submit" disabled={saving}>{saving ? 'Recording…' : 'Record'}</button>
			</div>
		</form>
	{/if}
	<Await
		task={cycles}
		isEmpty={(d) => (d.cycles ?? []).length === 0}
		empty="No breeding cycles recorded for this animal."
	>
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Heat</th><th>Status</th><th>Notes</th><th>Recorded by</th></tr></thead>
					<tbody>
						{#each d.cycles as cy (cy.id)}
							<tr>
								<td>{instant(cy.heat_date)}</td>
								<td><Chip tone="neutral">{label(cy.status)}</Chip></td>
								<td>{cy.notes || '—'}</td>
								<td class="mono">{cy.created_by}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Health</h2>
	<div class="controls">
		<button class="ghost" onclick={() => open('vaccination')}>
			{sheet === 'vaccination' ? 'Cancel' : 'Record a vaccination'}
		</button>
		<button class="ghost" onclick={() => open('treatment')}>
			{sheet === 'treatment' ? 'Cancel' : 'Record a treatment'}
		</button>
		<button class="ghost" onclick={() => open('visit')}>
			{sheet === 'visit' ? 'Cancel' : 'Record a vet visit'}
		</button>
	</div>

	{#if sheet === 'vaccination'}
		<form
			class="panel"
			onsubmit={(e) =>
				submit(
					e,
					() =>
						settings.api().herd.recordVaccination({
							cattle_id: id,
							vaccine_name: vac.vaccine_name.trim(),
							batch_number: vac.batch_number.trim(),
							administered_at: nowISO(),
							next_due_date: vac.next_due_date ? new Date(vac.next_due_date).toISOString() : undefined,
							veterinarian_id: settings.actorOrUnknown,
							dosage: vac.dosage.trim(),
							created_by: settings.actorOrUnknown
						}),
					() => shots.run((s) => settings.api().herd.getVaccinationHistory(id, { signal: s }))
				)}
		>
			<ErrorNote error={saveError} />
			<div class="controls">
				<div class="field grow">
					<label for="vn">Vaccine</label>
					<input id="vn" bind:value={vac.vaccine_name} />
				</div>
				<div class="field">
					<label for="vb">Batch</label>
					<input id="vb" bind:value={vac.batch_number} size="14" />
				</div>
				<div class="field">
					<label for="vd">Dose</label>
					<input id="vd" bind:value={vac.dosage} size="14" />
				</div>
				<div class="field">
					<label for="vnd">Next due</label>
					<input id="vnd" type="date" bind:value={vac.next_due_date} />
				</div>
				<button type="submit" disabled={saving || !vac.vaccine_name.trim()}>
					{saving ? 'Recording…' : 'Record'}
				</button>
			</div>
		</form>
	{/if}

	{#if sheet === 'treatment'}
		<form
			class="panel"
			onsubmit={(e) =>
				submit(
					e,
					() =>
						settings.api().herd.recordTreatment({
							cattle_id: id,
							diagnosis_code: tre.diagnosis_code.trim(),
							diagnosis: tre.diagnosis.trim(),
							medicine_name: tre.medicine_name.trim(),
							dosage: tre.dosage.trim(),
							treated_at: nowISO(),
							treated_by: settings.actorOrUnknown,
							status: tre.status.trim(),
							created_by: settings.actorOrUnknown
						}),
					() => treatments.run((s) => settings.api().herd.getTreatmentHistory(id, { signal: s }))
				)}
		>
			<ErrorNote error={saveError} />
			<div class="controls">
				<div class="field grow">
					<label for="td">Diagnosis</label>
					<input id="td" bind:value={tre.diagnosis} />
				</div>
				<div class="field">
					<label for="tc">Code</label>
					<input id="tc" bind:value={tre.diagnosis_code} size="12" />
				</div>
				<div class="field">
					<label for="tm">Medicine</label>
					<input id="tm" bind:value={tre.medicine_name} size="18" />
				</div>
				<div class="field">
					<label for="tds">Dose</label>
					<input id="tds" bind:value={tre.dosage} size="14" />
				</div>
				<div class="field">
					<label for="ts">Status</label>
					<input id="ts" bind:value={tre.status} size="10" />
				</div>
				<button type="submit" disabled={saving || !tre.diagnosis.trim()}>
					{saving ? 'Recording…' : 'Record'}
				</button>
			</div>
		</form>
	{/if}

	{#if sheet === 'visit'}
		<form
			class="panel"
			onsubmit={(e) =>
				submit(
					e,
					() =>
						settings.api().herd.scheduleVetVisit({
							cattle_id: id,
							veterinarian_id: settings.actorOrUnknown,
							visit_date: nowISO(),
							purpose: vis.purpose.trim(),
							notes: vis.notes.trim(),
							cost: vis.cost.trim(),
							currency: vis.currency.trim().toUpperCase(),
							created_by: settings.actorOrUnknown
						}),
					() => {}
				)}
		>
			<ErrorNote error={saveError} />
			<div class="controls">
				<div class="field grow">
					<label for="pp">Purpose</label>
					<input id="pp" bind:value={vis.purpose} />
				</div>
				<div class="field">
					<label for="pc">Cost</label>
					<input id="pc" bind:value={vis.cost} size="10" inputmode="decimal" />
				</div>
				<div class="field">
					<label for="pu">Currency</label>
					<input id="pu" bind:value={vis.currency} size="4" />
				</div>
				<button type="submit" disabled={saving}>{saving ? 'Recording…' : 'Record'}</button>
			</div>
			<p class="muted note">
				The currency goes with the amount and is not optional. An amount recorded without one is
				the defect a whole layer of this platform's database exists to prevent.
			</p>
		</form>
	{/if}

	<h3>Vaccinations</h3>
	<Await task={shots} isEmpty={(d) => (d.vaccinations ?? []).length === 0} empty="None recorded.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Vaccine</th><th>Batch</th><th>Given</th><th>Next due</th><th>Dose</th></tr></thead>
					<tbody>
						{#each d.vaccinations as v (v.id)}
							<tr>
								<td>{v.vaccine_name}</td>
								<td class="mono">{v.batch_number || '—'}</td>
								<td>{instant(v.administered_at)}</td>
								<td>{v.next_due_date ? instant(v.next_due_date) : '—'}</td>
								<td>{v.dosage || '—'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>

	<h3>Treatments</h3>
	<Await task={treatments} isEmpty={(d) => (d.treatments ?? []).length === 0} empty="None recorded.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Diagnosis</th><th>Medicine</th><th>Treated</th><th>Status</th></tr></thead>
					<tbody>
						{#each d.treatments as tr (tr.id)}
							<tr>
								<td>{tr.diagnosis}</td>
								<td>{tr.medicine_name || '—'}</td>
								<td>{instant(tr.treated_at)}</td>
								<td><Chip tone={tr.status === 'ongoing' ? 'attention' : 'calm'}>{label(tr.status)}</Chip></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<section>
	<h2>Feed</h2>
	<div class="controls">
		<button class="ghost" onclick={() => open('feed')}>
			{sheet === 'feed' ? 'Cancel' : 'Record what she was fed'}
		</button>
	</div>
	{#if sheet === 'feed'}
		<form
			class="panel"
			onsubmit={(e) =>
				submit(
					e,
					() =>
						settings.api().herd.recordFeedConsumption({
							cattle_id: id,
							feed_type_id: fed.feed_type_id,
							quantity_kg: fed.quantity_kg.trim(),
							fed_at: nowISO(),
							fed_by: settings.actorOrUnknown,
							created_by: settings.actorOrUnknown
						}),
					() => {}
				)}
		>
			<ErrorNote error={saveError} />
			<div class="controls">
				<div class="field">
					<label for="ft">Feed</label>
					<select id="ft" bind:value={fed.feed_type_id}>
						<option value="">—</option>
						{#each feeds.data?.feed_types ?? [] as f (f.id)}<option value={f.id}>{f.name}</option>{/each}
					</select>
				</div>
				<div class="field">
					<label for="fq">Quantity (kg)</label>
					<input id="fq" bind:value={fed.quantity_kg} size="8" inputmode="decimal" />
				</div>
				<button type="submit" disabled={saving || !fed.feed_type_id}>
					{saving ? 'Recording…' : 'Record'}
				</button>
			</div>
		</form>
	{/if}
	<p class="muted note">
		Feed types and standing rations are kept on <a href="/farms">Farms &amp; feed</a>.
	</p>
</section>

<style>
	section {
		margin-top: 2rem;
	}

	h3 {
		margin: 1.4rem 0 0.6rem;
		font-size: 0.9rem;
	}

	.figure {
		font-size: 1.6rem;
		margin: 0.4rem 0 0.2rem;
	}

	.figure .muted {
		font-size: 0.85rem;
	}

	.note {
		max-width: var(--measure);
		font-size: 0.82rem;
	}

	.kv.wide {
		grid-template-columns: max-content 1fr;
	}

	.field.grow {
		flex: 1 1 16rem;
	}
</style>
