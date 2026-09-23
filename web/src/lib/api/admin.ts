import type { ApiClient, CallOptions } from './client';
import * as T from './types';

/**
 * Administration, as procedures: tenants, the audit chain, the inbox, reports
 * and the file register.
 *
 * Five services that had no client. A sub-facade rather than more methods on
 * Gavya, for the reason HerdApi gives.
 *
 * Two things here are deliberately awkward, because the services are.
 * `updateTenant` demands every field, because the handler writes a whole record
 * and an omitted field is a cleared one. `fileDownloadPath` and
 * `reportDownloadPath` are named for what they return — a stored path — rather
 * than for the procedures that return it, which are named GetDownloadURL and
 * answer with no URL; `reportContent` is what actually hands a report over.
 */
export class AdminApi {
	readonly #client: ApiClient;
	readonly #tenantId: string;
	readonly #session: string;

	constructor(client: ApiClient, session: string, tenantId: string) {
		this.#client = client;
		this.#session = session;
		this.#tenantId = tenantId;
	}

	#opts(extra?: Partial<CallOptions>): CallOptions {
		return { session: this.#session, ...extra };
	}

	get tenantId(): string {
		return this.#tenantId;
	}

	/* ---- tenants ---- */

	createTenant(req: T.CreateTenantRequest, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateTenantRequest, T.TenantResponse>(
			`${T.TENANT}/CreateTenant`,
			req,
			this.#opts(extra)
		);
	}

	/**
	 * A tenant by id. Not scoped to the session's tenant, because the procedure
	 * is not: it takes an id and nothing else, and the gateway's permission
	 * table is what decides who may ask.
	 */
	getTenant(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantIDRequest, T.TenantResponse>(
			`${T.TENANT}/GetTenant`,
			{ id },
			this.#opts(extra)
		);
	}

	/** The signed-in tenant's own record. */
	getOwnTenant(extra?: Partial<CallOptions>) {
		return this.getTenant(this.#tenantId, extra);
	}

	listTenants(extra?: Partial<CallOptions>) {
		return this.#client.call<Record<string, never>, T.ListTenantsResponse>(
			`${T.TENANT}/ListTenants`,
			{},
			this.#opts(extra)
		);
	}

	/**
	 * Every mutable field, every time.
	 *
	 * The request type says so and this signature enforces it: the handler
	 * builds a whole record out of what arrives and writes it, so a partial
	 * update blanks whatever it left out — including the currency every money
	 * view in the platform reads its code from.
	 */
	updateTenant(req: T.UpdateTenantRequest, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UpdateTenantRequest, T.TenantResponse>(
			`${T.TENANT}/UpdateTenant`,
			req,
			this.#opts(extra)
		);
	}

	suspendTenant(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantActionRequest, T.TenantResponse>(
			`${T.TENANT}/SuspendTenant`,
			{ id, updated_by: actor },
			this.#opts(extra)
		);
	}

	activateTenant(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantActionRequest, T.TenantResponse>(
			`${T.TENANT}/ActivateTenant`,
			{ id, updated_by: actor },
			this.#opts(extra)
		);
	}

