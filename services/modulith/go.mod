module github.com/ppusapati/gavya/services/modulith

go 1.26.6

require (
	github.com/ppusapati/gavya/libs/integrity v0.0.0
	github.com/ppusapati/gavya/services/audit-service v0.0.0
	github.com/ppusapati/gavya/services/balance-service v0.0.0
	github.com/ppusapati/gavya/services/billing-service v0.0.0
	github.com/ppusapati/gavya/services/breeding-service v0.0.0
	github.com/ppusapati/gavya/services/canonical-service v0.0.0
	github.com/ppusapati/gavya/services/cattle-market-service v0.0.0
	github.com/ppusapati/gavya/services/cattle-service v0.0.0
	github.com/ppusapati/gavya/services/farm-service v0.0.0
	github.com/ppusapati/gavya/services/feed-service v0.0.0
	github.com/ppusapati/gavya/services/file-service v0.0.0
	github.com/ppusapati/gavya/services/gateway-service v0.0.0
	github.com/ppusapati/gavya/services/health-service v0.0.0
	github.com/ppusapati/gavya/services/identity-service v0.0.0
	github.com/ppusapati/gavya/services/ingestion-service v0.0.0
	github.com/ppusapati/gavya/services/inventory-service v0.0.0
	github.com/ppusapati/gavya/services/laboratory-service v0.0.0
	github.com/ppusapati/gavya/services/material-service v0.0.0
	github.com/ppusapati/gavya/services/milk-service v0.0.0
	github.com/ppusapati/gavya/services/notification-service v0.0.0
	github.com/ppusapati/gavya/services/observation-service v0.0.0
	github.com/ppusapati/gavya/services/order-service v0.0.0
	github.com/ppusapati/gavya/services/pooling-service v0.0.0
	github.com/ppusapati/gavya/services/procurement-service v0.0.0
	github.com/ppusapati/gavya/services/product-catalog-service v0.0.0
	github.com/ppusapati/gavya/services/production-service v0.0.0
	github.com/ppusapati/gavya/services/reporting-service v0.0.0
	github.com/ppusapati/gavya/services/settlement-service v0.0.0
	github.com/ppusapati/gavya/services/shadow-settlement-service v0.0.0
	github.com/ppusapati/gavya/services/tenant-service v0.0.0
	p9e.in/samavaya/packages v0.0.0
)

replace p9e.in/samavaya/packages => ../../pkg

replace github.com/ppusapati/gavya/libs/integrity => ../../libs/integrity

replace github.com/ppusapati/gavya/services/audit-service => ../audit-service
replace github.com/ppusapati/gavya/services/balance-service => ../balance-service
replace github.com/ppusapati/gavya/services/billing-service => ../billing-service
replace github.com/ppusapati/gavya/services/breeding-service => ../breeding-service
replace github.com/ppusapati/gavya/services/canonical-service => ../canonical-service
replace github.com/ppusapati/gavya/services/cattle-market-service => ../cattle-market-service
replace github.com/ppusapati/gavya/services/cattle-service => ../cattle-service
replace github.com/ppusapati/gavya/services/farm-service => ../farm-service
replace github.com/ppusapati/gavya/services/feed-service => ../feed-service
replace github.com/ppusapati/gavya/services/file-service => ../file-service
replace github.com/ppusapati/gavya/services/gateway-service => ../gateway-service
replace github.com/ppusapati/gavya/services/health-service => ../health-service
replace github.com/ppusapati/gavya/services/identity-service => ../identity-service
replace github.com/ppusapati/gavya/services/ingestion-service => ../ingestion-service
replace github.com/ppusapati/gavya/services/inventory-service => ../inventory-service
replace github.com/ppusapati/gavya/services/laboratory-service => ../laboratory-service
replace github.com/ppusapati/gavya/services/material-service => ../material-service
replace github.com/ppusapati/gavya/services/milk-service => ../milk-service
replace github.com/ppusapati/gavya/services/notification-service => ../notification-service
replace github.com/ppusapati/gavya/services/observation-service => ../observation-service
replace github.com/ppusapati/gavya/services/order-service => ../order-service
replace github.com/ppusapati/gavya/services/pooling-service => ../pooling-service
replace github.com/ppusapati/gavya/services/procurement-service => ../procurement-service
replace github.com/ppusapati/gavya/services/product-catalog-service => ../product-catalog-service
replace github.com/ppusapati/gavya/services/production-service => ../production-service
replace github.com/ppusapati/gavya/services/reporting-service => ../reporting-service
replace github.com/ppusapati/gavya/services/settlement-service => ../settlement-service
replace github.com/ppusapati/gavya/services/shadow-settlement-service => ../shadow-settlement-service
replace github.com/ppusapati/gavya/services/tenant-service => ../tenant-service
