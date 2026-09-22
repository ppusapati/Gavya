/**
 * The plant: what moved, what was made, what was tested, what was measured.
 *
 * Field names mirror the Go handlers' json tags exactly, and clients_test.go
 * compares the two on every run of the gate.
 *
 * Two rules run through all four of these services and are preserved here rather
 * than smoothed over.
 *
 * A quantity is a value and a unit, never a bare number. Litres and kilograms
 * differ by about three per cent, which is larger than most of the margins in
 * this business and looks exactly like a plausible transit loss. There is no
 * default unit anywhere in these types and the screens never supply one.
 *
 * A figure that could not be worked out is absent and says why, rather than
 * arriving as zero. `variance_unavailable_reason`, `unavailable_reason`,
 * `shares_unavailable_reason`, `spread_unavailable_reason` and
 * `uncertainty_missing` all exist for that: a blank cell in a variance report is
 * read as a zero, and a zero yield is an alarm.
 */

/* ---- shared shapes ---- */

/** An amount and what it is measured in. LITRES or KILOGRAMS. */
export interface Qty {
	value: string;
	unit: string;
}

/**
 * A conversion factor between litres and kilograms, as read off this milk.
 *
 * Material-service and production-service accept the identical shape, down to
 * the field names, deliberately: two spellings of a density is how a figure
 * crosses a service boundary and loses the scale it was written to.
 */
export interface Density {
	/** The factor as a decimal literal, never a JSON number. */
	kg_per_litre: string;
	scale: number;
	/** Tenths of a degree, so 4.0 degrees is 40. */
	at_celsius: number;
	/** LACTOMETER, ANALYSED or DECLARED. */
	source: string;
}

export const UNITS = ['LITRES', 'KILOGRAMS'] as const;
export const DENSITY_SOURCES = ['LACTOMETER', 'ANALYSED', 'DECLARED'] as const;
export const ROUNDING_MODES = [
	'HALF_UP',
	'HALF_EVEN',
	'HALF_DOWN',
	'DOWN',
	'UP',
	'TOWARD_ZERO'
] as const;

/* ---- material: nodes, movements, instruments, flows ---- */

export const NODE_KINDS = [
	'COLLECTION_CENTRE',
	'BULK_COOLER',
	'CHILLING_UNIT',
	'TANKER',
	'PLANT'
] as const;

/** How a quantity was measured. DECLARED means nobody measured it. */
export const MEASURE_METHODS = ['DIP', 'FLOWMETER', 'WEIGHBRIDGE', 'DECLARED'] as const;

export interface Node {
	id: string;
	tenant_id: string;
	code: string;
	name: string;
	kind: string;
	capacity?: Qty;
	active: boolean;
}

export interface Movement {
	id: string;
	tenant_id: string;

	from_node_id: string;
	to_node_id: string;

	dispatched_at: string;
	dispatched: Qty;
	dispatch_method: string;
	dispatched_by: string;

	received_at?: string;
	received?: Qty;
	receipt_method?: string;
	received_by?: string;

	/**
	 * What stayed in the sending vessel. Beside the variance rather than folded
	 * into it: the holdup is in a vessel somebody can look inside, and the
	 * variance is not anywhere.
	 */
	holdup?: Qty;
	density?: Density;

	variance?: Qty;
	variance_unavailable_reason?: string;

	status: string;
	abandoned_reason?: string;
}

export interface RegisterNodeRequest {
	tenant_id: string;
	code: string;
	name: string;
	kind: string;
	capacity?: Qty;
	actor: string;
}
export interface NodeResponse {
	node: Node;
}
export interface GetNodeRequest {
	tenant_id: string;
	id: string;
}
export interface ListNodesRequest {
	tenant_id: string;
	kind?: string;
}
export interface ListNodesResponse {
	nodes: Node[];
}

