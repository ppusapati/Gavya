package domain

import "time"

type Tenant struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Plan         string `json:"plan"`
	Status       string `json:"status"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	Address      string `json:"address"`
	Country      string `json:"country"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	// CurrencyScale is how many digits after the point this tenant's currency
	// has: 2 for a rupee, 0 for a yen, 3 for a dinar. It is stored beside the
	// code rather than derived on read, so an amount already recorded cannot
	// change meaning if the currency table is ever corrected.
	CurrencyScale int32      `json:"currency_scale"`
	MaxUsers      int        `json:"max_users"`
	MaxCattle     int        `json:"max_cattle"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
	DeletedAt     *time.Time `json:"deleted_at"`
}

type TenantSetting struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	DataType  string    `json:"data_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}
