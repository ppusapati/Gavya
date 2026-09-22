import { AdminApi } from './admin';
import { ApiClient, ApiError, type CallOptions } from './client';
import { HerdApi } from './herd';
import { IdentityApi } from './identity';
import { CommerceApi } from './commerce';
import { MoneyApi } from './money';
import { PlantApi } from './plant';
import * as T from './types';

export { AdminApi, ApiClient, ApiError, CommerceApi, HerdApi, IdentityApi, MoneyApi, PlantApi };
export * from './types';

/**
 * Facade over the procedures the workspaces actually use.
 *
 * Every method names its service explicitly, so a reader can see which service
 * answers a screen without tracing through a router.
 */
export class Gavya {
	readonly #client: ApiClient;
	readonly #tenantId: string;
	readonly #session: string;

	/**
	 * @param session the session id from SignIn. Every call carries it as a
	 * bearer token, because the gateway refuses anything that does not.
	 * @param tenantId the tenant that session belongs to. It goes in request
	 * bodies, where the services read it; it is not sent as a header, because
	 * the gateway asserts the tenant itself from the session and strips any
	 * claim arriving with the request.
	 */
	/**
	 * The herd and the morning's work: cattle, milk, breeding, health, feed and
	 * farms.
	 *
	 * A sub-facade rather than three dozen more methods here. This class covers
	 * the integrity spine — the four services the console was built for — and the
	 * platform has twenty-eight; one class with every procedure on it is one
	 * nobody can read.
	 */
	readonly herd: HerdApi;

	/** Rate cards, priced collections, pools, settlement and billing. */
	readonly money: MoneyApi;

	/** The catalogue, the stock, the order book and the cattle market. */
	readonly commerce: CommerceApi;

	/**
	 * The plant: movements between vessels, batches and their genealogy, the
	 * laboratory's samples, and the meters a reading is taken with.
	 */
	readonly plant: PlantApi;

	/**
	 * Tenants and their settings, the audit chain, the inbox, reports and the
	 * file register.
	 */
	readonly admin: AdminApi;

	/**
	 * People, roles, machine credentials and the session itself.
	 *
	 * The console could sign in and do nothing else here: thirteen of this
	 * service's fifteen procedures had no caller anywhere, so a co-operative was
	 * authorised against roles nobody could be given.
	 */
	readonly identity: IdentityApi;

	constructor(client: ApiClient, session: string, tenantId: string) {
		this.#client = client;
		this.#session = session;
		this.#tenantId = tenantId;
		this.herd = new HerdApi(client, session, tenantId);
		this.money = new MoneyApi(client, session, tenantId);
		this.commerce = new CommerceApi(client, session, tenantId);
		this.plant = new PlantApi(client, session, tenantId);
		this.admin = new AdminApi(client, session, tenantId);
		this.identity = new IdentityApi(client, session, tenantId);
	}

	#opts(extra?: Partial<CallOptions>): CallOptions {
		return { session: this.#session, ...extra };
	}

	/**
	 * Exchange an email and password for a session.
	 *
	 * Static because it is the one call made before there is a session to make
	 * calls with, and it is one of the three procedures the gateway lets through
	 * unauthenticated.
	 */
	static signIn(
		client: ApiClient,
		req: T.SignInRequest,
		extra?: Partial<CallOptions>
	): Promise<T.SignInResponse> {
		return client.call<T.SignInRequest, T.SignInResponse>(
			`${T.IDENTITY}/SignIn`,
			req,
			{ ...extra }
		);
	}

