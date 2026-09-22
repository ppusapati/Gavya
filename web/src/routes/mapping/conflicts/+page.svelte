<script lang="ts">
	import {
		ApiError,
		type ClaimSlotResponse,
		type CollectionSlot,
		type GetEffectivePolicyResponse,
		type GetSlotResponse,
		type ListConflictsResponse,
		type ListPoliciesResponse
	} from '$lib/api';
	import { fromLocalInput, instant, isOpenEnded, label, shortId, toLocalInput } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const PAGE = 50;

	let offset = $state(0);
	const task = new Task<ListConflictsResponse>();

	function load() {
		task.run((signal) => settings.api().listSlotConflicts({ limit: PAGE, offset }, { signal }));
	}

	$effect(() => {
		void offset;
		load();
	});

	const slots = $derived(task.data?.slots ?? []);

	/* ---- the policies that decide a conflict ---- */

	const policies = new Task<ListPoliciesResponse>();
	const effective = new Task<GetEffectivePolicyResponse>();

	function loadPolicies() {
		policies.run((s) => settings.api().listPolicies({ signal: s }));
	}
	$effect(loadPolicies);

	let effectiveAt = $state(toLocalInput(new Date()));
	let declaring = $state(false);
	let np = $state({
		name: '',
		dimensions: 'producer_ref,centre_ref,shift,collected_on',
		resolution: 'FIRST_WINS',
		version: 1,
		effective_from: toLocalInput(new Date()),
		effective_to: ''
	});

	/* ---- one slot, and claiming one ---- */

	const oneSlot = new Task<GetSlotResponse>();
	let lookup = $state({ slot_key: '', origin_kind: '' });

	const claimed = new Task<ClaimSlotResponse>();
	let claim = $state({
		source_ref: '',
		origin: '',
		values: 'producer_ref=, centre_ref=, shift=, collected_on=',
		quality: 0,
		recorded_at: toLocalInput(new Date()),
		collected_at: ''
	});

	/**
	 * "a=1, b=2" to {a: '1', b: '2'}.
	 *
	 * The dimensions a policy names are the tenant's, not this console's, so the
	 * form cannot offer fields for them. A pair with no '=' is dropped rather
	 * than stored under an empty key — a value filed under "" is a value nobody
	 * will find again.
	 */
	function parseValues(text: string): Record<string, string> {
		const out: Record<string, string> = {};
		for (const pair of text.split(',')) {
			const eq = pair.indexOf('=');
			if (eq <= 0) continue;
			const k = pair.slice(0, eq).trim();
			if (k === '') continue;
			out[k] = pair.slice(eq + 1).trim();
		}
		return out;
	}

	const claimValues = $derived(parseValues(claim.values));

	function outcomeTone(o: string) {
		switch (o) {
			case 'CLAIMED':
			case 'KEPT':
				return 'calm';
			case 'CONFLICT':
				return 'critical';
			case 'REPLACED':
				return 'attention';
			default:
				return 'neutral';
		}
	}

	let open = $state<string | undefined>(undefined);
	let choice = $state('');
	let why = $state('');
	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function expand(slot: CollectionSlot) {
		if (open === slot.id) {
			open = undefined;
			return;
		}
		open = slot.id;
		choice = slot.authoritative_ref;
		why = '';
		saveError = undefined;
	}

	async function resolve(event: SubmitEvent, slot: CollectionSlot) {
		event.preventDefault();
		if (!choice || !why.trim() || saving) return;
		saving = true;
		saveError = undefined;
		try {
			await settings.api().resolveSlotConflict({
				slot_id: slot.id,
				authoritative_ref: choice,
				resolution: why.trim(),
				actor: settings.actorOrUnknown
			});
			open = undefined;
			load();
		} catch (cause) {
			saveError =
				cause instanceof ApiError
					? cause
					: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
		} finally {
			saving = false;
		}
	}
</script>

<div class="page-head">
	<h1>Collection slot conflicts</h1>
	<p>
		A slot is one collection — a producer, a centre, a shift, a day — and only one record can be the
		authoritative one for it. When a second record arrives that the slot's policy cannot rank against
		the first, neither is discarded: the slot is held open here for someone to decide.
	</p>
</div>

<div class="controls">
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
</div>

