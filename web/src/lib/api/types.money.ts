/**
 * The money path: rate cards, priced collections, pools, settlement, billing.
 *
 * Field names mirror the Go handlers' json tags exactly, and clients_test.go
 * compares the two on every run of the gate.
 *
 * # EVERY AMOUNT IS A STRING, AND MOST CARRY THEIR SCALE
 *
 * The services send an amount three ways at once and each is deliberate: a
 * decimal string for a person to read, minor units for arithmetic that must not
 * round, and the scale and currency that say what both mean. A rupee has two
 * decimals and a dinar has three, so an amount without its scale is not an
 * amount — which is the defect a whole layer of this platform's database exists
 * to prevent.
 *
 * Nothing here is typed `number` except a count, a scale and minor units, and
 * minor units are int64 on the wire.
 */

/* ---- procurement: rate cards and priced collections ---- */

/** A quantity or a component reading: the digits, and where the point is. */
export interface Point {
	value: string;
	scale: number;
}

export interface RateCardCell {
	fat: Point;
	snf: Point;
	rate: string;
	rate_scale: number;
}

export interface RateCardTerm {
	component: string;
	rate: string;
	rate_scale: number;
}

export interface RateCard {
	id: string;
	tenant_id: string;
	name: string;
	kind: string;
	currency: string;
	amount_scale: number;
	basis?: string;
	between_points?: string;
	outside_chart?: string;
	rounding: string;
	cells?: RateCardCell[];
	terms?: RateCardTerm[];
	valid_from: string;
	valid_to?: string;
}

/**
 * Declaring a card sends the card's own fields flattened, plus the actor.
 *
 * The Go request embeds RateCardProto rather than nesting it, so there is no
 * `rate_card` key on the way in — which is easy to get wrong from the response
 * shape alone, and is why it is written down here.
 */
export interface DeclareRateCardRequest extends Omit<RateCard, 'id' | 'tenant_id'> {
	id?: string;
	tenant_id: string;
	actor: string;
}
export interface RateCardResponse {
	rate_card: RateCard;
}
export interface ListRateCardsRequest {
	tenant_id: string;
	limit?: number;
	offset?: number;
}
export interface ListRateCardsResponse {
	rate_cards: RateCard[];
}
export interface GetRateCardInForceRequest {
	tenant_id: string;
	at: string;
}

export interface PricedCollection {
	id: string;
	tenant_id: string;
	producer_ref: string;
	society_code?: string;
	collected_on: string;
	shift: string;
	quantity: Point;
	quantity_unit: string;
	fat?: Point;
	snf?: Point;
	rate_card_id: string;
	rate?: string;
	currency: string;
	amount_scale: number;
	amount: string;
	amount_minor_units: number;
	/** Why this collection is worth what it is worth, in a sentence. */
	explanation: string;
	origin_kind: string;
	source_system_id?: string;
	source_record_id?: string;
	created_at: string;
	created_by: string;
	superseded_at?: string;
	superseded_by?: string;
	supersedes?: string;
	correction_reason?: string;
}

export interface RecordCollectionRequest {
	tenant_id: string;
	producer_ref: string;
	society_code?: string;
	collected_on: string;
	shift: string;
	quantity: Point;
	quantity_unit: string;
	fat?: Point;
	snf?: Point;
	fat_kg?: Point;
	snf_kg?: Point;
	origin_kind?: string;
	actor: string;
}
export interface CollectionResponse {
	collection: PricedCollection;
}
export interface ListCollectionsRequest {
	tenant_id: string;
	producer_ref?: string;
	from: string;
	to?: string;
	limit?: number;
	offset?: number;
	include_superseded?: boolean;
}
export interface ListCollectionsResponse {
	collections: PricedCollection[];
	total: string;
	total_minor_units: number;
	currency?: string;
	amount_scale?: number;
}
export interface CorrectCollectionRequest {
	tenant_id: string;
	id: string;
	quantity: Point;
	quantity_unit: string;
	fat?: Point;
	snf?: Point;
	reason: string;
	actor: string;
}
export interface CorrectCollectionResponse {
	collection: PricedCollection;
	supersedes: string;
}
export interface GetCollectionVersionsResponse {
	versions: PricedCollection[];
}

/* ---- pooling ---- */

export interface Pool {
	id: string;
	tenant_id: string;
	name: string;
	period_start: string;
	period_end: string;
	unit: string;
	currency: string;
	amount_scale: number;
	status: string;
	rate_card_id?: string;
	policy_version?: string;
	created_at: string;
	updated_at: string;
}

export interface ProducerMilk {
	id: string;
	tenant_id: string;
	pool_id: string;
	producer_ref: string;
	quantity: string;
	components?: Record<string, string>;
	slot_refs?: string[];
	origin_kind: string;
	created_at: string;
}

