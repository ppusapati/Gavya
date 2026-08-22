<script lang="ts">
	import {
		ApiError,
		type EntityKind,
		type ListIdentitiesResponse,
		type MappingMethod,
		type ResolveIdentityResponse
	} from '$lib/api';
	import { fromLocalInput, instant, isOpenEnded, label, toLocalInput } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const KINDS: EntityKind[] = ['PRODUCER', 'CATTLE', 'ROUTE', 'CENTRE', 'DEVICE', 'SETTLEMENT'];
	const METHODS: MappingMethod[] = ['EXACT', 'MANUAL', 'INFERRED'];
	const PAGE = 50;

	function asError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	/* ---- the list ---- */

	let sourceFilter = $state('');
	let offset = $state(0);
	const list = new Task<ListIdentitiesResponse>();

	function loadList() {
		list.run((signal) =>
			settings
				.api()
				.listIdentities({ source_system_id: sourceFilter.trim(), limit: PAGE, offset }, { signal })
		);
	}

	$effect(() => {
		void [sourceFilter, offset];
		loadList();
	});

	const rows = $derived(list.data?.identities ?? []);

	/* ---- resolving one identifier at an instant ---- */

	let rSource = $state('');
	let rKind = $state<EntityKind>('PRODUCER');
	let rExternal = $state('');
	let rAsOf = $state(toLocalInput(new Date()));
	const resolved = new Task<ResolveIdentityResponse>();

	function doResolve(event: SubmitEvent) {
		event.preventDefault();
		const as_of = fromLocalInput(rAsOf);
		if (!as_of) return;
		resolved.run((signal) =>
			settings.api().resolveIdentity(
				{
					source_system_id: rSource.trim(),
					entity_kind: rKind,
					external_id: rExternal.trim(),
					as_of
				},
				{ signal }
			)
		);
	}

	/* ---- recording a new mapping ---- */

	let mSource = $state('');
	let mKind = $state<EntityKind>('PRODUCER');
	let mExternal = $state('');
	let mEntity = $state('');
	let mMethod = $state<MappingMethod>('MANUAL');
	let mFrom = $state(toLocalInput(new Date()));
	let mTo = $state('');
	let mNote = $state('');
	let mapping = $state(false);
	let mapError = $state<ApiError | undefined>(undefined);
	let mapped = $state('');

	const canMap = $derived(
		!mapping &&
			mSource.trim() !== '' &&
			mExternal.trim() !== '' &&
			mEntity.trim() !== '' &&
			fromLocalInput(mFrom) !== ''
	);

	async function doMap(event: SubmitEvent) {
		event.preventDefault();
		if (!canMap) return;
		mapping = true;
		mapError = undefined;
		mapped = '';
		try {
			const res = await settings.api().mapIdentity({
				source_system_id: mSource.trim(),
				entity_kind: mKind,
				external_id: mExternal.trim(),
				entity_id: mEntity.trim(),
				method: mMethod,
				note: mNote.trim(),
				valid_from: fromLocalInput(mFrom),
				valid_to: fromLocalInput(mTo),
				actor: settings.actorOrUnknown
			});
			mapped = res.identity.id;
			mExternal = '';
			mEntity = '';
			mNote = '';
			loadList();
		} catch (cause) {
			mapError = asError(cause);
		} finally {
			mapping = false;
		}
	}
</script>

<div class="page-head">
	<h1>External identities</h1>
	<p>
		An identifier from another system means whatever it meant at the moment it was used. Producer
		“114” can be one person this year and someone else the next, so a mapping is recorded with the
		interval it holds over — and resolving one always takes an instant, never just an identifier.
	</p>
</div>

