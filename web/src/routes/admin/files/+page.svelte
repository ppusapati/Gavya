<script lang="ts">
	import {
		ApiError,
		type FileDownloadResponse,
		type FileRecord,
		type FileRecordResponse,
		type ListEntityFilesResponse
	} from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const files = new Task<ListEntityFilesResponse>();
	const one = new Task<FileRecordResponse>();
	const stored = new Task<FileDownloadResponse>();

	let entityType = $state('');
	let entityId = $state('');

	function load() {
		if (!entityType.trim() || !entityId.trim()) return;
		files.run((s) =>
			settings.api().admin.listEntityFiles(entityType.trim(), entityId.trim(), { signal: s })
		);
	}

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

	let recording = $state(false);
	let rec = $state({
		original_name: '',
		stored_name: '',
		content_type: '',
		size_bytes: 0,
		storage_path: '',
		is_public: false
	});

	let open = $state<string | undefined>(undefined);

	function openFile(f: FileRecord) {
		if (open === f.id) {
			open = undefined;
			one.reset();
			stored.reset();
			return;
		}
		open = f.id;
		one.run((s) => settings.api().admin.getFileRecord(f.id, { signal: s }));
		stored.reset();
	}

	function bytes(n: number): string {
		if (n < 1024) return `${n} B`;
		if (n < 1024 * 1024) return `${Math.round(n / 102.4) / 10} kB`;
		return `${Math.round(n / 104857.6) / 10} MB`;
	}
</script>

<div class="page-head">
	<h1>Files</h1>
	<p>
		A register of files, attached to the things they belong to. The register is all it is.
	</p>
</div>

<!--
	No upload box, because there is nothing to upload to.

	file-service registers five procedures and none of them accepts a byte: no
	multipart route, no presigned-upload procedure, nothing. CreateFileRecord
	takes a storage_path the caller has already written to. A file picker on this
	screen would offer something the platform cannot do, and would fail at the
	moment somebody was relying on it.
-->
<p class="warning">
	<Chip tone="attention">no upload here</Chip>
	This service never sees a file. It records where somebody else put one, so everything below is a
	claim about a file rather than the file itself — and nothing checks that the object is there.
</p>

<ErrorNote error={saveError} />

<div class="controls">
	<div class="field"><label for="et">Attached to</label><input id="et" bind:value={entityType} size="16" placeholder="kind" /></div>
	<div class="field"><label for="ei">Identifier</label><input id="ei" bind:value={entityId} size="26" /></div>
	<button class="ghost" onclick={load} disabled={!entityType.trim() || !entityId.trim() || files.pending}>
		List
	</button>
	<button
		disabled={!entityType.trim() || !entityId.trim()}
		onclick={() => { recording = !recording; saveError = undefined; }}
	>
		{recording ? 'Cancel' : 'Register a file'}
	</button>
</div>

