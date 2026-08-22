<script lang="ts">
	import { page } from '$app/state';
	import { ApiError, type Flow, type ListFlowsResponse, type ReconciliationRun } from '$lib/api';
	import { instant, label, shortId } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const id = $derived(page.params.id ?? '');

	const flows = new Task<ListFlowsResponse>();
	const runs = new Task<{ runs: ReconciliationRun[] }>();

	function load() {
		flows.run((signal) => settings.api().listFlows(id, { signal }));
		runs.run((signal) => settings.api().listRuns({ window_id: id, limit: 20 }, { signal }));
	}

	$effect(() => {
		void id;
		load();
	});

	// Runs come back newest first, so the head of the list is what the window
	// currently stands at.
	const latest = $derived(runs.data?.runs?.[0]);
	const accepted = $derived(runs.data?.runs?.find((r) => !!r.accepted_at));

	function asError(cause: unknown): ApiError {
		return cause instanceof ApiError
			? cause
			: new ApiError('unknown', String((cause as Error)?.message ?? cause), 0, '');
	}

	/* ---- running a reconciliation ---- */

	let threshold = $state(0);
	let running = $state(false);
	let runError = $state<ApiError | undefined>(undefined);

	async function reconcile(event: SubmitEvent) {
		event.preventDefault();
		if (running) return;
		running = true;
		runError = undefined;
		try {
			await settings.api().reconcile({
				window_id: id,
				gross_error_threshold: threshold > 0 ? threshold : undefined,
				actor: settings.actorOrUnknown
			});
			load();
		} catch (cause) {
			runError = asError(cause);
		} finally {
			running = false;
		}
	}

	/* ---- accepting one ---- */

	let accepting = $state(false);
	let acceptError = $state<ApiError | undefined>(undefined);

	async function accept(run: ReconciliationRun) {
		if (accepting) return;
		accepting = true;
		acceptError = undefined;
		try {
			await settings.api().acceptRun({ run_id: run.id, actor: settings.actorOrUnknown });
			load();
		} catch (cause) {
			acceptError = asError(cause);
		} finally {
			accepting = false;
		}
	}

	/** The empty string is the system boundary: milk entering or leaving the network. */
	function node(ref: string, kind: string | undefined): string {
		if (!ref) return 'outside the network';
		return kind ? `${ref} (${label(kind)})` : ref;
	}

	const measuredFlows = $derived((flows.data?.flows ?? []).filter((f) => !f.unmeasured));
	const unmeasuredFlows = $derived((flows.data?.flows ?? []).filter((f) => f.unmeasured));

	function adjustmentOf(run: ReconciliationRun, flow: Flow) {
		return run.flows.find((r) => r.flow_id === flow.flow_id);
	}
</script>

<div class="page-head">
	<a class="rowlink" href="/balance">← Back to windows</a>
	<h1>Balance window</h1>
</div>

