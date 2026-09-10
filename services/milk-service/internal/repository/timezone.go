package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrTimezoneMismatch is milk recorded against a tenant under a timezone that is
// not the one that tenant reckons its days by.
var ErrTimezoneMismatch = errors.New("this tenant does not reckon its days in that timezone")

// ErrTimezoneUnset is a tenant that has not recorded any milk here, so nothing
// has fixed which day its readings fall on.
var ErrTimezoneUnset = errors.New("this tenant has no timezone recorded yet")

// ErrUnknownTimezone is a name the service's own tzdata cannot load. Refused
// rather than stored: a zone the database accepts and the code cannot read is a
// row that makes every later query fail somewhere further away.
var ErrUnknownTimezone = errors.New("that is not a timezone this service knows")

// NormaliseTimezone checks a zone name against the tzdata this binary carries.
//
// UTC and an empty string are not interchangeable and neither is accepted as a
// stand-in for the other. A tenant that has not said where it is has not said
// where it is, and defaulting that to UTC is how a society in Andhra Pradesh
// ends up with its evening collection filed under the previous day.
func NormaliseTimezone(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: a tenant must say which timezone its days are "+
			"reckoned in, and there is no default", ErrUnknownTimezone)
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnknownTimezone, name)
	}
	return name, nil
}

// PinTenantTimezone fixes a tenant's timezone on first use and refuses any other
// afterwards.
//
// The same shape as PinTenantMoney in the services that hold money, and for the
// same reason. A tenant's day begins once. Without a pin the invariant lives
// only in whatever the caller happened to send, and one mistyped request leaves
// a tenant with readings filed under two different reckonings of the same
// evening — indistinguishable afterwards, because what is stored is an instant
// and the disagreement is about how to read it.
//
// The upsert takes the row lock even when it changes nothing, so two first
// writes racing each other cannot both decide they are the first.
func (r *repo) PinTenantTimezone(ctx context.Context, tenantID, zone string) error {
	zone, err := NormaliseTimezone(zone)
	if err != nil {
		return err
	}
	var have string
	err = r.db.QueryRow(ctx,
		`INSERT INTO tenant_timezone (tenant_id, timezone)
		 VALUES ($1, $2)
		 ON CONFLICT (tenant_id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id
		 RETURNING timezone`,
		tenantID, zone).Scan(&have)
	if err != nil {
		return fmt.Errorf("pin timezone: %w", err)
	}
	if have != zone {
		return fmt.Errorf("%w: it reckons in %s, not %s", ErrTimezoneMismatch, have, zone)
	}
	return nil
}

// TenantTimezone reports the timezone a tenant reckons its days in.
//
// Read from the pin rather than from tenant-service, so recording a reading does
// not depend on another service being reachable — the argument the currency pin
// already makes, and the reason tenant-service's own Timezone column is not
// consulted here. The two are meant to agree; if they ever do not, this one is
// what the readings were actually filed under.
func (r *repo) TenantTimezone(ctx context.Context, tenantID string) (string, error) {
	var zone string
	err := r.db.QueryRow(ctx,
		`SELECT timezone FROM tenant_timezone WHERE tenant_id=$1`, tenantID).Scan(&zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTimezoneUnset
	}
	if err != nil {
		return "", fmt.Errorf("read tenant timezone: %w", err)
	}
	return zone, nil
}