export interface Utilisation {
	id: string;
	tenant_id: string;
	pool_id: string;
	class: string;
	quantity: string;
	price: string;
	price_scale: number;
	created_at: string;
}

/** One precision-losing step, kept so a figure can be defended afterwards. */
export interface RoundingStep {
	operation: string;
	mode: string;
	from_scale: number;
	to_scale: number;
	discarded: number;
	result: string;
}

export interface Valuation {
	id: string;
	tenant_id: string;
	pool_id: string;
	currency: string;
	amount_scale: number;
	classified_value: string;
	component_value: string;
	/** The residual: classified value less component value, and not a free number. */
	producer_settlement_fund: string;
	total_quantity: string;
	blend_price: string;
	blend_price_scale: number;
	rounding_trail?: RoundingStep[];
	computed_at: string;
}

export interface Allocation {
	id: string;
	tenant_id: string;
	pool_id: string;
	valuation_id: string;
	producer_ref: string;
	currency: string;
	amount_scale: number;
	component_value: string;
	fund_share: string;
	total: string;
	weight: number;
	created_at: string;
}

export interface EconomicEvent {
	id: string;
	tenant_id: string;
	pool_id: string;
	allocation_id: string;
	producer_ref: string;
	currency: string;
	amount_scale: number;
	amount: string;
	kind: string;
	supersedes_event_id?: string;
	created_at: string;
}

export interface ComponentPrice {
	component: string;
	price: string;
	scale: number;
}

export interface RetroactivityPolicy {
	id: string;
	tenant_id: string;
	name: string;
	mode: string;
	max_lookback_days: number;
	currency: string;
	amount_scale: number;
	minimum_adjustment: string;
	effective_from: string;
	effective_to?: string;
}

export interface CreatePoolRequest {
	tenant_id: string;
	name: string;
	period_start: string;
	period_end: string;
	unit?: string;
	currency: string;
	amount_scale: number;
	rate_card_id?: string;
	policy_version?: string;
	actor: string;
}
export interface PoolResponse {
	pool: Pool;
}
export interface ListPoolsRequest {
	tenant_id: string;
	from?: string;
	to?: string;
	limit?: number;
	offset?: number;
}
export interface ListPoolsResponse {
	pools: Pool[];
}
export interface AddProducerMilkRequest {
	tenant_id: string;
	pool_id: string;
	producer_ref: string;
	quantity: string;
	components?: Record<string, string>;
	slot_refs?: string[];
	origin_kind?: string;
	actor: string;
}
export interface AddProducerMilkResponse {
	producer_milk: ProducerMilk;
}
export interface ListProducerMilkResponse {
	producer_milk: ProducerMilk[];
}
export interface RecordUtilisationRequest {
	tenant_id: string;
	pool_id: string;
	class: string;
	quantity: string;
	price: string;
	price_scale: number;
	actor: string;
}
export interface RecordUtilisationResponse {
	utilisation: Utilisation;
}
export interface ListUtilisationsResponse {
	utilisations: Utilisation[];
}
export interface ValuePoolRequest {
	tenant_id: string;
	pool_id: string;
	component_prices: ComponentPrice[];
	actor: string;
}
export interface ValuePoolResponse {
	pool: Pool;
	valuation: Valuation;
	allocations: Allocation[];
}
export interface GetValuationResponse {
	valuation: Valuation;
}
export interface ListAllocationsRequest {
	tenant_id: string;
	pool_id: string;
	valuation_id?: string;
}
export interface ListAllocationsResponse {
	allocations: Allocation[];
}
export interface SettlePoolResponse {
	pool: Pool;
	events: EconomicEvent[];
}
export interface ListEconomicEventsRequest {
	tenant_id: string;
	pool_id?: string;
	producer_ref?: string;
	limit?: number;
	offset?: number;
}
export interface ListEconomicEventsResponse {
	events: EconomicEvent[];
}
export interface DeclareRetroactivityPolicyRequest {
	tenant_id: string;
	name: string;
	mode: string;
	max_lookback_days: number;
	currency: string;
	amount_scale: number;
	minimum_adjustment?: string;
	effective_from: string;
	effective_to?: string;
	actor: string;
}
export interface RetroactivityPolicyResponse {
	policy: RetroactivityPolicy;
}
export interface GetEffectiveRetroactivityPolicyRequest {
	tenant_id: string;
	at?: string;
}
export interface ApplyCorrectionRequest {
	tenant_id: string;
	pool_id: string;
	component_prices: ComponentPrice[];
	at?: string;
	actor: string;
}
export interface ApplyCorrectionResponse {
	/** What the policy decided, which is not always that a correction was made. */
	outcome: string;
	reason: string;
	event_kind?: string;
	adjustment: string;
	pool: Pool;
	valuation?: Valuation;
}

