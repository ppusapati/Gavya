/**
 * Administration: tenants, the audit chain, the inbox, reports and files.
 *
 * Field names mirror the Go handlers' json tags exactly, and clients_test.go
 * compares the two on every run of the gate.
 *
 * One of these services promises less than its name suggests, and the types say
 * so where the screens have to act on it: `GetDownloadURLResponse.url` and
 * `ReportDownloadResponse.url` are stored paths rather than URLs, because
 * neither service signs anything and this platform has no object storage.
 *
 * Reports used to be the other. `RequestReport` wrote a row with status
 * "pending" and nothing in the platform ever moved it, and `next_run_at` was a
 * column nothing computed. Both are now run by reporting-service's own runner:
 * a report is produced and its bytes come back from `GetReportContent`, and a
 * schedule fires in its own timezone.
 */

/* ---- tenants ---- */

export interface Tenant {
	id: string;
	name: string;
	slug: string;
	plan: string;
	status: string;
	contact_email: string;
	contact_phone: string;
	address: string;
	country: string;
	/** An IANA zone. This is the one the console should read dates in. */
	timezone: string;
	currency: string;
	/**
	 * How many digits after the point this tenant's currency has: 2 for a rupee,
	 * 0 for a yen, 3 for a dinar. Stored beside the code rather than derived on
	 * read, so an amount already recorded cannot change meaning if a currency
	 * table is corrected.
	 */
	currency_scale: number;
	max_users: number;
	max_cattle: number;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;
}

export interface TenantSetting {
	id: string;
	tenant_id: string;
	key: string;
	value: string;
	data_type: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface CreateTenantRequest {
	name: string;
	slug: string;
	plan: string;
	contact_email: string;
	contact_phone: string;
	address: string;
	country: string;
	timezone: string;
	currency: string;
	max_users: number;
	max_cattle: number;
	created_by: string;
}

export interface TenantResponse {
	tenant: Tenant;
}

export interface TenantIDRequest {
	id: string;
}

export interface TenantActionRequest {
	id: string;
	updated_by: string;
}

/**
 * Every field, every time.
 *
 * The handler builds a whole domain.Tenant out of this request and writes it,
 * so an omitted field is not "leave it alone" — it is "set it to empty". A
 * caller that sends only the name blanks the contact details, the timezone and
 * the currency, and the currency is what every money view in the platform reads
 * its code from.
 */
export interface UpdateTenantRequest {
	id: string;
	name: string;
	contact_email: string;
	contact_phone: string;
	address: string;
	country: string;
	timezone: string;
	currency: string;
	max_users: number;
	max_cattle: number;
	updated_by: string;
}

export interface ListTenantsRequest {
	[k: string]: never;
}

export interface ListTenantsResponse {
	tenants: Tenant[];
}

export interface UpsertTenantSettingRequest {
	tenant_id: string;
	key: string;
	value: string;
	data_type: string;
	created_by: string;
}

export interface TenantSettingResponse {
	setting: TenantSetting;
}

export interface ListTenantSettingsRequest {
	tenant_id: string;
}

export interface ListTenantSettingsResponse {
	settings: TenantSetting[];
}

/* ---- the audit chain ---- */

export interface AuditLog {
	id: string;
	tenant_id: string;
	actor_id: string;
	actor_type: string;
	action: string;
	resource_type: string;
	resource_id: string;
	old_value: string;
	new_value: string;
	ip_address: string;
	user_agent: string;
	service_name: string;
	trace_id: string;
	created_at: string;
	created_by: string;
}

export interface CreateAuditLogRequest {
	tenant_id: string;
	actor_id: string;
	actor_type: string;
	action: string;
	resource_type: string;
	resource_id: string;
	old_value: string;
	new_value: string;
	ip_address: string;
	user_agent: string;
	service_name: string;
	trace_id: string;
	created_by: string;
}

export interface AuditLogResponse {
	audit_log: AuditLog;
}

export interface GetAuditLogRequest {
	id: string;
	tenant_id: string;
}

export interface ListAuditLogsRequest {
	tenant_id: string;
}

export interface ListAuditLogsByResourceRequest {
	tenant_id: string;
	resource_type: string;
	resource_id: string;
}

export interface ListAuditLogsByActorRequest {
	tenant_id: string;
	actor_id: string;
}

export interface ListAuditLogsResponse {
	audit_logs: AuditLog[];
}

export interface SealAuditChainRequest {
	tenant_id: string;
	/**
	 * Bounds one run, so a tenant with a long backlog is caught up over several
	 * passes rather than in one transaction holding the sealer's lock.
	 */
	limit?: number;
}

export interface SealAuditChainResponse {
	sealed: number;
	last_seq: number;
	last_hash: string;
	anchored_at_seq?: number;
	anchor_taken: boolean;
	anchor_hash?: string;
}

export interface VerifyAuditChainRequest {
	tenant_id: string;
}

export interface VerifyAuditChainResponse {
	intact: boolean;
	rows_checked: number;
	broken_at_seq?: number;
	broken_id?: string;
	detail?: string;

