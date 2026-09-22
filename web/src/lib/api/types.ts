/**
 * Wire types, written against the services' JSON contracts.
 *
 * Field names mirror the Go handlers' json tags exactly. Where the platform
 * distinguishes something the UI must not flatten — a deterministic
 * classification from an advisory hypothesis, a delta's minor units from its
 * display string — that distinction is preserved here rather than collapsed.
 */

export const SHADOW_SETTLEMENT = 'shadowsettlement.v1.ShadowSettlementService';
export const CANONICAL = 'canonical.v1.CanonicalService';
export const INGESTION = 'ingestion.v1.IngestionService';
export const BALANCE = 'balance.v1.BalanceService';

/**
 * The one service every other call depends on.
 *
 * Sign-in is how the console gets the session the gateway asks for. Without it
 * every procedure below returns 401, which is what the console did until this
 * was added.
 */
export const IDENTITY = 'gavya.identity.v1.IdentityService';

/**
 * The herd and the morning's work.
 *
 * These six services had no client at all: twenty-nine services are served,
 * permissioned and exercised end to end, and until now a person could reach four
 * of them. "The platform works" and "a person can use the platform" are
 * different sentences.
 */
export const CATTLE = 'cattle.v1.CattleService';
export const MILK = 'milk.v1.MilkService';
export const BREEDING = 'breeding.v1.BreedingService';
export const HEALTH = 'health.v1.HealthService';
export const FEED = 'feed.v1.FeedService';
export const FARM = 'farm.v1.FarmService';

/**
 * The money path. What milk is worth, what a pool came to, what a member is
 * paid, and what the society bills.
 */
export const PROCUREMENT = 'procurement.v1.ProcurementService';
export const POOLING = 'pooling.v1.PoolingService';
export const SETTLEMENT = 'settlement.v1.SettlementService';
export const BILLING = 'billing.v1.BillingService';

/**
 * The plant. What moved between vessels, what was made from what, what the
 * laboratory read, and what a meter measured.
 */
export const MATERIAL = 'material.v1.MaterialService';
export const PRODUCTION = 'production.v1.ProductionService';
export const LABORATORY = 'laboratory.v1.LaboratoryService';
export const OBSERVATION = 'observation.v1.ObservationService';

/**
 * Administration. Tenants and their settings, the audit chain, the inbox,
 * reports and the file register.
 */
export const TENANT = 'tenant.v1.TenantService';
export const AUDIT = 'audit.v1.AuditService';
export const NOTIFICATION = 'notification.v1.NotificationService';
export const REPORTING = 'reporting.v1.ReportingService';
export const FILE = 'file.v1.FileService';

/** Commerce: the catalogue, the stock, the order book, the cattle market. */
export const CATALOGUE = 'productcatalog.v1.ProductCatalogService';
export const INVENTORY = 'inventory.v1.InventoryService';
export const ORDER = 'order.v1.OrderService';
export const MARKET = 'cattlemarket.v1.CattleMarketService';

// The wire types for those six. Re-exported rather than written here, because
// this file was already four hundred lines about the integrity spine and one
// file holding every type the platform has is one nobody reads.
export * from './types.herd';
export * from './types.money';
export * from './types.commerce';
export * from './types.plant';
export * from './types.admin';
export * from './types.identity';

export interface SignInRequest {
	email: string;
	password: string;
	/** Which of the person's tenants to sign in to. Omitted when they have one. */
	tenant_id?: string;
}

export interface SignInResponse {
	session_id: string;
	tenant_id: string;
	user_id: string;
	role_id?: string;
	role_name?: string;
	expires_at: string;
}

/** The deterministic classifier's closed vocabulary. */
export type Classification =
	| 'MATCH'
	| 'ROUNDING_DIFFERENCE'
	| 'INPUT_DIFFERENCE'
	| 'POLICY_DIFFERENCE'
	| 'RECOVERY_DIFFERENCE'
	| 'UNEXPLAINED'
	| 'INSUFFICIENT_EVIDENCE';

export type DivergenceStatus =
	| 'OPEN'
	| 'UNDER_REVIEW'
	| 'ACCEPTED'
	| 'EXTERNAL_CONFIRMED'
	| 'SHADOW_CONFIRMED'
	| 'RESOLVED';