<details class="policies">
	<summary>The policies that decide these</summary>
	<p class="muted note">
		A policy says what makes a slot — the dimensions whose values together identify one — and how a
		second claim on an occupied slot is settled. It is versioned and dated, so a collection from
		March is judged by March's policy rather than today's; that is why the claims below carry the
		day the milk was collected as well as the day they were recorded.
	</p>

	<div class="controls">
		<button class="ghost" onclick={loadPolicies} disabled={policies.pending}>Refresh</button>
		<div class="field"><label for="ea">In force at</label><input id="ea" type="datetime-local" bind:value={effectiveAt} /></div>
		<button
			class="ghost"
			disabled={effective.pending}
			onclick={() => effective.run((s) => settings.api().getEffectivePolicy(fromLocalInput(effectiveAt) || undefined, { signal: s }))}
		>
			Which applied then?
		</button>
		<button class="ghost" onclick={() => (declaring = !declaring)}>
			{declaring ? 'Cancel' : 'Declare one'}
		</button>
	</div>

	{#if effective.settled}
		<Await task={effective} isEmpty={(d) => !d.policy} empty="No policy was in force at that moment.">
			{#snippet children(d)}
				<p class="banner">
					<Chip tone="neutral">{d.policy.name} v{d.policy.version}</Chip>
					{label(d.policy.resolution)} over <span class="mono">{d.policy.dimensions.join(', ')}</span>
				</p>
			{/snippet}
		</Await>
	{/if}

	{#if declaring}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				const from = fromLocalInput(np.effective_from);
				if (!from) return;
				saving = true;
				saveError = undefined;
				settings
					.api()
					.declarePolicy({
						name: np.name.trim(),
						dimensions: np.dimensions.split(',').map((x) => x.trim()).filter(Boolean),
						resolution: np.resolution,
						version: np.version,
						effective_from: from,
						effective_to: fromLocalInput(np.effective_to) || undefined,
						actor: settings.actorOrUnknown
					})
					.then(() => {
						declaring = false;
						loadPolicies();
					})
					.catch((c) => {
						saveError =
							c instanceof ApiError
								? c
								: new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
					})
					.finally(() => (saving = false));
			}}
		>
			<div class="controls">
				<div class="field"><label for="pn">Name</label><input id="pn" bind:value={np.name} size="18" /></div>
				<div class="field grow"><label for="pd">Dimensions</label><input id="pd" bind:value={np.dimensions} /></div>
				<div class="field">
					<label for="pr">When two claim it</label>
					<select id="pr" bind:value={np.resolution}>
						<option value="FIRST_WINS">First wins</option>
						<option value="LAST_WINS">Last wins</option>
						<option value="HIGHEST_QUALITY">Highest quality wins</option>
						<option value="MANUAL">Hold it for a person</option>
					</select>
				</div>
				<div class="field"><label for="pv">Version</label><input id="pv" type="number" min="1" bind:value={np.version} size="4" /></div>
				<div class="field"><label for="pf">From</label><input id="pf" type="datetime-local" bind:value={np.effective_from} /></div>
				<div class="field"><label for="pt">Until</label><input id="pt" type="datetime-local" bind:value={np.effective_to} /></div>
				<button type="submit" disabled={saving || !np.name.trim()}>Declare</button>
			</div>
		</form>
	{/if}

	<Await task={policies} retry={loadPolicies} isEmpty={(d) => (d.policies ?? []).length === 0} empty="No policy has been declared.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Name</th><th class="num">Version</th><th>Dimensions</th><th>When two claim it</th><th>In force</th></tr></thead>
					<tbody>
						{#each d.policies as pol (pol.id)}
							<tr>
								<td>{pol.name}</td>
								<td class="num">{pol.version}</td>
								<td class="mono">{pol.dimensions.join(', ')}</td>
								<td>{label(pol.resolution)}</td>
								<td>
									{instant(pol.effective_from)} →
									{pol.effective_to && !isOpenEnded(pol.effective_to) ? instant(pol.effective_to) : 'open'}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</details>

<details class="policies">
	<summary>Look up or claim a slot</summary>

	<div class="controls">
		<div class="field"><label for="sk">Slot key</label><input id="sk" bind:value={lookup.slot_key} size="28" /></div>
		<div class="field"><label for="so">Origin</label><input id="so" bind:value={lookup.origin_kind} size="14" /></div>
		<button
			class="ghost"
			disabled={!lookup.slot_key.trim() || oneSlot.pending}
			onclick={() => oneSlot.run((s) => settings.api().getSlot(lookup.slot_key.trim(), lookup.origin_kind.trim(), { signal: s }))}
		>
			Look up
		</button>
	</div>

	{#if oneSlot.settled}
		<Await task={oneSlot} isEmpty={(d) => !d.slot} empty="No such slot.">
			{#snippet children(d)}
				<dl class="kv">
					<dt>Status</dt>
					<dd><Chip tone={d.slot.status === 'CONFLICT' ? 'attention' : 'calm'}>{label(d.slot.status)}</Chip></dd>
					<dt>Holds</dt><dd class="mono">{d.slot.authoritative_ref}</dd>
					<dt>Under</dt><dd>{d.slot.policy_id} v{d.slot.policy_version}</dd>
					<dt>Contenders</dt><dd>{d.slot.contenders.length}</dd>
				</dl>
			{/snippet}
		</Await>
	{/if}

	<form
		onsubmit={(e) => {
			e.preventDefault();
			const at = fromLocalInput(claim.recorded_at);
			if (!at) return;
			claimed.run((s) =>
				settings.api().claimSlot(
					{
						source_ref: claim.source_ref.trim(),
						values: claimValues,
						origin: claim.origin.trim(),
						recorded_at: at,
						quality: claim.quality || undefined,
						collected_at: fromLocalInput(claim.collected_at) || undefined,
						actor: settings.actorOrUnknown
					},
					{ signal: s }
				)
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="cs">Record</label><input id="cs" bind:value={claim.source_ref} size="24" /></div>
			<div class="field"><label for="co">Origin</label><input id="co" bind:value={claim.origin} size="14" /></div>
			<div class="field grow"><label for="cv">Values</label><input id="cv" bind:value={claim.values} /></div>
			<div class="field"><label for="cq">Quality</label><input id="cq" type="number" min="0" bind:value={claim.quality} size="5" /></div>
			<div class="field"><label for="cr">Recorded</label><input id="cr" type="datetime-local" bind:value={claim.recorded_at} /></div>
			<div class="field"><label for="cc">Collected</label><input id="cc" type="datetime-local" bind:value={claim.collected_at} /></div>
			<button type="submit" disabled={claimed.pending || !claim.source_ref.trim()}>Claim</button>
		</div>
		<p class="muted note">
			Values are written as <span class="mono">key=value</span> pairs, because the dimensions are
			the tenant's and not this console's — the policy above names them. The moment of collection
			selects the policy version in force when the milk was collected rather than the one in force
			today, which is the whole reason policies are versioned.
		</p>
		{#if Object.keys(claimValues).length > 0}
			<p class="muted note">
				Reading as: {#each Object.entries(claimValues) as [k, v] (k)}<Chip tone="neutral">{k} = {v || '(empty)'}</Chip>{/each}
			</p>
		{/if}
	</form>

	{#if claimed.settled}
		<Await task={claimed} isEmpty={(d) => !d.slot} empty="Nothing came back.">
			{#snippet children(d)}
				<p class="banner">
					<Chip tone={outcomeTone(d.outcome)}>{label(d.outcome)}</Chip>
					{d.reason}
				</p>
				<dl class="kv">
					<dt>Slot now holds</dt><dd class="mono">{d.slot.authoritative_ref}</dd>
					<dt>Status</dt><dd>{label(d.slot.status)}</dd>
				</dl>
			{/snippet}
		</Await>
	{/if}
</details>

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.slots ?? []).length === 0}
	empty="No slot is in conflict. Every collection has exactly one authoritative record."
>
	{#snippet children()}
		{#each slots as slot (slot.id)}
			<div class="panel slot">
				<div class="head">
					<div>
						<h2 class="mono key">{slot.slot_key}</h2>
						<p class="muted origin">
							{label(slot.origin_kind)} · policy
							<span class="mono">{shortId(slot.policy_id)}</span> v{slot.policy_version}
						</p>
					</div>
					<Chip tone="attention">{label(slot.status)}</Chip>
				</div>

				{#if Object.keys(slot.values ?? {}).length}
					<dl class="kv values">
						{#each Object.entries(slot.values) as [k, v] (k)}
							<dt>{label(k)}</dt>
							<dd class="mono">{v}</dd>
						{/each}
					</dl>
				{/if}

				<div class="tablewrap">
					<table>
						<thead>
							<tr>
								<th></th>
								<th>Record</th>
								<th>Origin</th>
								<th>Recorded</th>
								<th>Quality</th>
								<th>Why it could not be ranked</th>
							</tr>
						</thead>
						<tbody>
							<tr>
								<td><Chip tone="calm">holding</Chip></td>
								<td class="mono">{slot.authoritative_ref}</td>
								<td>—</td>
								<td>{instant(slot.incumbent_recorded_at)}</td>
								<td class="num">{slot.incumbent_quality}</td>
								<td class="muted">the record currently standing for this slot</td>
							</tr>
							{#each slot.contenders ?? [] as c, i (c.source_ref + i)}
								<tr>
									<td><Chip>contender</Chip></td>
									<td class="mono">{c.source_ref}</td>
									<td>{label(c.origin)}</td>
									<td>{instant(c.recorded_at)}</td>
									<td class="num">{c.quality}</td>
									<td class="muted">{c.reason}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>

				<button class="ghost expand" onclick={() => expand(slot)}>
					{open === slot.id ? 'Cancel' : 'Decide which record stands'}
				</button>

				{#if open === slot.id}
					<form class="decide" onsubmit={(e) => resolve(e, slot)}>
						<ErrorNote error={saveError} />
						<div class="field">
							<label for="pick-{slot.id}">Authoritative record</label>
							<select id="pick-{slot.id}" bind:value={choice}>
								<option value={slot.authoritative_ref}>{slot.authoritative_ref} (holding)</option>
								{#each slot.contenders ?? [] as c, i (c.source_ref + i)}
									<option value={c.source_ref}>{c.source_ref}</option>
								{/each}
							</select>
						</div>
						<div class="field wide">
							<label for="why-{slot.id}">Why</label>
							<textarea
								id="why-{slot.id}"
								rows="2"
								bind:value={why}
								placeholder="What settles it — a weighbridge slip, an operator's account, a device fault."
							></textarea>
						</div>
						<button type="submit" disabled={!choice || !why.trim() || saving}>
							{saving ? 'Recording…' : 'Record decision'}
						</button>
						<p class="muted">
							The records that did not win are kept. A slot records which one stands, it does not
							erase the others.
						</p>
					</form>
				{/if}
			</div>
		{/each}
	{/snippet}
</Await>

<div class="pager">
	<button class="ghost" disabled={offset === 0 || task.pending} onclick={() => (offset = Math.max(0, offset - PAGE))}>
		← Previous
	</button>
	<span class="muted">Slots {offset + 1}–{offset + slots.length}</span>
	<button class="ghost" disabled={slots.length < PAGE || task.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	.policies { margin: 1rem 0; }
	.policies summary { cursor: pointer; font-size: 0.9rem; font-weight: 600; margin-bottom: 0.6rem; }
	.note { max-width: var(--measure); margin: 0.5rem 0; font-size: 0.82rem; }
	.field.grow { flex: 1 1 14rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.6rem 0; font-size: 0.85rem; }

	.slot {
		margin-bottom: 1rem;
	}

	.head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 1rem;
		margin-bottom: 0.9rem;
	}

	.key {
		font-size: 0.95rem;
		overflow-wrap: anywhere;
	}

	.origin {
		margin: 0.3rem 0 0;
		font-size: 0.8rem;
	}

	.values {
		margin-bottom: 0.9rem;
		font-size: 0.84rem;
	}

	.expand {
		margin-top: 0.9rem;
	}

	.decide {
		margin-top: 1rem;
		border-top: 1px solid var(--rule);
		padding-top: 1rem;
	}

	.decide .field {
		margin-bottom: 0.8rem;
	}

	.decide .field.wide {
		max-width: var(--measure);
	}

	.decide textarea {
		width: 100%;
	}

	.decide p {
		font-size: 0.8rem;
		margin: 0.6rem 0 0;
		max-width: var(--measure);
	}

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
