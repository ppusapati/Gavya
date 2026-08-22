<script lang="ts">
	import { ApiError, type CollectionSlot, type ListConflictsResponse } from '$lib/api';
	import { instant, label, shortId } from '$lib/display';
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