export interface Evidence {
	kind: string;
	external_minor_units: number;
	shadow_minor_units: number;
	delta_minor_units: number;
	only_external?: boolean;
	only_shadow?: boolean;
	quantity_differs?: boolean;
	rate_differs?: boolean;
}

/**
 * An advisory explanation from the ML tier. `advisory` is always true and is
 * emitted explicitly so no consumer can mistake a model's hypothesis for the
 * platform's classification.
 */
export interface MlHypothesis {
	classification: string;
	confidence: number;
	rationale: string;
	supporting_fields?: string[];
	model_version: string;
	advisory: boolean;
}

export interface Divergence {
	id: string;
	tenant_id: string;
	assertion_id: string;
	computation_id: string;
	producer_ref: string;
	currency: string;
	amount_scale: number;
	delta: string;
	delta_minor_units: number;
	classification: Classification;
	rationale: string;
	evidence: Evidence[];
	ml_hypotheses?: MlHypothesis[];
	status: DivergenceStatus;
	resolution?: string;
	resolved_at?: string;
	resolved_by?: string;
	needs_review: boolean;
	created_at: string;
	updated_at: string;
}

export interface ListDivergencesRequest {
	tenant_id: string;
	status?: string;
	classification?: string;
	producer_ref?: string;
	min_abs_delta?: number;
	limit?: number;
	offset?: number;
}
export interface ListDivergencesResponse {
	divergences: Divergence[];
}

export interface GetDivergenceRequest {
	id: string;
	tenant_id: string;
}
export interface GetDivergenceResponse {
	divergence: Divergence;
}

export interface ResolveDivergenceRequest {
	id: string;
	tenant_id: string;
	status: DivergenceStatus;
	resolution: string;
	actor: string;
}
export interface ResolveDivergenceResponse {
	divergence: Divergence;
}

export interface ClassSummary {
	classification: Classification;
	currency: string;
	/** The unit of total_abs_minor_units. Rows are grouped by it, never summed across it. */
	amount_scale: number;
	count: number;
	total_abs_minor_units: number;
}
export interface SummariseRequest {
	tenant_id: string;
	from: string;
	to: string;
}
export interface SummariseResponse {
	summaries: ClassSummary[];
}

export type EntityKind = 'PRODUCER' | 'CATTLE' | 'ROUTE' | 'CENTRE' | 'DEVICE' | 'SETTLEMENT';
export type MappingMethod = 'EXACT' | 'MANUAL' | 'INFERRED';

export interface ExternalIdentity {
	id: string;
	tenant_id: string;
	source_system_id: string;
	entity_kind: EntityKind;
	external_id: string;
	entity_id: string;
	method: MappingMethod;
	confidence?: number;
	note?: string;
	valid_from: string;
	valid_to: string;
	recorded_at: string;
	superseded_at?: string;
}

export interface ListIdentitiesRequest {
	tenant_id: string;
	source_system_id?: string;
	limit?: number;
	offset?: number;
}
export interface ListIdentitiesResponse {
	identities: ExternalIdentity[];
}

export interface ResolveIdentityRequest {
	tenant_id: string;
	source_system_id: string;
	entity_kind: EntityKind;
	external_id: string;
	/** Required: identifiers are reused, so a mapping has no meaning without an instant. */
	as_of: string;
}
export interface ResolveIdentityResponse {
	identity: ExternalIdentity;
}

export interface MapIdentityRequest {
	tenant_id: string;
	source_system_id: string;
	entity_kind: EntityKind;
	external_id: string;
	entity_id: string;
	method: MappingMethod;
	confidence?: number;
	note?: string;
	valid_from: string;
	valid_to?: string;
	actor: string;
}
export interface MapIdentityResponse {
	identity: ExternalIdentity;
}

export type SlotStatus = 'SETTLED' | 'CONFLICT';

export interface Contender {
	source_ref: string;
	origin: string;
	recorded_at: string;
	quality: number;
	reason: string;
}

export interface CollectionSlot {
	id: string;
	tenant_id: string;
	slot_key: string;
	origin_kind: string;
	policy_id: string;
	policy_version: number;
	authoritative_ref: string;
	status: SlotStatus;
	incumbent_recorded_at: string;
	incumbent_quality: number;
	contenders: Contender[];
	values: Record<string, string>;
	resolution?: string;
	resolved_at?: string;
	resolved_by?: string;
}