	/**
	 * These travel with the verdict rather than separately, because "intact" on
	 * its own invites the reading that everything is accounted for, when what it
	 * means is that everything *sealed* is accounted for. A screen that shows
	 * the verdict and hides these is the reading the service went out of its way
	 * to prevent.
	 */
	total_rows: number;
	sealed_rows: number;
	unsealed_rows: number;
	unsealed_for_seconds?: number;
	oldest_unsealed_at?: string;
}

/* ---- notifications ---- */

export interface Notification {
	id: string;
	tenant_id: string;
	recipient_id: string;
	recipient_type: string;
	channel: string;
	title: string;
	body: string;
	status: string;
	priority: string;
	reference_id: string;
	reference_type: string;
	sent_at?: string;
	read_at?: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;
}

export interface NotificationTemplate {
	id: string;
	tenant_id: string;
	event_type: string;
	channel: string;
	title: string;
	body_template: string;
	is_active: boolean;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;
}

export interface SendNotificationRequest {
	tenant_id: string;
	recipient_id: string;
	recipient_type: string;
	channel: string;
	title: string;
	body: string;
	priority: string;
	reference_id: string;
	reference_type: string;
	created_by: string;
}

export interface NotificationResponse {
	notification: Notification;
}

export interface GetNotificationRequest {
	id: string;
	tenant_id: string;
}

export interface ListNotificationsRequest {
	tenant_id: string;
	channel: string;
	status: string;
	/**
	 * Whose inbox. Both optional and both filters when set; without them the
	 * listing is the whole tenant's, which is not an inbox — it is everybody's
	 * mail on one table.
	 */
	recipient_id?: string;
	recipient_type?: string;
}

export interface ListNotificationsResponse {
	notifications: Notification[];
}

export interface MarkReadRequest {
	id: string;
	tenant_id: string;
	updated_by: string;
}

export interface MarkAllReadRequest {
	recipient_id: string;
	tenant_id: string;
	updated_by: string;
}

export interface MarkAllReadResponse {
	status: string;
}

export interface CreateTemplateRequest {
	tenant_id: string;
	event_type: string;
	channel: string;
	title: string;
	body_template: string;
	created_by: string;
}

export interface TemplateResponse {
	template: NotificationTemplate;
}

export interface ListTemplatesRequest {
	tenant_id: string;
}

export interface ListTemplatesResponse {
	templates: NotificationTemplate[];
}

export interface UnreadCountRequest {
	tenant_id: string;
	recipient_id: string;
}

export interface UnreadCountResponse {
	count: number;
}

/* ---- reports ---- */

export interface Report {
	id: string;
	tenant_id: string;
	name: string;
	report_type: string;
	/** Opaque to this client: the service stores whatever string it was given. */
	parameters: string;
	/**
	 * pending, running, completed or failed.
	 *
	 * The runner moves it. A report goes pending on request, running when a
	 * sweep claims it, and then completed or failed — and a failed one always
	 * carries `failure_reason`, because a failure nobody can read is one
	 * nobody can correct.
	 */
	status: string;
	/**
	 * Where the bytes are. A locator, not a URL, and not something a browser
	 * can fetch — `GetReportContent` is what hands the report over.
	 */
	file_path: string;
	file_format: string;
	/** The person who asked, or the schedule that did. */
	requested_by: string;
	started_at?: string;
	completed_at?: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;

