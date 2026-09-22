import type { ApiClient, CallOptions } from './client';
import * as T from './types';

/**
 * Commerce, as procedures: the catalogue, the stock, the order book, the market.
 *
 * Thirty-seven procedures across four services. Same shape as the other
 * sub-facades and the same rule about amounts: the string the operator typed
 * goes out, and the service parses it at the scale it holds.
 */
export class CommerceApi {
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

	/* ---- catalogue ---- */

	listCategories(extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string }, T.ListCategoriesResponse>(
			`${T.CATALOGUE}/ListCategories`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createCategory(req: Omit<T.CreateCategoryRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateCategoryRequest, T.CategoryResponse>(
			`${T.CATALOGUE}/CreateCategory`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listBrands(extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string }, T.ListBrandsResponse>(
			`${T.CATALOGUE}/ListBrands`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createBrand(req: Omit<T.CreateBrandRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateBrandRequest, T.BrandResponse>(
			`${T.CATALOGUE}/CreateBrand`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listProducts(req: { product_type?: string; status?: string } = {}, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListProductsRequest, T.ListProductsResponse>(
			`${T.CATALOGUE}/ListProducts`,
			{ tenant_id: this.#tenantId, product_type: req.product_type ?? '', status: req.status ?? '' },
			this.#opts(extra)
		);
	}

	getProduct(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.ProductResponse>(
			`${T.CATALOGUE}/GetProduct`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createProduct(req: Omit<T.CreateProductRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateProductRequest, T.ProductResponse>(
			`${T.CATALOGUE}/CreateProduct`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listProductSKUs(productId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListProductSKUsRequest, T.ListProductSKUsResponse>(
			`${T.CATALOGUE}/ListProductSKUs`,
			{ product_id: productId, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getSKU(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.SKUResponse>(
			`${T.CATALOGUE}/GetSKU`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createSKU(req: Omit<T.CreateSKURequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateSKURequest, T.SKUResponse>(
			`${T.CATALOGUE}/CreateSKU`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	updateSKUPrice(req: Omit<T.UpdateSKUPriceRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.UpdateSKUPriceRequest, T.SKUResponse>(
			`${T.CATALOGUE}/UpdateSKUPrice`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/* ---- inventory ---- */

	listWarehouses(extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string }, T.ListWarehousesResponse>(
			`${T.INVENTORY}/ListWarehouses`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	getWarehouse(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.WarehouseResponse>(
			`${T.INVENTORY}/GetWarehouse`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createWarehouse(req: Omit<T.CreateWarehouseRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateWarehouseRequest, T.WarehouseResponse>(
			`${T.INVENTORY}/CreateWarehouse`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/**
	 * A movement in, out, transferred, or the count after a stocktake.
	 *
	 * `adjustment` replaces the running total rather than moving it, so what goes
	 * in the quantity is what was counted on the shelf — not the difference.
	 */
	adjustStock(req: Omit<T.AdjustStockRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AdjustStockRequest, T.StockMovementResponse>(
			`${T.INVENTORY}/AdjustStock`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	listStockMovements(
		req: { warehouse_id: string; limit?: number; offset?: number },
		extra?: Partial<CallOptions>
	) {
		return this.#client.call<T.ListStockMovementsRequest, T.ListStockMovementsResponse>(
			`${T.INVENTORY}/ListStockMovements`,
			{
				tenant_id: this.#tenantId,
				warehouse_id: req.warehouse_id,
				limit: req.limit ?? 50,
				offset: req.offset ?? 0
			},
			this.#opts(extra)
		);
	}

	createBatch(req: Omit<T.CreateBatchRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateBatchRequest, T.BatchResponse>(
			`${T.INVENTORY}/CreateBatch`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getBatch(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.BatchResponse>(
			`${T.INVENTORY}/GetBatch`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listExpiringBatches(extra?: Partial<CallOptions>) {
		return this.#client.call<{ tenant_id: string }, T.ListBatchesResponse>(
			`${T.INVENTORY}/ListExpiringBatches`,
			{ tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/* ---- orders ---- */

	getOrder(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.OrderResponse>(
			`${T.ORDER}/GetOrder`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createOrder(req: Omit<T.CreateOrderRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateOrderRequest, T.OrderResponse>(
			`${T.ORDER}/CreateOrder`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	addOrderItem(req: Omit<T.AddOrderItemRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.AddOrderItemRequest, T.OrderItemResponse>(
			`${T.ORDER}/AddOrderItem`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	confirmOrder(id: string, updatedBy: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.OrderActionRequest, T.OrderResponse>(
			`${T.ORDER}/ConfirmOrder`,
			{ id, tenant_id: this.#tenantId, updated_by: updatedBy },
			this.#opts(extra)
		);
	}

	cancelOrder(id: string, updatedBy: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.OrderActionRequest, T.OrderResponse>(
			`${T.ORDER}/CancelOrder`,
			{ id, tenant_id: this.#tenantId, updated_by: updatedBy },
			this.#opts(extra)
		);
	}

	generateInvoice(orderId: string, createdBy: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.GenerateInvoiceRequest, T.OrderInvoiceResponse>(
			`${T.ORDER}/GenerateInvoice`,
			{ order_id: orderId, tenant_id: this.#tenantId, created_by: createdBy },
			this.#opts(extra)
		);
	}

	requestReturn(req: Omit<T.RequestReturnRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RequestReturnRequest, T.ReturnResponse>(
			`${T.ORDER}/RequestReturn`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	decideReturn(req: Omit<T.DecideReturnRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.DecideReturnRequest, T.ReturnResponse>(
			`${T.ORDER}/DecideReturn`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	getReturn(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.ReturnResponse>(
			`${T.ORDER}/GetReturn`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	listOrderReturns(orderId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListOrderReturnsRequest, T.ListReturnsResponse>(
			`${T.ORDER}/ListOrderReturns`,
			{ order_id: orderId, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	/* ---- the cattle market ---- */

	listActiveListings(req: { limit?: number; offset?: number } = {}, extra?: Partial<CallOptions>) {
		return this.#client.call<T.ListActiveRequest, T.ListActiveResponse>(
			`${T.MARKET}/ListActiveListings`,
			{ tenant_id: this.#tenantId, limit: req.limit ?? 50, offset: req.offset ?? 0 },
			this.#opts(extra)
		);
	}

	getListing(id: string, extra?: Partial<CallOptions>) {
		return this.#client.call<{ id: string; tenant_id: string }, T.ListingResponse>(
			`${T.MARKET}/GetListing`,
			{ id, tenant_id: this.#tenantId },
			this.#opts(extra)
		);
	}

	createListing(req: Omit<T.CreateListingRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.CreateListingRequest, T.ListingResponse>(
			`${T.MARKET}/CreateListing`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	placeBid(req: Omit<T.PlaceBidRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.PlaceBidRequest, T.BidResponse>(
			`${T.MARKET}/PlaceBid`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	acceptBid(id: string, updatedBy: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.BidActionRequest, T.BidResponse>(
			`${T.MARKET}/AcceptBid`,
			{ id, tenant_id: this.#tenantId, updated_by: updatedBy },
			this.#opts(extra)
		);
	}

	rejectBid(id: string, updatedBy: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.BidActionRequest, T.BidResponse>(
			`${T.MARKET}/RejectBid`,
			{ id, tenant_id: this.#tenantId, updated_by: updatedBy },
			this.#opts(extra)
		);
	}

	recordSale(req: Omit<T.RecordSaleRequest, 'tenant_id'>, extra?: Partial<CallOptions>) {
		return this.#client.call<T.RecordSaleRequest, T.SaleResponse>(
			`${T.MARKET}/RecordSale`,
			{ tenant_id: this.#tenantId, ...req },
			this.#opts(extra)
		);
	}

	/** Who has owned this animal, and how each of them came by it. */
	getOwnershipHistory(cattleId: string, extra?: Partial<CallOptions>) {
		return this.#client.call<T.OwnershipRequest, T.OwnershipResponse>(
			`${T.MARKET}/GetOwnershipHistory`,
			{ tenant_id: this.#tenantId, cattle_id: cattleId },
			this.#opts(extra)
		);
	}
}
