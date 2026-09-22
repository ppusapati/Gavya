<script lang="ts">
	import {
		ApiError,
		type AuditLog,
		type AuditLogResponse,
		type ListAuditLogsResponse,
		type SealAuditChainResponse,
		type VerifyAuditChainResponse
	} from '$lib/api';
	import { instant, label, shortId } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const logs = new Task<ListAuditLogsResponse>();
	const entry = new Task<AuditLogResponse>();
	const verified = new Task<VerifyAuditChainResponse>();
	const sealed = new Task<SealAuditChainResponse>();

	type Scope = 'all' | 'resource' | 'actor';
	let scope = $state<Scope>('all');
	let resourceType = $state('');
	let resourceId = $state('');
	let actorId = $state('');

	function load() {
		const api = settings.api().admin;
		if (scope === 'resource' && resourceType.trim() && resourceId.trim()) {
			logs.run((s) =>
				api.listAuditLogsByResource(resourceType.trim(), resourceId.trim(), { signal: s })
			);
			return;
		}
		if (scope === 'actor' && actorId.trim()) {
			logs.run((s) => api.listAuditLogsByActor(actorId.trim(), { signal: s }));
			return;
		}
		logs.run((s) => api.listAuditLogs({ signal: s }));
	}
	$effect(() => {
		void scope;
		load();
	});

	let saving = $state(false);
	let saveError = $state<ApiError | undefined>(undefined);

	function asApiError(c: unknown): ApiError {
		return c instanceof ApiError
			? c
			: new ApiError('unknown', String((c as Error)?.message ?? c), 0, '');
	}

	async function run(fn: () => Promise<unknown>, after: () => void = () => {}) {
		if (saving) return;
		saving = true;
		saveError = undefined;
		try {
			await fn();
			after();
		} catch (c) {
			saveError = asApiError(c);
		} finally {
			saving = false;
		}
	}

	let sealLimit = $state(1000);
	let open = $state<string | undefined>(undefined);

	function openEntry(l: AuditLog) {
		if (open === l.id) {
			open = undefined;
			entry.reset();
			return;
		}
		open = l.id;
		entry.run((s) => settings.api().admin.getAuditLog(l.id, { signal: s }));
	}

	let recording = $state(false);
	let note = $state({
		action: '',
		resource_type: '',
		resource_id: '',
		old_value: '',
		new_value: ''
	});

	function elapsed(seconds: number | undefined): string {
		if (!seconds || seconds <= 0) return '';
		const h = Math.floor(seconds / 3600);
		if (h >= 24) return `${Math.floor(h / 24)} day${Math.floor(h / 24) === 1 ? '' : 's'}`;
		if (h >= 1) return `${h} hour${h === 1 ? '' : 's'}`;
		return `${Math.floor(seconds / 60)} minutes`;
	}
</script>

<div class="page-head">
	<h1>Audit</h1>
	<p>
		What happened, who did it, and whether the record of it has been altered since. The chain is
		what makes the second question answerable: each sealed row carries the hash of the one before
		it, so a row edited in the database breaks every row after it.
	</p>
</div>

<ErrorNote error={saveError} />

