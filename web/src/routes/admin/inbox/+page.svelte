<script lang="ts">
	import {
		ApiError,
		type ListNotificationsResponse,
		type ListTemplatesResponse,
		type Notification,
		type NotificationResponse,
		type UnreadCountResponse
	} from '$lib/api';
	import { instant, label } from '$lib/display';
	import { settings } from '$lib/settings.svelte';
	import { Task } from '$lib/task.svelte';
	import Await from '$lib/ui/Await.svelte';
	import Chip from '$lib/ui/Chip.svelte';
	import ErrorNote from '$lib/ui/ErrorNote.svelte';

	const notifications = new Task<ListNotificationsResponse>();
	const unread = new Task<UnreadCountResponse>();
	const templates = new Task<ListTemplatesResponse>();
	const one = new Task<NotificationResponse>();

	/**
	 * Whose mail. Defaults to the signed-in user rather than the whole tenant.
	 *
	 * Without a recipient the service returns every notification the tenant has,
	 * which is not an inbox — it is everybody's mail on one table, and a person
	 * reading it is reading other people's.
	 */
	let recipientId = $state(settings.actorOrUnknown);
	let recipientType = $state('user');
	let channel = $state('');
	let status = $state('');

	function load() {
		const api = settings.api().admin;
		notifications.run((s) =>
			api.listNotifications(
				{
					channel,
					status,
					recipient_id: recipientId.trim() || undefined,
					recipient_type: recipientId.trim() ? recipientType : undefined
				},
				{ signal: s }
			)
		);
		if (recipientId.trim()) {
			unread.run((s) => api.getUnreadCount(recipientId.trim(), { signal: s }));
		} else {
			unread.reset();
		}
	}
	$effect(() => {
		void channel;
		void status;
		load();
	});
	$effect(() => {
		templates.run((s) => settings.api().admin.listTemplates({ signal: s }));
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

	let sending = $state(false);
	let msg = $state({
		recipient_id: '',
		recipient_type: 'user',
		channel: 'in_app',
		title: '',
		body: '',
		priority: 'normal',
		reference_id: '',
		reference_type: ''
	});

	let addingTemplate = $state(false);
	let tpl = $state({ event_type: '', channel: 'in_app', title: '', body_template: '' });

	let open = $state<string | undefined>(undefined);

	function openOne(n: Notification) {
		if (open === n.id) {
			open = undefined;
			one.reset();
			return;
		}
		open = n.id;
		one.run((s) => settings.api().admin.getNotification(n.id, { signal: s }));
	}

	function priorityTone(p: string) {
		switch (p) {
			case 'urgent':
			case 'high':
				return 'critical';
			case 'low':
				return 'neutral';
			default:
				return 'neutral';
		}
	}
</script>

<div class="page-head">
	<h1>Inbox</h1>
	<p>
		What the platform has told somebody. Filtered to one recipient by default, because the listing
		without one is the whole tenant's mail rather than anybody's inbox.
	</p>
</div>

<ErrorNote error={saveError} />

<div class="controls">
	<div class="field"><label for="ri">Recipient</label><input id="ri" bind:value={recipientId} size="24" /></div>
	<div class="field">
		<label for="rt">As</label>
		<select id="rt" bind:value={recipientType}>
			<option value="user">A person</option>
			<option value="role">A role</option>
		</select>
	</div>
	<div class="field">
		<label for="ch">Channel</label>
		<select id="ch" bind:value={channel}>
			<option value="">Every channel</option>
			<option value="in_app">In app</option>
			<option value="email">Email</option>
			<option value="sms">SMS</option>
			<option value="push">Push</option>
		</select>
	</div>
	<div class="field">
		<label for="st">Status</label>
		<select id="st" bind:value={status}>
			<option value="">Any</option>
			<option value="pending">Pending</option>
			<option value="sent">Sent</option>
			<option value="read">Read</option>
			<option value="failed">Failed</option>
		</select>
	</div>
	<button class="ghost" onclick={load} disabled={notifications.pending}>Look up</button>
	<button onclick={() => { sending = !sending; saveError = undefined; }}>
		{sending ? 'Cancel' : 'Send one'}
	</button>
</div>

{#if unread.settled}
	<Await task={unread} isEmpty={() => false} empty="">
		{#snippet children(u)}
			<p class="banner">
				<Chip tone={u.count > 0 ? 'attention' : 'calm'}>{u.count} unread</Chip>
				for <span class="mono">{recipientId}</span>
				{#if u.count > 0}
					<button
						class="linklike"
						disabled={saving}
						onclick={() => run(() => settings.api().admin.markAllRead(recipientId.trim(), settings.actorOrUnknown), load)}
					>
						Mark all read
					</button>
				{/if}
			</p>
		{/snippet}
	</Await>
{:else}
	<p class="muted note">
		Name a recipient above to see an unread count. Without one this page is showing the tenant's
		whole table.
	</p>
{/if}

{#if sending}
	<form
		class="panel"
		onsubmit={(e) => {
			e.preventDefault();
			run(
				() =>
					settings.api().admin.sendNotification({
						recipient_id: msg.recipient_id.trim(),
						recipient_type: msg.recipient_type,
						channel: msg.channel,
						title: msg.title.trim(),
						body: msg.body,
						priority: msg.priority,
						reference_id: msg.reference_id.trim(),
						reference_type: msg.reference_type.trim(),
						created_by: settings.actorOrUnknown
					}),
				() => {
					sending = false;
					msg = { ...msg, title: '', body: '', reference_id: '' };
					load();
				}
			);
		}}
	>
		<div class="controls">
			<div class="field"><label for="mr">To</label><input id="mr" bind:value={msg.recipient_id} size="24" /></div>
			<div class="field">
				<label for="mrt">As</label>
				<select id="mrt" bind:value={msg.recipient_type}>
					<option value="user">A person</option>
					<option value="role">A role</option>
				</select>
			</div>
			<div class="field">
				<label for="mc">Channel</label>
				<select id="mc" bind:value={msg.channel}>
					<option value="in_app">In app</option>
					<option value="email">Email</option>
					<option value="sms">SMS</option>
					<option value="push">Push</option>
				</select>
			</div>
			<div class="field">
				<label for="mp">Priority</label>
				<select id="mp" bind:value={msg.priority}>
					<option value="low">Low</option>
					<option value="normal">Normal</option>
					<option value="high">High</option>
					<option value="urgent">Urgent</option>
				</select>
			</div>
		</div>
		<div class="controls">
			<div class="field grow"><label for="mt">Title</label><input id="mt" bind:value={msg.title} /></div>
			<div class="field"><label for="mrf">About</label><input id="mrf" bind:value={msg.reference_type} size="14" placeholder="kind" /></div>
			<div class="field"><label for="mrid">Reference</label><input id="mrid" bind:value={msg.reference_id} size="22" /></div>
		</div>
		<div class="field grow">
			<label for="mb">Body</label>
			<textarea id="mb" bind:value={msg.body} rows="3"></textarea>
		</div>
		<div class="controls">
			<button type="submit" disabled={saving || !msg.recipient_id.trim() || !msg.title.trim()}>Send</button>
		</div>
		<p class="muted note">
			"Send" records the notification. Whether anything leaves the platform depends on the channel
			having a carrier behind it; the row's status is the only honest answer to what happened, and
			it is in the table below.
		</p>
	</form>
{/if}

<Await task={notifications} retry={load} isEmpty={(d) => (d.notifications ?? []).length === 0} empty="Nothing here.">
	{#snippet children(d)}
		<div class="tablewrap">
			<table>
				<thead>
					<tr><th>When</th><th>To</th><th>Channel</th><th>Title</th><th>Priority</th><th>Status</th><th></th></tr>
				</thead>
				<tbody>
					{#each d.notifications as n (n.id)}
						<tr class:unread={!n.read_at}>
							<td>{instant(n.created_at)}</td>
							<td><span class="mono">{n.recipient_id}</span> <span class="muted">{n.recipient_type}</span></td>
							<td>{label(n.channel)}</td>
							<td>{n.title}</td>
							<td><Chip tone={priorityTone(n.priority)}>{label(n.priority)}</Chip></td>
							<td>
								<Chip tone={n.read_at ? 'calm' : 'neutral'}>{label(n.status)}</Chip>
								{#if !n.sent_at && n.status !== 'failed'}
									<Chip tone="attention" title="Recorded, and not yet reported as sent.">not sent</Chip>
								{/if}
							</td>
							<td class="actions">
								<button class="ghost" onclick={() => openOne(n)}>{open === n.id ? 'Close' : 'Open'}</button>
								{#if !n.read_at}
									<button class="ghost" disabled={saving} onclick={() => run(() => settings.api().admin.markAsRead(n.id, settings.actorOrUnknown), load)}>
										Mark read
									</button>
								{/if}
							</td>
						</tr>
						{#if open === n.id}
							<tr class="detail">
								<td colspan="7">
									<Await task={one} isEmpty={(x) => !x.notification} empty="No such notification.">
										{#snippet children(x)}
											<dl class="kv">
												<dt>Title</dt><dd>{x.notification.title}</dd>
												<dt>Body</dt><dd class="pre">{x.notification.body}</dd>
												<dt>About</dt>
												<dd>
													{x.notification.reference_type || '—'}
													<span class="mono muted">{x.notification.reference_id}</span>
												</dd>
												<dt>Sent</dt><dd>{x.notification.sent_at ? instant(x.notification.sent_at) : 'Not reported as sent.'}</dd>
												<dt>Read</dt><dd>{x.notification.read_at ? instant(x.notification.read_at) : 'Not read.'}</dd>
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

<section>
	<h2>Templates</h2>
	<div class="controls">
		<button class="ghost" onclick={() => { addingTemplate = !addingTemplate; saveError = undefined; }}>
			{addingTemplate ? 'Cancel' : 'Add a template'}
		</button>
	</div>

	{#if addingTemplate}
		<form
			class="panel"
			onsubmit={(e) => {
				e.preventDefault();
				run(
					() =>
						settings.api().admin.createTemplate({
							event_type: tpl.event_type.trim(),
							channel: tpl.channel,
							title: tpl.title.trim(),
							body_template: tpl.body_template,
							created_by: settings.actorOrUnknown
						}),
					() => {
						addingTemplate = false;
						tpl = { event_type: '', channel: 'in_app', title: '', body_template: '' };
						templates.run((s) => settings.api().admin.listTemplates({ signal: s }));
					}
				);
			}}
		>
			<div class="controls">
				<div class="field"><label for="te">Event</label><input id="te" bind:value={tpl.event_type} size="20" /></div>
				<div class="field">
					<label for="tc">Channel</label>
					<select id="tc" bind:value={tpl.channel}>
						<option value="in_app">In app</option>
						<option value="email">Email</option>
						<option value="sms">SMS</option>
						<option value="push">Push</option>
					</select>
				</div>
				<div class="field grow"><label for="tt">Title</label><input id="tt" bind:value={tpl.title} /></div>
			</div>
			<div class="field grow">
				<label for="tb">Body</label>
				<textarea id="tb" bind:value={tpl.body_template} rows="3"></textarea>
			</div>
			<div class="controls">
				<button type="submit" disabled={saving || !tpl.event_type.trim() || !tpl.title.trim()}>Add</button>
			</div>
		</form>
	{/if}

	<Await task={templates} isEmpty={(d) => (d.templates ?? []).length === 0} empty="No templates.">
		{#snippet children(d)}
			<div class="tablewrap">
				<table>
					<thead><tr><th>Event</th><th>Channel</th><th>Title</th><th>Body</th><th>Active</th></tr></thead>
					<tbody>
						{#each d.templates as t (t.id)}
							<tr>
								<td class="mono">{t.event_type}</td>
								<td>{label(t.channel)}</td>
								<td>{t.title}</td>
								<td class="muted pre">{t.body_template}</td>
								<td><Chip tone={t.is_active ? 'calm' : 'neutral'}>{t.is_active ? 'Yes' : 'No'}</Chip></td>
							</tr>
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
	.unread td:first-child { box-shadow: inset 3px 0 0 var(--accent, #3d6b4a); }
	.note { max-width: var(--measure); margin: 0.6rem 0 0.8rem; font-size: 0.82rem; }
	.field.grow { flex: 1 1 12rem; }
	.actions { display: flex; gap: 0.5rem; }
	.banner { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; max-width: var(--measure); margin: 0.6rem 0; font-size: 0.85rem; }
	.pre { white-space: pre-wrap; word-break: break-word; }
	textarea { width: 100%; max-width: var(--measure); font: inherit; }
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