export interface DispatchRequest {
	tenant_id: string;
	from_node_id: string;
	to_node_id: string;
	at: string;
	quantity: Qty;
	method: string;
	holdup?: Qty;
	actor: string;
}
export interface ReceiveRequest {
	tenant_id: string;
	movement_id: string;
	at: string;
	quantity: Qty;
	method: string;
	/** Needed only when the two ends were measured in different units. */
	density?: Density;
	/** Required alongside a density, because converting rounds. */
	rounding?: string;
	actor: string;
}
export interface AbandonMovementRequest {
	tenant_id: string;
	movement_id: string;
	reason: string;
	actor: string;
}
export interface MovementResponse {
	movement: Movement;
}
export interface GetMovementRequest {
	tenant_id: string;
	id: string;
}
export interface ListMovementsRequest {
	tenant_id: string;
	/** Matches at either end: what a tanker loaded and what it delivered. */
	node_id?: string;
	from: string;
	to?: string;
	limit?: number;
}
export interface ListMovementsResponse {
	movements: Movement[];
	/**
	 * How many arrived without a variance being computable. A caller totalling
	 * transit loss needs to know how much of the period is missing from that
	 * total, or the figure reads as complete when it is not.
	 */
	unreconciled: number;
}

export interface Instrument {
	id: string;
	tenant_id: string;
	node_id: string;
	method: string;
	label: string;
	/** Parts per million of the reading, as a flowmeter certificate reads. */
	relative_ppm?: number;
	/** A fixed quantity, as a weighbridge certificate reads. Exactly one of the two. */
	absolute?: Qty;
	certificate_ref: string;
	calibrated_on: string;
	valid_until: string;
}

export interface RegisterInstrumentRequest {
	tenant_id: string;
	node_id: string;
	method: string;
	label: string;
	relative_ppm?: number;
	absolute?: Qty;
	certificate_ref: string;
	calibrated_on: string;
	valid_until: string;
	actor: string;
}
export interface InstrumentResponse {
	instrument: Instrument;
}
export interface ListInstrumentsRequest {
	tenant_id: string;
}
export interface ListInstrumentsResponse {
	instruments: Instrument[];
	/** How many were out of calibration as at as_of. */
	expired: number;
	as_of: string;
}

export interface ProposedFlow {
	flow_id: string;
	from_node_id: string;
	to_node_id: string;
	measured: Qty;
	standard_uncertainty?: Qty;
	unmeasured: boolean;
	unmeasured_reason?: string;
	movement_id: string;
}

export interface ProposeFlowsRequest {
	tenant_id: string;
	from: string;
	to: string;
	/** No default: it decides which of two nearly-equal legs gets the blame. */
	rounding: string;
}
export interface ProposeFlowsResponse {
	flows: ProposedFlow[];
	/**
	 * How many legs the reconciler will solve for rather than weigh. A window
	 * mostly made of these is one whose answer is mostly inference.
	 */
	unmeasured: number;
}

/* ---- production: batches, genealogy, yield, recipes ---- */

export const BATCH_KINDS = ['RAW', 'INTERMEDIATE', 'FINISHED'] as const;
export const BATCH_STATUSES = [
	'OPEN',
	'RELEASED',
	'QUARANTINED',
	'RECALLED',
	'DISPOSED'
] as const;
export const BATCH_SOURCE_KINDS = ['MOVEMENT', 'NODE'] as const;
export const TRACE_DIRECTIONS = ['BACKWARD', 'FORWARD'] as const;

export interface ProductionBatch {
	id: string;
	tenant_id: string;
	code: string;
	kind: string;
	product_ref: string;
	produced: Qty;
	produced_at: string;
	produced_by: string;
	source_kind?: string;
	source_ref?: string;
	formulation_id?: string;
	status: string;
	status_reason?: string;
	/**
	 * Whether this lot may be fed into anything. Carried explicitly rather than
	 * derived from the status, because a caller that derives it wrongly ships
	 * quarantined milk.
	 */
	held: boolean;
}