<section>
	<h2>The chain</h2>
	<div class="controls">
		<button class="ghost" disabled={verified.pending} onclick={() => verified.run((s) => settings.api().admin.verifyAuditChain({ signal: s }))}>
			Verify
		</button>
		<div class="field"><label for="sl">Seal at most</label><input id="sl" type="number" min="1" bind:value={sealLimit} size="7" /></div>
		<button
			class="ghost"
			disabled={saving}
			onclick={() => run(() => sealed.run((s) => settings.api().admin.sealAuditChain(sealLimit, { signal: s })), () => verified.run((s) => settings.api().admin.verifyAuditChain({ signal: s })))}
		>
			Seal
		</button>
	</div>
	<p class="muted note">
		Sealing is bounded so a tenant with a long backlog is caught up over several passes rather than
		in one transaction holding the sealer's lock for minutes. Run it again until nothing is left.
	</p>

	{#if sealed.settled}
		<Await task={sealed} isEmpty={() => false} empty="">
			{#snippet children(s)}
				<p class="banner">
					<Chip tone={s.sealed > 0 ? 'calm' : 'neutral'}>{s.sealed} sealed</Chip>
					up to sequence {s.last_seq}.
					{#if s.anchor_taken}
						<Chip tone="calm">anchored at {s.anchored_at_seq}</Chip>
					{:else}
						<span class="muted">No anchor was taken on this pass.</span>
					{/if}
				</p>
			{/snippet}
		</Await>
	{/if}

	{#if verified.settled}
		<Await task={verified} isEmpty={() => false} empty="">
			{#snippet children(v)}
				<div class="panel">
					<p class="banner">
						{#if v.intact}
							<Chip tone="calm">sealed rows intact</Chip>
						{:else}
							<Chip tone="critical">chain broken</Chip>
						{/if}
						{v.rows_checked} row{v.rows_checked === 1 ? '' : 's'} checked.
					</p>

					{#if !v.intact}
						<p class="warn">
							Broken at sequence {v.broken_at_seq}
							{#if v.broken_id}<span class="mono"> ({v.broken_id})</span>{/if}.
							{v.detail}
						</p>
					{/if}

					<!--
						These travel with the verdict, never under it.

						"Intact" on its own invites the reading that everything is accounted
						for. What it means is that everything SEALED is accounted for — an
						unsealed row is outside the chain, and a row that was never sealed
						can be altered without breaking anything.
					-->
					<dl class="kv">
						<dt>Rows in total</dt><dd>{v.total_rows}</dd>
						<dt>Sealed</dt><dd>{v.sealed_rows}</dd>
						<dt>Not yet sealed</dt>
						<dd>
							{v.unsealed_rows}
							{#if v.unsealed_rows > 0}
								<Chip tone="attention">outside the chain</Chip>
							{/if}
						</dd>
						{#if v.oldest_unsealed_at}
							<dt>Oldest unsealed</dt>
							<dd>
								{instant(v.oldest_unsealed_at)}
								<span class="muted">{elapsed(v.unsealed_for_seconds)} ago</span>
							</dd>
						{/if}
					</dl>

					{#if v.unsealed_rows > 0}
						<p class="warn">
							{v.unsealed_rows} row{v.unsealed_rows === 1 ? ' is' : 's are'} outside the chain and
							the verdict above says nothing about {v.unsealed_rows === 1 ? 'it' : 'them'}. A row
							that has never been sealed can be altered without breaking anything.
						</p>
					{/if}
				</div>
			{/snippet}
		</Await>
	{/if}
</section>

<section>
	<h2>Entries</h2>
	<div class="controls">
		<div class="field">
			<label for="sc">Show</label>
			<select id="sc" bind:value={scope}>
				<option value="all">Everything</option>
				<option value="resource">One resource</option>
				<option value="actor">One actor</option>
			</select>
		</div>
		{#if scope === 'resource'}
			<div class="field"><label for="rt">Resource type</label><input id="rt" bind:value={resourceType} size="16" /></div>
			<div class="field"><label for="ri">Resource id</label><input id="ri" bind:value={resourceId} size="24" /></div>
		{/if}
		{#if scope === 'actor'}
			<div class="field"><label for="ai">Actor id</label><input id="ai" bind:value={actorId} size="24" /></div>
		{/if}
		<button class="ghost" onclick={load} disabled={logs.pending}>Look up</button>
		<button class="ghost" onclick={() => { recording = !recording; saveError = undefined; }}>
			{recording ? 'Cancel' : 'Record an entry'}
		</button>
	</div>

	{#if recording}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().admin.createAuditLog({
							actor_id: settings.actorOrUnknown,
							actor_type: 'user',
							action: note.action.trim(),
							resource_type: note.resource_type.trim(),
							resource_id: note.resource_id.trim(),
							old_value: note.old_value,
							new_value: note.new_value,
							// The browser cannot see its own address and must not invent
							// one; the service records what it was told and an invented
							// address is worse than a blank.
							ip_address: '',
							user_agent: typeof navigator === 'undefined' ? '' : navigator.userAgent,
							service_name: 'web-console',
							trace_id: '',
							created_by: settings.actorOrUnknown
						}),
					() => {
						recording = false;
						note = { action: '', resource_type: '', resource_id: '', old_value: '', new_value: '' };
						load();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field"><label for="na">Action</label><input id="na" bind:value={note.action} size="18" /></div>
				<div class="field"><label for="nrt">Resource type</label><input id="nrt" bind:value={note.resource_type} size="16" /></div>
				<div class="field"><label for="nri">Resource id</label><input id="nri" bind:value={note.resource_id} size="24" /></div>
			</div>
			<div class="controls">
				<div class="field grow"><label for="nov">Before</label><input id="nov" bind:value={note.old_value} /></div>
				<div class="field grow"><label for="nnv">After</label><input id="nnv" bind:value={note.new_value} /></div>
				<button type="submit" disabled={saving || !note.action.trim()}>Record</button>
			</div>
			<p class="muted note">
				Recorded against {settings.actorOrUnknown}, which is the signed-in user rather than a name
				somebody typed. The address field is left empty: a browser cannot see its own, and an
				invented one in an audit trail is worse than a blank.
			</p>
		</form>
	{/if}

	<Await task={logs} retry={load} isEmpty={(d) => (d.audit_logs ?? []).length === 0} empty="Nothing is recorded.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>When</th><th>Actor</th><th>Action</th><th>Resource</th><th>Service</th><th></th></tr>
					</thead>
					<tbody>
						{#each d.audit_logs as l (l.id)}
							<tr>
								<td>{instant(l.created_at)}</td>
								<td><span class="mono">{shortId(l.actor_id)}</span> <span class="muted">{l.actor_type}</span></td>
								<td>{label(l.action)}</td>
								<td>{l.resource_type} <span class="mono muted">{shortId(l.resource_id)}</span></td>
								<td class="muted">{l.service_name || '—'}</td>
								<td><button class="ghost" onclick={() => openEntry(l)}>{open === l.id ? 'Close' : 'Open'}</button></td>
							</tr>
							{#if open === l.id}
								<tr class="detail">
									<td colspan="6">
										<Await task={entry} isEmpty={(x) => !x.audit_log} empty="No such entry.">
											{#snippet children(x)}
												<dl class="kv">
													<dt>Entry</dt><dd class="mono">{x.audit_log.id}</dd>
													<dt>Actor</dt><dd class="mono">{x.audit_log.actor_id} ({x.audit_log.actor_type})</dd>
													<dt>Resource</dt><dd class="mono">{x.audit_log.resource_type} / {x.audit_log.resource_id}</dd>
													<dt>From</dt><dd class="mono pre">{x.audit_log.old_value || '—'}</dd>
													<dt>To</dt><dd class="mono pre">{x.audit_log.new_value || '—'}</dd>
													<dt>Address</dt><dd class="mono">{x.audit_log.ip_address || '—'}</dd>
													<dt>Agent</dt><dd class="muted">{x.audit_log.user_agent || '—'}</dd>
													<dt>Trace</dt><dd class="mono">{x.audit_log.trace_id || '—'}</dd>
												</dl>
											{/snippet}
										</Await>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/snippet}
	</Await>
</section>

<style>
	section { margin-top: 2.2rem; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.4rem 0 0.8rem; font-size: 0.85rem; }
	.warn { max-width: var(--measure); margin: 0.6rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
	.pre { white-space: pre-wrap; word-break: break-word; }
</style>
