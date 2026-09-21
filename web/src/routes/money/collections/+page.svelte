<script lang="ts">
	import { ApiError, type ListCollectionsResponse, type PricedCollection } from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const task = new Task<ListCollectionsResponse>();
	const versions = new Task<{ versions: PricedCollection[] }>();

	let producer = $state('');
	let from = $state(today());
	let to = $state(today());
	let includeSuperseded = $state(false);

	function load() {
		task.run((signal) =>
			settings.api().money.listCollections(
				{
					producer_ref: producer.trim() || undefined,
					from: new Date(from).toISOString(),
					to: new Date(to).toISOString(),
					include_superseded: includeSuperseded,
					limit: 100
				},
				{ signal }
			)
		);
	}

	$effect(() => {
		void [from, to, includeSuperseded];
		load();
	});

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let recording = $state(false);
	let rec = $state({ producer_ref: '', society_code: '', collected_on: today(), shift: 'MORNING', quantity: '', unit: 'PER_LITRE', fat: '', snf: '' });
	let correcting = $state<string | undefined>(undefined);
	let fix = $state({ quantity: '', fat: '', snf: '', reason: '' });

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError ? cause : new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	/** A decimal the operator typed, as digits and the scale they wrote it at. */
	function point(v: string) {
		const t = v.trim();
		if (!t) return undefined;
		const [, frac = ''] = t.split('.');
		return { value: t.replace('.', ''), scale: frac.length };
	}

	async function run(fn: () => Promise<unknown>, after: () => void) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}
</script>

<div class="page-head">
	<h1>Priced collections</h1>
	<p>
		Every collection, what it was worth, and the sentence saying why. A correction supersedes rather
		than edits, so what a fortnight was paid on stays readable after somebody changes it.
	</p>
</div>

<div class="controls">
	<div class="field"><label for="pr">Producer</label><input id="pr" bind:value={producer} size="22" onchange={load} /></div>
	<div class="field"><label for="fr">From</label><input id="fr" type="date" bind:value={from} /></div>
	<div class="field"><label for="to">To</label><input id="to" type="date" bind:value={to} /></div>
	<label class="check"><input type="checkbox" bind:checked={includeSuperseded} /> Show superseded</label>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { recording = !recording; saveError = undefined; }}>
		{recording ? 'Cancel' : 'Record one'}
	</button>
</div>

<ErrorNote error={saveError} />

