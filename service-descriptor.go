package eurus

import "time"

type ServiceDescriptor struct {
	Name             string             `msgpack:"name"`
	ID               string             `msgpack:"id"`
	GatewayNames     []string           `msgpack:"gatewayNames"`
	RouteDescriptors []*RouteDescriptor `msgpack:"httpRouteDescriptors"`

	LastSeenAt *time.Time `msgpack:"-"`
	socketIDs  []string   `msgpack:"-"`
}
