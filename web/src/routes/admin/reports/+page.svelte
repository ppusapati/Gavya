<script lang="ts">
	import {
		ApiError,
		REPORT_WINDOWS,
		type ListReportKindsResponse,
		type ListReportsResponse,
		type ListSchedulesResponse,
		type Report,
		type ReportDownloadResponse,
		type ReportResponse
	} from '$lib/api';
	import { instant, label, today } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const reports = new Task<ListReportsResponse>();
	const schedules = new Task<ListSchedulesResponse>();
	const kinds = new Task<ListReportKindsResponse>();
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
	$effect(() => {
		kinds.run((s) => settings.api().admin.reportKinds({ signal: s }));
	});

	const catalogue = $derived(kinds.data?.kinds ?? []);
	const schedulable = $derived(catalogue.filter((k) => k.schedulable));

	/**
	 * A report still being worked on.
	 *
	 * The runner claims within seconds and most renders finish in one, so the
	 * page polls only while something is actually moving. A list that refreshed
	 * for ever would keep a tab busy all night for nothing.
	 */
	const working = $derived(
		(reports.data?.reports ?? []).some((r) => r.status === 'pending' || r.status === 'running')
	);

	$effect(() => {
		if (!working) return;
		const t = setInterval(load, 3000);
		return () => clearInterval(t);
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

	let requesting = $state(false);
	let req = $state({
		name: '',
		report_type: '',
		from: weekAgo(),
		to: today(),
		cycle_id: '',
		file_format: 'csv'
	});

	function weekAgo(): string {
		const d = new Date();
		d.setDate(d.getDate() - 7);
		return d.toISOString().slice(0, 10);
	}

	const chosen = $derived(catalogue.find((k) => k.name === req.report_type));
	const needsPeriod = $derived(!!chosen?.needs.includes('from'));
	const needsCycle = $derived(!!chosen?.needs.includes('cycle_id'));

	/** Only the keys the chosen type declares it needs. */
	function parametersFor(): string {
		const p: Record<string, string> = {};
		if (needsPeriod) {
			p.from = req.from;
			p.to = req.to;
		}
		if (needsCycle) p.cycle_id = req.cycle_id.trim();
		return JSON.stringify(p);
	}

	let addingSchedule = $state(false);
	let sched = $state({
		report_type: '',
		schedule: '0 7 * * *',
		window: 'yesterday',
		timezone: settings.timezone
	});

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
			case 'running':
				return 'attention';
			case 'pending':
				return 'neutral';
			default:
				return 'neutral';
		}
	}

	/**
	 * Hand the report to the browser.
	 *
	 * The bytes arrive base64 because that is what a JSON transport carries and
	 * how Go marshals a []byte. They are decoded into a Blob and offered as a
	 * download rather than shown: a report is a file somebody opens in a
	 * spreadsheet, and the filename comes from the service rather than from the
	 * report's name, which is free text and may hold a slash.
	 */
	let downloading = $state<string | undefined>(undefined);

	/**
	 * Copying a link.
	 *
	 * The clipboard needs a secure context and permission, and refuses in a
	 * plain-http development setup — so the link is printed on the page as
	 * well, and a failure to copy says so rather than appearing to work.
	 */
	let copied = $state<string | undefined>(undefined);

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = text;
			setTimeout(() => (copied = undefined), 2000);
		} catch {
			saveError = new ApiError(
				'unknown',
				'This browser would not let the page use the clipboard. The link is printed below; select it and copy it.',
				0,
				''
			);
		}
	}

	async function download(id: string) {
		if (downloading) return;
		downloading = id;
		saveError = undefined;
		try {
			const res = await settings.api().admin.reportContent(id);
			const raw = atob(res.content);
			const bytes = new Uint8Array(raw.length);
			for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);

			if (bytes.length !== res.bytes) {
				// The length is sent alongside so a truncated transfer is caught
				// here rather than becoming a short spreadsheet nobody queries.
				throw new ApiError(
					'unknown',
					`the report is ${res.bytes} bytes and ${bytes.length} arrived`,
					0,
					''
				);
			}

			const url = URL.createObjectURL(new Blob([bytes], { type: res.content_type }));
			const a = document.createElement('a');
			a.href = url;
			a.download = res.filename;
			a.click();
			URL.revokeObjectURL(url);
		} catch (c) {
			saveError = asApiError(c);
		} finally {
			downloading = undefined;
		}
	}