/* ---- settlement ---- */

export interface Cycle {
	id: string;
	tenant_id: string;
	society_code: string;
	name: string;
	period_start: string;
	period_end: string;
	currency: string;
	amount_scale: number;
	deduction_policy: string;
	status: string;
	gathered_at?: string;
	approved_at?: string;
	approved_by?: string;
	paid_at?: string;
}

export interface Payable {
	id: string;
	tenant_id: string;
	cycle_id: string;
	producer_ref: string;
	currency: string;
	amount_scale: number;
	gross: string;
	deducted: string;
	net: string;
	carried_forward: string;
	gross_minor_units: number;
	deducted_minor_units: number;
	net_minor_units: number;
	kind: string;
	adjusts_payable_id?: string;
	reason?: string;
	status: string;
	approved_at?: string;
	approved_by?: string;
	paid_at?: string;
	paid_by?: string;
	payment_reference?: string;
	held_reason?: string;
}

export interface Recovery {
	id: string;
	tenant_id: string;
	producer_ref: string;
	kind: string;
	reference?: string;
	currency: string;
	amount_scale: number;
	principal: string;
	recovered: string;
	outstanding: string;
	instalment: string;
	principal_minor_units: number;
	recovered_minor_units: number;
	outstanding_minor_units: number;
	priority: number;
	status: string;
	opened_on: string;
}

export interface StatementLine {
	collected_on: string;
	shift: string;
	quantity: string;
	quantity_unit: string;
	rate?: string;
	amount: string;
	collection_id: string;
}

export interface StatementDeduction {
	kind: string;
	reference?: string;
	amount: string;
}

export interface StatementAdjustment {
	id: string;
	amount: string;
	reason: string;
	status: string;
	paid_at?: string;
}

export interface Statement {
	tenant_id: string;
	producer_ref: string;
	society_code: string;
	cycle_id: string;
	cycle_name: string;
	period_start: string;
	period_end: string;
	cycle_status: string;
	lines: StatementLine[];
	deductions: StatementDeduction[];
	quantities: Record<string, string>;
	currency: string;
	gross: string;
	deducted: string;
	net: string;
	carried_forward?: string;
	status?: string;
	paid_at?: string;
	payment_reference?: string;
	held_reason?: string;
	adjustments?: StatementAdjustment[];
}

/** One version of a collection, as the explanation shows it. */
export interface CollectionVersion {
	id: string;
	collected_on: string;
	shift: string;
	quantity: string;
	quantity_unit: string;
	fat?: string;
	snf?: string;
	rate_card_id: string;
	rate?: string;
	amount: string;
	explanation: string;
	origin_kind: string;
	source_system_id?: string;
	source_record_id?: string;
	created_at?: string;
	superseded_at?: string;
	superseded_by?: string;
	supersedes?: string;
	correction_reason?: string;
}

export interface ExplainedLine {
	paid: StatementLine;
	current?: CollectionVersion;
	versions?: CollectionVersion[];
	/** False means the figure paid is not the figure the platform now holds. */
	paid_matches_current: boolean;
}

export interface RateCardUse {
	id: string;
	name?: string;
	kind?: string;
	basis?: string;
	currency?: string;
	valid_from?: string;
	valid_to?: string;
	lines_priced: number;
	unreadable?: string;
}

export interface IdentityMapping {
	id: string;
	entity_id: string;
	method: string;
	note?: string;
	valid_from: string;
	valid_to: string;
	recorded_at: string;
	superseded_at?: string;
	superseded_by?: string;
}

export interface IdentityTrace {
	source_system_id: string;
	external_id: string;
	lines: number;
	mappings: IdentityMapping[];
}

/** Which services answered, and where one did not, why. */
export interface Consulted {
	procurement: boolean;
	canonical: boolean;
	procurement_why_not?: string;
	canonical_why_not?: string;
}

export interface Explanation {
	payable: Payable;
	cycle: Cycle;
	lines: ExplainedLine[];
	deductions: StatementDeduction[];
	rate_cards: RateCardUse[];
	identities: IdentityTrace[];
	findings: string[];
	consulted: Consulted;
}

