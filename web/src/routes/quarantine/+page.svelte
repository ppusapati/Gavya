<script lang="ts">
	import {
		ApiError,
		type GetQuarantinedResponse,
		type ListDeviceSessionsResponse,
		type ListGenerationsResponse,
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

	/* ---- the device a held record names ---- */

	let deviceId = $state('');
	const generations = new Task<ListGenerationsResponse>();
	const sessions = new Task<ListDeviceSessionsResponse>();

	function loadDevice() {
		const id = deviceId.trim();
		if (!id) return;
		generations.run((s) => settings.api().listGenerations(id, { signal: s }));
		sessions.run((s) =>
			settings.api().listSessions({ device_id: id, limit: 50, offset: 0 }, { signal: s })
		);
	}

	let open = $state<string | undefined>(undefined);
	let why = $state('');
	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	/**
	 * The held record in full, payload included.
	 *
	 * The listing carries a hash of the payload and not the payload, which is
	 * right for a list and useless for the decision: what a reviewer is actually
	 * doing is comparing what the device sent against the record it collided
	 * with, and a hash does not let them.
	 */
	const full = new Task<GetQuarantinedResponse>();

	function expand(r: QuarantinedRecord) {
		if (open !== r.id) full.run((s) => settings.api().getQuarantined(r.id, { signal: s }));
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

									{#if open === r.id && full.settled}
										<Await task={full} isEmpty={(d) => !d.record} empty="This record could not be read in full.">
											{#snippet children(d)}
												<h3>What the device sent</h3>
												<pre class="payload">{JSON.stringify(d.payload, null, 2)}</pre>
											{/snippet}
										</Await>
									{/if}

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

<section>
	<h2>The devices behind these</h2>
	<p class="muted note">
		A held record names a device, a generation and a session. These are the three it names: a
		generation is a sequence space the device has since closed, and a session is one shift's worth
		of records within it. A record quarantined for a stale generation or an unknown session is
		asking a question about the rows below.
	</p>
	<div class="controls">
		<div class="field"><label for="dv">Device</label><input id="dv" bind:value={deviceId} size="24" /></div>
		<button class="ghost" disabled={!deviceId.trim() || generations.pending} onclick={loadDevice}>
			Look up
		</button>
	</div>

	{#if generations.settled}
		<Await task={generations} isEmpty={(d) => (d.generations ?? []).length === 0} empty="That device has no generations.">
			{#snippet children(d)}
				<h3>Generations</h3>
				<div class="tablewrap">
					<table>
						<thead><tr><th class="num">Generation</th><th>Opened</th><th>Closed</th><th>Why it rolled</th></tr></thead>
						<tbody>
							{#each d.generations as g (g.id)}
								<tr>
									<td class="num">{g.generation}</td>
									<td>{instant(g.opened_at)}</td>
									<td>
										{#if g.closed_at}
											{instant(g.closed_at)}
										{:else}
											<Chip tone="calm">open</Chip>
										{/if}
									</td>
									<td class="muted">{g.reason || '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/snippet}
		</Await>
	{/if}

	{#if sessions.settled}
		<Await task={sessions} isEmpty={(d) => (d.sessions ?? []).length === 0} empty="That device has no sessions.">
			{#snippet children(d)}
				<h3>Sessions</h3>
				<div class="tablewrap">
					<table>
						<thead>
							<tr><th>Session</th><th class="num">Gen</th><th>Operator</th><th>Status</th><th class="num">Last sequence</th><th class="num">Records</th><th>Opened</th></tr>
						</thead>
						<tbody>
							{#each d.sessions as sess (sess.id)}
								<tr>
									<td class="mono">{sess.external_session_id}</td>
									<td class="num">{sess.generation}</td>
									<td class="mono">{sess.operator_ref || '—'}</td>
									<td>
										<Chip tone={sess.closed_at ? 'neutral' : 'calm'}>{label(sess.status)}</Chip>
									</td>
									<td class="num">{sess.last_sequence}</td>
									<td class="num">{sess.record_count}</td>
									<td>{instant(sess.opened_at)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/snippet}
		</Await>
	{/if}
</section>

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
	section { margin-top: 2.2rem; }
	h3 { margin: 1.2rem 0 0.4rem; font-size: 0.85rem; font-weight: 600; }
	.payload {
		background: var(--surface-2);
		padding: 0.8rem;
		overflow-x: auto;
		font-family: var(--mono);
		font-size: 0.75rem;
		line-height: 1.4;
		max-height: 22rem;
	}

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
