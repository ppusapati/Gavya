import type { ApiClient, CallOptions } from './client';
import * as T from './types';

/**
 * The herd and the morning's work, as procedures.
 *
 * A sub-facade rather than more methods on Gavya, because the alternative is one
 * class of two hundred and eighteen methods. Each of these names its service
 * explicitly, so a reader can see which service answers a screen without tracing
 * through a router — the rule the original facade set and the reason it is worth
 * keeping as this grows.
 *
 * Every method takes the tenant from the session rather than from its caller.
 * The gateway decides it and asserts it downstream; what goes in the body is the
 * same value, because the services read it from there.
 */
export class HerdApi {
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

	/* ---- cattle ---- */

	listCattle(req: { status?: string; limit?: number; offset?: number }, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListCattleRequest, T.ListCattleResponse>(
			`${T.CATTLE}/ListCattle`,
			{
				tenant_id: this.#tenantId,
				status: req.status ?? '',
				limit: req.limit ?? 50,
				offset: req.offset ?? 0
			},
			this.#opts(extra)
		);
	}

	getCattle(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.CattleResponse>(
			`${T.CATTLE}/GetCattle`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createCattle(req: Omit<T.CreateCattleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateCattleRequest, T.CattleResponse>(
			`${T.CATTLE}/CreateCattle`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	updateCattle(req: Omit<T.UpdateCattleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UpdateCattleRequest, T.CattleResponse>(
			`${T.CATTLE}/UpdateCattle`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	deleteCattle(req: Omit<T.DeleteCattleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DeleteCattleRequest, T.DeleteCattleResponse>(
			`${T.CATTLE}/DeleteCattle`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listBreeds(extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantRequest, T.ListBreedsResponse>(
			`${T.CATTLE}/ListBreeds`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createBreed(req: Omit<T.CreateBreedRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateBreedRequest, T.BreedResponse>(
			`${T.CATTLE}/CreateBreed`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- milk ---- */

	listSessions(req: { limit?: number; offset?: number }, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListSessionsRequest, T.ListSessionsResponse>(
			`${T.MILK}/ListSessions`,
			{ tenant_id: this.#tenantId, limit: req.limit ?? 50, offset: req.offset ?? 0 },
			this.#opts(extra)
		);
	}

	getSession(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.SessionResponse>(
			`${T.MILK}/GetSession`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createSession(req: Omit<T.CreateSessionRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateSessionRequest, T.SessionResponse>(
			`${T.MILK}/CreateSession`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	updateSessionStatus(req: Omit<T.UpdateSessionRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UpdateSessionRequest, T.SessionResponse>(
			`${T.MILK}/UpdateSessionStatus`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordMilk(req: Omit<T.RecordMilkRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordMilkRequest, T.RecordResponse>(
			`${T.MILK}/RecordMilk`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getRecord(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.RecordResponse>(
			`${T.MILK}/GetRecord`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listSessionRecords(sessionId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListRecordsRequest, T.ListRecordsResponse>(
			`${T.MILK}/ListSessionRecords`,
			{ session_id: sessionId, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getDailyYield(cattleId: string, date: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DailyYieldRequest, T.DailyYieldResponse>(
			`${T.MILK}/GetDailyYield`,
			{ tenant_id: this.#tenantId, cattle_id: cattleId, date },
			this.#opts(extra)
		);
	}

	recordQuality(req: Omit<T.RecordQualityRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordQualityRequest, Record<string, never>>(
			`${T.MILK}/RecordQuality`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- breeding ---- */

	listActivePregnancies(extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantRequest, T.ListActivePregnanciesResponse>(
			`${T.BREEDING}/ListActivePregnancies`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getBreedingHistory(cattleId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GetBreedingHistoryRequest, T.GetBreedingHistoryResponse>(
			`${T.BREEDING}/GetBreedingHistory`,
			{ tenant_id: this.#tenantId, cattle_id: cattleId },
			this.#opts(extra)
		);
	}

	createBreedingCycle(
		req: Omit<T.CreateBreedingCycleRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.CreateBreedingCycleRequest, T.BreedingCycleResponse>(
			`${T.BREEDING}/CreateBreedingCycle`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordInsemination(
		req: Omit<T.RecordInseminationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordInseminationRequest, T.InseminationResponse>(
			`${T.BREEDING}/RecordInsemination`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	confirmPregnancy(req: Omit<T.ConfirmPregnancyRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ConfirmPregnancyRequest, T.PregnancyResponse>(
			`${T.BREEDING}/ConfirmPregnancy`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordCalving(req: Omit<T.RecordCalvingRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordCalvingRequest, T.CalvingRecordResponse>(
			`${T.BREEDING}/RecordCalving`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- health ---- */

	listUpcomingVaccinations(extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantRequest, T.ListVaccinationsResponse>(
			`${T.HEALTH}/ListUpcomingVaccinations`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getVaccinationHistory(cattleId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.HistoryRequest, T.ListVaccinationsResponse>(
			`${T.HEALTH}/GetVaccinationHistory`,
			{ tenant_id: this.#tenantId, cattle_id: cattleId },
			this.#opts(extra)
		);
	}

	recordVaccination(
		req: Omit<T.RecordVaccinationRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordVaccinationRequest, T.VaccinationResponse>(
			`${T.HEALTH}/RecordVaccination`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getTreatmentHistory(cattleId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.HistoryRequest, T.ListTreatmentsResponse>(
			`${T.HEALTH}/GetTreatmentHistory`,
			{ tenant_id: this.#tenantId, cattle_id: cattleId },
			this.#opts(extra)
		);
	}

	recordTreatment(req: Omit<T.RecordTreatmentRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordTreatmentRequest, T.TreatmentResponse>(
			`${T.HEALTH}/RecordTreatment`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	scheduleVetVisit(req: Omit<T.ScheduleVetVisitRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ScheduleVetVisitRequest, T.VetVisitResponse>(
			`${T.HEALTH}/ScheduleVetVisit`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- feed ---- */

	listFeedTypes(extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantRequest, T.ListFeedTypesResponse>(
			`${T.FEED}/ListFeedTypes`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createFeedType(req: Omit<T.CreateFeedTypeRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateFeedTypeRequest, T.FeedTypeResponse>(
			`${T.FEED}/CreateFeedType`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getNutritionPlan(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.NutritionPlanResponse>(
			`${T.FEED}/GetNutritionPlan`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createNutritionPlan(
		req: Omit<T.CreateNutritionPlanRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.CreateNutritionPlanRequest, T.NutritionPlanResponse>(
			`${T.FEED}/CreateNutritionPlan`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	recordFeedConsumption(
		req: Omit<T.RecordFeedConsumptionRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.RecordFeedConsumptionRequest, T.FeedConsumptionResponse>(
			`${T.FEED}/RecordFeedConsumption`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getFeedConsumptionReport(
		req: Omit<T.GetFeedConsumptionReportRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<
			T.GetFeedConsumptionReportRequest,
			T.GetFeedConsumptionReportResponse
		>(
			`${T.FEED}/GetFeedConsumptionReport`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- farms ---- */

	listFarms(extra?: Partial<CallOptions>) {
		return this.#client.call<T.TenantRequest, T.ListFarmsResponse>(
			`${T.FARM}/ListFarms`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getFarm(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.FarmResponse>(
			`${T.FARM}/GetFarm`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createFarm(req: Omit<T.CreateFarmRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateFarmRequest, T.FarmResponse>(
			`${T.FARM}/CreateFarm`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	updateFarm(req: Omit<T.UpdateFarmRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UpdateFarmRequest, T.FarmResponse>(
			`${T.FARM}/UpdateFarm`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	updateFarmCapacity(
		req: Omit<T.UpdateFarmCapacityRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.UpdateFarmCapacityRequest, T.FarmResponse>(
			`${T.FARM}/UpdateFarmCapacity`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listFarmSections(farmId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListFarmSectionsRequest, T.ListFarmSectionsResponse>(
			`${T.FARM}/ListFarmSections`,
			{ tenant_id: this.#tenantId, farm_id: farmId },
			this.#opts(extra)
		);
	}

	createFarmSection(
		req: Omit<T.CreateFarmSectionRequest, 'tenant_id'>,
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.CreateFarmSectionRequest, T.FarmSectionResponse>(
			`${T.FARM}/CreateFarmSection`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}
}