	upsertTenantSetting(
		req: Omit<T.UpsertTenantSettingRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.UpsertTenantSettingRequest, T.TenantSettingResponse>(
			`${T.TENANT}/UpsertTenantSetting`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listTenantSettings(tenantId?: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListTenantSettingsRequest, T.ListTenantSettingsResponse>(
			`${T.TENANT}/ListTenantSettings`,
			{ tenant_id: tenantId ?? this.#tenantId },
			this.#opts(extra)
		);
	}

	/* ---- the audit chain ---- */

	createAuditLog(req: Omit<T.CreateAuditLogRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateAuditLogRequest, T.AuditLogResponse>(
			`${T.AUDIT}/CreateAuditLog`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getAuditLog(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetAuditLogRequest, T.AuditLogResponse>(
			`${T.AUDIT}/GetAuditLog`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listAuditLogs(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListAuditLogsRequest, T.ListAuditLogsResponse>(
			`${T.AUDIT}/ListAuditLogs`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listAuditLogsByResource(
		resourceType: string,
		resourceId: string,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListAuditLogsByResourceRequest, T.ListAuditLogsResponse>(
			`${T.AUDIT}/ListAuditLogsByResource`,
			{ tenant_id: this.#tenantId, resource_type: resourceType, resource_id: resourceId },
			this.#opts(extra)
		);
	}

	listAuditLogsByActor(actorId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListAuditLogsByActorRequest, T.ListAuditLogsResponse>(
			`${T.AUDIT}/ListAuditLogsByActor`,
			{ tenant_id: this.#tenantId, actor_id: actorId },
			this.#opts(extra)
		);
	}

	/** Link the tenant's new rows into its chain and anchor the head. */
	sealAuditChain(limit?: number, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SealAuditChainRequest, T.SealAuditChainResponse>(
			`${T.AUDIT}/SealAuditChain`,
			{ tenant_id: this.#tenantId, limit },
			this.#opts(extra)
		);
	}

	/**
	 * Recompute the chain and report what was found.
	 *
	 * The answer's `intact` means everything *sealed* is accounted for, which is
	 * why the counts arrive beside it. Anything reading this must show both.
	 */
	verifyAuditChain(extra?: Partial<CallOptions>) {
		return this.#client.call<T.VerifyAuditChainRequest, T.VerifyAuditChainResponse>(
			`${T.AUDIT}/VerifyAuditChain`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/* ---- notifications ---- */

	sendNotification(req: Omit<T.SendNotificationRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SendNotificationRequest, T.NotificationResponse>(
			`${T.NOTIFICATION}/SendNotification`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getNotification(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetNotificationRequest, T.NotificationResponse>(
			`${T.NOTIFICATION}/GetNotification`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listNotifications(
		req: Omit<T.ListNotificationsRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListNotificationsRequest, T.ListNotificationsResponse>(
			`${T.NOTIFICATION}/ListNotifications`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	markAsRead(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.MarkReadRequest, T.NotificationResponse>(
			`${T.NOTIFICATION}/MarkAsRead`,
			{ id, tenant_id: this.#tenantId, updated_by: actor },
			this.#opts(extra)
		);
	}

	markAllRead(recipientId: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.MarkAllReadRequest, T.MarkAllReadResponse>(
			`${T.NOTIFICATION}/MarkAllRead`,
			{ recipient_id: recipientId, tenant_id: this.#tenantId, updated_by: actor },
			this.#opts(extra)
		);
	}

	createTemplate(req: Omit<T.CreateTemplateRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateTemplateRequest, T.TemplateResponse>(
			`${T.NOTIFICATION}/CreateTemplate`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listTemplates(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListTemplatesRequest, T.ListTemplatesResponse>(
			`${T.NOTIFICATION}/ListTemplates`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getUnreadCount(recipientId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UnreadCountRequest, T.UnreadCountResponse>(
			`${T.NOTIFICATION}/GetUnreadCount`,
			{ tenant_id: this.#tenantId, recipient_id: recipientId },
			this.#opts(extra)
		);
	}

	/* ---- reports ---- */

	/**
	 * Ask for a report.
	 *
	 * It used to be worth saying that this only recorded a request, because
	 * nothing in the platform ran one. reporting-service now has a runner: the
	 * row goes pending, a sweep claims it within seconds, and it ends
	 * completed or failed with a reason.
	 *
	 * The type and its parameters are checked here rather than at the moment
	 * the runner picks it up, so a request that cannot be produced is refused
	 * while somebody is still standing at the screen.
	 */
	requestReport(req: Omit<T.RequestReportRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RequestReportRequest, T.ReportResponse>(
			`${T.REPORTING}/RequestReport`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getReport(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetReportRequest, T.ReportResponse>(
			`${T.REPORTING}/GetReport`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listReports(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListReportsRequest, T.ListReportsResponse>(
			`${T.REPORTING}/ListReports`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/** The report's stored file path. The procedure is called GetReportDownloadURL
	 * and returns the path verbatim; nothing signs it and nothing resolves it.
	 * reportContent below is what actually hands the report over. */
	reportDownloadPath(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetReportDownloadURLRequest, T.ReportDownloadResponse>(
			`${T.REPORTING}/GetReportDownloadURL`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/**
	 * The report itself.
	 *
	 * The bytes, base64-encoded, rather than a path. This platform has no
	 * object storage and never had a URL to give, so a report that cannot be
	 * read this way is a report nobody can reach.
	 */
	reportContent(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetReportContentRequest, T.GetReportContentResponse>(
			`${T.REPORTING}/GetReportContent`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/**
	 * What this platform can produce.
	 *
	 * Asked rather than held here, because a client with its own list is one
	 * that offers a type the platform stopped producing, or hides one it
	 * started. The same list the runner reads is the list a person chooses
	 * from.
	 */
	reportKinds(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListReportKindsRequest, T.ListReportKindsResponse>(
			`${T.REPORTING}/ListReportKinds`,
			{},
			this.#opts(extra)
		);
	}

	createSchedule(req: Omit<T.CreateScheduleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateScheduleRequest, T.ScheduleResponse>(
			`${T.REPORTING}/CreateSchedule`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listSchedules(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListSchedulesRequest, T.ListSchedulesResponse>(
			`${T.REPORTING}/ListSchedules`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	updateSchedule(id: string, isActive: boolean, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UpdateScheduleRequest, T.ScheduleResponse>(
			`${T.REPORTING}/UpdateSchedule`,
			{ id, tenant_id: this.#tenantId, is_active: isActive, updated_by: actor },
			this.#opts(extra)
		);
	}

	deleteSchedule(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DeleteScheduleRequest, T.DeleteScheduleResponse>(
			`${T.REPORTING}/DeleteSchedule`,
			{ id, tenant_id: this.#tenantId, updated_by: actor },
			this.#opts(extra)
		);
	}

	/* ---- the file register ---- */

	/**
	 * Record that a file exists somewhere.
	 *
	 * file-service has no upload procedure and no multipart route. These five
	 * procedures are a register of records about files something else stored,
	 * and `storage_path` is the caller's word for where it put them.
	 */
	createFileRecord(req: Omit<T.CreateFileRecordRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateFileRecordRequest, T.FileRecordResponse>(
			`${T.FILE}/CreateFileRecord`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getFileRecord(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetFileRecordRequest, T.FileRecordResponse>(
			`${T.FILE}/GetFileRecord`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listEntityFiles(entityType: string, entityId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListEntityFilesRequest, T.ListEntityFilesResponse>(
			`${T.FILE}/ListEntityFiles`,
			{ tenant_id: this.#tenantId, entity_type: entityType, entity_id: entityId },
			this.#opts(extra)
		);
	}

	deleteFile(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DeleteFileRequest, T.DeleteFileResponse>(
			`${T.FILE}/DeleteFile`,
			{ id, tenant_id: this.#tenantId, updated_by: actor },
			this.#opts(extra)
		);
	}

	/** `<bucket>/<stored_name>`, concatenated. Not a URL and not signed. */
	fileDownloadPath(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetDownloadURLRequest, T.FileDownloadResponse>(
			`${T.FILE}/GetDownloadURL`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}
}
