// Package origin models where an authoritative record came from.
//
// The platform runs in shadow mode: it imports records produced by an incumbent
// system alongside records it captures itself, and must never confuse the two.
// Every persisted row carries an Origin so a divergence can be attributed to
// the right side.
package origin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Kind distinguishes records the platform captured from records it ingested.
type Kind string

const (
	// Native records were captured by this platform's own devices and operators.
	Native Kind = "NATIVE"
	// Imported records were produced by an external system of record.
	Imported Kind = "IMPORTED"
	// Derived records were computed by this platform from other records.
	Derived Kind = "DERIVED"
)

var ErrInvalidKind = errors.New("origin: invalid kind")

func ParseKind(s string) (Kind, error) {
	switch k := Kind(strings.ToUpper(strings.TrimSpace(s))); k {
	case Native, Imported, Derived:
		return k, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidKind, s)
	}
}

// Origin is the provenance stamp carried by every authoritative record.
//
// For Native records only Kind is set. For Imported records the source system,
// import batch, source record identifier and payload hash are all required:
// without the hash a re-import cannot be told from an amendment.
type Origin struct {
	Kind              Kind   `json:"kind"`
	SourceSystemID    string `json:"source_system_id,omitempty"`
	ImportBatchID     string `json:"import_batch_id,omitempty"`
	SourceRecordID    string `json:"source_record_id,omitempty"`
	SourcePayloadHash string `json:"source_payload_hash,omitempty"`
	DerivationID      string `json:"derivation_id,omitempty"`
}

func NewNative() Origin { return Origin{Kind: Native} }

func NewImported(sourceSystemID, importBatchID, sourceRecordID, payloadHash string) (Origin, error) {
	o := Origin{
		Kind:              Imported,
		SourceSystemID:    strings.TrimSpace(sourceSystemID),
		ImportBatchID:     strings.TrimSpace(importBatchID),
		SourceRecordID:    strings.TrimSpace(sourceRecordID),
		SourcePayloadHash: strings.TrimSpace(payloadHash),
	}
	return o, o.Validate()
}

func NewDerived(derivationID string) (Origin, error) {
	o := Origin{Kind: Derived, DerivationID: strings.TrimSpace(derivationID)}
	return o, o.Validate()
}

func (o Origin) Validate() error {
	switch o.Kind {
	case Native:
		return nil
	case Imported:
		missing := make([]string, 0, 4)
		if o.SourceSystemID == "" {
			missing = append(missing, "source_system_id")
		}
		if o.ImportBatchID == "" {
			missing = append(missing, "import_batch_id")
		}
		if o.SourceRecordID == "" {
			missing = append(missing, "source_record_id")
		}
		if o.SourcePayloadHash == "" {
			missing = append(missing, "source_payload_hash")
		}
		if len(missing) > 0 {
			return fmt.Errorf("origin: imported record missing %s", strings.Join(missing, ", "))
		}
		return nil
	case Derived:
		if o.DerivationID == "" {
			return fmt.Errorf("origin: derived record missing derivation_id")
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidKind, o.Kind)
	}
}

func (o Origin) IsImported() bool { return o.Kind == Imported }
func (o Origin) IsNative() bool   { return o.Kind == Native }
func (o Origin) IsDerived() bool  { return o.Kind == Derived }

// HashPayload produces the canonical content hash stored as SourcePayloadHash.
// The same source record re-delivered byte-for-byte must hash identically, so
// the caller passes the raw payload exactly as received.
func HashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashFields produces a payload hash from a field map for sources that deliver
// structured rows rather than opaque documents. Keys are sorted so the hash
// does not depend on map iteration order.
func HashFields(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0x1f})
		h.Write([]byte(fields[k]))
		h.Write([]byte{0x1e})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