<form class="panel" onsubmit={doResolve}>
	<h2>What did this identifier mean?</h2>
	<div class="controls">
		<div class="field">
			<label for="rs">Source system</label>
			<input id="rs" bind:value={rSource} size="16" placeholder="required" />
		</div>
		<div class="field">
			<label for="rk">Entity</label>
			<select id="rk" bind:value={rKind}>
				{#each KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
			</select>
		</div>
		<div class="field">
			<label for="re">External id</label>
			<input id="re" bind:value={rExternal} size="16" placeholder="required" />
		</div>
		<div class="field">
			<label for="ra">As of</label>
			<input id="ra" type="datetime-local" bind:value={rAsOf} />
		</div>
		<button type="submit" disabled={resolved.pending || !rSource.trim() || !rExternal.trim()}>
			Resolve
		</button>
	</div>

	{#if resolved.error?.code === 'not_found'}
		<!-- Not an error: the honest answer is that nothing was mapped then. -->
		<p class="muted">
			Nothing was mapped to that identifier at that instant. It may have been unassigned, or not yet
			mapped — try a different moment before concluding it is unknown.
		</p>
	{:else}
		<ErrorNote error={resolved.error} />
	{/if}

	{#if resolved.data?.identity}
		{@const i = resolved.data.identity}
		<dl class="kv result">
			<dt>Resolves to</dt>
			<dd class="mono">{i.entity_id}</dd>
			<dt>Method</dt>
			<dd>
				<Chip tone={i.method === 'INFERRED' ? 'attention' : 'neutral'}>{label(i.method)}</Chip>
				{#if i.confidence}<span class="muted">{(i.confidence * 100).toFixed(0)}% confident</span>{/if}
			</dd>
			<dt>Holds from</dt>
			<dd>{instant(i.valid_from)}</dd>
			<dt>Holds until</dt>
			<dd>{isOpenEnded(i.valid_to) ? 'no end recorded' : instant(i.valid_to)}</dd>
			{#if i.note}<dt>Note</dt><dd>{i.note}</dd>{/if}
		</dl>
	{:else if resolved.settled && !resolved.error}
		<p class="muted">No mapping held at that instant.</p>
	{/if}
</form>

<form class="panel" onsubmit={doMap}>
	<h2>Record a mapping</h2>
	<p class="muted">
		Leave the end open unless the identifier is known to have been reassigned. A mapping cannot
		overlap another for the same identifier — the service rejects one that would.
	</p>

	<ErrorNote error={mapError} />
	{#if mapped}
		<p class="ok">Recorded as <span class="mono">{mapped}</span>.</p>
	{/if}

	<div class="controls">
		<div class="field">
			<label for="ms">Source system</label>
			<input id="ms" bind:value={mSource} size="16" />
		</div>
		<div class="field">
			<label for="mk">Entity</label>
			<select id="mk" bind:value={mKind}>
				{#each KINDS as k (k)}<option value={k}>{label(k)}</option>{/each}
			</select>
		</div>
		<div class="field">
			<label for="me">External id</label>
			<input id="me" bind:value={mExternal} size="14" />
		</div>
		<div class="field">
			<label for="mi">Our id</label>
			<input id="mi" bind:value={mEntity} size="20" />
		</div>
		<div class="field">
			<label for="mm">How known</label>
			<select id="mm" bind:value={mMethod}>
				{#each METHODS as m (m)}<option value={m}>{label(m)}</option>{/each}
			</select>
		</div>
		<div class="field">
			<label for="mf">Holds from</label>
			<input id="mf" type="datetime-local" bind:value={mFrom} />
		</div>
		<div class="field">
			<label for="mt">Holds until</label>
			<input id="mt" type="datetime-local" bind:value={mTo} />
		</div>
	</div>

	<div class="field wide">
		<label for="mn">Note</label>
		<input id="mn" bind:value={mNote} placeholder="how this was established" />
	</div>

	<button type="submit" disabled={!canMap}>{mapping ? 'Recording…' : 'Record mapping'}</button>
</form>

<div class="controls listbar">
	<div class="field">
		<label for="sf">Filter by source system</label>
		<input
			id="sf"
			value={sourceFilter}
			placeholder="all"
			onchange={(e) => {
				sourceFilter = e.currentTarget.value;
				offset = 0;
			}}
		/>
	</div>
	<button class="ghost" onclick={loadList} disabled={list.pending}>Refresh</button>
</div>

<Await
	task={list}
	retry={loadList}
	isEmpty={(d) => (d.identities ?? []).length === 0}
	empty="No mapping has been recorded for this filter."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Source</th>
						<th>Entity</th>
						<th>External id</th>
						<th>Our id</th>
						<th>Method</th>
						<th>Holds</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as i (i.id)}
						<tr class:superseded={!!i.superseded_at}>
							<td class="mono">{i.source_system_id}</td>
							<td>{label(i.entity_kind)}</td>
							<td class="mono">{i.external_id}</td>
							<td class="mono">{i.entity_id}</td>
							<td>
								<Chip tone={i.method === 'INFERRED' ? 'attention' : 'neutral'}>
									{label(i.method)}
								</Chip>
							</td>
							<td>
								{instant(i.valid_from)} →
								{isOpenEnded(i.valid_to) ? 'open' : instant(i.valid_to)}
								{#if i.superseded_at}
									<Chip tone="neutral" title="Corrected on {instant(i.superseded_at)}. It is kept, not deleted.">
										superseded
									</Chip>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</Await>

<div class="pager">
	<button class="ghost" disabled={offset === 0 || list.pending} onclick={() => (offset = Math.max(0, offset - PAGE))}>
		← Previous
	</button>
	<span class="muted">Rows {offset + 1}–{offset + rows.length}</span>
	<button class="ghost" disabled={rows.length < PAGE || list.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	.result {
		margin-top: 1rem;
		border-top: 1px solid var(--rule);
		padding-top: 1rem;
	}

	.ok {
		color: var(--accent);
		font-size: 0.86rem;
	}

	.field.wide {
		max-width: var(--measure);
		margin-bottom: 1rem;
	}

	.field.wide input {
		width: 100%;
	}

	.listbar {
		margin-top: 1.75rem;
	}

	/* A superseded mapping is history, not noise — it stays visible but recedes. */
	.superseded {
		opacity: 0.6;
	}

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
