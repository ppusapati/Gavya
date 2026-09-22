<script lang="ts">
	import {
		ApiError,
		type ListReportsResponse,
		type ListSchedulesResponse,
		type Report,
		type ReportDownloadResponse,
		type ReportResponse
	} from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const reports = new Task<ListReportsResponse>();
	const schedules = new Task<ListSchedulesResponse>();
	const one = new Task<ReportResponse>();
	const stored = new Task<ReportDownloadResponse>();

	function load() {
		reports.run((s) => settings.api().admin.listReports({ signal: s }));
	}
	function loadSchedules() {
		schedules.run((s) => settings.api().admin.listSchedules({ signal: s }));
	}
	$effect(load);
	$effect(loadSchedules);

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

	let requesting = $state(false);
	let req = $state({ name: '', report_type: '', parameters: '{}', file_format: 'csv' });

	let addingSchedule = $state(false);
	let sched = $state({ report_type: '', schedule: '', parameters: '{}' });

	let open = $state<string | undefined>(undefined);

	function openReport(r: Report) {
		if (open === r.id) {
			open = undefined;
			one.reset();
			stored.reset();
			return;
		}
		open = r.id;
		one.run((s) => settings.api().admin.getReport(r.id, { signal: s }));
		stored.reset();
	}

	function statusTone(s: string) {
		switch (s) {
			case 'completed':
				return 'calm';
			case 'failed':
				return 'critical';
			case 'pending':
				return 'attention';
			default:
				return 'neutral';
		}
	}

	function validJSON(s: string): boolean {
		if (s.trim() === '') return true;
		try {
			JSON.parse(s);
			return true;
		} catch {
			return false;
		}
	}
</script>

<div class="page-head">
	<h1>Reports</h1>
	<p>
		Requests for reports, and the schedules somebody wrote down. Read the warning below before using
		either: this part of the platform records intentions and does not act on them.
	</p>
</div>

<!--
	The most important thing on this screen is what it cannot do.

	reporting-service writes a row with status "pending" and there is nothing in
	this platform that picks one up: no worker, no queue consumer, no runner.
	ReportSchedule has a next_run_at column that nothing computes and nothing
	fires on. A screen with a "Generate" button and a spinner would be a control
	that reports success while doing nothing, which is the most expensive kind of
	lie a console can tell — somebody waits for a report that is never coming, and
	the platform never says so.
-->
<p class="warning">
	<Chip tone="critical">nothing runs these</Chip>
	Requesting a report records a request. No worker in this platform picks one up, so a report stays
	<span class="mono">pending</span> until something outside it writes a file and updates the row. A
	schedule is a row too: nothing computes its next run and nothing fires on it.
</p>

<ErrorNote error={saveError} />

<div class="controls">
	<button class="ghost" onclick={load} disabled={reports.pending}>Refresh</button>
	<button onclick={() => { requesting = !requesting; saveError = undefined; }}>
		{requesting ? 'Cancel' : 'Record a request'}
	</button>
</div>