</script>

<div class="page-head">
	<h1>Reports</h1>
	<p>
		Ask for a report and it is produced within seconds. Write a schedule and it fires in the
		society's own timezone, asking for the period it names each time rather than the same fixed
		dates for ever.
	</p>
</div>

<ErrorNote error={saveError} />

<Await task={kinds} isEmpty={(d) => (d.kinds ?? []).length === 0} empty="This platform produces no reports.">
	{#snippet children(d)}
		<details class="catalogue">
			<summary>What this platform can produce ({d.kinds.length})</summary>
			<p class="muted note">
				Asked of the service rather than listed here, so the screen cannot offer a report the
				platform stopped producing or hide one it started. A type not on this list is refused
				when it is requested, with this list in the refusal.
			</p>
			<div class="tablewrap">
				<table>
					<thead><tr><th>Type</th><th>What it is</th><th>Needs</th><th>Schedulable</th></tr></thead>
					<tbody>
						{#each d.kinds as k (k.name)}
							<tr>
								<td class="mono">{k.name}</td>
								<td class="muted">{k.summary}</td>
								<td class="mono">{k.needs.join(', ') || '—'}</td>
								<td>
									{#if k.schedulable}
										<Chip tone="calm">Yes</Chip>
									{:else}
										<Chip tone="neutral" title="It names something a schedule has no way to supply.">
											One at a time
										</Chip>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</details>
	{/snippet}
</Await>

<div class="controls">
	<button class="ghost" onclick={load} disabled={reports.pending}>Refresh</button>
	<button onclick={() => { requesting = !requesting; saveError = undefined; }}>
		{requesting ? 'Cancel' : 'Ask for a report'}
	</button>
	{#if working}
		<span class="muted">Something is being produced; this list is refreshing.</span>
	{/if}
</div>

{#if requesting}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().admin.requestReport({
						name: req.name.trim() || req.report_type,
						report_type: req.report_type,
						parameters: parametersFor(),
						file_format: req.file_format,
						requested_by: settings.actorOrUnknown,
						created_by: settings.actorOrUnknown
					}),
				() => {
					requesting = false;
					req = { ...req, name: '', cycle_id: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field">
				<label for="rt">Type</label>
				<select id="rt" bind:value={req.report_type}>
					<option value="">—</option>
					{#each catalogue as k (k.name)}<option value={k.name}>{k.name}</option>{/each}
				</select>
			</div>
			<div class="field grow"><label for="rn">Name</label><input id="rn" bind:value={req.name} placeholder="optional" /></div>
			{#if needsPeriod}
				<div class="field"><label for="rf">From</label><input id="rf" type="date" bind:value={req.from} /></div>
				<div class="field"><label for="rto">To</label><input id="rto" type="date" bind:value={req.to} /></div>
			{/if}
			{#if needsCycle}
				<div class="field"><label for="rc">Cycle</label><input id="rc" bind:value={req.cycle_id} size="24" /></div>
			{/if}
			<button
				type="submit"
				disabled={saving || !req.report_type || (needsCycle && !req.cycle_id.trim())}
			>
				{saving ? 'Asking…' : 'Ask'}
			</button>
		</div>
		{#if chosen}
			<p class="muted note">{chosen.summary}</p>
		{/if}
		{#if needsPeriod}
			<p class="muted note">
				The period includes both days named: 1 to 15 September means the whole of the 15th.
			</p>
		{/if}
	</form>
{/if}

<Await task={reports} retry={load} isEmpty={(d) => (d.reports ?? []).length === 0} empty="No reports have been asked for.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>Name</th><th>Type</th><th>Asked</th><th>By</th><th class="num">Rows</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.reports as r (r.id)}
						<tr>
							<td>{r.name}</td>
							<td class="mono">{r.report_type}</td>
							<td>{instant(r.created_at)}</td>
							<td class="mono">{r.requested_by}</td>
							<td class="num">
								{#if r.row_count != null}
									{r.row_count}
									{#if r.truncated}
										<Chip tone="critical" title="The run stopped at its ceiling. The figures in this report are not the whole period.">
											stopped short
										</Chip>
									{/if}
								{:else}
									—
								{/if}
							</td>
							<td>
								<Chip tone={statusTone(r.status)} title={r.failure_reason ?? ''}>{label(r.status)}</Chip>
								{#if r.status === 'failed' && r.attempts > 1}
									<span class="muted">after {r.attempts} tries</span>
								{/if}
							</td>
							<td class="actions">
								{#if r.status === 'completed'}
									<button disabled={downloading === r.id} onclick={() => download(r.id)}>
										{downloading === r.id ? 'Fetching…' : 'Download'}
									</button>
								{/if}
								<button class="ghost" onclick={() => openReport(r)}>{open === r.id ? 'Close' : 'Open'}</button>
							</td>
						</tr>

						{#if r.status === 'failed' && r.failure_reason}
							<tr class="detail"><td colspan="7"><strong>Failed:</strong> {r.failure_reason}</td></tr>
						{/if}

						{#if open === r.id}
							<tr class="detail">
								<td colspan="7">
									<Await task={one} isEmpty={(x) => !x.report} empty="No such report.">
										{#snippet children(x)}
											<dl class="kv">
												<dt>Identifier</dt><dd class="mono">{x.report.id}</dd>
												<dt>Parameters</dt><dd class="mono pre">{x.report.parameters || '—'}</dd>
												<dt>Started</dt><dd>{x.report.started_at ? instant(x.report.started_at) : 'Not started.'}</dd>
												<dt>Finished</dt><dd>{x.report.completed_at ? instant(x.report.completed_at) : 'Not finished.'}</dd>
												<dt>Attempts</dt><dd>{x.report.attempts} of 3</dd>
												{#if x.report.truncated}
													<dt>Complete</dt>
													<dd>
														<Chip tone="critical">no</Chip>
														<span class="muted">
															The run stopped at its ceiling, so any total taken from this
															report is short by an amount the report cannot say.
														</span>
													</dd>
												{/if}
											</dl>

											<div class="controls">
												<button
													class="ghost"
													disabled={stored.pending}
													onclick={() => stored.run((s) => settings.api().admin.reportDownloadLink(r.id, { signal: s }))}
												>
													Get a link to send
												</button>
											</div>
											{#if stored.settled}
												<Await task={stored} isEmpty={(p) => !p.url} empty="No link.">
													{#snippet children(p)}
														<!--
															A real link now, so it is shown as one.

															It carries its own authority rather than a session, which is
															what lets a browser follow it and what lets somebody send it
															on. That is also why the warning below is not decoration: a
															link is a bearer credential, and anybody who has it can
															fetch this report until it expires.
														-->
														<p class="banner">
															<a href={p.url} rel="noreferrer">Open the report</a>
															<button class="linklike" onclick={() => copy(p.url)}>
																{copied === p.url ? 'Copied' : 'Copy the link'}
															</button>
														</p>
														<p class="mono link">{p.url}</p>
														<p class="warn">
															Anybody holding this link can read the report until it
															expires — it needs no password and it is not tied to whoever
															you send it to. It appears in browser history and in the logs
															of anything it passes through.
														</p>
													{/snippet}
												</Await>
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
	<p class="muted note">
		A schedule fires in its own timezone, and asks for the period its window names at the moment it
		fires. That is why the window is a keyword rather than two dates: fixed dates would produce the
		same report for ever, which is a scheduled report that is wrong in a way nobody notices until
		they compare two of them.
	</p>
	<div class="controls">
		<button class="ghost" onclick={loadSchedules} disabled={schedules.pending}>Refresh</button>
		<button class="ghost" onclick={() => { addingSchedule = !addingSchedule; saveError = undefined; }}>
			{addingSchedule ? 'Cancel' : 'Write one'}
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
							report_type: sched.report_type,
							schedule: sched.schedule.trim(),
							parameters: JSON.stringify({ window: sched.window }),
							timezone: sched.timezone.trim(),
							created_by: settings.actorOrUnknown
						}),
					() => {
						addingSchedule = false;
						loadSchedules();
					}
				);
			}}
		>
			<div class="controls">
				<div class="field">
					<label for="st">Type</label>
					<select id="st" bind:value={sched.report_type}>
						<option value="">—</option>
						{#each schedulable as k (k.name)}<option value={k.name}>{k.name}</option>{/each}
					</select>
				</div>
				<div class="field"><label for="ss">When</label><input id="ss" bind:value={sched.schedule} size="14" placeholder="0 7 * * *" /></div>
				<div class="field">
					<label for="sw">Covering</label>
					<select id="sw" bind:value={sched.window}>
						{#each REPORT_WINDOWS as w (w)}<option value={w}>{label(w)}</option>{/each}
					</select>
				</div>
				<div class="field"><label for="sz">Timezone</label><input id="sz" bind:value={sched.timezone} size="18" /></div>
				<button type="submit" disabled={saving || !sched.report_type || !sched.timezone.trim()}>
					Save
				</button>
			</div>
			<p class="muted note">
				Five cron fields: minute, hour, day of month, month, day of week. Both day fields
				restricted means either — <span class="mono">0 0 1 * 1</span> fires on the first of the
				month and on every Monday, which is how cron has always read it and not how it looks.
			</p>
			{#if settings.zoneSource === 'browser'}
				<p class="warn">
					That timezone came from this browser, not from the tenant's record. Seven in the
					morning is seven where the society is; set it to the society's own zone before
					saving, or a report meant for before milking arrives after it.
				</p>
			{/if}
			{#if schedulable.length < catalogue.length}
				<p class="muted note">
					Only the types a schedule can supply are offered. A settlement summary names a cycle,
					and a schedule firing at two in the morning has no way to know which one is meant.
				</p>
			{/if}
		</form>
	{/if}

	<Await task={schedules} retry={loadSchedules} isEmpty={(d) => (d.schedules ?? []).length === 0} empty="No schedules.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>Type</th><th>When</th><th>Zone</th><th>Last run</th><th>Next run</th><th>Active</th><th></th></tr>
					</thead>
					<tbody>
						{#each d.schedules as s (s.id)}
							<tr>
								<td class="mono">{s.report_type}</td>
								<td class="mono">{s.schedule}</td>
								<td class="mono">{s.timezone}</td>
								<td>{s.last_run_at ? instant(s.last_run_at) : 'Never.'}</td>
								<td>
									{#if s.next_run_at}
										{instant(s.next_run_at)}
									{:else}
										<Chip tone="critical">none</Chip>
									{/if}
								</td>
								<td><Chip tone={s.is_active ? 'calm' : 'neutral'}>{s.is_active ? 'Yes' : 'No'}</Chip></td>
								<td class="actions">
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.updateSchedule(s.id, !s.is_active, settings.actorOrUnknown), loadSchedules)}>
										{s.is_active ? 'Deactivate' : 'Activate'}
									</button>
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.deleteSchedule(s.id, settings.actorOrUnknown), loadSchedules)}>
										Delete
									</button>
								</td>
							</tr>
							{#if s.last_error}
								<tr class="detail">
									<td colspan="7">
										<strong>Last firing:</strong> {s.last_error}
										{#if !s.is_active}
											<span class="muted">
												It has been deactivated because it has no next firing. Correct it and
												activate it again.
											</span>
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
</section>

<style>
	section { margin-top: 2.2rem; }
	.catalogue { margin: 1rem 0; }
	.catalogue summary { cursor: pointer; font-size: 0.9rem; font-weight: 600; margin-bottom: 0.6rem; }
	.detail td { background: var(--surface-2); }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.6rem 0; font-size: 0.85rem; }
	.warn { max-width: var(--measure); margin: 0.6rem 0 0; font-size: 0.82rem; color: var(--bad, #b3261e); }
	.pre { white-space: pre-wrap; word-break: break-word; }
	.link { word-break: break-all; font-size: 0.75rem; max-width: var(--measure); }
	.linklike {
		background: none;
		border: 0;
		padding: 0;
		color: inherit;
		font: inherit;
		cursor: pointer;
		text-decoration: underline;
	}
</style>
