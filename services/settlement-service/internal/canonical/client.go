// Package canonical reads what an external identifier has meant, from the
// service that keeps that history.
//
// Settlement pays a producer_ref. For milk a society recorded itself that is
// the society's own code and there is nothing to look up. For milk that arrived
// through an import it is a member number from another system, and what that
// number meant — which producer, over which dates, and whether somebody has
// since decided it was wrong — is canonical-service's to say.
//
// The history rather than the resolution, deliberately. Resolving asks what an
// identifier means now and must never answer with a retired mapping; explaining
// a payment asks what it meant when the milk was gathered, and the mapping that
// answers that is very often the one that has since been retired — because
// somebody noticing it was wrong is the usual reason anybody is asking.
package canonical

import (
	"context"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	serviceName = "canonical.v1.CanonicalService"
	history     = "/" + serviceName + "/GetIdentityHistory"
)

// Mapping is one assertion that an external identifier meant a producer.
type Mapping struct {
	ID         string
	ExternalID string
	EntityID   string
	Method     string
	Note       string

	ValidFrom  time.Time
	ValidTo    time.Time
	RecordedAt time.Time

	SupersededAt *time.Time
	SupersededBy string
}

// Retired reports whether this mapping has been withdrawn.
func (m Mapping) Retired() bool { return m.SupersededAt != nil }

type Client struct {
	svc  *svcclient.Client
	opts func() svcclient.CallOptions
}

// New builds a reader over an already-configured client.
func New(c *svcclient.Client, opts func() svcclient.CallOptions) *Client {
	return &Client{svc: c, opts: opts}
}

type historyRequest struct {
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	EntityKind     string `json:"entity_kind"`
	ExternalID     string `json:"external_id"`
}

type identityProto struct {
	ID           string `json:"id"`
	ExternalID   string `json:"external_id"`
	EntityID     string `json:"entity_id"`
	Method       string `json:"method"`
	Note         string `json:"note"`
	ValidFrom    string `json:"valid_from"`
	ValidTo      string `json:"valid_to"`
	RecordedAt   string `json:"recorded_at"`
	SupersededAt string `json:"superseded_at"`
	SupersededBy string `json:"superseded_by"`
}

type historyResponse struct {
	Identities []identityProto `json:"identities"`
}

// History returns every mapping ever recorded for one producer identifier in
// one source system, retired ones included, oldest first.
func (c *Client) History(ctx context.Context, tenantID, sourceSystemID, externalID string) ([]Mapping, error) {
	resp, err := svcclient.Call[historyRequest, historyResponse](ctx, c.svc, history,
		historyRequest{
			TenantID: tenantID, SourceSystemID: sourceSystemID,
			EntityKind: "PRODUCER", ExternalID: externalID,
		}, c.opts())
	if err != nil {
		return nil, fmt.Errorf("read the identity history of %s in %s from canonical: %w",
			externalID, sourceSystemID, err)
	}
	out := make([]Mapping, 0, len(resp.Identities))
	for _, p := range resp.Identities {
		m := Mapping{
			ID: p.ID, ExternalID: p.ExternalID, EntityID: p.EntityID,
			Method: p.Method, Note: p.Note, SupersededBy: p.SupersededBy,
		}
		var err error
		if m.ValidFrom, err = stamp(p.ValidFrom, p.ID, "valid_from"); err != nil {
			return nil, err
		}
		if m.ValidTo, err = stamp(p.ValidTo, p.ID, "valid_to"); err != nil {
			return nil, err
		}
		if m.RecordedAt, err = stamp(p.RecordedAt, p.ID, "recorded_at"); err != nil {
			return nil, err
		}
		if p.SupersededAt != "" {
			t, err := stamp(p.SupersededAt, p.ID, "superseded_at")
			if err != nil {
				return nil, err
			}
			m.SupersededAt = &t
		}
		out = append(out, m)
	}
	return out, nil
}

func stamp(v, id, field string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("identity %s: %s %q is not a time", id, field, v)
	}
	return t.UTC(), nil
}