export interface CreateProductionBatchRequest {
	tenant_id: string;
	code: string;
	kind: string;
	product_ref: string;
	produced: Qty;
	produced_at: string;
	produced_by: string;
	source_kind?: string;
	source_ref?: string;
	formulation_id?: string;
	/** Resolved to the version in force at produced_at. */
	formulation_code?: string;
	status?: string;
	status_reason?: string;
	actor: string;
}
export interface ProductionBatchResponse {
	batch: ProductionBatch;
}
export interface GetProductionBatchRequest {
	tenant_id: string;
	id?: string;
	/** What a person reads off the vessel. Either identifies the batch. */
	code?: string;
}
export interface ListProductionBatchesRequest {
	tenant_id: string;
	from: string;
	to?: string;
	limit?: number;
}
export interface ListProductionBatchesResponse {
	batches: ProductionBatch[];
}
export interface SetProductionBatchStatusRequest {
	tenant_id: string;
	id?: string;
	code?: string;
	status: string;
	/** Required to put a batch under hold, and required to lift one. */
	reason?: string;
	actor: string;
}

export interface BatchInput {
	id: string;
	output_batch_id: string;
	input_batch_id: string;
	input_code?: string;
	consumed: Qty;
}

export interface RecordInputRequest {
	tenant_id: string;
	output_batch_id?: string;
	output_code?: string;
	input_batch_id?: string;
	input_code?: string;
	consumed: Qty;
	actor: string;
}
export interface RecordInputResponse {
	input: BatchInput;
	/** What the consumed lot has left afterwards. */
	remaining: Qty;
}

export interface GetBatchGenealogyRequest {
	tenant_id: string;
	id?: string;
	code?: string;
}
export interface GetBatchGenealogyResponse {
	batch: ProductionBatch;
	inputs: BatchInput[];
	remaining: Qty;
}

export interface Affected {
	batch: ProductionBatch;
	/** Process steps from the origin, by the shortest route. */
	depth: number;
	/** A batch one step nearer the origin, so the link can be explained. */
	via?: string;
}

export interface TraceBatchRequest {
	tenant_id: string;
	id?: string;
	code?: string;
	/** FORWARD into what was made from this, BACKWARD into what made it. */
	direction: string;
	/** Give both bounds or neither; the service refuses one alone. */
	max_depth?: number;
	max_batches?: number;
}
export interface TraceBatchResponse {
	batch: ProductionBatch;
	direction: string;
	affected: Affected[];
	/** The subset that left the plant. This is the list a recall produces. */
	finished: Affected[];
	/**
	 * False when the walk stopped at a bound or could not read a batch it
	 * reached. A caller that ignores this and acts on the list is recalling
	 * some of what it should.
	 */
	complete: boolean;
	frontier?: string[];
	warning?: string;
	unreadable?: string[];
}

export interface GetBatchYieldRequest {
	tenant_id: string;
	id?: string;
	code?: string;
	density?: Density;
	rounding_mode?: string;
}
export interface GetBatchYieldResponse {
	batch: ProductionBatch;
	produced: Qty;
	consumed: Qty;
	observed_yield_ppm?: number;
	observed_yield_percent?: string;
	/** Why there is no observed figure. Never a blank where a number should be. */
	unavailable_reason?: string;
	expected_yield_ppm?: number;
	variance_ppm?: number;
	variance_percent?: string;
	/** The plant declared no target, which is not the same as having met one. */
	no_expectation_declared: boolean;
}

export interface FormulationInput {
	product_ref: string;
	/** Optional: a recipe's declared shares need not sum to a million. */
	expected_share_ppm?: number;
	/** Without it no share finding is raised at all. */
	share_tolerance_ppm?: number;
	required: boolean;
}

export interface Formulation {
	id: string;
	tenant_id: string;
	code: string;
	name: string;
	output_product_ref: string;
	output_unit: string;
	expected_yield_ppm?: number;
	expected_yield_percent?: string;
	/** Where the figure came from, required beside one. */
	expectation_basis?: string;
	/** DRAFT, APPROVED or WITHDRAWN. */
	status: string;
	approved_by?: string;
	approved_at?: string;
	approval_note?: string;
	/** Whether a batch may be made against this version. */
	usable_for_production: boolean;
	withdrawn_reason?: string;
	valid_from: string;
	valid_to?: string;
	inputs?: FormulationInput[];
}

