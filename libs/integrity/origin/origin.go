// Package origin models where an authoritative record came from.
//
// The platform runs in shadow mode: it imports records produced by an incumbent
// system alongside records it captures itself, and must never confuse the two.
// Every persisted row carries an Origin so a divergence can be attributed to
// the right side.
package origin

import (
	"crypto/sha256"
	"encoding/binary"
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
// structured rows rather than opaque documents. Keys are sorted so the hash does
// not depend on map iteration order.
//
// Each key and value is length-prefixed rather than delimited, and that is not
// fastidiousness. The first version separated them with 0x1f and 0x1e, which
// collides the moment a field contains one of those bytes:
//
//	{"producer\x1fP-001": "x"}  and  {"producer": "P-001\x1fx"}
//	{"k": "v\x1ek2\x1fv2"}      and  {"k": "v", "k2": "v2"}
//
// Both pairs hashed identically. Two structurally different source records with
// one payload hash is exactly the thing this hash exists to make impossible: a
// re-import of one becomes indistinguishable from an amendment of the other,
// and the idempotency the whole import path rests on stops holding.
//
// A length prefix cannot be forged by content, because the length is written
// outside the bytes it describes.
func HashFields(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	var n [8]byte
	write := func(s string) {
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	// The field count leads, so the encoding says how many pairs follow rather
	// than being read to the end to find out. Length-prefixing alone already
	// makes a smaller map hash differently from a larger one — a shorter byte
	// string is a different byte string — so this is not what stops that; it is
	// here so the encoding is self-describing if anything ever has to parse it
	// back rather than only hash it.
	binary.BigEndian.PutUint64(n[:], uint64(len(keys)))
	h.Write(n[:])
	for _, k := range keys {
		write(k)
		write(fields[k])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
