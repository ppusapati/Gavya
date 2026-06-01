package encoding

import (
	"errors"
	"strings"
	"sync"
)

// Codec defines the interface Transport uses to encode and decode messages.  Note
// that implementations of this interface must be thread safe; a Codec's
// methods can be called from concurrent goroutines.
type Codec interface {
	// Marshal returns the wire format of v.
	Marshal(v interface{}) ([]byte, error)
	// Unmarshal parses the wire format into v.
	Unmarshal(data []byte, v interface{}) error
	// Name returns the name of the Codec implementation. The returned string
	// will be used as part of content type in transmission.  The result must be
	// static; the result cannot change between calls.
	Name() string
}

var (
	registeredCodecs = make(map[string]Codec)
	codecMu          sync.RWMutex

	// ErrNilCodec is returned when attempting to register a nil codec.
	ErrNilCodec = errors.New("cannot register a nil Codec")
	// ErrEmptyCodecName is returned when a codec has an empty name.
	ErrEmptyCodecName = errors.New("cannot register Codec with empty string result for Name()")
)

// RegisterCodec registers the provided Codec for use with all Transport clients and
// servers. Returns an error if the codec is nil or has an empty name.
func RegisterCodec(codec Codec) error {
	if codec == nil {
		return ErrNilCodec
	}
	if codec.Name() == "" {
		return ErrEmptyCodecName
	}
	contentSubtype := strings.ToLower(codec.Name())

	codecMu.Lock()
	registeredCodecs[contentSubtype] = codec
	codecMu.Unlock()

	return nil
}

// MustRegisterCodec registers the provided Codec and panics on error.
// This is provided for use in init() functions where error handling is impractical.
func MustRegisterCodec(codec Codec) {
	if err := RegisterCodec(codec); err != nil {
		panic(err)
	}
}

// GetCodec gets a registered Codec by content-subtype, or nil if no Codec is
// registered for the content-subtype.
//
// The content-subtype is expected to be lowercase.
func GetCodec(contentSubtype string) Codec {
	codecMu.RLock()
	defer codecMu.RUnlock()
	return registeredCodecs[contentSubtype]
}
