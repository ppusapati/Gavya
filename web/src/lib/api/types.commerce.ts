/**
 * Commerce: the catalogue, the stock, the order book, and the cattle market.
 *
 * Field names mirror the Go handlers' json tags exactly, and clients_test.go
 * compares the two on every run of the gate.
 *
 * Every price and every quantity is a decimal string — `exact.Fixed` and the
 * money views both marshal that way — and none of them is typed `number` here.
 * A price rounded in a browser is a price the customer was quoted and the
 * platform never agreed to.
 */

/* ---- catalogue ---- */

export interface Category {
	id: string;
	tenant_id: string;
	name: string;
	slug: string;
	parent_id?: string;
	description: string;
	sort_order: number;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface Brand {
	id: string;
	tenant_id: string;
	name: string;
	slug: string;
	logo_url: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface Product {
	id: string;
	tenant_id: string;
	category_id: string;
	brand_id: string;
	name: string;
	slug: string;
	description: string;
	product_type: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface SKU {
	id: string;
	tenant_id: string;
	product_id: string;
	code: string;
	name: string;
	price: string;
	currency: string;
	unit: string;
	/** exact.Fixed — 500.000 of `unit`, not a count of packs. */
	unit_size: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface CreateCategoryRequest {
	tenant_id: string;
	name: string;
	slug: string;
	parent_id?: string;
	description: string;
	sort_order: number;
	created_by: string;
}
export interface CategoryResponse {
	category: Category;
}
export interface ListCategoriesResponse {
	categories: Category[];
}
export interface CreateBrandRequest {
	tenant_id: string;
	name: string;
	slug: string;
	logo_url: string;
	created_by: string;
}
export interface BrandResponse {
	brand: Brand;
}
export interface ListBrandsResponse {
	brands: Brand[];
}
export interface CreateProductRequest {
	tenant_id: string;
	category_id: string;
	brand_id: string;
	name: string;
	slug: string;
	description: string;
	product_type: string;
	status: string;
	created_by: string;
}
export interface ProductResponse {
	product: Product;
}
export interface ListProductsRequest {
	tenant_id: string;
	product_type: string;
	status: string;
}
export interface ListProductsResponse {
	products: Product[];
}
export interface CreateSKURequest {
	tenant_id: string;
	product_id: string;
	code: string;
	name: string;
	price: string;
	currency: string;
	unit: string;
	unit_size: string;
	status: string;
	created_by: string;
}
export interface SKUResponse {
	sku: SKU;
}
export interface ListProductSKUsRequest {
	product_id: string;
	tenant_id: string;
}
export interface ListProductSKUsResponse {
	skus: SKU[];
}
export interface UpdateSKUPriceRequest {
	id: string;
	tenant_id: string;
	price: string;
	updated_by: string;
}

/* ---- inventory ---- */

export interface Warehouse {
	id: string;
	tenant_id: string;
	name: string;
	code: string;
	address: string;
	manager_id: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface InventoryItem {
	id: string;
	tenant_id: string;
	warehouse_id: string;
	sku_id: string;
	quantity_on_hand: string;
	quantity_reserved: string;
	reorder_point: string;
	max_stock: string;
	last_updated_at: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface StockMovement {
	id: string;
	tenant_id: string;
	warehouse_id: string;
	sku_id: string;
	movement_type: string;
	/**
	 * Always positive: the direction is carried by movement_type, and a negative
	 * quantity would be an `out` disguised as an `in`.
	 *
	 * An `adjustment` is not a delta. It states the count after a stocktake and
	 * replaces the running total — the service's own repository does exactly
	 * that, and a client sending the difference would set the shelf to the
	 * difference.
	 */
	quantity: string;
	reference_id: string;
	reference_type: string;
	notes: string;
	moved_at: string;
	moved_by: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface Batch {
	id: string;
	tenant_id: string;
	warehouse_id: string;
	sku_id: string;
	batch_number: string;
	quantity: string;
	manufactured_at?: string;
	expires_at?: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface CreateWarehouseRequest {
	tenant_id: string;
	name: string;
	code: string;
	address: string;
	manager_id: string;
	status: string;
	created_by: string;
}
export interface WarehouseResponse {
	warehouse: Warehouse;
}
export interface ListWarehousesResponse {
	warehouses: Warehouse[];
}
export interface AdjustStockRequest {
	tenant_id: string;
	warehouse_id: string;
	sku_id: string;
	movement_type: string;
	quantity: string;
	reference_id: string;
	reference_type: string;
	notes: string;
	moved_at: string;
	moved_by: string;
	created_by: string;
}
export interface StockMovementResponse {
	movement: StockMovement;
	item?: InventoryItem;
}
export interface ListStockMovementsRequest {
	tenant_id: string;
	warehouse_id: string;
	limit: number;
	offset: number;
}
export interface ListStockMovementsResponse {
	movements: StockMovement[];
}
export interface CreateBatchRequest {
	tenant_id: string;
	warehouse_id: string;
	sku_id: string;
	batch_number: string;
	quantity: string;
	manufactured_at?: string;
	expires_at?: string;
	status: string;
	created_by: string;
}
export interface BatchResponse {
	batch: Batch;
}
export interface ListBatchesResponse {
	batches: Batch[];
}

/* ---- orders ---- */

export interface Order {
	id: string;
	tenant_id: string;
	customer_id: string;
	order_number: string;
	status: string;
	sub_total: string;
	tax_amount: string;
	total_amount: string;
	currency: string;
	tax_inclusive: boolean;
	shipping_address: string;
	notes: string;
	ordered_at: string;
	delivered_at?: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface OrderItem {
	id: string;
	tenant_id: string;
	order_id: string;
	sku_id: string;
	product_id: string;
	quantity: string;
	unit_price: string;
	total_price: string;
	currency: string;
	/** exact.Fixed, as a percentage. */
	tax_rate: string;
	status: string;
	created_at: string;
	updated_at: string;
	created_by: string;
	updated_by: string;
}

export interface OrderInvoice {
	id: string;
	tenant_id: string;
	order_id: string;
	invoice_number: string;
	status: string;
	sub_total: string;
	tax_amount: string;
	total_amount: string;
	currency: string;
	issued_at: string;
	due_at: string;
	paid_at?: string;
	created_by: string;
	updated_by: string;
}

export interface Return {
	id: string;
	tenant_id: string;
	order_id: string;
	reason: string;
	status: string;
	refund_amount: string;
	currency: string;
	requested_at: string;
	processed_at?: string;
	created_by: string;
	updated_by: string;
}

export interface CreateOrderRequest {
	tenant_id: string;
	customer_id: string;
	currency: string;
	tax_inclusive: boolean;
	shipping_address: string;
	notes: string;
	ordered_at: string;
	created_by: string;
}
export interface OrderResponse {
	order: Order;
}
export interface AddOrderItemRequest {
	tenant_id: string;
	order_id: string;
	sku_id: string;
	product_id: string;
	quantity: string;
	unit_price: string;
	tax_rate: string;
	created_by: string;
}
export interface OrderItemResponse {
	item: OrderItem;
	order?: Order;
}
export interface OrderActionRequest {
	id: string;
	tenant_id: string;
	updated_by: string;
}
export interface GenerateInvoiceRequest {
	order_id: string;
	tenant_id: string;
	created_by: string;
}
export interface OrderInvoiceResponse {
	invoice: OrderInvoice;
}
export interface RequestReturnRequest {
	tenant_id: string;
	order_id: string;
	reason: string;
	refund_amount: string;
	created_by: string;
}
export interface ReturnResponse {
	return: Return;
}
export interface DecideReturnRequest {
	id: string;
	tenant_id: string;
	status: string;
	updated_by: string;
}
export interface ListOrderReturnsRequest {
	order_id: string;
	tenant_id: string;
}
export interface ListReturnsResponse {
	returns: Return[];
}

/* ---- the cattle market ---- */

export interface Listing {
	id: string;
	tenant_id: string;
	cattle_id: string;
	title: string;
	asking_price: string;
	currency: string;
	listing_type: string;
	status: string;
}

export interface Bid {
	id: string;
	tenant_id: string;
	listing_id: string;
	bidder_id: string;
	bid_amount: string;
	currency: string;
	status: string;
}

export interface Sale {
	id: string;
	tenant_id: string;
	listing_id: string;
	sale_price: string;
	currency: string;
	status: string;
}

export interface Ownership {
	id: string;
	tenant_id: string;
	cattle_id: string;
	owner_id: string;
	acquisition_type: string;
}

export interface CreateListingRequest {
	tenant_id: string;
	cattle_id: string;
	seller_id: string;
	title: string;
	description: string;
	asking_price: string;
	currency: string;
	listing_type: string;
	created_by: string;
}
export interface ListingResponse {
	listing: Listing;
}
export interface ListActiveRequest {
	tenant_id: string;
	limit: number;
	offset: number;
}
export interface ListActiveResponse {
	listings: Listing[];
}
export interface PlaceBidRequest {
	tenant_id: string;
	listing_id: string;
	bidder_id: string;
	bid_amount: string;
	message: string;
	created_by: string;
}
export interface BidResponse {
	bid: Bid;
}
export interface BidActionRequest {
	id: string;
	tenant_id: string;
	updated_by: string;
}
export interface RecordSaleRequest {
	tenant_id: string;
	listing_id: string;
	seller_id: string;
	buyer_id: string;
	cattle_id: string;
	sale_price: string;
	created_by: string;
}
export interface SaleResponse {
	sale: Sale;
}
export interface OwnershipRequest {
	tenant_id: string;
	cattle_id: string;
}
export interface OwnershipResponse {
	history: Ownership[];
}
