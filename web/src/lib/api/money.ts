import type { ApiClient, CallOptions } from './client';
import * as T from './types';

/**
 * The money path, as procedures: procurement, pooling, settlement, billing.
 *
 * Forty-eight procedures across four services, and the reason this is the group
 * worth building first after the herd: settlement is what the platform is for.
 * Everything else here records something; this decides what somebody is paid.
 *
 * Every amount goes out as the string the operator typed. The services parse it
 * themselves at the scale they hold, and a browser that turned "693.60" into a
 * double on the way would be rounding a figure at the one edge of the system
 * that exists to stop that.
 */
export class MoneyApi {
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

	/* ---- procurement: what milk is worth ---- */

	listRateCards(req: { limit?: number; offset?: number } = {}, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListRateCardsRequest, T.ListRateCardsResponse>(
			`${T.PROCUREMENT}/ListRateCards`,
			{ tenant_id: this.#tenantId, limit: req.limit ?? 50, offset: req.offset ?? 0 },
			this.#opts(extra)
		);
	}

	getRateCard(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; id: string }, T.RateCardResponse>(
			`${T.PROCUREMENT}/GetRateCard`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	/** Which card priced a collection on a given day. */
	getRateCardInForce(at: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetRateCardInForceRequest, T.RateCardResponse>(
			`${T.PROCUREMENT}/GetRateCardInForce`,
			{ tenant_id: this.#tenantId, at },
			this.#opts(extra)
		);
	}

	declareRateCard(req: Omit<T.DeclareRateCardRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DeclareRateCardRequest, T.RateCardResponse>(
			`${T.PROCUREMENT}/DeclareRateCard`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listCollections(
		req: Omit<T.ListCollectionsRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListCollectionsRequest, T.ListCollectionsResponse>(
			`${T.PROCUREMENT}/ListCollections`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getCollection(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; id: string }, T.CollectionResponse>(
			`${T.PROCUREMENT}/GetCollection`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	recordCollection(req: Omit<T.RecordCollectionRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordCollectionRequest, T.CollectionResponse>(
			`${T.PROCUREMENT}/RecordCollection`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** A correction supersedes; it never edits. The reason is required. */
	correctCollection(
		req: Omit<T.CorrectCollectionRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.CorrectCollectionRequest, T.CorrectCollectionResponse>(
			`${T.PROCUREMENT}/CorrectCollection`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getCollectionVersions(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; id: string }, T.GetCollectionVersionsResponse>(
			`${T.PROCUREMENT}/GetCollectionVersions`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	/* ---- pooling ---- */

	listPools(req: Omit<T.ListPoolsRequest, 'tenant_id'> = {}, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListPoolsRequest, T.ListPoolsResponse>(
			`${T.POOLING}/ListPools`,
			{ tenant_id: this.#tenantId, limit: 50, ...req },
			this.#opts(extra)
		);
	}

	getPool(poolId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; pool_id: string }, T.PoolResponse>(
			`${T.POOLING}/GetPool`,
			{ tenant_id: this.#tenantId, pool_id: poolId },
			this.#opts(extra)
		);
	}

	createPool(req: Omit<T.CreatePoolRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreatePoolRequest, T.PoolResponse>(
			`${T.POOLING}/CreatePool`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listProducerMilk(poolId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<
			{ tenant_id: string; pool_id: string },
			T.ListProducerMilkResponse
		>(
			`${T.POOLING}/ListProducerMilk`,
			{ tenant_id: this.#tenantId, pool_id: poolId },
			this.#opts(extra)
		);
	}

	addProducerMilk(req: Omit<T.AddProducerMilkRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AddProducerMilkRequest, T.AddProducerMilkResponse>(
			`${T.POOLING}/AddProducerMilk`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listUtilisations(poolId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; pool_id: string }, T.ListUtilisationsResponse>(
			`${T.POOLING}/ListUtilisations`,
			{ tenant_id: this.#tenantId, pool_id: poolId },
			this.#opts(extra)
		);
	}

	recordUtilisation(
		req: Omit<T.RecordUtilisationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordUtilisationRequest, T.RecordUtilisationResponse>(
			`${T.POOLING}/RecordUtilisation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	valuePool(req: Omit<T.ValuePoolRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ValuePoolRequest, T.ValuePoolResponse>(
			`${T.POOLING}/ValuePool`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getValuation(poolId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; pool_id: string }, T.GetValuationResponse>(
			`${T.POOLING}/GetValuation`,
			{ tenant_id: this.#tenantId, pool_id: poolId },
			this.#opts(extra)
		);
	}

	listAllocations(req: Omit<T.ListAllocationsRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListAllocationsRequest, T.ListAllocationsResponse>(
			`${T.POOLING}/ListAllocations`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	settlePool(poolId: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<
			{ tenant_id: string; pool_id: string; actor: string },
			T.SettlePoolResponse
		>(
			`${T.POOLING}/SettlePool`,
			{ tenant_id: this.#tenantId, pool_id: poolId, actor },
			this.#opts(extra)
		);
	}

	listEconomicEvents(
		req: Omit<T.ListEconomicEventsRequest, 'tenant_id'> = {},
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListEconomicEventsRequest, T.ListEconomicEventsResponse>(
			`${T.POOLING}/ListEconomicEvents`,
			{ tenant_id: this.#tenantId, limit: 50, ...req },
			this.#opts(extra)
		);
	}

	declareRetroactivityPolicy(
		req: Omit<T.DeclareRetroactivityPolicyRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<
			T.DeclareRetroactivityPolicyRequest,
			T.RetroactivityPolicyResponse
		>(
			`${T.POOLING}/DeclareRetroactivityPolicy`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getEffectiveRetroactivityPolicy(at?: string, extra?: Partial<CallOptions>) {
		return this.#client.call<
			T.GetEffectiveRetroactivityPolicyRequest,
			T.RetroactivityPolicyResponse
		>(
			`${T.POOLING}/GetEffectiveRetroactivityPolicy`,
			{ tenant_id: this.#tenantId, ...(at ? { at } : {}) },
			this.#opts(extra)
		);
	}

	/** What the retroactivity policy decides about reopening a settled pool. */
	applyCorrection(req: Omit<T.ApplyCorrectionRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ApplyCorrectionRequest, T.ApplyCorrectionResponse>(
			`${T.POOLING}/ApplyCorrection`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- settlement ---- */

	listCycles(req: Omit<T.ListCyclesRequest, 'tenant_id'> = {}, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListCyclesRequest, T.ListCyclesResponse>(
			`${T.SETTLEMENT}/ListCycles`,
			{ tenant_id: this.#tenantId, limit: 50, ...req },
			this.#opts(extra)
		);
	}

	getCycle(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; id: string }, T.CycleResponse>(
			`${T.SETTLEMENT}/GetCycle`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	openCycle(req: Omit<T.OpenCycleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.OpenCycleRequest, T.CycleResponse>(
			`${T.SETTLEMENT}/OpenCycle`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** Gather the collections into payables. Nothing is owed until this runs. */
	gatherCycle(cycleId: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CycleActionRequest, T.CycleResponse>(
			`${T.SETTLEMENT}/GatherCycle`,
			{ tenant_id: this.#tenantId, cycle_id: cycleId, actor },
			this.#opts(extra)
		);
	}

	approveCycle(cycleId: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CycleActionRequest, T.CycleResponse>(
			`${T.SETTLEMENT}/ApproveCycle`,
			{ tenant_id: this.#tenantId, cycle_id: cycleId, actor },
			this.#opts(extra)
		);
	}

	abandonCycle(cycleId: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CycleActionRequest, T.AbandonCycleResponse>(
			`${T.SETTLEMENT}/AbandonCycle`,
			{ tenant_id: this.#tenantId, cycle_id: cycleId, actor },
			this.#opts(extra)
		);
	}

	listPayables(cycleId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListPayablesRequest, T.ListPayablesResponse>(
			`${T.SETTLEMENT}/ListPayables`,
			{ tenant_id: this.#tenantId, cycle_id: cycleId },
			this.#opts(extra)
		);
	}

	getPayable(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string; id: string }, T.PayableResponse>(
			`${T.SETTLEMENT}/GetPayable`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	approvePayable(id: string, actor: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ApprovePayableRequest, T.PayableResponse>(
			`${T.SETTLEMENT}/ApprovePayable`,
			{ tenant_id: this.#tenantId, id, actor },
			this.#opts(extra)
		);
	}

	markPaid(req: Omit<T.MarkPaidRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.MarkPaidRequest, T.PayableResponse>(
			`${T.SETTLEMENT}/MarkPaid`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** Holding a payment requires a reason, because a member will ask for one. */
	holdPayable(req: Omit<T.HoldPayableRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.HoldPayableRequest, T.PayableResponse>(
			`${T.SETTLEMENT}/HoldPayable`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	raiseAdjustment(req: Omit<T.RaiseAdjustmentRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RaiseAdjustmentRequest, T.PayableResponse>(
			`${T.SETTLEMENT}/RaiseAdjustment`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	openRecovery(req: Omit<T.OpenRecoveryRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.OpenRecoveryRequest, T.RecoveryResponse>(
			`${T.SETTLEMENT}/OpenRecovery`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listRecoveries(
		req: Omit<T.ListRecoveriesRequest, 'tenant_id'> = {},
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListRecoveriesRequest, T.ListRecoveriesResponse>(
			`${T.SETTLEMENT}/ListRecoveries`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getProducerStatement(
		req: Omit<T.GetProducerStatementRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.GetProducerStatementRequest, T.GetProducerStatementResponse>(
			`${T.SETTLEMENT}/GetProducerStatement`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * One call from a payable to everything that caused it.
	 *
	 * The collections gathered into it, the rate card each was priced against,
	 * and the identity mapping that attributed each to the producer — retired
	 * mappings included. It says which services it managed to consult and, where
	 * one would not answer, why: an explanation that quietly left something out
	 * would be worse than none.
	 */
	explainPayable(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ExplainPayableRequest, T.Explanation>(
			`${T.SETTLEMENT}/ExplainPayable`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	printProducerStatement(
		req: Omit<T.PrintStatementRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.PrintStatementRequest, T.PrintStatementResponse>(
			`${T.SETTLEMENT}/PrintProducerStatement`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	printCycleStatements(req: Omit<T.PrintCycleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.PrintCycleRequest, T.PrintCycleResponse>(
			`${T.SETTLEMENT}/PrintCycleStatements`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- billing ---- */

	getOutstandingInvoices(extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string }, T.ListInvoicesResponse>(
			`${T.BILLING}/GetOutstandingInvoices`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createInvoice(req: Omit<T.CreateInvoiceRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateInvoiceRequest, T.InvoiceResponse>(
			`${T.BILLING}/CreateInvoice`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	addInvoiceItem(req: Omit<T.AddInvoiceItemRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AddInvoiceItemRequest, T.InvoiceItemResponse>(
			`${T.BILLING}/AddInvoiceItem`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	sendInvoice(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.InvoiceActionRequest, T.InvoiceResponse>(
			`${T.BILLING}/SendInvoice`,
			{ id, tenant_id: this.#tenantId, updated_by: '' },
			this.#opts(extra)
		);
	}

	voidInvoice(id: string, updatedBy: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.InvoiceActionRequest, T.InvoiceResponse>(
			`${T.BILLING}/VoidInvoice`,
			{ id, tenant_id: this.#tenantId, updated_by: updatedBy },
			this.#opts(extra)
		);
	}

	recordPayment(req: Omit<T.RecordPaymentRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordPaymentRequest, T.PaymentResponse>(
			`${T.BILLING}/RecordPayment`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}
}