	listDivergences(req: Omit<T.ListDivergencesRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListDivergencesRequest, T.ListDivergencesResponse>(
			`${T.SHADOW_SETTLEMENT}/ListDivergences`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getDivergence(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetDivergenceRequest, T.GetDivergenceResponse>(
			`${T.SHADOW_SETTLEMENT}/GetDivergence`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	resolveDivergence(
		req: Omit<T.ResolveDivergenceRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ResolveDivergenceRequest, T.ResolveDivergenceResponse>(
			`${T.SHADOW_SETTLEMENT}/ResolveDivergence`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	summarise(from: string, to: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.SummariseRequest, T.SummariseResponse>(
			`${T.SHADOW_SETTLEMENT}/Summarise`,
			{ tenant_id: this.#tenantId, from, to },
			this.#opts(extra)
		);
	}

	listIdentities(req: Omit<T.ListIdentitiesRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListIdentitiesRequest, T.ListIdentitiesResponse>(
			`${T.CANONICAL}/ListIdentities`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	resolveIdentity(
		req: Omit<T.ResolveIdentityRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ResolveIdentityRequest, T.ResolveIdentityResponse>(
			`${T.CANONICAL}/ResolveIdentity`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	mapIdentity(req: Omit<T.MapIdentityRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.MapIdentityRequest, T.MapIdentityResponse>(
			`${T.CANONICAL}/MapIdentity`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listSlotConflicts(req: Omit<T.ListConflictsRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListConflictsRequest, T.ListConflictsResponse>(
			`${T.CANONICAL}/ListConflicts`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	resolveSlotConflict(
		req: Omit<T.ResolveConflictRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ResolveConflictRequest, T.ResolveConflictResponse>(
			`${T.CANONICAL}/ResolveConflict`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listWindows(req: Omit<T.ListWindowsRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListWindowsRequest, T.ListWindowsResponse>(
			`${T.BALANCE}/ListWindows`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listFlows(windowId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListFlowsRequest, T.ListFlowsResponse>(
			`${T.BALANCE}/ListFlows`,
			{ tenant_id: this.#tenantId, window_id: windowId },
			this.#opts(extra)
		);
	}

	listRuns(req: Omit<T.ListRunsRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListRunsRequest, T.ListRunsResponse>(
			`${T.BALANCE}/ListRuns`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getRun(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetRunRequest, T.GetRunResponse>(
			`${T.BALANCE}/GetRun`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	reconcile(req: Omit<T.ReconcileRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ReconcileRequest, T.ReconcileResponse>(
			`${T.BALANCE}/Reconcile`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	acceptRun(req: Omit<T.AcceptRunRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AcceptRunRequest, T.AcceptRunResponse>(
			`${T.BALANCE}/AcceptRun`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listQuarantined(req: Omit<T.ListQuarantinedRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListQuarantinedRequest, T.ListQuarantinedResponse>(
			`${T.INGESTION}/ListQuarantined`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	resolveQuarantine(
		req: Omit<T.ResolveQuarantineRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ResolveQuarantineRequest, T.ResolveQuarantineResponse>(
			`${T.INGESTION}/ResolveQuarantine`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- canonical: policies and slots ---- */

	declarePolicy(req: Omit<T.DeclarePolicyRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DeclarePolicyRequest, T.DeclarePolicyResponse>(
			`${T.CANONICAL}/DeclarePolicy`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** The policy in force at an instant. Empty `at` means now. */
	getEffectivePolicy(at?: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetEffectivePolicyRequest, T.GetEffectivePolicyResponse>(
			`${T.CANONICAL}/GetEffectivePolicy`,
			{ tenant_id: this.#tenantId, at },
			this.#opts(extra)
		);
	}

	listPolicies(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListPoliciesRequest, T.ListPoliciesResponse>(
			`${T.CANONICAL}/ListPolicies`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	claimSlot(req: Omit<T.ClaimSlotRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ClaimSlotRequest, T.ClaimSlotResponse>(
			`${T.CANONICAL}/ClaimSlot`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getSlot(slotKey: string, originKind: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetSlotRequest, T.GetSlotResponse>(
			`${T.CANONICAL}/GetSlot`,
			{ tenant_id: this.#tenantId, slot_key: slotKey, origin_kind: originKind },
			this.#opts(extra)
		);
	}

	/** Every external identifier that has ever pointed at this entity. */
	reverseResolve(
		req: Omit<T.ReverseResolveRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ReverseResolveRequest, T.ReverseResolveResponse>(
			`${T.CANONICAL}/ReverseResolve`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * Everything one external identifier has ever meant.
	 *
	 * No as_of, unlike resolveIdentity: the point is every instant rather than
	 * one of them, which is what makes a settlement from January explainable
	 * after somebody corrects who AMCU-0417 is.
	 */
	getIdentityHistory(
		req: Omit<T.GetIdentityHistoryRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.GetIdentityHistoryRequest, T.GetIdentityHistoryResponse>(
			`${T.CANONICAL}/GetIdentityHistory`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** Close a mapping. It is superseded, never deleted. */
	retireIdentity(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RetireIdentityRequest, T.RetireIdentityResponse>(
			`${T.CANONICAL}/RetireIdentity`,
			{ tenant_id: this.#tenantId, id, actor },
			this.#opts(extra)
		);
	}

	/* ---- ingestion: devices and one record ---- */

	listGenerations(deviceId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListGenerationsRequest, T.ListGenerationsResponse>(
			`${T.INGESTION}/ListGenerations`,
			{ tenant_id: this.#tenantId, device_id: deviceId },
			this.#opts(extra)
		);
	}

	listSessions(
		req: Omit<T.ListDeviceSessionsRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListDeviceSessionsRequest, T.ListDeviceSessionsResponse>(
			`${T.INGESTION}/ListSessions`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** One quarantined record, with the payload in full. */
	getQuarantined(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetQuarantinedRequest, T.GetQuarantinedResponse>(
			`${T.INGESTION}/GetQuarantined`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/* ---- balance: building a window ---- */

	createWindow(req: Omit<T.CreateWindowRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateWindowRequest, T.CreateWindowResponse>(
			`${T.BALANCE}/CreateWindow`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getWindow(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetWindowRequest, T.GetWindowResponse>(
			`${T.BALANCE}/GetWindow`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	addFlow(req: Omit<T.AddFlowRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AddFlowRequest, T.AddFlowResponse>(
			`${T.BALANCE}/AddFlow`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * What this window's instruments can establish, before any milk is
	 * compared. The useful time to ask is before a route runs.
	 */
	observability(windowId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ObservabilityRequest, T.ObservabilityResponse>(
			`${T.BALANCE}/Observability`,
			{ tenant_id: this.#tenantId, window_id: windowId },
			this.#opts(extra)
		);
	}

	/* ---- shadow settlement: what goes in ---- */

	ingestAssertion(
		req: Omit<T.IngestAssertionRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.IngestAssertionRequest, T.IngestAssertionResponse>(
			`${T.SHADOW_SETTLEMENT}/IngestAssertion`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordComputation(
		req: Omit<T.RecordComputationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordComputationRequest, T.RecordComputationResponse>(
			`${T.SHADOW_SETTLEMENT}/RecordComputation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** Compare one assertion against one computation and record the verdict. */
	adjudicate(req: Omit<T.AdjudicateRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AdjudicateRequest, T.AdjudicateResponse>(
			`${T.SHADOW_SETTLEMENT}/Adjudicate`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}
}
