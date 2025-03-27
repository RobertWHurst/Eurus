package eurus

import (
	"time"
)

type ServiceDescriptor struct {
	Name             string             `msgpack:"name"`
	GatewayNames     []string           `msgpack:"gatewayNames"`
	RouteDescriptors []*RouteDescriptor `msgpack:"httpRouteDescriptors"`
	LastSeenAt       *time.Time         `msgpack:"-"`
	UnreachableAt    *time.Time         `msgpack:"-"`
	UnreachableCount int                `msgpack:"-"`
}
