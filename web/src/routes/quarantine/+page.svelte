<script lang="ts">
	import {
		ApiError,
		type ListQuarantinedResponse,
		type QuarantinedRecord,
		type QuarantineReason
	} from '$lib/api';
	import { QUARANTINE_MEANING, instant, label, shortId } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const REASONS: QuarantineReason[] = [
		'TRANSPORT_IDENTITY_CONFLICT',
		'SEQUENCE_REGRESSION',
		'UNTRUSTED_SESSION_IDENTITY',
		'STALE_GENERATION',
		'SESSION_NOT_ACCEPTING'
	];

	const PAGE = 50;

	let reason = $state('');
	let offset = $state(0);
	const task = new Task<ListQuarantinedResponse>();

	function load() {
		task.run((signal) => settings.api().listQuarantined({ reason, limit: PAGE, offset }, { signal }));
	}

	$effect(() => {
		void [reason, offset];
		load();
	});

	const records = $derived(task.data?.records ?? []);

	let open = $state<string | undefined>(undefined);
	let why = $state('');
	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function expand(r: QuarantinedRecord) {
		open = open === r.id ? undefined : r.id;
		why = '';
		saveError = undefined;
	}

	async function resolve(event: SubmitEvent, r: QuarantinedRecord) {
		event.preventDefault();
		if (!why.trim() || saving) return;
		saving = true;
		saveError = undefined;
		try {
			await settings.api().resolveQuarantine({ id: r.id, resolution: why.trim(), actor: settings.actorOrUnknown });
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
	<h1>Quarantine</h1>
	<p>
		A record whose transport identity cannot be trusted is never admitted and never dropped. It is
		held here with the reason it could not be admitted, so a person can work out what the device
		actually did before anything is counted as a collection.
	</p>
</div>

<div class="controls">
	<div class="field">
		<label for="rz">Reason</label>
		<select
			id="rz"
			value={reason}
			onchange={(e) => {
				reason = e.currentTarget.value;
				offset = 0;
			}}
		>
			<option value="">Any</option>
			{#each REASONS as r (r)}<option value={r}>{label(r)}</option>{/each}
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={task.pending}>Refresh</button>
</div>

<Await
	{task}
	retry={load}
	isEmpty={(d) => (d.records ?? []).length === 0}
	empty="Nothing is in quarantine. Every delivered record was admitted or recognised as a replay."
>
	{#snippet children()}
		<div class="tablewrap">
			<table>
				<thead>
					<tr>
						<th>Reason</th>
						<th>Device</th>
						<th>Gen</th>
						<th>Session</th>
						<th>Seq</th>
						<th>Captured</th>
						<th>Received</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each records as r (r.id)}
						<tr class:done={r.resolved}>
							<td>
								<Chip tone={r.resolved ? 'calm' : 'attention'} title={QUARANTINE_MEANING[r.reason] ?? ''}>
									{label(r.reason)}
								</Chip>
							</td>
							<td class="mono" title={r.device_id}>{shortId(r.device_id)}</td>
							<td class="num">{r.generation}</td>
							<td class="mono">{r.external_session_id}</td>
							<td class="num">{r.sequence}</td>
							<td>{instant(r.captured_at)}</td>
							<td>{instant(r.received_at)}</td>
							<td>
								{#if r.resolved}
									<span class="muted">{r.resolved_by || 'unattributed'}</span>
								{:else}
									<button class="ghost" onclick={() => expand(r)}>
										{open === r.id ? 'Cancel' : 'Resolve'}
									</button>
								{/if}
							</td>
						</tr>
						{#if open === r.id || r.resolved}
							<tr class="detail">
								<td colspan="8">
									<p class="meaning">{QUARANTINE_MEANING[r.reason] ?? r.detail}</p>
									<dl class="kv">
										<dt>Detail</dt>
										<dd>{r.detail}</dd>
										<dt>Payload hash</dt>
										<dd class="mono hash">{r.payload_hash}</dd>
										{#if r.conflicting_record_id}
											<dt>Collided with</dt>
											<dd class="mono">{r.conflicting_record_id}</dd>
										{/if}
										{#if r.resolved}
											<dt>Resolution</dt>
											<dd>{r.resolution} — {instant(r.resolved_at)}</dd>
										{/if}
									</dl>

									{#if !r.resolved}
										<form onsubmit={(e) => resolve(e, r)}>
											<ErrorNote error={saveError} />
											<div class="field wide">
												<label for="q-{r.id}">What you established</label>
												<textarea
													id="q-{r.id}"
													rows="2"
													bind:value={why}
													placeholder="What the device actually did, and what should stand as a result."
												></textarea>
											</div>
											<button type="submit" disabled={!why.trim() || saving}>
												{saving ? 'Recording…' : 'Record'}
											</button>
											<span class="muted note">
												The held record stays exactly as delivered. Recording this notes what was
												decided about it.
											</span>
										</form>
									{/if}
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
	<span class="muted">Records {offset + 1}–{offset + records.length}</span>
	<button class="ghost" disabled={records.length < PAGE || task.pending} onclick={() => (offset += PAGE)}>
		Next →
	</button>
</div>

<style>
	.done {
		opacity: 0.6;
	}

	.detail td {
		background: var(--surface-2);
	}

	.meaning {
		max-width: var(--measure);
		margin: 0 0 0.8rem;
	}

	.hash {
		font-size: 0.74rem;
		overflow-wrap: anywhere;
	}

	.field.wide {
		max-width: var(--measure);
		margin: 0.9rem 0 0.6rem;
	}

	.field.wide textarea {
		width: 100%;
	}

	.note {
		font-size: 0.8rem;
		margin-left: 0.7rem;
	}

	.pager {
		display: flex;
		align-items: center;
		gap: 1rem;
		margin-top: 1rem;
		font-size: 0.82rem;
	}
</style>