export interface CreateFormulationRequest {
	tenant_id: string;
	code: string;
	name: string;
	output_product_ref: string;
	output_unit: string;
	expected_yield_ppm?: number;
	expectation_basis?: string;
	valid_from: string;
	valid_to?: string;
	inputs?: FormulationInput[];
	actor: string;
}
export interface FormulationResponse {
	formulation: Formulation;
}
export interface GetFormulationRequest {
	tenant_id: string;
	id?: string;
	/** A code needs `at` beside it: a recipe is several versions. */
	code?: string;
	at?: string;
}
export interface ListFormulationsRequest {
	tenant_id: string;
}
export interface ListFormulationsResponse {
	formulations: Formulation[];
}
export interface ApproveFormulationRequest {
	tenant_id: string;
	id: string;
	approver: string;
	/** When it was agreed, not when it was typed. */
	at?: string;
	note?: string;
}
export interface WithdrawFormulationRequest {
	tenant_id: string;
	id: string;
	reason: string;
	actor: string;
}

export interface ShareReading {
	product_ref: string;
	expected_share_ppm: number;
	observed_share_ppm: number;
	difference_ppm: number;
	expected_percent: string;
	observed_percent: string;
	/** Absent means the two figures are reported and nothing is judged. */
	tolerance_ppm?: number;
}

export interface RecipeFinding {
	kind: string;
	product_ref: string;
	/** A report where everything is serious is one where nothing is. */
	serious: boolean;
	explanation: string;
}

export interface CheckRecipeRequest {
	tenant_id: string;
	id?: string;
	code?: string;
}
export interface CheckRecipeResponse {
	batch: { id: string; code: string; product_ref: string };
	formulation: Formulation;
	findings: RecipeFinding[];
	serious_count: number;
	shares: ShareReading[];
	/** Why there are none. Never a silent empty section. */
	shares_unavailable_reason?: string;
}

export interface GetObservedYieldRequest {
	tenant_id: string;
	id?: string;
	code?: string;
	at?: string;
}
export interface GetObservedYieldResponse {
	formulation_id: string;
	code: string;
	output_product_ref: string;
	batches_counted: number;
	batches_needing_a_density: number;
	lowest_ppm?: number;
	lower_quartile_ppm?: number;
	median_ppm?: number;
	upper_quartile_ppm?: number;
	highest_ppm?: number;
	lowest_percent?: string;
	median_percent?: string;
	highest_percent?: string;
	expected_yield_ppm?: number;
	expectation_basis?: string;
	/**
	 * How many batches the figures rest on and what was left out. It travels
	 * with them because a median over three vats and a median over three
	 * hundred look identical on a screen.
	 */
	note: string;
}

/* ---- laboratory: samples, custody, results ---- */

export const SAMPLE_SOURCE_KINDS = ['COLLECTION', 'MOVEMENT', 'NODE', 'BATCH'] as const;
export const SAMPLE_PURPOSES = [
	'PAYMENT',
	'DUPLICATE',
	'DISPUTE',
	'PROCESS_CHECK',
	'REGULATORY'
] as const;
export const ANALYTES = [
	'FAT',
	'SNF',
	'PROTEIN',
	'LACTOSE',
	'ADDED_WATER',
	'ACIDITY',
	'MBRT',
	'TEMPERATURE'
] as const;

/**
 * A measured value and the resolution it was read to.
 *
 * The scale is not cosmetic. 4.1 and 4.10 are the same number and not the same
 * claim about how precisely it was read, and the service refuses a value whose
 * decimal places and stated scale disagree.
 */
export interface Reading {
	value: string;
	scale: number;
}

export interface Sample {
	id: string;
	tenant_id: string;
	code: string;
	source_kind: string;
	source_ref: string;
	drawn_at: string;
	drawn_by: string;
	seal_number?: string;
	seal_broken_at?: string;
	seal_broken_by?: string;
	seal_broken_reason?: string;
	sealed: boolean;
	purpose: string;
	duplicates_sample_id?: string;
}