{#if !entityType.trim() || !entityId.trim()}
	<p class="muted note">
		Name what the files are attached to. The only listing this service offers is by entity — there
		is no procedure that lists a tenant's files, so nothing here can show you everything.
	</p>
{/if}

{#if recording}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().admin.createFileRecord({
						original_name: rec.original_name.trim(),
						stored_name: rec.stored_name.trim(),
						content_type: rec.content_type.trim(),
						size_bytes: rec.size_bytes,
						storage_path: rec.storage_path.trim(),
						entity_type: entityType.trim(),
						entity_id: entityId.trim(),
						uploaded_by: settings.actorOrUnknown,
						is_public: rec.is_public,
						created_by: settings.actorOrUnknown
					}),
				() => {
					recording = false;
					rec = { ...rec, original_name: '', stored_name: '', storage_path: '', size_bytes: 0 };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field grow"><label for="fo">Original name</label><input id="fo" bind:value={rec.original_name} /></div>
			<div class="field grow"><label for="fs">Stored as</label><input id="fs" bind:value={rec.stored_name} /></div>
			<div class="field"><label for="fc">Content type</label><input id="fc" bind:value={rec.content_type} size="18" /></div>
			<div class="field"><label for="fb">Size in bytes</label><input id="fb" type="number" min="0" bind:value={rec.size_bytes} size="10" /></div>
		</div>
		<div class="controls">
			<div class="field grow"><label for="fp">Storage path</label><input id="fp" bind:value={rec.storage_path} /></div>
			<label class="check"><input type="checkbox" bind:checked={rec.is_public} /> Public</label>
			<button type="submit" disabled={saving || !rec.original_name.trim() || !rec.storage_path.trim()}>
				Register
			</button>
		</div>
		<p class="muted note">
			Every field is what the caller says, including the size. Nothing here is measured from a
			file, because there is no file here to measure.
		</p>
	</form>
{/if}

{#if files.settled}
	<Await task={files} retry={load} isEmpty={(d) => (d.files ?? []).length === 0} empty="Nothing is registered against that.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead>
						<tr><th>Name</th><th>Type</th><th class="num">Size</th><th>Provider</th><th>Registered</th><th>Public</th><th></th></tr>
					</thead>
					<tbody>
						{#each d.files as f (f.id)}
							<tr class:deleted={!!f.deleted_at}>
								<td>{f.original_name}</td>
								<td class="muted">{f.content_type || '—'}</td>
								<td class="num">{bytes(f.size_bytes)}</td>
								<td>{f.storage_provider ? label(f.storage_provider) : '—'}</td>
								<td>{instant(f.created_at)} <span class="muted">by {f.uploaded_by}</span></td>
								<td><Chip tone={f.is_public ? 'attention' : 'neutral'}>{f.is_public ? 'Yes' : 'No'}</Chip></td>
								<td class="actions">
									<button class="ghost" onclick={() => openFile(f)}>{open === f.id ? 'Close' : 'Open'}</button>
									{#if !f.deleted_at}
										<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.deleteFile(f.id, settings.actorOrUnknown), load)}>
											Remove
										</button>
									{/if}
								</td>
							</tr>
							{#if open === f.id}
								<tr class="detail">
									<td colspan="7">
										<Await task={one} isEmpty={(x) => !x.file} empty="No such record.">
											{#snippet children(x)}
												<dl class="kv">
													<dt>Identifier</dt><dd class="mono">{x.file.id}</dd>
													<dt>Stored as</dt><dd class="mono">{x.file.stored_name}</dd>
													<dt>Storage path</dt><dd class="mono">{x.file.storage_path}</dd>
													<dt>Provider</dt><dd>{x.file.storage_provider || '—'}</dd>
													<dt>Attached to</dt>
													<dd>{x.file.entity_type} <span class="mono">{x.file.entity_id}</span></dd>
													{#if x.file.deleted_at}
														<dt>Removed</dt>
														<dd>
															{instant(x.file.deleted_at)}
															<span class="muted">
																from the register. Whether the object itself was removed is
																not something this service knows.
															</span>
														</dd>
													{/if}
												</dl>
												<div class="controls">
													<button
														class="ghost"
														disabled={stored.pending}
														onclick={() => stored.run((s) => settings.api().admin.fileDownloadPath(f.id, { signal: s }))}
													>
														Ask where it is stored
													</button>
												</div>
												{#if stored.settled}
													<Await task={stored} isEmpty={(p) => !p.url} empty="No path.">
														{#snippet children(p)}
															<!--
																Text, never an anchor.

																GetDownloadURL concatenates the configured bucket with the
																stored name and returns that. It signs nothing, checks
																nothing, and the result is not reachable from a browser. A
																link here would break and would imply the platform had
																granted access to something.
															-->
															<p class="banner">
																<Chip tone="neutral">stored at</Chip>
																<span class="mono">{p.url}</span>
															</p>
															<p class="muted note">
																The bucket and the stored name, joined. It is not a signed
																URL, nothing checked that the object is there, and this
																console cannot fetch it.
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
{/if}

<style>
	.detail td { background: var(--surface-2); }
	.deleted { opacity: 0.55; }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.check { display: flex; gap: 0.4rem; align-items: center; font-size: 0.85rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.6rem 0; font-size: 0.85rem; }
	.warning {
		display: flex;
		gap: 0.6rem;
		align-items: baseline;
		flex-wrap: wrap;
		max-width: var(--measure);
		margin: 1rem 0;
		padding: 0.8rem 1rem;
		font-size: 0.85rem;
		background: color-mix(in srgb, var(--warn, #8a6d1f) 10%, transparent);
		border-left: 3px solid var(--warn, #8a6d1f);
	}
</style>
