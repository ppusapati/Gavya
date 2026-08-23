package domain

import "time"

type Tenant struct {
	ID, Name, Slug, Plan, Status         string
	ContactEmail, ContactPhone           string
	Address, Country, Timezone, Currency string
	// CurrencyScale is how many digits after the point this tenant's currency
	// has: 2 for a rupee, 0 for a yen, 3 for a dinar. It is stored beside the
	// code rather than derived on read, so an amount already recorded cannot
	// change meaning if the currency table is ever corrected.
	CurrencyScale int32
	MaxUsers, MaxCattle                  int
	CreatedAt, UpdatedAt                 time.Time
	CreatedBy, UpdatedBy                 string
	DeletedAt                            *time.Time
}

type TenantSetting struct {
	ID, TenantID, Key, Value, DataType string
	CreatedAt, UpdatedAt               time.Time
	CreatedBy, UpdatedBy               string
}