export interface DrawSampleRequest {
	tenant_id: string;
	code: string;
	source_kind: string;
	source_ref: string;
	drawn_at: string;
	drawn_by: string;
	/** A process check is not sealed. A payment sample without one is not eligible. */
	seal_number?: string;
	purpose: string;
	duplicates_sample_id?: string;
	actor: string;
}
export interface SampleResponse {
	sample: Sample;
}
export interface GetSampleRequest {
	tenant_id: string;
	id: string;
}
export interface ListSamplesRequest {
	tenant_id: string;
	from: string;
	to?: string;
	limit?: number;
}
export interface ListSamplesResponse {
	samples: Sample[];
}
export interface BreakSealRequest {
	tenant_id: string;
	id: string;
	/** Required: the seal is what rules out the sample having been changed. */
	reason: string;
	/** When it was actually broken, not when the book was written up. */
	at?: string;
	actor: string;
}

export interface Handover {
	sequence: number;
	at: string;
	from: string;
	to: string;
	note?: string;
}

export interface RecordHandoverRequest {
	tenant_id: string;
	sample_id: string;
	at: string;
	from: string;
	to: string;
	note?: string;
	actor: string;
}
export interface HandoverResponse {
	handover: Handover;
}

export interface LabResult {
	id: string;
	sample_id: string;
	analyte: string;
	reading: Reading;
	method: string;
	instrument_ref: string;
	instrument_valid_until?: string;
	instrument_certificate?: string;
	analysed_at: string;
	analysed_by: string;
	/**
	 * Whether this reading is fit to price milk, and why not. Never absent: a
	 * caller shown a reading with no verdict beside it will use it.
	 */
	eligibility: string;
	eligibility_reason: string;
}

export interface RecordResultRequest {
	tenant_id: string;
	sample_id: string;
	analyte: string;
	reading: Reading;
	method: string;
	instrument_ref: string;
	/** Empty means nobody recorded one, which reads as UNKNOWN, not as a finding. */
	instrument_valid_until?: string;
	instrument_certificate?: string;
	analysed_at: string;
	analysed_by: string;
	actor: string;
}
export interface ResultResponse {
	result: LabResult;
}

export interface AnalyteGroup {
	analyte: string;
	results: LabResult[];
	/**
	 * The difference between the highest and lowest where one analyte was read
	 * more than once. Reported, never resolved: a laboratory that ran a sample
	 * twice did so to find out whether the two agree, and picking one throws
	 * away the answer.
	 */
	spread?: Reading;
	spread_unavailable_reason?: string;
}

export interface GetSampleReportRequest {
	tenant_id: string;
	sample_id: string;
}
export interface GetSampleReportResponse {
	sample: Sample;
	chain: Handover[];
	custody_intact: boolean;
	custody_holder?: string;
	custody_reason?: string;
	analytes: AnalyteGroup[];
	/** How many of this sample's readings are fit to price milk. */
	eligible: number;
	total: number;
}

/* ---- observation: readings, instruments, certificates ---- */

export const SUBJECT_KINDS = ['CATTLE', 'PRODUCER', 'ROUTE', 'TANKER', 'BATCH'] as const;
export const QUANTITY_KINDS = [
	'VOLUME_LITRES',
	'MASS_KG',
	'FAT_PERCENT',
	'SNF_PERCENT',
	'LACTOSE_PERCENT',
	'PROTEIN_PERCENT',
	'TEMPERATURE_C',
	'SOMATIC_CELL_COUNT',
	'ADULTERATION_INDEX'
] as const;
export const INSTRUMENT_KINDS = [
	'WEIGHBRIDGE',
	'MILK_ANALYSER',
	'PLATFORM_SCALE',
	'FLOW_METER',
	'THERMOMETER',
	'MANUAL_ENTRY'
] as const;
export const ORIGIN_KINDS = ['NATIVE', 'IMPORTED', 'DERIVED'] as const;

export interface Origin {
	kind: string;
	source_system_id?: string;
	import_batch_id?: string;
	source_record_id?: string;
	source_payload_hash?: string;
	derivation_id?: string;
}

export interface SubjectRef {
	kind: string;
	id: string;
}

/**
 * Absent means the value carries no stated confidence, not a perfect one —
 * which is what `uncertainty_missing` on the observation says out loud.
 */
export interface Uncertainty {
	uncertainty_model_id: string;
	model_version?: string;
	standard_uncertainty: number;
	expanded_uncertainty: number;
	coverage_factor: number;
	coverage_probability: number;
	estimated_at?: string;
}

