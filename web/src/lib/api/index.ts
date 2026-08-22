import { ApiClient, ApiError, type CallOptions } from './client';
import * as T from './types';

export { ApiClient, ApiError };
export * from './types';

/** Formats minor units for display, e.g. 5000 at scale 2 in INR -> "50.00". */
export function formatMinorUnits(minorUnits: number, scale: number): string {
	const negative = minorUnits < 0;
	const digits = Math.abs(minorUnits).toString().padStart(scale + 1, '0');
	const whole = digits.slice(0, digits.length - scale);
	const frac = scale > 0 ? '.' + digits.slice(digits.length - scale) : '';
	return `${negative ? '-' : ''}${whole}${frac}`;
}

/**
 * Facade over the procedures the workspaces actually use.
 *
 * Every method names its service explicitly, so a reader can see which service
 * answers a screen without tracing through a router.
 */
export class Gavya {
	readonly #client: ApiClient;
	readonly #tenantId: string;

	constructor(client: ApiClient, tenantId: string) {
		this.#client = client;
		this.#tenantId = tenantId;
	}

	#opts(extra?: Partial<CallOptions>): CallOptions {
		return { tenantId: this.#tenantId, ...extra };
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