export interface ListConflictsRequest {
	tenant_id: string;
	limit?: number;
	offset?: number;
}
export interface ListConflictsResponse {
	slots: CollectionSlot[];
}

export interface ResolveConflictRequest {
	tenant_id: string;
	slot_id: string;
	authoritative_ref: string;
	resolution: string;
	actor: string;
}
export interface ResolveConflictResponse {
	slot: CollectionSlot;
}

export type QuarantineReason =
	| 'TRANSPORT_IDENTITY_CONFLICT'
	| 'SEQUENCE_REGRESSION'
	| 'UNTRUSTED_SESSION_IDENTITY'
	| 'STALE_GENERATION'
	| 'SESSION_NOT_ACCEPTING';

export interface QuarantinedRecord {
	id: string;
	tenant_id: string;
	reason: QuarantineReason;
	detail: string;
	device_id: string;
	generation: number;
	external_session_id: string;
	sequence: number;
	payload_hash: string;
	conflicting_record_id?: string;
	captured_at: string;
	received_at: string;
	resolved: boolean;
	resolution?: string;
	resolved_at?: string;
	resolved_by?: string;
}

export interface ListQuarantinedRequest {
	tenant_id: string;
	reason?: string;
	limit?: number;
	offset?: number;
}
export interface ListQuarantinedResponse {
	records: QuarantinedRecord[];
}

export interface ResolveQuarantineRequest {
	id: string;
	tenant_id: string;
	resolution: string;
	actor: string;
}
export interface ResolveQuarantineResponse {
	record: QuarantinedRecord;
}

/* ---- mass balance reconciliation ---- */

export type WindowStatus = 'OPEN' | 'RECONCILED' | 'ACCEPTED';

export interface BalanceWindow {
	id: string;
	tenant_id: string;
	route_ref: string;
	period_start: string;
	period_end: string;
	unit: string;
	status: WindowStatus;
	created_at: string;
	updated_at: string;
}

export interface Flow {
	id: string;
	tenant_id: string;
	window_id: string;
	flow_id: string;
	/** The empty string is the system boundary — milk entering or leaving the network. */
	from_node: string;
	from_node_kind?: string;
	to_node: string;
	to_node_kind?: string;
	/** A decimal literal, not a number: a JSON float would already have lost the third decimal. */
	measured: string;
	standard_uncertainty?: string;
	unmeasured: boolean;
	observation_ref?: string;
	created_at: string;
}

export interface ReconciledFlow {
	flow_id: string;
	measured: string;
	reconciled: string;
	adjustment: string;
	test_statistic: number;
	gross_error: boolean;
	unmeasured: boolean;
}

export interface ReconciliationRun {
	id: string;
	tenant_id: string;
	window_id: string;
	converged: boolean;
	residual_before: string;
	/** Absent when no model answered; the window's imbalance is still in residual_before. */
	residual_after?: string;
	model_version?: string;
	gross_error_threshold?: number;
	reason?: string;
	flows: ReconciledFlow[];
	suspect_flow_ids?: string[];
	accepted_at?: string;
	accepted_by?: string;
	created_at: string;
}

export interface ListWindowsRequest {
	tenant_id: string;
	status?: string;
	limit?: number;
	offset?: number;
}
export interface ListWindowsResponse {
	windows: BalanceWindow[];
}

export interface ListFlowsRequest {
	tenant_id: string;
	window_id: string;
}
export interface ListFlowsResponse {
	flows: Flow[];
}

export interface ListRunsRequest {
	tenant_id: string;
	window_id?: string;
	limit?: number;
	offset?: number;
}
export interface ListRunsResponse {
	runs: ReconciliationRun[];
}

export interface GetRunRequest {
	tenant_id: string;
	id: string;
}
export interface GetRunResponse {
	run: ReconciliationRun;
}

export interface ReconcileRequest {
	tenant_id: string;
	window_id: string;
	gross_error_threshold?: number;
	actor: string;
}
export interface ReconcileResponse {
	run: ReconciliationRun;
}

export interface AcceptRunRequest {
	tenant_id: string;
	run_id: string;
	actor: string;
}
export interface AcceptRunResponse {
	run: ReconciliationRun;
}