<Await task={flows} retry={load}>
	{#snippet children()}
		<div class="panel">
			<h2>Flows</h2>
			<p class="muted">
				What was measured moving through this route, and how well. A flow's uncertainty is what
				decides how much of any imbalance it is asked to absorb.
			</p>

			{#if (flows.data?.flows ?? []).length === 0}
				<p class="muted">No flow has been recorded for this window.</p>
			{:else}
				<div class="tablewrap">
					<table>
						<thead>
							<tr>
								<th>Flow</th>
								<th>From</th>
								<th>To</th>
								<th>Measured</th>
								<th>Uncertainty</th>
								{#if latest}<th>Reconciled</th><th>Adjustment</th>{/if}
							</tr>
						</thead>
						<tbody>
							{#each [...measuredFlows, ...unmeasuredFlows] as f (f.id)}
								{@const r = latest ? adjustmentOf(latest, f) : undefined}
								<tr class:suspect={r?.gross_error}>
									<td class="mono">
										{f.flow_id}
										{#if f.unmeasured}<Chip title="Nothing measured this leg; the reconciler infers it.">
												inferred
											</Chip>{/if}
									</td>
									<td>{node(f.from_node, f.from_node_kind)}</td>
									<td>{node(f.to_node, f.to_node_kind)}</td>
									<td class="num">{f.unmeasured ? '—' : f.measured}</td>
									<td class="num muted">{f.standard_uncertainty || '—'}</td>
									{#if latest}
										<td class="num">{r?.reconciled ?? '—'}</td>
										<td class="num">
											{r?.adjustment ?? '—'}
											{#if r?.gross_error}
												<Chip tone="critical" title="Its adjustment is larger than its uncertainty explains.">
													gross error
												</Chip>
											{/if}
										</td>
									{/if}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/snippet}
</Await>

<form class="panel" onsubmit={reconcile}>
	<h2>Reconcile</h2>
	<p class="muted">
		Distributes the window's imbalance across its flows in proportion to their uncertainty. Running
		it again does not replace the previous run — each is kept, and only one is ever accepted.
	</p>
	<ErrorNote error={runError} />
	<div class="controls">
		<div class="field">
			<label for="th">Gross error threshold</label>
			<input
				id="th"
				type="number"
				min="0"
				step="0.1"
				size="8"
				bind:value={threshold}
				title="The test statistic above which a flow is reported as carrying a gross error. Zero uses the reconciler's default."
			/>
		</div>
		<button type="submit" disabled={running}>{running ? 'Reconciling…' : 'Reconcile now'}</button>
	</div>
</form>

<Await task={runs} retry={load} isEmpty={(d) => (d.runs ?? []).length === 0}
	empty="This window has not been reconciled yet.">
	{#snippet children()}
		<ErrorNote error={acceptError} />
		{#each runs.data?.runs ?? [] as run (run.id)}
			<div class="panel run" class:settled={!!run.accepted_at}>
				<div class="head">
					<div>
						<h3 class="mono">{shortId(run.id)}</h3>
						<p class="muted when">{instant(run.created_at)}</p>
					</div>
					<div class="chips">
						{#if run.accepted_at}
							<Chip tone="calm">accepted</Chip>
						{:else if run.converged}
							<Chip>converged</Chip>
						{:else}
							<Chip tone="attention">did not converge</Chip>
						{/if}
						{#if run.model_version}
							<Chip advisory title="A model produced this distribution. The residual it started from is a measured fact; how it was spread is the model's answer.">
								{run.model_version}
							</Chip>
						{/if}
					</div>
				</div>

				<dl class="kv">
					<dt>Imbalance before</dt>
					<dd class="mono">{run.residual_before}</dd>
					<dt>After</dt>
					<dd class="mono">
						{#if run.residual_after}
							{run.residual_after}
						{:else}
							<span class="muted">
								no model answered — the imbalance stands at {run.residual_before}
							</span>
						{/if}
					</dd>
					{#if run.suspect_flow_ids?.length}
						<dt>Suspect flows</dt>
						<dd class="mono">{run.suspect_flow_ids.join(', ')}</dd>
					{/if}
					{#if run.reason}
						<dt>Note</dt>
						<dd>{run.reason}</dd>
					{/if}
					{#if run.accepted_at}
						<dt>Accepted</dt>
						<dd>{instant(run.accepted_at)} by {run.accepted_by || 'unattributed'}</dd>
					{/if}
				</dl>

				{#if !run.accepted_at && !accepted}
					<button disabled={accepting} onclick={() => accept(run)}>
						{accepting ? 'Accepting…' : 'Accept this reconciliation'}
					</button>
					<p class="muted caveat">
						Accepting makes these the figures the window stands on. The measured values are kept
						either way — an adjustment is recorded beside a flow, never written over it.
					</p>
				{:else if !run.accepted_at && accepted}
					<p class="muted caveat">
						Another run on this window has already been accepted.
					</p>
				{/if}
			</div>
		{/each}
	{/snippet}
</Await>

<style>
	.head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 1rem;
		margin-bottom: 0.9rem;
	}

	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
	}

	.when {
		margin: 0.25rem 0 0;
		font-size: 0.8rem;
	}

	.run.settled {
		border-left: 3px solid var(--accent);
	}

	.suspect {
		background: var(--critical-wash);
	}

	.caveat {
		font-size: 0.8rem;
		margin: 0.6rem 0 0;
		max-width: var(--measure);
	}

	.run button {
		margin-top: 0.9rem;
	}
</style>