{#if recording}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			const q = point(rec.quantity);
			if (!q) return;
			run(
				() =>
					settings.api().money.recordCollection({
						producer_ref: rec.producer_ref.trim(),
						society_code: rec.society_code.trim() || undefined,
						collected_on: rec.collected_on,
						shift: rec.shift,
						quantity: q,
						quantity_unit: rec.unit,
						fat: point(rec.fat),
						snf: point(rec.snf),
						actor: settings.actorOrUnknown
					}),
				() => { recording = false; rec.quantity = ''; }
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="cp">Producer</label><input id="cp" bind:value={rec.producer_ref} size="22" /></div>
			<div class="field"><label for="cs">Society</label><input id="cs" bind:value={rec.society_code} size="12" /></div>
			<div class="field"><label for="cd">Day</label><input id="cd" type="date" bind:value={rec.collected_on} /></div>
			<div class="field">
				<label for="ch">Shift</label>
				<select id="ch" bind:value={rec.shift}><option value="MORNING">Morning</option><option value="EVENING">Evening</option></select>
			</div>
			<div class="field"><label for="cq">Quantity</label><input id="cq" bind:value={rec.quantity} size="9" inputmode="decimal" placeholder="6.250" /></div>
			<div class="field">
				<label for="cu">Unit</label>
				<select id="cu" bind:value={rec.unit}><option value="PER_LITRE">Litres</option><option value="PER_KG">Kilograms</option></select>
			</div>
			<div class="field"><label for="cf">Fat</label><input id="cf" bind:value={rec.fat} size="6" inputmode="decimal" /></div>
			<div class="field"><label for="cn">SNF</label><input id="cn" bind:value={rec.snf} size="6" inputmode="decimal" /></div>
			<button type="submit" disabled={saving || !rec.producer_ref.trim() || !rec.quantity.trim()}>
				{saving ? 'Recording…' : 'Record'}
			</button>
		</div>
		<p class="muted note">
			The rate card in force on the day prices it; nothing here chooses one. The reply carries the
			sentence explaining what it was priced against.
		</p>
	</form>
{/if}

<Await {task} retry={load} isEmpty={(d) => (d.collections ?? []).length === 0} empty="No collections in that window.">
	{#snippet children(d)}
		{#if d.total}
			<p class="figure">{formatExact(d.total, d.currency ?? '')} <span class="muted">across {d.collections.length}</span></p>
		{/if}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Day</th><th>Shift</th><th>Producer</th><th class="num">Quantity</th><th class="num">Rate</th><th class="num">Amount</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.collections as c (c.id)}
						<tr class:superseded={!!c.superseded_at}>
							<td>{instant(c.collected_on)}</td>
							<td>{label(c.shift)}</td>
							<td class="mono">{c.producer_ref}</td>
							<td class="num">{c.quantity.value}<span class="muted">/{c.quantity.scale}</span></td>
							<td class="num">{c.rate ?? '—'}</td>
							<td class="num">{formatExact(c.amount, c.currency)}</td>
							<td class="actions">
								{#if c.superseded_at}<Chip tone="neutral">superseded</Chip>{/if}
								<button class="ghost" onclick={() => versions.run((s) => settings.api().money.getCollectionVersions(c.id, { signal: s }))}>Versions</button>
								{#if !c.superseded_at}
									<button class="ghost" onclick={() => { correcting = correcting === c.id ? undefined : c.id; fix = { quantity: '', fat: '', snf: '', reason: '' }; }}>
										{correcting === c.id ? 'Cancel' : 'Correct'}
									</button>
								{/if}
							</td>
						</tr>
						<tr class="detail"><td colspan="7" class="muted">{c.explanation}</td></tr>
						{#if correcting === c.id}
							<tr class="detail">
								<td colspan="7">
									<form
										onsubmit={(e) => {
											e.preventDefault();
											const q = point(fix.quantity) ?? c.quantity;
											run(
												() =>
													settings.api().money.correctCollection({
														id: c.id,
														quantity: q,
														quantity_unit: c.quantity_unit,
														fat: point(fix.fat) ?? c.fat,
														snf: point(fix.snf) ?? c.snf,
														reason: fix.reason.trim(),
														actor: settings.actorOrUnknown
													}),
												() => (correcting = undefined)
											);
										}}
									>
										<div class="controls">
											<div class="field"><label for="fq-{c.id}">Quantity</label><input id="fq-{c.id}" bind:value={fix.quantity} size="9" inputmode="decimal" /></div>
											<div class="field"><label for="ff-{c.id}">Fat</label><input id="ff-{c.id}" bind:value={fix.fat} size="6" inputmode="decimal" /></div>
											<div class="field"><label for="fn-{c.id}">SNF</label><input id="fn-{c.id}" bind:value={fix.snf} size="6" inputmode="decimal" /></div>
											<div class="field grow"><label for="fr-{c.id}">Why</label><input id="fr-{c.id}" bind:value={fix.reason} /></div>
											<button type="submit" disabled={saving || !fix.reason.trim()}>Correct</button>
										</div>
										<p class="muted note">
											A reason is required and is not a formality. The service refuses a correction
											without one, because a figure that changed and cannot be shown to have changed
											is one nobody can defend to the member who asks about it.
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

{#if versions.settled}
	<Await task={versions} isEmpty={(d) => (d.versions ?? []).length === 0} empty="">
		{#snippet children(d)}
			<h2>Every version</h2>
			<div class="tablewrap">
				<table>
					<thead><tr><th>Recorded</th><th class="num">Quantity</th><th class="num">Amount</th><th>Why</th></tr></thead>
					<tbody>
						{#each d.versions as v (v.id)}
							<tr class:superseded={!!v.superseded_at}>
								<td>{instant(v.created_at)}</td>
								<td class="num">{v.quantity.value}/{v.quantity.scale}</td>
								<td class="num">{formatExact(v.amount, v.currency)}</td>
								<td class="muted">{v.correction_reason ?? v.explanation}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
{/if}

<style>
	.detail td { background: var(--surface-2); font-size: 0.82rem; }
	.superseded { opacity: 0.55; }
	.note { max-width: var(--measure); margin: 0.7rem 0 0; font-size: 0.82rem; }
	.figure { font-size: 1.4rem; margin: 0.4rem 0 1rem; }
	.figure .muted { font-size: 0.85rem; }
	.field.grow { flex: 1 1 14rem; }
	.actions { display: flex; gap: 0.5rem; align-items: center; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.82rem; }
</style>