/* ------------------------------------------------------------------------- *
 * The rest of the spine.
 *
 * Eighteen procedures the console never called. They were not the reviewing
 * screens' work — these are the ones that put data in, ask a window what its
 * instruments can establish, and read a mapping's whole history rather than one
 * instant of it.
 * ------------------------------------------------------------------------- */

/* ---- canonical: policies, slots, identity history ---- */

export interface SlotPolicy {
	id: string;
	tenant_id: string;
	name: string;
	/** What makes a slot: the fields whose values together identify one. */
	dimensions: string[];
	/** How a second claim on an occupied slot is settled. */
	resolution: string;
	version: number;
	effective_from: string;
	effective_to?: string;
}

export interface DeclarePolicyRequest {
	tenant_id: string;
	name: string;
	dimensions: string[];
	resolution: string;
	version: number;
	effective_from: string;
	effective_to?: string;
	actor: string;
}
export interface DeclarePolicyResponse {
	policy: SlotPolicy;
}

export interface GetEffectivePolicyRequest {
	tenant_id: string;
	/** Empty means now. A collection from March wants March's policy. */
	at?: string;
}
export interface GetEffectivePolicyResponse {
	policy: SlotPolicy;
}

export interface ListPoliciesRequest {
	tenant_id: string;
}
export interface ListPoliciesResponse {
	policies: SlotPolicy[];
}

export interface ClaimSlotRequest {
	tenant_id: string;
	source_ref: string;
	values: Record<string, string>;
	origin: string;
	/** Orders claims under the first- and last-wins policies. */
	recorded_at: string;
	quality?: number;
	/** Selects the policy version in force when the milk was collected. */
	collected_at?: string;
	actor: string;
}
export interface ClaimSlotResponse {
	outcome: string;
	reason: string;
	slot: CollectionSlot;
}

export interface GetSlotRequest {
	tenant_id: string;
	slot_key: string;
	origin_kind: string;
}
export interface GetSlotResponse {
	slot: CollectionSlot;
}

export interface ReverseResolveRequest {
	tenant_id: string;
	entity_kind: EntityKind;
	entity_id: string;
}
export interface ReverseResolveResponse {
	identities: ExternalIdentity[];
}

/**
 * What one external identifier has ever meant.
 *
 * The same four fields that identify a mapping and no as_of, because the point
 * is every instant rather than one of them.
 */
export interface GetIdentityHistoryRequest {
	tenant_id: string;
	source_system_id: string;
	entity_kind: EntityKind;
	external_id: string;
}
export interface GetIdentityHistoryResponse {
	identities: ExternalIdentity[];
}

export interface RetireIdentityRequest {
	tenant_id: string;
	id: string;
	actor: string;
}
export interface RetireIdentityResponse {
	retired: boolean;
}

/* ---- ingestion: devices, sessions, one quarantined record ---- */

export interface DeviceGeneration {
	id: string;
	device_id: string;
	generation: number;
	reason: string;
	opened_at: string;
	closed_at?: string;
}

export interface ListGenerationsRequest {
	tenant_id: string;
	device_id: string;
}
export interface ListGenerationsResponse {
	generations: DeviceGeneration[];
}

export interface DeviceSession {
	id: string;
	tenant_id: string;
	device_id: string;
	generation: number;
	external_session_id: string;
	operator_ref: string;
	status: string;
	opened_at: string;
	closed_at?: string;
	last_sequence: number;
	record_count: number;
}

export interface ListDeviceSessionsRequest {
	tenant_id: string;
	device_id: string;
	limit: number;
	offset: number;
}
export interface ListDeviceSessionsResponse {
	sessions: DeviceSession[];
}

export interface GetQuarantinedRequest {
	id: string;
	tenant_id: string;
}
export interface GetQuarantinedResponse {
	record: QuarantinedRecord;
	/**
	 * The payload in full, so a reviewer can compare it against the record it
	 * collided with. Typed unknown rather than a shape: it is whatever the
	 * device sent, which is the whole reason it is quarantined.
	 */
	payload: unknown;
}

/* ---- balance: windows, flows, observability ---- */

