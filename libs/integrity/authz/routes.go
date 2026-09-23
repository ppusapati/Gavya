// Code in this file is written out on purpose. See the note on Table.
package authz

// Table is the permission each procedure requires.
//
// Every one of the platform's procedures appears here exactly once, and the
// exhaustive test in this package fails the build if a route is added without an
// entry or an entry names a route that no longer exists. That test is the whole
// reason this is a table rather than a rule: fail-closed only helps if somebody
// finds out, and finding out at build time beats finding out when a co-operative
// cannot record its morning collection.
//
// The first draft was generated from the service name and the method's leading
// verb, and then read through by hand. Ten entries were wrong, and they are
// worth naming because they are why the generated rule is not the thing that
// runs:
//
//   - ResolveIdentity is a lookup, not a decision. The rule saw "Resolve" and
//     filed it with ResolveConflict, which is a person adjudicating a disputed
//     slot. One is what every collection does on its way in; the other is a
//     supervisor's judgement.
//   - CorrectCollection re-prices the collection against the rate card in force,
//     so it changes what a producer is paid. The rule called it an ordinary
//     write, which would have let the collector who recorded a figure amend it
//     afterwards with nobody else involved.
//   - ConfirmPregnancy records a vet's finding. "Confirm" made it an approval,
//     which would have stopped a clerk entering it.
//   - MarkAsRead, MarkAllRead and CloseSession are routine, and the rule made
//     approvals of them. A device that cannot close its own capture session is
//     a device that cannot work.
//   - DeclareRetroactivityPolicy decides how far back money may be moved. The
//     rule made it an ordinary write because it begins with "Declare".
//
// Six of those ten would have handed somebody a power they should not have, and
// four would have taken away one they need. Neither is visible from the outside:
// the first looks like a working system and the second looks like a bug in
// whatever the person was trying to do.
var table = map[string]Permission{

	// audit.v1.AuditService
	"audit.v1.AuditService/CreateAuditLog":          AuditWrite,
	"audit.v1.AuditService/GetAuditLog":             AuditRead,
	"audit.v1.AuditService/ListAuditLogs":           AuditRead,
	"audit.v1.AuditService/ListAuditLogsByResource": AuditRead,
	"audit.v1.AuditService/ListAuditLogsByActor":    AuditRead,
	"audit.v1.AuditService/SealAuditChain":          AuditWrite,
	"audit.v1.AuditService/VerifyAuditChain":        AuditWrite,

	// balance.v1.BalanceService
	"balance.v1.BalanceService/CreateWindow":  PlantWrite,
	"balance.v1.BalanceService/GetWindow":     PlantRead,
	"balance.v1.BalanceService/ListWindows":   PlantRead,
	"balance.v1.BalanceService/AddFlow":       PlantWrite,
	"balance.v1.BalanceService/ListFlows":     PlantRead,
	"balance.v1.BalanceService/Observability": PlantRead,
	"balance.v1.BalanceService/Reconcile":     PlantWrite,
	"balance.v1.BalanceService/GetRun":        PlantRead,
	"balance.v1.BalanceService/ListRuns":      PlantRead,
	"balance.v1.BalanceService/AcceptRun":     PlantApprove,

	// billing.v1.BillingService
	"billing.v1.BillingService/CreateInvoice":          SalesWrite,
	"billing.v1.BillingService/AddInvoiceItem":         SalesWrite,
	"billing.v1.BillingService/SendInvoice":            SalesWrite,
	"billing.v1.BillingService/RecordPayment":          SalesWrite,
	"billing.v1.BillingService/VoidInvoice":            SalesWrite,
	"billing.v1.BillingService/GetOutstandingInvoices": SalesRead,

	// breeding.v1.BreedingService
	"breeding.v1.BreedingService/CreateBreedingCycle":   HerdWrite,
	"breeding.v1.BreedingService/RecordInsemination":    HerdWrite,
	"breeding.v1.BreedingService/ConfirmPregnancy":      HerdWrite,
	"breeding.v1.BreedingService/RecordCalving":         HerdWrite,
	"breeding.v1.BreedingService/GetBreedingHistory":    HerdRead,
	"breeding.v1.BreedingService/ListActivePregnancies": HerdRead,

	// canonical.v1.CanonicalService
	"canonical.v1.CanonicalService/MapIdentity":        IdentityWrite,
	"canonical.v1.CanonicalService/ResolveIdentity":    IdentityRead,
	"canonical.v1.CanonicalService/ReverseResolve":     IdentityRead,
	"canonical.v1.CanonicalService/ListIdentities":     IdentityRead,
	"canonical.v1.CanonicalService/GetIdentityHistory": IdentityRead,
	"canonical.v1.CanonicalService/RetireIdentity":     IdentityAdmin,
	"canonical.v1.CanonicalService/DeclarePolicy":      IdentityWrite,
	"canonical.v1.CanonicalService/GetEffectivePolicy": IdentityRead,
	"canonical.v1.CanonicalService/ListPolicies":       IdentityRead,
	"canonical.v1.CanonicalService/ClaimSlot":          IdentityWrite,
	"canonical.v1.CanonicalService/GetSlot":            IdentityRead,
	"canonical.v1.CanonicalService/ListConflicts":      IdentityRead,
	"canonical.v1.CanonicalService/ResolveConflict":    IdentityApprove,

	// cattle.v1.CattleService
	"cattle.v1.CattleService/CreateCattle": HerdWrite,
	"cattle.v1.CattleService/GetCattle":    HerdRead,
	"cattle.v1.CattleService/ListCattle":   HerdRead,
	"cattle.v1.CattleService/UpdateCattle": HerdWrite,
	"cattle.v1.CattleService/DeleteCattle": HerdAdmin,
	"cattle.v1.CattleService/CreateBreed":  HerdWrite,
	"cattle.v1.CattleService/ListBreeds":   HerdRead,

	// cattlemarket.v1.CattleMarketService
	"cattlemarket.v1.CattleMarketService/CreateListing":       MarketWrite,
	"cattlemarket.v1.CattleMarketService/GetListing":          MarketRead,
	"cattlemarket.v1.CattleMarketService/ListActiveListings":  MarketRead,
	"cattlemarket.v1.CattleMarketService/PlaceBid":            MarketWrite,
	"cattlemarket.v1.CattleMarketService/AcceptBid":           MarketApprove,
	"cattlemarket.v1.CattleMarketService/RejectBid":           MarketApprove,
	"cattlemarket.v1.CattleMarketService/RecordSale":          MarketWrite,
	"cattlemarket.v1.CattleMarketService/GetOwnershipHistory": MarketRead,

	// farm.v1.FarmService
	"farm.v1.FarmService/CreateFarm":         HerdWrite,
	"farm.v1.FarmService/GetFarm":            HerdRead,
	"farm.v1.FarmService/ListFarms":          HerdRead,
	"farm.v1.FarmService/UpdateFarm":         HerdWrite,
	"farm.v1.FarmService/CreateFarmSection":  HerdWrite,
	"farm.v1.FarmService/ListFarmSections":   HerdRead,
	"farm.v1.FarmService/UpdateFarmCapacity": HerdWrite,

	// feed.v1.FeedService
	"feed.v1.FeedService/CreateFeedType":           HerdWrite,
	"feed.v1.FeedService/ListFeedTypes":            HerdRead,
	"feed.v1.FeedService/CreateNutritionPlan":      HerdWrite,
	"feed.v1.FeedService/GetNutritionPlan":         HerdRead,
	"feed.v1.FeedService/RecordFeedConsumption":    HerdWrite,
	"feed.v1.FeedService/GetFeedConsumptionReport": HerdRead,

	// file.v1.FileService
	"file.v1.FileService/CreateFileRecord": PlatformWrite,
	"file.v1.FileService/GetFileRecord":    PlatformRead,
	"file.v1.FileService/ListEntityFiles":  PlatformRead,
	"file.v1.FileService/DeleteFile":       PlatformAdmin,
	"file.v1.FileService/GetDownloadURL":   PlatformRead,

	// gavya.identity.v1.IdentityService — administration
	//
	// Setting up a co-operative: its people, what they may do, and the
	// credentials its machines hold. tenant.admin throughout for the changes,
	// because handing somebody the accountant role is a larger act than anything
	// the accountant role can then do.
	"gavya.identity.v1.IdentityService/ListRoles":             TenantRead,
	"gavya.identity.v1.IdentityService/ListMembers":           TenantRead,
	"gavya.identity.v1.IdentityService/ListServiceIdentities": TenantRead,
	"gavya.identity.v1.IdentityService/AddMember":             TenantAdmin,
	"gavya.identity.v1.IdentityService/SetMemberRole":         TenantAdmin,
	"gavya.identity.v1.IdentityService/SetMemberStatus":       TenantAdmin,
	"gavya.identity.v1.IdentityService/SetMemberPassword":     TenantAdmin,
	"gavya.identity.v1.IdentityService/IssueServiceIdentity":  TenantAdmin,
	"gavya.identity.v1.IdentityService/RevokeServiceIdentity": TenantAdmin,
	// The one administration route needing no permission. A person holding no
	// role at all must still be able to change the credential they were handed,
	// and the current password is what authorises the change. The gateway still
	// requires a session to reach it: Public here means "no particular
	// permission", not "no authentication".
	"gavya.identity.v1.IdentityService/ChangePassword": Public,

	// gavya.identity.v1.IdentityService
	"gavya.identity.v1.IdentityService/SignIn":            Public,
	"gavya.identity.v1.IdentityService/SignInService":     Public,
	"gavya.identity.v1.IdentityService/VerifySession":     Public,
	"gavya.identity.v1.IdentityService/SignOut":           Public,
	"gavya.identity.v1.IdentityService/SignOutEverywhere": Public,

	// health.v1.HealthService
	"health.v1.HealthService/RecordVaccination":        HerdWrite,
	"health.v1.HealthService/GetVaccinationHistory":    HerdRead,
	"health.v1.HealthService/RecordTreatment":          HerdWrite,
	"health.v1.HealthService/GetTreatmentHistory":      HerdRead,
	"health.v1.HealthService/ScheduleVetVisit":         HerdWrite,
	"health.v1.HealthService/ListUpcomingVaccinations": HerdRead,

	// ingestion.v1.IngestionService
	"ingestion.v1.IngestionService/DeliverRecord":     PlatformWrite,
	"ingestion.v1.IngestionService/DeliverBatch":      PlatformWrite,
	"ingestion.v1.IngestionService/RegisterDevice":    PlatformWrite,
	"ingestion.v1.IngestionService/GetDevice":         PlatformRead,
	"ingestion.v1.IngestionService/ListDevices":       PlatformRead,
	"ingestion.v1.IngestionService/RollGeneration":    PlatformWrite,
	"ingestion.v1.IngestionService/ListGenerations":   PlatformRead,
	"ingestion.v1.IngestionService/OpenSession":       PlatformWrite,
	"ingestion.v1.IngestionService/CloseSession":      PlatformWrite,
	"ingestion.v1.IngestionService/ListSessions":      PlatformRead,
	"ingestion.v1.IngestionService/ListQuarantined":   PlatformRead,
	"ingestion.v1.IngestionService/GetQuarantined":    PlatformRead,
	"ingestion.v1.IngestionService/ResolveQuarantine": PlatformApprove,

	// inventory.v1.InventoryService
	"inventory.v1.InventoryService/CreateWarehouse":     PlantWrite,
	"inventory.v1.InventoryService/GetWarehouse":        PlantRead,
	"inventory.v1.InventoryService/ListWarehouses":      PlantRead,
	"inventory.v1.InventoryService/AdjustStock":         PlantWrite,
	"inventory.v1.InventoryService/ListStockMovements":  PlantRead,
	"inventory.v1.InventoryService/CreateBatch":         PlantWrite,
	"inventory.v1.InventoryService/GetBatch":            PlantRead,
	"inventory.v1.InventoryService/ListExpiringBatches": PlantRead,

	// laboratory.v1.LaboratoryService
	"laboratory.v1.LaboratoryService/DrawSample":      LabWrite,
	"laboratory.v1.LaboratoryService/GetSample":       LabRead,
	"laboratory.v1.LaboratoryService/ListSamples":     LabRead,
	"laboratory.v1.LaboratoryService/BreakSeal":       LabApprove,
	"laboratory.v1.LaboratoryService/RecordHandover":  LabWrite,
	"laboratory.v1.LaboratoryService/RecordResult":    LabWrite,
	"laboratory.v1.LaboratoryService/GetSampleReport": LabRead,

	// material.v1.MaterialService
	"material.v1.MaterialService/RegisterNode":       PlantWrite,
	"material.v1.MaterialService/GetNode":            PlantRead,
	"material.v1.MaterialService/ListNodes":          PlantRead,
	"material.v1.MaterialService/Dispatch":           PlantWrite,
	"material.v1.MaterialService/Receive":            PlantWrite,
	"material.v1.MaterialService/AbandonMovement":    PlantApprove,
	"material.v1.MaterialService/GetMovement":        PlantRead,
	"material.v1.MaterialService/ListMovements":      PlantRead,
	"material.v1.MaterialService/RegisterInstrument": PlantWrite,
	"material.v1.MaterialService/ListInstruments":    PlantRead,
	"material.v1.MaterialService/ProposeFlows":       PlantWrite,

	// milk.v1.MilkService
	"milk.v1.MilkService/CreateSession":       MilkWrite,
	"milk.v1.MilkService/GetSession":          MilkRead,
	"milk.v1.MilkService/ListSessions":        MilkRead,
	"milk.v1.MilkService/UpdateSessionStatus": MilkWrite,
	"milk.v1.MilkService/RecordMilk":          MilkWrite,
	"milk.v1.MilkService/GetRecord":           MilkRead,
	"milk.v1.MilkService/ListSessionRecords":  MilkRead,
	"milk.v1.MilkService/GetDailyYield":       MilkRead,
	"milk.v1.MilkService/RecordQuality":       MilkWrite,

	// notification.v1.NotificationService
	"notification.v1.NotificationService/SendNotification":  PlatformWrite,
	"notification.v1.NotificationService/GetNotification":   PlatformRead,
	"notification.v1.NotificationService/ListNotifications": PlatformRead,
	"notification.v1.NotificationService/MarkAsRead":        PlatformWrite,
	"notification.v1.NotificationService/MarkAllRead":       PlatformWrite,
	"notification.v1.NotificationService/CreateTemplate":    PlatformWrite,
	"notification.v1.NotificationService/ListTemplates":     PlatformRead,
	"notification.v1.NotificationService/GetUnreadCount":    PlatformRead,

	// observation.v1.ObservationService
	"observation.v1.ObservationService/RecordObservation":          LabWrite,
	"observation.v1.ObservationService/GetObservation":             LabRead,
	"observation.v1.ObservationService/ListObservationsForSubject": LabRead,
	"observation.v1.ObservationService/ListFlaggedObservations":    LabRead,
	"observation.v1.ObservationService/RegisterInstrument":         LabWrite,
	"observation.v1.ObservationService/GetInstrument":              LabRead,
	"observation.v1.ObservationService/RecordCertificate":          LabWrite,
	"observation.v1.ObservationService/GetActiveCertificate":       LabRead,

	// order.v1.OrderService
	"order.v1.OrderService/CreateOrder":      SalesWrite,
	"order.v1.OrderService/GetOrder":         SalesRead,
	"order.v1.OrderService/AddOrderItem":     SalesWrite,
	"order.v1.OrderService/ConfirmOrder":     SalesWrite,
	"order.v1.OrderService/CancelOrder":      SalesApprove,
	"order.v1.OrderService/GenerateInvoice":  SalesWrite,
	"order.v1.OrderService/RequestReturn":    SalesWrite,
	"order.v1.OrderService/DecideReturn":     SalesApprove,
	"order.v1.OrderService/GetReturn":        SalesRead,
	"order.v1.OrderService/ListOrderReturns": SalesRead,

	// pooling.v1.PoolingService
	"pooling.v1.PoolingService/CreatePool":                      SettlementWrite,
	"pooling.v1.PoolingService/GetPool":                         SettlementRead,
	"pooling.v1.PoolingService/ListPools":                       SettlementRead,
	"pooling.v1.PoolingService/AddProducerMilk":                 SettlementWrite,
	"pooling.v1.PoolingService/ListProducerMilk":                SettlementRead,
	"pooling.v1.PoolingService/RecordUtilisation":               SettlementWrite,
	"pooling.v1.PoolingService/ListUtilisations":                SettlementRead,
	"pooling.v1.PoolingService/ValuePool":                       SettlementWrite,
	"pooling.v1.PoolingService/GetValuation":                    SettlementRead,
	"pooling.v1.PoolingService/ListAllocations":                 SettlementRead,
	"pooling.v1.PoolingService/SettlePool":                      SettlementApprove,
	"pooling.v1.PoolingService/ListEconomicEvents":              SettlementRead,
	"pooling.v1.PoolingService/DeclareRetroactivityPolicy":      SettlementApprove,
	"pooling.v1.PoolingService/GetEffectiveRetroactivityPolicy": SettlementRead,
	"pooling.v1.PoolingService/ApplyCorrection":                 SettlementApprove,

	// procurement.v1.ProcurementService
	"procurement.v1.ProcurementService/DeclareRateCard":       RateCardWrite,
	"procurement.v1.ProcurementService/GetRateCard":           RateCardRead,
	"procurement.v1.ProcurementService/ListRateCards":         RateCardRead,
	"procurement.v1.ProcurementService/GetRateCardInForce":    RateCardRead,
	"procurement.v1.ProcurementService/RecordCollection":      MilkWrite,
	"procurement.v1.ProcurementService/GetCollection":         MilkRead,
	"procurement.v1.ProcurementService/ListCollections":       MilkRead,
	"procurement.v1.ProcurementService/CorrectCollection":     MilkApprove,
	"procurement.v1.ProcurementService/GetCollectionVersions": MilkRead,

	// productcatalog.v1.ProductCatalogService
	"productcatalog.v1.ProductCatalogService/CreateCategory":  CatalogueWrite,
	"productcatalog.v1.ProductCatalogService/ListCategories":  CatalogueRead,
	"productcatalog.v1.ProductCatalogService/CreateBrand":     CatalogueWrite,
	"productcatalog.v1.ProductCatalogService/ListBrands":      CatalogueRead,
	"productcatalog.v1.ProductCatalogService/CreateProduct":   CatalogueWrite,
	"productcatalog.v1.ProductCatalogService/GetProduct":      CatalogueRead,
	"productcatalog.v1.ProductCatalogService/ListProducts":    CatalogueRead,
	"productcatalog.v1.ProductCatalogService/CreateSKU":       CatalogueWrite,
	"productcatalog.v1.ProductCatalogService/GetSKU":          CatalogueRead,
	"productcatalog.v1.ProductCatalogService/ListProductSKUs": CatalogueRead,
	"productcatalog.v1.ProductCatalogService/UpdateSKUPrice":  CatalogueWrite,

	// production.v1.ProductionService
	"production.v1.ProductionService/CreateBatch":         PlantWrite,
	"production.v1.ProductionService/GetBatch":            PlantRead,
	"production.v1.ProductionService/ListBatches":         PlantRead,
	"production.v1.ProductionService/SetBatchStatus":      PlantWrite,
	"production.v1.ProductionService/RecordInput":         PlantWrite,
	"production.v1.ProductionService/GetBatchGenealogy":   PlantRead,
	"production.v1.ProductionService/TraceBatch":          PlantRead,
	"production.v1.ProductionService/GetBatchYield":       PlantRead,
	"production.v1.ProductionService/CreateFormulation":   PlantWrite,
	"production.v1.ProductionService/ApproveFormulation":  PlantApprove,
	"production.v1.ProductionService/WithdrawFormulation": PlantApprove,
	"production.v1.ProductionService/GetFormulation":      PlantRead,
	"production.v1.ProductionService/ListFormulations":    PlantRead,
	"production.v1.ProductionService/CheckRecipe":         PlantRead,
	"production.v1.ProductionService/GetObservedYield":    PlantRead,

	// reporting.v1.ReportingService
	"reporting.v1.ReportingService/RequestReport":        PlatformWrite,
	"reporting.v1.ReportingService/GetReport":            PlatformRead,
	"reporting.v1.ReportingService/ListReports":          PlatformRead,
	"reporting.v1.ReportingService/GetReportDownloadURL": PlatformRead,
	// The report itself. Read, like everything else about a report: the content
	// is the tenant's own collections or its own settlement, and anybody
	// entitled to list a report is entitled to read what it says.
	"reporting.v1.ReportingService/GetReportContent": PlatformRead,
	// The catalogue. Read, because choosing a report to ask for is a thing
	// somebody does before they have asked for one.
	"reporting.v1.ReportingService/ListReportKinds": PlatformRead,
	"reporting.v1.ReportingService/CreateSchedule":  PlatformWrite,
	"reporting.v1.ReportingService/ListSchedules":   PlatformRead,
	"reporting.v1.ReportingService/UpdateSchedule":  PlatformWrite,
	"reporting.v1.ReportingService/DeleteSchedule":  PlatformAdmin,

	// settlement.v1.SettlementService
	"settlement.v1.SettlementService/OpenCycle":              SettlementWrite,
	"settlement.v1.SettlementService/GetCycle":               SettlementRead,
	"settlement.v1.SettlementService/ListCycles":             SettlementRead,
	"settlement.v1.SettlementService/GatherCycle":            SettlementWrite,
	"settlement.v1.SettlementService/ApproveCycle":           SettlementApprove,
	"settlement.v1.SettlementService/AbandonCycle":           SettlementApprove,
	"settlement.v1.SettlementService/OpenRecovery":           SettlementWrite,
	"settlement.v1.SettlementService/ListRecoveries":         SettlementRead,
	"settlement.v1.SettlementService/ListPayables":           SettlementRead,
	"settlement.v1.SettlementService/GetPayable":             SettlementRead,
	"settlement.v1.SettlementService/MarkPaid":               SettlementApprove,
	"settlement.v1.SettlementService/HoldPayable":            SettlementApprove,
	"settlement.v1.SettlementService/RaiseAdjustment":        SettlementWrite,
	"settlement.v1.SettlementService/ApprovePayable":         SettlementApprove,
	"settlement.v1.SettlementService/GetProducerStatement":   SettlementRead,
	"settlement.v1.SettlementService/ExplainPayable":         SettlementRead,
	"settlement.v1.SettlementService/PrintProducerStatement": SettlementRead,
	"settlement.v1.SettlementService/PrintCycleStatements":   SettlementRead,

	// shadowsettlement.v1.ShadowSettlementService
	"shadowsettlement.v1.ShadowSettlementService/IngestAssertion":   SettlementWrite,
	"shadowsettlement.v1.ShadowSettlementService/RecordComputation": SettlementWrite,
	"shadowsettlement.v1.ShadowSettlementService/Adjudicate":        SettlementWrite,
	"shadowsettlement.v1.ShadowSettlementService/GetDivergence":     SettlementRead,
	"shadowsettlement.v1.ShadowSettlementService/ListDivergences":   SettlementRead,
	"shadowsettlement.v1.ShadowSettlementService/ResolveDivergence": SettlementApprove,
	"shadowsettlement.v1.ShadowSettlementService/Summarise":         SettlementRead,

	// tenant.v1.TenantService
	"tenant.v1.TenantService/CreateTenant":        TenantWrite,
	"tenant.v1.TenantService/GetTenant":           TenantRead,
	"tenant.v1.TenantService/ListTenants":         TenantRead,
	"tenant.v1.TenantService/UpdateTenant":        TenantWrite,
	"tenant.v1.TenantService/SuspendTenant":       TenantAdmin,
	"tenant.v1.TenantService/ActivateTenant":      TenantAdmin,
	"tenant.v1.TenantService/UpsertTenantSetting": TenantWrite,
	"tenant.v1.TenantService/ListTenantSettings":  TenantRead,
}

// Table returns the permission table.
//
// A function rather than an exported variable so no caller can edit the map that
// every authorisation decision in the platform is made from.
func Table() map[string]Permission {
	out := make(map[string]Permission, len(table))
	for k, v := range table {
		out[k] = v
	}
	return out
}
