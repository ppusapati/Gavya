import type { ApiClient, CallOptions } from './client';
import * as T from './types';

/**
 * The plant, as procedures: what moved, what was made, what was tested, what
 * was measured.
 *
 * Four services that had no client at all — material, production, laboratory
 * and observation. A sub-facade rather than more methods on Gavya, for the
 * reason HerdApi gives: one class with every procedure on it is one nobody can
 * read.
 *
 * Nothing here supplies a unit, a density, a rounding mode or a tolerance on the
 * caller's behalf. Every one of those is a decision the services deliberately
 * refuse to make, and a client that quietly made it would be putting the
 * platform's opinion into a plant's report.
 */
export class PlantApi {
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

	/* ---- material: nodes ---- */

	registerNode(req: Omit<T.RegisterNodeRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RegisterNodeRequest, T.NodeResponse>(
			`${T.MATERIAL}/RegisterNode`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getNode(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetNodeRequest, T.NodeResponse>(
			`${T.MATERIAL}/GetNode`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	listNodes(kind = '', extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListNodesRequest, T.ListNodesResponse>(
			`${T.MATERIAL}/ListNodes`,
			{ tenant_id: this.#tenantId, kind },
			this.#opts(extra)
		);
	}

	/* ---- material: movements ---- */

	dispatch(req: Omit<T.DispatchRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DispatchRequest, T.MovementResponse>(
			`${T.MATERIAL}/Dispatch`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	receive(req: Omit<T.ReceiveRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ReceiveRequest, T.MovementResponse>(
			`${T.MATERIAL}/Receive`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	abandonMovement(
		req: Omit<T.AbandonMovementRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.AbandonMovementRequest, T.MovementResponse>(
			`${T.MATERIAL}/AbandonMovement`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getMovement(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetMovementRequest, T.MovementResponse>(
			`${T.MATERIAL}/GetMovement`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	listMovements(req: Omit<T.ListMovementsRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListMovementsRequest, T.ListMovementsResponse>(
			`${T.MATERIAL}/ListMovements`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- material: instruments and flows ---- */

	registerInstrument(
		req: Omit<T.RegisterInstrumentRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RegisterInstrumentRequest, T.InstrumentResponse>(
			`${T.MATERIAL}/RegisterInstrument`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listInstruments(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListInstrumentsRequest, T.ListInstrumentsResponse>(
			`${T.MATERIAL}/ListInstruments`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/**
	 * Shape a period's movements for balance-service.
	 *
	 * `rounding` has no default here because it has none there: it is a small
	 * effect on one flow and it decides which of two nearly-equal legs the
	 * reconciler blames when a window does not close.
	 */
	proposeFlows(req: Omit<T.ProposeFlowsRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ProposeFlowsRequest, T.ProposeFlowsResponse>(
			`${T.MATERIAL}/ProposeFlows`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- production: batches ---- */

	createBatch(
		req: Omit<T.CreateProductionBatchRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.CreateProductionBatchRequest, T.ProductionBatchResponse>(
			`${T.PRODUCTION}/CreateBatch`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** By id, or by the code written on the vessel — whichever the caller has. */
	getBatch(req: { id?: string; code?: string }, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetProductionBatchRequest, T.ProductionBatchResponse>(
			`${T.PRODUCTION}/GetBatch`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listBatches(
		req: Omit<T.ListProductionBatchesRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListProductionBatchesRequest, T.ListProductionBatchesResponse>(
			`${T.PRODUCTION}/ListBatches`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	setBatchStatus(
		req: Omit<T.SetProductionBatchStatusRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.SetProductionBatchStatusRequest, T.ProductionBatchResponse>(
			`${T.PRODUCTION}/SetBatchStatus`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- production: genealogy ---- */

	recordInput(req: Omit<T.RecordInputRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordInputRequest, T.RecordInputResponse>(
			`${T.PRODUCTION}/RecordInput`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getBatchGenealogy(req: { id?: string; code?: string }, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetBatchGenealogyRequest, T.GetBatchGenealogyResponse>(
			`${T.PRODUCTION}/GetBatchGenealogy`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * Walk the genealogy in one direction. The direction is the caller's to
	 * name: FORWARD and BACKWARD answer different questions, and picking one
	 * would answer the wrong one silently.
	 */
	traceBatch(req: Omit<T.TraceBatchRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.TraceBatchRequest, T.TraceBatchResponse>(
			`${T.PRODUCTION}/TraceBatch`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getBatchYield(req: Omit<T.GetBatchYieldRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetBatchYieldRequest, T.GetBatchYieldResponse>(
			`${T.PRODUCTION}/GetBatchYield`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- production: recipes ---- */

	createFormulation(
		req: Omit<T.CreateFormulationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.CreateFormulationRequest, T.FormulationResponse>(
			`${T.PRODUCTION}/CreateFormulation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	approveFormulation(
		req: Omit<T.ApproveFormulationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ApproveFormulationRequest, T.FormulationResponse>(
			`${T.PRODUCTION}/ApproveFormulation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	withdrawFormulation(
		req: Omit<T.WithdrawFormulationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.WithdrawFormulationRequest, T.FormulationResponse>(
			`${T.PRODUCTION}/WithdrawFormulation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** By id, or by code and the moment to look the version up for. */
	getFormulation(req: { id?: string; code?: string; at?: string }, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetFormulationRequest, T.FormulationResponse>(
			`${T.PRODUCTION}/GetFormulation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listFormulations(extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListFormulationsRequest, T.ListFormulationsResponse>(
			`${T.PRODUCTION}/ListFormulations`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	checkRecipe(req: { id?: string; code?: string }, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CheckRecipeRequest, T.CheckRecipeResponse>(
			`${T.PRODUCTION}/CheckRecipe`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * What this plant's own vats actually yielded.
	 *
	 * The platform holds no table of standard yields and will not invent one.
	 * This is the alternative: show a plant its own history and let it decide.
	 */
	getObservedYield(
		req: { id?: string; code?: string; at?: string },
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.GetObservedYieldRequest, T.GetObservedYieldResponse>(
			`${T.PRODUCTION}/GetObservedYield`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- laboratory ---- */

	drawSample(req: Omit<T.DrawSampleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DrawSampleRequest, T.SampleResponse>(
			`${T.LABORATORY}/DrawSample`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getSample(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetSampleRequest, T.SampleResponse>(
			`${T.LABORATORY}/GetSample`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	listSamples(req: Omit<T.ListSamplesRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListSamplesRequest, T.ListSamplesResponse>(
			`${T.LABORATORY}/ListSamples`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	breakSeal(req: Omit<T.BreakSealRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.BreakSealRequest, T.SampleResponse>(
			`${T.LABORATORY}/BreakSeal`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordHandover(req: Omit<T.RecordHandoverRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordHandoverRequest, T.HandoverResponse>(
			`${T.LABORATORY}/RecordHandover`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordResult(req: Omit<T.RecordResultRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordResultRequest, T.ResultResponse>(
			`${T.LABORATORY}/RecordResult`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getSampleReport(sampleId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetSampleReportRequest, T.GetSampleReportResponse>(
			`${T.LABORATORY}/GetSampleReport`,
			{ tenant_id: this.#tenantId, sample_id: sampleId },
			this.#opts(extra)
		);
	}

	/* ---- observation ---- */

	recordObservation(
		req: Omit<T.RecordObservationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordObservationRequest, T.RecordObservationResponse>(
			`${T.OBSERVATION}/RecordObservation`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getObservation(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetObservationRequest, T.GetObservationResponse>(
			`${T.OBSERVATION}/GetObservation`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	listObservationsForSubject(
		req: Omit<T.ListObservationsForSubjectRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<
			T.ListObservationsForSubjectRequest,
			T.ListObservationsForSubjectResponse
		>(
			`${T.OBSERVATION}/ListObservationsForSubject`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listFlaggedObservations(
		req: { limit?: number; offset?: number } = {},
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListFlaggedObservationsRequest, T.ListFlaggedObservationsResponse>(
			`${T.OBSERVATION}/ListFlaggedObservations`,
			{ tenant_id: this.#tenantId, limit: req.limit ?? 50, offset: req.offset ?? 0 },
			this.#opts(extra)
		);
	}

	/**
	 * Register a meter. Named apart from registerInstrument above because these
	 * are two registers of different things: material-service holds the
	 * instruments a movement is weighed on, and observation-service holds the
	 * meters a reading is taken with, each with its own certificate regime.
	 */
	registerMeter(req: Omit<T.RegisterMeterRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RegisterMeterRequest, T.RegisterMeterResponse>(
			`${T.OBSERVATION}/RegisterInstrument`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getMeter(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetMeterRequest, T.GetMeterResponse>(
			`${T.OBSERVATION}/GetInstrument`,
			{ tenant_id: this.#tenantId, id },
			this.#opts(extra)
		);
	}

	recordCertificate(
		req: Omit<T.RecordCertificateRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordCertificateRequest, T.RecordCertificateResponse>(
			`${T.OBSERVATION}/RecordCertificate`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * The certificate in force, and — when a quantity is named — the verdict a
	 * settlement would record against it, without recording anything.
	 */
	getActiveCertificate(
		req: Omit<T.GetActiveCertificateRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.GetActiveCertificateRequest, T.GetActiveCertificateResponse>(
			`${T.OBSERVATION}/GetActiveCertificate`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}
}
