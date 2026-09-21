/**
 * The herd, and the morning's work: cattle, milk, breeding, health, feed, farms.
 *
 * Field names mirror the Go handlers' json tags exactly, and
 * services/gateway-service/handler/clients_test.go compares the two on every run
 * of the gate — so a renamed tag fails the build rather than drawing a blank
 * cell on a screen a supervisor opens once a fortnight.
 *
 * # TWO THINGS THAT ARE STRINGS AND LOOK LIKE NUMBERS
 *
 * `exact.Fixed` crosses the wire as a JSON string — "6.250", not 6.25 — because
 * the whole platform holds quantities and money as exact decimals and a double
 * cannot carry 0.1. Every field below that the services declare as exact.Fixed
 * is typed `string` here for that reason, and must stay that way: typing one as
 * `number` would round it at the edge of the system that exists to stop exactly
 * that.
 *
 * Timestamps are ISO-8601 strings, as Go's time.Time marshals them.
 */

/* ---- cattle ---- */

export interface Cattle {
	id: string;
	tenant_id: string;
	tag_number: string;
	name: string;
	breed_id: string;
	gender: string;
	status: string;
	/** exact.Fixed. Kilograms, exactly as recorded. */
	weight: string;
}

export interface Breed {
	id: string;
	tenant_id: string;
	name: string;
	origin: string;
	description: string;
}

export interface CreateCattleRequest {
	tenant_id: string;
	tag_number: string;
	name: string;
	breed_id: string;
	gender: string;
	weight: string;
	created_by: string;
}
export interface CattleResponse {
	cattle: Cattle;
}
export interface ListCattleRequest {
	tenant_id: string;
	status: string;
	limit: number;
	offset: number;
}
export interface ListCattleResponse {
	cattle: Cattle[];
	total: number;
}
export interface UpdateCattleRequest {
	id: string;
	tenant_id: string;
	status: string;
	weight: string;
	updated_by: string;
}
export interface DeleteCattleRequest {
	id: string;
	tenant_id: string;
	deleted_by: string;
}
export interface DeleteCattleResponse {
	success: boolean;
}
export interface CreateBreedRequest {
	tenant_id: string;
	name: string;
	origin: string;
	description: string;
	created_by: string;
}
export interface BreedResponse {
	breed: Breed;
}
export interface ListBreedsResponse {
	breeds: Breed[];
}

/* ---- milk ---- */

export interface MilkSession {
	id: string;
	tenant_id: string;
	cattle_id: string;
	shift_type: string;
	status: string;
}

export interface MilkRecord {
	id: string;
	tenant_id: string;
	session_id: string;
	cattle_id: string;
	/** exact.Fixed. Litres. */
	quantity_liters: string;
}

export interface CreateSessionRequest {
	tenant_id: string;
	cattle_id: string;
	shift_type: string;
	/**
	 * Which day a reading falls on is a local fact, and the service refuses to
	 * guess it. An IANA name, as in Asia/Kolkata.
	 */
	timezone: string;
	created_by: string;
}
export interface SessionResponse {
	session: MilkSession;
}
export interface ListSessionsRequest {
	tenant_id: string;
	limit: number;
	offset: number;
}
export interface ListSessionsResponse {
	sessions: MilkSession[];
}
export interface UpdateSessionRequest {
	id: string;
	tenant_id: string;
	status: string;
	updated_by: string;
}
export interface RecordMilkRequest {
	tenant_id: string;
	session_id: string;
	cattle_id: string;
	quantity_liters: string;
	created_by: string;
}
export interface RecordResponse {
	record: MilkRecord;
}
export interface ListRecordsRequest {
	session_id: string;
	tenant_id: string;
}
export interface ListRecordsResponse {
	records: MilkRecord[];
}
export interface DailyYieldRequest {
	tenant_id: string;
	cattle_id: string;
	date: string;
}
export interface DailyYieldResponse {
	/** exact.Fixed. */
	total_liters: string;
}
export interface RecordQualityRequest {
	tenant_id: string;
	record_id: string;
	fat_percent: string;
	snf_percent: string;
	lactose: string;
}

/* ---- breeding ---- */