export interface OpenCycleRequest {
	tenant_id: string;
	society_code: string;
	name: string;
	period_start: string;
	period_end: string;
	currency: string;
	amount_scale: number;
	deduction_policy: string;
	actor: string;
}
export interface CycleResponse {
	cycle: Cycle;
}
export interface ListCyclesRequest {
	tenant_id: string;
	society_code?: string;
	limit?: number;
	offset?: number;
}
export interface ListCyclesResponse {
	cycles: Cycle[];
}
export interface CycleActionRequest {
	tenant_id: string;
	cycle_id: string;
	actor: string;
}
export interface AbandonCycleResponse {
	abandoned: boolean;
}
export interface OpenRecoveryRequest {
	tenant_id: string;
	producer_ref: string;
	kind: string;
	reference?: string;
	currency: string;
	amount_scale: number;
	principal: string;
	instalment?: string;
	already_recovered?: string;
	priority: number;
	opened_on: string;
	actor: string;
}
export interface RecoveryResponse {
	recovery: Recovery;
}
export interface ListRecoveriesRequest {
	tenant_id: string;
	producer_ref?: string;
	outstanding_only?: boolean;
}
export interface ListRecoveriesResponse {
	recoveries: Recovery[];
}
export interface ListPayablesRequest {
	tenant_id: string;
	cycle_id: string;
}
export interface ListPayablesResponse {
	payables: Payable[];
	total_net?: string;
	total_net_minor_units: number;
	total_gross?: string;
	currency?: string;
}
export interface PayableResponse {
	payable: Payable;
}
export interface MarkPaidRequest {
	tenant_id: string;
	id: string;
	payment_reference?: string;
	actor: string;
}
export interface HoldPayableRequest {
	tenant_id: string;
	id: string;
	reason: string;
	actor: string;
}
export interface ApprovePayableRequest {
	tenant_id: string;
	id: string;
	actor: string;
}
export interface RaiseAdjustmentRequest {
	tenant_id: string;
	cycle_id: string;
	producer_ref: string;
	currency: string;
	amount_scale: number;
	amount: string;
	adjusts_payable_id?: string;
	reason: string;
	actor: string;
}
export interface GetProducerStatementRequest {
	tenant_id: string;
	cycle_id: string;
	producer_ref: string;
}
export interface GetProducerStatementResponse {
	statement: Statement;
}
export interface ExplainPayableRequest {
	tenant_id: string;
	id: string;
}
export interface PrintStatementRequest {
	tenant_id: string;
	cycle_id: string;
	producer_ref: string;
	width?: number;
	society_name?: string;
}
export interface PrintStatementResponse {
	producer_ref: string;
	/** A fixed-width page, ready for a dot-matrix printer at the society office. */
	page: string;
	width: number;
}
export interface PrintCycleRequest {
	tenant_id: string;
	cycle_id: string;
	width?: number;
	society_name?: string;
}
export interface PrintCycleResponse {
	statements: PrintStatementResponse[];
	width: number;
}

/* ---- billing ---- */

export interface Invoice {
	id: string;
	tenant_id: string;
	customer_id: string;
	invoice_number: string;
	reference_id?: string;
	reference_type?: string;
	status: string;
	sub_total: string;
	tax_amount: string;
	total_amount: string;
	currency: string;
	tax_inclusive: boolean;
	issued_at: string;
	due_at: string;
	paid_at?: string;
	notes?: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface InvoiceItem {
	id: string;
	tenant_id: string;
	invoice_id: string;
	description: string;
	/** exact.Fixed. */
	quantity: string;
	unit_price: string;
	total_price: string;
	currency: string;
	/** exact.Fixed, as a percentage. */
	tax_rate: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface Payment {
	id: string;
	tenant_id: string;
	invoice_id: string;
	amount: string;
	currency: string;
	payment_method: string;
	reference_no?: string;
	paid_at: string;
	notes?: string;
	created_by: string;
	updated_by: string;
}

export interface CreateInvoiceRequest {
	tenant_id: string;
	customer_id: string;
	reference_id: string;
	reference_type: string;
	currency: string;
	tax_inclusive: boolean;
	issued_at: string;
	notes: string;
	created_by: string;
}
export interface InvoiceResponse {
	invoice: Invoice;
}
export interface AddInvoiceItemRequest {
	tenant_id: string;
	invoice_id: string;
	description: string;
	quantity: string;
	unit_price: string;
	tax_rate: string;
	created_by: string;
}
export interface InvoiceItemResponse {
	item: InvoiceItem;
	invoice?: Invoice;
}
export interface InvoiceActionRequest {
	id: string;
	tenant_id: string;
	updated_by: string;
}
export interface RecordPaymentRequest {
	tenant_id: string;
	invoice_id: string;
	amount: string;
	payment_method: string;
	reference_no: string;
	paid_at: string;
	notes: string;
	created_by: string;
}
export interface PaymentResponse {
	payment: Payment;
	invoice?: Invoice;
}
export interface ListInvoicesResponse {
	invoices: Invoice[];
}