	/** What it is, once produced. */
	content_type?: string;
	row_count?: number;
	/**
	 * The run stopped at its ceiling. Shown wherever the figures are, because a
	 * truncated total somebody acts on is short by an amount nothing else on
	 * the page discloses.
	 */
	truncated: boolean;
	/** Why it failed. Always present on a failed report. */
	failure_reason?: string;
	/** How many times it has been tried, of three. */
	attempts: number;
}

export interface ReportSchedule {
	id: string;
	tenant_id: string;
	report_type: string;
	schedule: string;
	parameters: string;
	/** Read by the runner: an inactive schedule is not swept. */
	is_active: boolean;
	last_run_at?: string;
	/** When it fires next, computed by the runner in `timezone`. */
	next_run_at?: string;
	/**
	 * The zone this schedule's times are in, as an IANA name.
	 *
	 * Seven in the morning is seven where the society is. Required when a
	 * schedule is written and with no default — UTC would be this platform
	 * deciding what time a co-operative starts work, and by five and a half
	 * hours in the country most of them are in.
	 */
	timezone: string;
	/**
	 * What went wrong the last time it fired. A schedule that has stopped
	 * producing its report is noticed weeks later, when somebody asks where the
	 * report went; this is the answer.
	 */
	last_error?: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;
}

export interface RequestReportRequest {
	tenant_id: string;
	name: string;
	report_type: string;
	parameters: string;
	file_format: string;
	requested_by: string;
	created_by: string;
}

export interface ReportResponse {
	report: Report;
}

export interface GetReportRequest {
	id: string;
	tenant_id: string;
}

export interface ListReportsRequest {
	tenant_id: string;
}

export interface ListReportsResponse {
	reports: Report[];
}

export interface GetReportDownloadURLRequest {
	id: string;
	tenant_id: string;
}

/** Named url; it is the report's stored file path, unsigned and unresolved. */
export interface ReportDownloadResponse {
	url: string;
}

export interface CreateScheduleRequest {
	tenant_id: string;
	/** Must be a type whose `schedulable` is true. */
	report_type: string;
	/** Five cron fields: minute, hour, day of month, month, day of week. */
	schedule: string;
	/**
	 * Must carry a window — yesterday, last_7_days or last_month — because the
	 * period a scheduled report covers has to move with the firing. A schedule
	 * carrying fixed dates would produce the same report for ever.
	 */
	parameters: string;
	/** An IANA zone such as Asia/Kolkata. Required, with no default. */
	timezone: string;
	created_by: string;
}

/** The windows a schedule can ask for. Anything else is refused. */
export const REPORT_WINDOWS = ['yesterday', 'last_7_days', 'last_month'] as const;

/** One report this platform can produce. */
export interface ReportKind {
	name: string;
	summary: string;
	/** Parameter keys without which it cannot run. */
	needs: string[];
	/**
	 * Whether a schedule can ask for it. False for a type that names something
	 * a schedule has no way to supply, such as a cycle.
	 */
	schedulable: boolean;
}

/**
 * Nothing. The catalogue is the same for every co-operative, so a tenant here
 * would be a field the caller fills in that changes no answer — and the
 * permission that scopes the call is carried by the session.
 */
export type ListReportKindsRequest = Record<string, never>;
export interface ListReportKindsResponse {
	kinds: ReportKind[];
}

export interface GetReportContentRequest {
	id: string;
	tenant_id: string;
}

/**
 * The report itself.
 *
 * `content` is base64, which is how Go marshals a []byte and what a JSON
 * transport can carry. It is decoded in the browser rather than shown raw —
 * this is the one thing on the reports screen that is not text to read.
 */
export interface GetReportContentResponse {
	content: string;
	content_type: string;
	/** What to save it as, built by the service rather than from free text. */
	filename: string;
	row_count: number;
	truncated: boolean;
	bytes: number;
}

export interface ScheduleResponse {
	schedule: ReportSchedule;
}

export interface ListSchedulesRequest {
	tenant_id: string;
}

export interface ListSchedulesResponse {
	schedules: ReportSchedule[];
}

export interface UpdateScheduleRequest {
	id: string;
	tenant_id: string;
	is_active: boolean;
	updated_by: string;
}

export interface DeleteScheduleRequest {
	id: string;
	tenant_id: string;
	updated_by: string;
}

export type DeleteScheduleResponse = Record<string, never>;

/* ---- files ---- */

export interface FileRecord {
	id: string;
	tenant_id: string;
	original_name: string;
	stored_name: string;
	content_type: string;
	size_bytes: number;
	storage_path: string;
	storage_provider: string;
	entity_type: string;
	entity_id: string;
	uploaded_by: string;
	is_public: boolean;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;
}

/**
 * file-service never sees a byte of the file.
 *
 * There is no upload procedure and no multipart route — the five procedures are
 * a register of records about files somebody else stored. `storage_path` is
 * where the caller says it put them. A console that offered a file picker here
 * would be offering something the platform cannot do.
 */
export interface CreateFileRecordRequest {
	tenant_id: string;
	original_name: string;
	stored_name: string;
	content_type: string;
	size_bytes: number;
	storage_path: string;
	entity_type: string;
	entity_id: string;
	uploaded_by: string;
	is_public: boolean;
	created_by: string;
}

export interface FileRecordResponse {
	file: FileRecord;
}

export interface GetFileRecordRequest {
	id: string;
	tenant_id: string;
}

export interface ListEntityFilesRequest {
	tenant_id: string;
	entity_type: string;
	entity_id: string;
}

export interface ListEntityFilesResponse {
	files: FileRecord[];
}

export interface DeleteFileRequest {
	id: string;
	tenant_id: string;
	updated_by: string;
}

export type DeleteFileResponse = Record<string, never>;

export interface GetDownloadURLRequest {
	id: string;
	tenant_id: string;
}

/**
 * Named url; it is `<bucket>/<stored_name>`, concatenated. Nothing signs it and
 * nothing checks that the object is there. Rendering it as a link would produce
 * a broken one and imply the platform granted access to something.
 */
export interface FileDownloadResponse {
	url: string;
}
