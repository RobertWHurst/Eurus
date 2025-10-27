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
}

// MarshalMsgpack returns the msgpack representation of the route descriptor.
func (r *RouteDescriptor) MarshalMsgpack() ([]byte, error) {
	return msgpack.Marshal(struct{ Pattern string }{Pattern: r.Pattern.String()})
}

// UnmarshalMsgpack parses the msgpack representation of the route descriptor.
func (r *RouteDescriptor) UnmarshalMsgpack(data []byte) error {
	fromMsgpackStruct := struct{ Pattern string }{}
	if err := msgpack.Unmarshal(data, &fromMsgpackStruct); err != nil {
		return err
	}

	pattern, err := velaros.NewPattern(fromMsgpackStruct.Pattern)
	if err != nil {
		return err
	}

	r.Pattern = pattern

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
