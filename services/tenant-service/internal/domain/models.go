package domain

import "time"

type Tenant struct {
	ID, Name, Slug, Plan, Status         string
	ContactEmail, ContactPhone           string
	Address, Country, Timezone, Currency string
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