export interface Anomaly {
	score: number;
	flagged: boolean;
	method?: string;
	model_version?: string;
	lower_bound?: number;
	upper_bound?: number;
	explanation?: string;
	scored_at?: string;
}

export interface Observation {
	id: string;
	tenant_id: string;
	subject: SubjectRef;
	quantity_kind: string;
	/** exact.Fixed — a decimal string, never parsed into a number here. */
	value: string;
	unit: string;
	instrument_id?: string;
	session_ref?: string;
	observed_by?: string;
	origin: Origin;
	valid_from: string;
	valid_to: string;
	recorded_at: string;
	superseded_at?: string;
	superseded_by?: string;
	supersedes?: string;
	current: boolean;
	eligibility_verdict: string;
	eligibility_reason: string;
	eligibility_certificate_id?: string;
	uncertainty?: Uncertainty;
	/** Stated outright so a consumer cannot read a missing estimate as zero. */
	uncertainty_missing: boolean;
	anomaly?: Anomaly;
	created_at: string;
	created_by: string;
}

export interface RecordObservationRequest {
	tenant_id: string;
	subject: SubjectRef;
	quantity_kind: string;
	value: string;
	instrument_id?: string;
	session_ref?: string;
	observed_by?: string;
	origin: Origin;
	valid_from: string;
	valid_to?: string;
	/** The reading this replaces. It is superseded, never edited. */
	corrects?: string;
	uncertainty_model_id?: string;
	uncertainty_inputs?: Record<string, number>;
	coverage_probability?: number;
	created_by: string;
}
export interface RecordObservationResponse {
	observation: Observation;
}
export interface GetObservationRequest {
	id: string;
	tenant_id: string;
}
export interface GetObservationResponse {
	observation: Observation;
}
export interface ListObservationsForSubjectRequest {
	tenant_id: string;
	subject: SubjectRef;
	quantity_kind?: string;
	/**
	 * valid_at selects the fact true of the world then; as_of selects what the
	 * platform knew then. They are different questions and the screen asks them
	 * separately.
	 */
	valid_at?: string;
	as_of?: string;
	limit: number;
	offset: number;
}
export interface ListObservationsForSubjectResponse {
	observations: Observation[];
}
export interface ListFlaggedObservationsRequest {
	tenant_id: string;
	limit: number;
	offset: number;
}
export interface ListFlaggedObservationsResponse {
	observations: Observation[];
}

export interface MeterInstrument {
	id: string;
	tenant_id: string;
	serial: string;
	kind: string;
	label?: string;
	make?: string;
	model?: string;
	created_at: string;
}

export interface RegisterMeterRequest {
	tenant_id: string;
	serial: string;
	kind: string;
	label?: string;
	make?: string;
	model?: string;
	actor: string;
}
export interface RegisterMeterResponse {
	instrument: MeterInstrument;
}
export interface GetMeterRequest {
	id: string;
	tenant_id: string;
}
export interface GetMeterResponse {
	instrument: MeterInstrument;
}

export interface VerificationCertificate {
	id: string;
	tenant_id: string;
	instrument_id: string;
	certificate_number: string;
	verifying_authority: string;
	issued_at: string;
	expires_at: string;
	origin: Origin;
	created_at: string;
}

export interface RecordCertificateRequest {
	tenant_id: string;
	instrument_id: string;
	certificate_number: string;
	verifying_authority: string;
	issued_at: string;
	expires_at: string;
	origin: Origin;
	created_by: string;
}
export interface RecordCertificateResponse {
	certificate: VerificationCertificate;
}

export interface Eligibility {
	verdict: string;
	reason: string;
	certificate_id?: string;
}

export interface GetActiveCertificateRequest {
	tenant_id: string;
	instrument_id: string;
	at?: string;
	/**
	 * Naming a quantity asks for the verdict this certificate would yield for
	 * it, without recording anything. The verdict depends on it: a quantity
	 * outside legal metrology is eligible whatever the certificate says.
	 */
	quantity_kind?: string;
}
export interface GetActiveCertificateResponse {
	certificate: VerificationCertificate;
	/** Present only when a quantity was named. */
	eligibility?: Eligibility;
}
