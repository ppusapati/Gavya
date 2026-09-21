<script lang="ts">
	import { ApiError, type Cattle, type ListCattleResponse } from '$lib/api';
	import { label, quantity } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const PAGE = 50;

	let status = $state('');
	let offset = $state(0);
	const task = new Task<ListCattleResponse>();
	const breeds = new Task<{ breeds: { id: string; name: string }[] }>();

	function load() {
		task.run((signal) => settings.api().herd.listCattle({ status, limit: PAGE, offset }, { signal }));
	}

	$effect(() => {
		void [status, offset];
		load();
	});

	$effect(() => {
		breeds.run((signal) => settings.api().herd.listBreeds({ signal }));
	});

	const herd = $derived(task.data?.cattle ?? []);
	const breedName = $derived(
		new Map((breeds.data?.breeds ?? []).map((b) => [b.id, b.name]))
	);

	/* ---- adding an animal ---- */

	let adding = $state(false);
	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);
	let form = $state({ tag_number: '', name: '', breed_id: '', gender: 'F', weight: '' });

	function reset() {
		form = { tag_number: '', name: '', breed_id: '', gender: 'F', weight: '' };
		saveError = undefined;
	}

	async function add(event: SubmitEvent) {
		event.preventDefault();
		if (saving || !form.tag_number.trim()) return;
		saving = true;
		saveError = undefined;
		try {
			await settings.api().herd.createCattle({
				tag_number: form.tag_number.trim(),
				name: form.name.trim(),
				breed_id: form.breed_id,
				gender: form.gender,
				// Sent as typed. The service holds weight as an exact decimal and
				// parses this string itself; turning it into a Number here would
				// round it before the platform ever saw it.
				weight: form.weight.trim(),
				created_by: settings.actorOrUnknown
			});
			adding = false;
			reset();
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	/* ---- changing one ---- */

	let editing = $state<string | undefined>(undefined);
	let edit = $state({ status: '', weight: '' });

	function startEdit(c: Cattle) {
		editing = editing === c.id ? undefined : c.id;
		edit = { status: c.status, weight: c.weight };
		saveError = undefined;
	}

	async function save(event: SubmitEvent, c: Cattle) {
		event.preventDefault();
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await settings.api().herd.updateCattle({
				id: c.id,
				status: edit.status.trim(),
				weight: edit.weight.trim(),
				updated_by: settings.actorOrUnknown
			});
			editing = undefined;
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	async function retire(c: Cattle) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await settings.api().herd.deleteCattle({ id: c.id, deleted_by: settings.actorOrUnknown });
			editing = undefined;
			load();
		} catch (cause) {
			saveError = asApiError(cause);
		} finally {
			saving = false;
		}
	}

	function asApiError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}
</script>

<div class="page-head">
	<h1>The herd</h1>
	<p>
		Every animal this society keeps records for. Weight is held as an exact decimal and shown as the
		platform sent it — a kilogram figure a member can check is the whole point of recording one.
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
			<option value="active">Active</option>
			<option value="dry">Dry</option>
			<option value="sold">Sold</option>
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
	<button onclick={() => { adding = !adding; reset(); }}>
		{adding ? 'Cancel' : 'Add an animal'}
	</button>
</div>

{#if adding}
	<form class="panel" onsubmit={add}>
		<h2>Add an animal</h2>
		<ErrorNote error={saveError} />
		<div class="controls">
			<div class="field">
				<label for="tag">Tag number</label>
				<input id="tag" bind:value={form.tag_number} size="12" placeholder="VD-0007" />
			</div>
			<div class="field">
				<label for="nm">Name</label>
				<input id="nm" bind:value={form.name} size="16" />
			</div>
			<div class="field">
				<label for="br">Breed</label>
				<select id="br" bind:value={form.breed_id}>
					<option value="">—</option>
					{#each breeds.data?.breeds ?? [] as b (b.id)}<option value={b.id}>{b.name}</option>{/each}
				</select>
			</div>
			<div class="field">
				<label for="gd">Sex</label>
				<select id="gd" bind:value={form.gender}>
					<option value="F">Female</option>
					<option value="M">Male</option>
				</select>
			</div>
			<div class="field">
				<label for="wt">Weight (kg)</label>
				<input id="wt" bind:value={form.weight} size="8" inputmode="decimal" placeholder="412.50" />
			</div>
			<button type="submit" disabled={saving || !form.tag_number.trim()}>
				{saving ? 'Adding…' : 'Add'}
			</button>
		</div>
	</form>
{/if}

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.cattle ?? []).length === 0}
	empty="No animals match. A society with no herd recorded is one nobody has entered yet."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Tag</th>
						<th>Name</th>
						<th>Breed</th>
						<th>Sex</th>
						<th>Status</th>
						<th class="num">Weight</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each herd as c (c.id)}
						<tr>
							<td class="mono">{c.tag_number}</td>
							<td><a href="/herd/{c.id}">{c.name || '—'}</a></td>
							<td>{breedName.get(c.breed_id) ?? '—'}</td>
							<td>{c.gender === 'F' ? 'Female' : c.gender === 'M' ? 'Male' : c.gender}</td>
							<td>
								<Chip tone={c.status === 'active' ? 'calm' : 'neutral'}>{label(c.status)}</Chip>
							</td>
							<td class="num">{quantity(c.weight, 'kg')}</td>
							<td>
								<button class="ghost" onclick={() => startEdit(c)}>
									{editing === c.id ? 'Cancel' : 'Change'}
								</button>
							</td>
						</tr>
						{#if editing === c.id}
							<tr class="detail">
								<td colspan="7">
									<form onsubmit={(e) => save(e, c)}>
										<ErrorNote error={saveError} />
										<div class="controls">
											<div class="field">
												<label for="es-{c.id}">Status</label>
												<input id="es-{c.id}" bind:value={edit.status} size="10" />
											</div>
											<div class="field">
												<label for="ew-{c.id}">Weight (kg)</label>
												<input id="ew-{c.id}" bind:value={edit.weight} size="8" inputmode="decimal" />
											</div>
											<button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
											<button type="button" class="ghost" onclick={() => retire(c)} disabled={saving}>
												Retire
											</button>
										</div>
										<p class="muted note">
											Retiring marks the animal as gone rather than removing it. Its collections,
											treatments and calvings stay exactly where they are, because a settlement
											drawn from them has to stay explainable afterwards.
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

<div class="pager">
	<button class="ghost" disabled={offset === 0 || task.pending} onclick={() => (offset = Math.max(0, offset - PAGE))}>
		← Previous
	</button>
	<span class="muted">
		Animals {offset + 1}–{offset + herd.length}{#if task.data?.total}of {task.data.total}{/if}
	</span>
	<button class="ghost" disabled={herd.length < PAGE || task.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	.detail td {
		background: var(--surface-2);
	}

	.note {
		max-width: var(--measure);
		margin: 0.8rem 0 0;
		font-size: 0.82rem;
	}

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
