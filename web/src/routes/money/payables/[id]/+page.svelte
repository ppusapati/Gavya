<script lang="ts">
	import { page } from '$app/state';
	import { type Explanation } from '$lib/api';
	import { instant, label } from '$lib/display';
	import { formatExact } from '$lib/money';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';

	const id = $derived(page.params.id ?? '');
	const task = new Task<Explanation>();

	function load() {
		task.run((signal) => settings.api().money.explainPayable(id, { signal }));
	}

	$effect(() => {
		void id;
		if (id) load();
	});

	let openLine = $state<string | undefined>(undefined);
</script>

<div class="page-head">
	<h1>Why this payment</h1>
	<p>
		One call from a payable to everything that caused it: the collections gathered into it, the rate
		card each was priced against, and the identity mapping that attributed each to the producer —
		retired mappings included. This used to take four services and a psql session.
	</p>
</div>

<Await {task} retry={load} isEmpty={(d) => !d.payable} empty="No such payable.">
	{#snippet children(x)}
		<div class="panel">
			<dl class="kv">
				<dt>Producer</dt><dd class="mono">{x.payable.producer_ref}</dd>
				<dt>Cycle</dt>
				<dd><a href="/money/cycles/{x.cycle.id}">{x.cycle.name}</a> — {instant(x.cycle.period_start)} to {instant(x.cycle.period_end)}</dd>
				<dt>Gross</dt><dd>{formatExact(x.payable.gross, x.payable.currency)}</dd>
				<dt>Deducted</dt><dd>{formatExact(x.payable.deducted, x.payable.currency)}</dd>
				<dt>Net</dt><dd><strong>{formatExact(x.payable.net, x.payable.currency)}</strong></dd>
				<dt>Status</dt><dd><Chip tone={x.payable.status === 'PAID' ? 'calm' : 'neutral'}>{label(x.payable.status)}</Chip></dd>
				{#if x.payable.payment_reference}
					<dt>Reference</dt><dd class="mono">{x.payable.payment_reference}</dd>
				{/if}
				{#if x.payable.held_reason}
					<dt>Held because</dt><dd>{x.payable.held_reason}</dd>
				{/if}
			</dl>
		</div>

		<!--
			What the explanation could not reach, first.

			An explanation that quietly left a service out would be worse than none:
			it would look complete. So this is above the figures rather than below
			them, and it is shown even when everything answered.
		-->
		<section>
			<h2>What was consulted</h2>
			<ul class="consulted">
				<li>
					<Chip tone={x.consulted.procurement ? 'calm' : 'critical'}>
						{x.consulted.procurement ? 'procurement answered' : 'procurement did not answer'}
					</Chip>
					{#if x.consulted.procurement_why_not}<span class="muted">{x.consulted.procurement_why_not}</span>{/if}
				</li>
				<li>
					<Chip tone={x.consulted.canonical ? 'calm' : 'critical'}>
						{x.consulted.canonical ? 'canonical answered' : 'canonical did not answer'}
					</Chip>
					{#if x.consulted.canonical_why_not}<span class="muted">{x.consulted.canonical_why_not}</span>{/if}
				</li>
			</ul>
		</section>

		{#if x.findings.length}
			<section>
				<h2>What it found</h2>
				<ul class="findings">
					{#each x.findings as f, i (i)}
						<li><Chip tone="attention">note</Chip> {f}</li>
					{/each}
				</ul>
			</section>
		{/if}

		<section>
			<h2>The collections it was paid on</h2>
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>Day</th><th>Shift</th><th class="num">Quantity</th><th class="num">Rate</th><th class="num">Amount</th><th></th></tr>
					</thead>
					<tbody>
						{#each x.lines as l (l.paid.collection_id)}
							<tr class:mismatch={!l.paid_matches_current}>
								<td>{instant(l.paid.collected_on)}</td>
								<td>{label(l.paid.shift)}</td>
								<td class="num">{l.paid.quantity} {l.paid.quantity_unit}</td>
								<td class="num">{l.paid.rate ?? '—'}</td>
								<td class="num">{formatExact(l.paid.amount, x.payable.currency)}</td>
								<td>
									{#if !l.paid_matches_current}
										<Chip tone="critical" title="The figure paid is not the figure the platform now holds.">
											corrected since
										</Chip>
									{/if}
									{#if (l.versions ?? []).length > 1}
										<button class="ghost" onclick={() => (openLine = openLine === l.paid.collection_id ? undefined : l.paid.collection_id)}>
											{openLine === l.paid.collection_id ? 'Hide' : `${l.versions?.length} versions`}
										</button>
									{/if}
								</td>
							</tr>
							{#if openLine === l.paid.collection_id}
								<tr class="detail">
									<td colspan="6">
										<p class="muted">
											Every version of this collection. A correction supersedes rather than edits, so
											what was paid on stays readable after somebody changes it.
										</p>
										<div class="tablewrap">
											<table>
												<thead><tr><th>Recorded</th><th class="num">Quantity</th><th>Fat</th><th>SNF</th><th class="num">Amount</th><th>Why</th></tr></thead>
												<tbody>
													{#each l.versions ?? [] as v (v.id)}
														<tr class:superseded={!!v.superseded_at}>
															<td>{instant(v.created_at)}</td>
															<td class="num">{v.quantity} {v.quantity_unit}</td>
															<td>{v.fat ?? '—'}</td>
															<td>{v.snf ?? '—'}</td>
															<td class="num">{formatExact(v.amount, x.payable.currency)}</td>
															<td class="muted">{v.correction_reason ?? v.explanation}</td>
														</tr>
													{/each}
												</tbody>
											</table>
										</div>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		</section>

		{#if x.deductions.length}
			<section>
				<h2>What was taken off</h2>
				<div class="tablewrap">
					<table>
						<thead><tr><th>Kind</th><th>Reference</th><th class="num">Amount</th></tr></thead>
						<tbody>
							{#each x.deductions as d, i (i)}
								<tr>
									<td>{label(d.kind)}</td>
									<td class="mono">{d.reference ?? '—'}</td>
									<td class="num">{formatExact(d.amount, x.payable.currency)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{/if}

		<section>
			<h2>What priced it</h2>
			<div class="tablewrap">
				<table>
					<thead><tr><th>Card</th><th>Kind</th><th>Basis</th><th>In force</th><th class="num">Lines</th></tr></thead>
					<tbody>
						{#each x.rate_cards as rc (rc.id)}
							<tr>
								<td><a href="/money/rate-cards">{rc.name ?? rc.id}</a></td>
								<td>{rc.kind ? label(rc.kind) : '—'}</td>
								<td>{rc.basis ? label(rc.basis) : '—'}</td>
								<td>{rc.valid_from ? instant(rc.valid_from) : '—'}{rc.valid_to ? ` — ${instant(rc.valid_to)}` : ''}</td>
								<td class="num">
									{rc.lines_priced}
									{#if rc.unreadable}<Chip tone="critical" title={rc.unreadable}>unreadable</Chip>{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>

		{#if x.identities.length}
			<section>
				<h2>Who these collections were attributed to</h2>
				<p class="muted note">
					A mapping is never edited. It is closed and a new one opened, so a settlement from
					January stays explainable after somebody corrects who AMCU-0417 is.
				</p>
				{#each x.identities as t (t.source_system_id + t.external_id)}
					<h3><span class="mono">{t.external_id}</span> from <span class="mono">{t.source_system_id}</span> — {t.lines} line{t.lines === 1 ? '' : 's'}</h3>
					<div class="tablewrap">
						<table>
							<thead><tr><th>Mapped to</th><th>How</th><th>From</th><th>To</th><th>Recorded</th><th></th></tr></thead>
							<tbody>
								{#each t.mappings as m (m.id)}
									<tr class:superseded={!!m.superseded_at}>
										<td class="mono">{m.entity_id}</td>
										<td>{label(m.method)}</td>
										<td>{instant(m.valid_from)}</td>
										<td>{instant(m.valid_to)}</td>
										<td>{instant(m.recorded_at)}</td>
										<td>
											{#if m.superseded_at}
												<Chip tone="neutral" title={`Superseded ${instant(m.superseded_at)}`}>retired</Chip>
											{:else}
												<Chip tone="calm">in force</Chip>
											{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/each}
			</section>
		{/if}
	{/snippet}
</Await>

<style>
	section { margin-top: 2rem; }
	h3 { margin: 1.2rem 0 0.5rem; font-size: 0.86rem; font-weight: 500; }
	.detail td { background: var(--surface-2); }
	.mismatch td { background: color-mix(in srgb, var(--bad, #b3261e) 8%, transparent); }
	.superseded { opacity: 0.6; }
	.note { max-width: var(--measure); font-size: 0.82rem; }
	.consulted, .findings { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.5rem; }
	.consulted li, .findings li { display: flex; gap: 0.6rem; align-items: baseline; }
	.findings li { max-width: var(--measure); }
</style>