{#if requesting}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().admin.requestReport({
						name: req.name.trim(),
						report_type: req.report_type.trim(),
						parameters: req.parameters.trim() || '{}',
						file_format: req.file_format,
						requested_by: settings.actorOrUnknown,
						created_by: settings.actorOrUnknown
					}),
				() => {
					requesting = false;
					req = { ...req, name: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field grow"><label for="rn">Name</label><input id="rn" bind:value={req.name} /></div>
			<div class="field"><label for="rt">Type</label><input id="rt" bind:value={req.report_type} size="20" /></div>
			<div class="field">
				<label for="rf">Format</label>
				<select id="rf" bind:value={req.file_format}>
					<option value="csv">CSV</option>
					<option value="xlsx">XLSX</option>
					<option value="pdf">PDF</option>
					<option value="json">JSON</option>
				</select>
			</div>
		</div>
		<div class="field grow">
			<label for="rp">Parameters</label>
			<textarea id="rp" bind:value={req.parameters} rows="3"></textarea>
		</div>
		{#if !validJSON(req.parameters)}
			<p class="warn">
				That is not valid JSON. The service stores this string without reading it, so anything
				here will be saved — and whatever eventually runs the report will have to parse it.
			</p>
		{/if}
		<div class="controls">
			<button type="submit" disabled={saving || !req.name.trim() || !req.report_type.trim()}>
				Record the request
			</button>
		</div>
	</form>
{/if}

<Await task={reports} retry={load} isEmpty={(d) => (d.reports ?? []).length === 0} empty="No reports have been requested.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Name</th><th>Type</th><th>Format</th><th>Requested</th><th>By</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.reports as r (r.id)}
						<tr>
							<td>{r.name}</td>
							<td class="mono">{r.report_type}</td>
							<td>{r.file_format}</td>
							<td>{instant(r.created_at)}</td>
							<td class="mono">{r.requested_by}</td>
							<td>
								<Chip tone={statusTone(r.status)}>{label(r.status)}</Chip>
								{#if r.status === 'pending'}
									<Chip tone="neutral" title="No worker in this platform picks up a pending report.">
										waiting on nothing
									</Chip>
								{/if}
							</td>
							<td><button class="ghost" onclick={() => openReport(r)}>{open === r.id ? 'Close' : 'Open'}</button></td>
						</tr>
						{#if open === r.id}
							<tr class="detail">
								<td colspan="7">
									<Await task={one} isEmpty={(x) => !x.report} empty="No such report.">
										{#snippet children(x)}
											<dl class="kv">
												<dt>Identifier</dt><dd class="mono">{x.report.id}</dd>
												<dt>Parameters</dt><dd class="mono pre">{x.report.parameters || '—'}</dd>
												<dt>Started</dt><dd>{x.report.started_at ? instant(x.report.started_at) : 'Never started.'}</dd>
												<dt>Finished</dt><dd>{x.report.completed_at ? instant(x.report.completed_at) : 'Never finished.'}</dd>
												<dt>File</dt><dd class="mono">{x.report.file_path || 'No file has been written.'}</dd>
											</dl>
											{#if x.report.file_path}
												<div class="controls">
													<button
														class="ghost"
														disabled={stored.pending}
														onclick={() => stored.run((s) => settings.api().admin.reportDownloadPath(r.id, { signal: s }))}
													>
														Ask where it is stored
													</button>
												</div>
												{#if stored.settled}
													<Await task={stored} isEmpty={(p) => !p.url} empty="No path.">
														{#snippet children(p)}
															<!--
																Shown as text, never as a link.

																The procedure is GetReportDownloadURL and it returns the
																report's file_path verbatim — nothing signs it and nothing
																resolves it. An anchor here would produce a broken link and
																imply the platform had granted access to something.
															-->
															<p class="banner">
																<Chip tone="neutral">stored at</Chip>
																<span class="mono">{p.url}</span>
															</p>
															<p class="muted note">
																A path inside whatever stores it, not a link. Nothing signs it
																and this console cannot fetch it.
															</p>
														{/snippet}
													</Await>
												{/if}
											{/if}
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

<section>
	<h2>Schedules</h2>
	<div class="controls">
		<button class="ghost" onclick={loadSchedules} disabled={schedules.pending}>Refresh</button>
		<button class="ghost" onclick={() => { addingSchedule = !addingSchedule; saveError = undefined; }}>
			{addingSchedule ? 'Cancel' : 'Write one down'}
		</button>
	</div>

	{#if addingSchedule}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().admin.createSchedule({
							report_type: sched.report_type.trim(),
							schedule: sched.schedule.trim(),
							parameters: sched.parameters.trim() || '{}',
							created_by: settings.actorOrUnknown
						}),
					() => {
						addingSchedule = false;
						sched = { report_type: '', schedule: '', parameters: '{}' };
						loadSchedules();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field"><label for="st">Type</label><input id="st" bind:value={sched.report_type} size="20" /></div>
				<div class="field"><label for="ss">Schedule</label><input id="ss" bind:value={sched.schedule} size="18" placeholder="0 2 * * 1" /></div>
				<div class="field grow"><label for="sp">Parameters</label><input id="sp" bind:value={sched.parameters} /></div>
				<button type="submit" disabled={saving || !sched.report_type.trim() || !sched.schedule.trim()}>Save</button>
			</div>
			<p class="muted note">
				Stored as written. Nothing in this platform parses the schedule, computes a next run, or
				fires on one — this records what somebody intends, ready for whatever eventually does it.
			</p>
		</form>
	{/if}

	<Await task={schedules} retry={loadSchedules} isEmpty={(d) => (d.schedules ?? []).length === 0} empty="No schedules.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>Type</th><th>Schedule</th><th>Last run</th><th>Next run</th><th>Active</th><th></th></tr>
					</thead>
					<tbody>
						{#each d.schedules as s (s.id)}
							<tr>
								<td class="mono">{s.report_type}</td>
								<td class="mono">{s.schedule}</td>
								<td>{s.last_run_at ? instant(s.last_run_at) : 'Never.'}</td>
								<td>
									{#if s.next_run_at}
										{instant(s.next_run_at)}
									{:else}
										<span class="muted">Not computed.</span>
									{/if}
								</td>
								<td><Chip tone={s.is_active ? 'neutral' : 'neutral'}>{s.is_active ? 'Yes' : 'No'}</Chip></td>
								<td class="actions">
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.updateSchedule(s.id, !s.is_active, settings.actorOrUnknown), loadSchedules)}>
										{s.is_active ? 'Deactivate' : 'Activate'}
									</button>
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.deleteSchedule(s.id, settings.actorOrUnknown), loadSchedules)}>
										Delete
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="muted note">
				"Active" is a flag on a row. Nothing reads it, so an active schedule and an inactive one
				do the same thing — which is nothing.
			</p>
		{/snippet}
	</Await>
</section>

<style>
	section { margin-top: 2.2rem; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.6rem 0; font-size: 0.85rem; }
	.warn { max-width: var(--measure); margin: 0.6rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
	.warning {
		display: flex;
		gap: 0.6rem;
		align-items: baseline;
		flex-wrap: wrap;
		max-width: var(--measure);
		margin: 1rem 0;
		padding: 0.8rem 1rem;
		font-size: 0.85rem;
		background: color-mix(in srgb, var(--bad, #b3261e) 8%, transparent);
		border-left: 3px solid var(--bad, #b3261e);
	}
	.pre { white-space: pre-wrap; word-break: break-word; }
	textarea { width: 100%; max-width: var(--measure); font: inherit; }
</style>