export interface BreedingCycle {
	id: string;
	tenant_id: string;
	cattle_id: string;
	heat_date: string;
	status: string;
	notes: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
	deleted_at?: string;
}

export interface Insemination {
	id: string;
	tenant_id: string;
	cycle_id: string;
	cattle_id: string;
	bull_id?: string;
	semen_batch_id?: string;
	inseminated_at: string;
	method: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface Pregnancy {
	id: string;
	tenant_id: string;
	cattle_id: string;
	insemination_id: string;
	confirmed_at: string;
	expected_calving_date: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface CalvingRecord {
	id: string;
	tenant_id: string;
	pregnancy_id: string;
	cattle_id: string;
	calf_id?: string;
	calving_date: string;
	calf_gender: string;
	/** exact.Fixed. Kilograms. */
	calf_weight: string;
	complications: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface CreateBreedingCycleRequest {
	tenant_id: string;
	cattle_id: string;
	heat_date: string;
	status: string;
	notes: string;
	created_by: string;
}
export interface BreedingCycleResponse {
	cycle: BreedingCycle;
}
export interface RecordInseminationRequest {
	tenant_id: string;
	cycle_id: string;
	cattle_id: string;
	bull_id?: string;
	semen_batch_id?: string;
	inseminated_at: string;
	method: string;
	created_by: string;
}
export interface InseminationResponse {
	insemination: Insemination;
}
export interface ConfirmPregnancyRequest {
	tenant_id: string;
	cattle_id: string;
	insemination_id: string;
	confirmed_at: string;
	expected_calving_date: string;
	created_by: string;
}
export interface PregnancyResponse {
	pregnancy: Pregnancy;
}
export interface RecordCalvingRequest {
	tenant_id: string;
	pregnancy_id: string;
	cattle_id: string;
	calf_id?: string;
	calf_gender: string;
	calf_weight: string;
	complications: string;
	status: string;
	created_by: string;
}
export interface CalvingRecordResponse {
	calving_record: CalvingRecord;
}
export interface GetBreedingHistoryRequest {
	tenant_id: string;
	cattle_id: string;
}
export interface GetBreedingHistoryResponse {
	cycles: BreedingCycle[];
}
export interface ListActivePregnanciesResponse {
	pregnancies: Pregnancy[];
}

/* ---- health ---- */

export interface Vaccination {
	id: string;
	tenant_id: string;
	cattle_id: string;
	vaccine_name: string;
	batch_number: string;
	administered_at: string;
	next_due_date?: string;
	veterinarian_id: string;
	dosage: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface Treatment {
	id: string;
	tenant_id: string;
	cattle_id: string;
	diagnosis_code: string;
	diagnosis: string;
	medicine_name: string;
	dosage: string;
	treated_at: string;
	treated_by: string;
	follow_up_date?: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

/**
 * A vet visit as the service reports it.
 *
 * `cost` is a decimal string and `currency` is beside it, which is the shape
 * this platform uses everywhere an amount is read back: an amount without its
 * currency is the defect a whole layer of the database exists to prevent.
 */
export interface VetVisit {
	id: string;
	tenant_id: string;
	cattle_id: string;
	veterinarian_id: string;
	visit_date: string;
	purpose: string;
	notes: string;
	cost: string;
	currency: string;
	created_at: string;
	updated_at: string;
	created_by: string;
}

export interface RecordVaccinationRequest {
	tenant_id: string;
	cattle_id: string;
	vaccine_name: string;
	batch_number: string;
	administered_at: string;
	next_due_date?: string;
	veterinarian_id: string;
	dosage: string;
	created_by: string;
}
export interface VaccinationResponse {
	vaccination: Vaccination;
}
export interface HistoryRequest {
	tenant_id: string;
	cattle_id: string;
}
export interface ListVaccinationsResponse {
	vaccinations: Vaccination[];
}
export interface RecordTreatmentRequest {
	tenant_id: string;
	cattle_id: string;
	diagnosis_code: string;
	diagnosis: string;
	medicine_name: string;
	dosage: string;
	treated_at: string;
	treated_by: string;
	follow_up_date?: string;
	status: string;
	created_by: string;
}
export interface TreatmentResponse {
	treatment: Treatment;
}
export interface ListTreatmentsResponse {
	treatments: Treatment[];
}
export interface ScheduleVetVisitRequest {
	tenant_id: string;
	cattle_id: string;
	veterinarian_id: string;
	visit_date: string;
	purpose: string;
	notes: string;
	cost: string;
	currency: string;
	created_by: string;
}
export interface VetVisitResponse {
	vet_visit: VetVisit;
}

/* ---- feed ---- */

export interface FeedType {
	id: string;
	tenant_id: string;
	name: string;
	category: string;
	unit: string;
	nutritional_info: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface NutritionPlan {
	id: string;
	tenant_id: string;
	cattle_id: string;
	feed_type_id: string;
	/** exact.Fixed. Kilograms a day. */
	daily_quantity_kg: string;
	start_date: string;
	end_date?: string;
	notes: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface FeedConsumption {
	id: string;
	tenant_id: string;
	cattle_id: string;
	feed_type_id: string;
	/** exact.Fixed. */
	quantity_kg: string;
	fed_at: string;
	fed_by: string;
	created_at: string;
	updated_at: string;
	created_by: string;
}

export interface FeedConsumptionReport {
	cattle_id: string;
	feed_type_id: string;
	/** exact.Fixed. */
	total_kg: string;
}

export interface CreateFeedTypeRequest {
	tenant_id: string;
	name: string;
	category: string;
	unit: string;
	nutritional_info: string;
	created_by: string;
}
export interface FeedTypeResponse {
	feed_type: FeedType;
}
export interface ListFeedTypesResponse {
	feed_types: FeedType[];
}
export interface CreateNutritionPlanRequest {
	tenant_id: string;
	cattle_id: string;
	feed_type_id: string;
	daily_quantity_kg: string;
	start_date: string;
	end_date?: string;
	notes: string;
	created_by: string;
}
export interface NutritionPlanResponse {
	nutrition_plan: NutritionPlan;
}
export interface RecordFeedConsumptionRequest {
	tenant_id: string;
	cattle_id: string;
	feed_type_id: string;
	quantity_kg: string;
	fed_at: string;
	fed_by: string;
	created_by: string;
}
export interface FeedConsumptionResponse {
	consumption: FeedConsumption;
}
export interface GetFeedConsumptionReportRequest {
	tenant_id: string;
	cattle_id: string;
	from: string;
	to: string;
}
export interface GetFeedConsumptionReportResponse {
	reports: FeedConsumptionReport[];
}

/* ---- farms ---- */

export interface Farm {
	id: string;
	tenant_id: string;
	name: string;
	code: string;
	address: string;
	city: string;
	state: string;
	country: string;
	capacity: number;
	manager_id: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface FarmSection {
	id: string;
	tenant_id: string;
	farm_id: string;
	name: string;
	section_type: string;
	capacity: number;
	current_occupancy: number;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface CreateFarmRequest {
	tenant_id: string;
	name: string;
	code: string;
	address: string;
	city: string;
	state: string;
	country: string;
	capacity: number;
	manager_id: string;
	status: string;
	created_by: string;
}
export interface FarmResponse {
	farm: Farm;
}
export interface ListFarmsResponse {
	farms: Farm[];
}
export interface UpdateFarmRequest {
	id: string;
	tenant_id: string;
	name: string;
	address: string;
	city: string;
	state: string;
	country: string;
	manager_id: string;
	status: string;
	updated_by: string;
}
export interface CreateFarmSectionRequest {
	tenant_id: string;
	farm_id: string;
	name: string;
	section_type: string;
	capacity: number;
	current_occupancy: number;
	created_by: string;
}
export interface FarmSectionResponse {
	section: FarmSection;
}
export interface ListFarmSectionsRequest {
	tenant_id: string;
	farm_id: string;
}
export interface ListFarmSectionsResponse {
	sections: FarmSection[];
}
export interface UpdateFarmCapacityRequest {
	id: string;
	tenant_id: string;
	capacity: number;
	updated_by: string;
}

/** Several services take nothing but the tenant. */
export interface TenantRequest {
	tenant_id: string;
}
