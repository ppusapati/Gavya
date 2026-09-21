import { ApiClient, ApiError, type CallOptions } from './client';
import { HerdApi } from './herd';
import * as T from './types';

export { ApiClient, ApiError, HerdApi };
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

	constructor(client: ApiClient, session: string, tenantId: string) {
		this.#client = client;
		this.#session = session;
		this.#tenantId = tenantId;
		this.herd = new HerdApi(client, session, tenantId);
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
}
