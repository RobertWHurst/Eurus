package eurus

import (
	"github.com/RobertWHurst/velaros"
	"github.com/vmihailenco/msgpack/v5"
)

// RouteDescriptor defines a route this service can handle. A route is a
// HTTP method, and a path matching pattern. It is used by the eurus gateway
// to determine which service to dispatch a request to.
type RouteDescriptor struct {
	Pattern *velaros.Pattern

	// Metadata carries arbitrary route metadata declared at the bind site
	// (for example with velaros.WithMetadata). After a msgpack round trip it
	// holds a msgpack.RawMessage; use UnmarshalMetadata to decode it into a
	// typed value regardless of how the descriptor was obtained.
	Metadata any
}

// UnmarshalMetadata decodes the route's metadata into the given value. It
// works both for descriptors received over a transport (where the metadata
// is a raw msgpack value) and for locally constructed descriptors (where it
// is the original value), so consumers behave identically across transports.
// Returns ErrNoRouteMetadata when the route has no metadata.
func (r *RouteDescriptor) UnmarshalMetadata(into any) error {
	if r.Metadata == nil {
		return ErrNoRouteMetadata
	}
	raw, ok := r.Metadata.(msgpack.RawMessage)
	if !ok {
		encoded, err := msgpack.Marshal(r.Metadata)
		if err != nil {
			return err
		}
		raw = encoded
	}
	return msgpack.Unmarshal(raw, into)
}

// MarshalMsgpack returns the msgpack representation of the route descriptor.
func (r *RouteDescriptor) MarshalMsgpack() ([]byte, error) {
	return msgpack.Marshal(struct {
		Pattern  string
		Metadata any `msgpack:",omitempty"`
	}{
		Pattern:  r.Pattern.String(),
		Metadata: r.Metadata,
	})
}

// UnmarshalMsgpack parses the msgpack representation of the route descriptor.
// Metadata is preserved as a raw msgpack value so consumers can decode it
// into their own types with UnmarshalMetadata.
func (r *RouteDescriptor) UnmarshalMsgpack(data []byte) error {
	fromMsgpackStruct := struct {
		Pattern  string
		Metadata msgpack.RawMessage `msgpack:",omitempty"`
	}{}
	if err := msgpack.Unmarshal(data, &fromMsgpackStruct); err != nil {
		return err
	}

	pattern, err := velaros.NewPattern(fromMsgpackStruct.Pattern)
	if err != nil {
		return err
	}

	r.Pattern = pattern
	if len(fromMsgpackStruct.Metadata) > 0 {
		r.Metadata = fromMsgpackStruct.Metadata
	} else {
		r.Metadata = nil
	}

	return nil
}

// NewRouteDescriptor creates a new RouteDescriptor from a path pattern.
// The pattern determines which URL path this route will match.
func NewRouteDescriptor(patternStr string) (*RouteDescriptor, error) {
	pattern, err := velaros.NewPattern(patternStr)
	if err != nil {
		return nil, err
	}
	return &RouteDescriptor{Pattern: pattern}, nil
}