export interface CreateWindowRequest {
	tenant_id: string;
	route_ref: string;
	period_start: string;
	period_end: string;
	/** LITRES or KILOGRAMS. Every flow in the window is in it. */
	unit: string;
	actor: string;
}
export interface CreateWindowResponse {
	window: BalanceWindow;
}

export interface GetWindowRequest {
	tenant_id: string;
	id: string;
}
export interface GetWindowResponse {
	window: BalanceWindow;
}

export interface AddFlowRequest {
	tenant_id: string;
	window_id: string;
	flow_id: string;
	/** Node ids; the empty string is the system boundary. */
	from_node: string;
	from_node_kind?: string;
	to_node: string;
	to_node_kind?: string;
	/** Decimal literals, never numbers. */
	measured: string;
	standard_uncertainty?: string;
	unmeasured?: boolean;
	observation_ref?: string;
	actor: string;
}
export interface AddFlowResponse {
	flow: Flow;
}

export interface ObservabilityRequest {
	tenant_id: string;
	window_id: string;
}

/**
 * What this window's instruments can and cannot establish, before any milk is
 * compared.
 *
 * Asked before a route runs rather than after, which is the useful time to find
 * out that the only leg anybody can verify is the tanker.
 */
export interface ObservabilityResponse {
	/** Unmeasured legs the node balances determine uniquely. */
	observable: string[];
	/**
	 * Unmeasured legs they do not. The reconciler still prints a figure for
	 * these; it is one of infinitely many that fit.
	 */
	unobservable: string[];
	/** Measured legs computable from the others, so a gross error is detectable. */
	redundant: string[];
	/**
	 * Measured legs that are not. Nothing in this window disagrees with them
	 * however wrong they are, which makes "the window reconciled" a much weaker
	 * statement than it sounds.
	 */
	just_determined: string[];
	fully_observable: boolean;
	fully_redundant: boolean;
}

/* ---- shadow settlement: what goes in before anything is compared ---- */

export interface SettlementComponent {
	kind: string;
	label?: string;
	/** A decimal literal; the scale comes from the enclosing settlement. */
	amount: string;
	quantity?: string;
	rate?: string;
}

export interface Assertion {
	id: string;
	tenant_id: string;
	source_system_id: string;
	external_settlement_id: string;
	producer_ref: string;
	period_start: string;
	period_end: string;
	currency: string;
	amount_scale: number;
	total: string;
	components: SettlementComponent[];
	asserted_at: string;
	origin_kind: string;
	import_batch_id: string;
	source_record_id: string;
	source_payload_hash: string;
	valid_from: string;
	valid_to: string;
	recorded_at: string;
	superseded_at?: string;
}

export interface IngestAssertionRequest {
	tenant_id: string;
	source_system_id: string;
	external_settlement_id: string;
	producer_ref: string;
	period_start: string;
	period_end: string;
	currency: string;
	amount_scale: number;
	total: string;
	components: SettlementComponent[];
	asserted_at: string;
	import_batch_id: string;
	source_record_id: string;
	/** The source record verbatim. Hashed for replay detection, never persisted. */
	raw_payload: unknown;
	created_by: string;
}
export interface IngestAssertionResponse {
	assertion: Assertion;
	/**
	 * False when the payload had already been ingested, which makes a replayed
	 * import batch observably idempotent to the caller.
	 */
	created: boolean;
}

export interface Computation {
	id: string;
	tenant_id: string;
	assertion_id?: string;
	producer_ref: string;
	period_start: string;
	period_end: string;
	currency: string;
	amount_scale: number;
	total: string;
	components: SettlementComponent[];
	policy_version: string;
	rate_card_id: string;
	input_digest: string;
	as_of: string;
	created_at: string;
}

export interface RecordComputationRequest {
	tenant_id: string;
	assertion_id: string;
	producer_ref: string;
	period_start: string;
	period_end: string;
	currency: string;
	amount_scale: number;
	total: string;
	components: SettlementComponent[];
	policy_version: string;
	rate_card_id: string;
	input_digest: string;
	as_of: string;
	created_by: string;
}
export interface RecordComputationResponse {
	computation: Computation;
}

export interface AdjudicateRequest {
	tenant_id: string;
	assertion_id: string;
	computation_id: string;
	actor: string;
}
export interface AdjudicateResponse {
	divergence: Divergence;
}
