package eurus

type ServiceDescriptor struct {
	Name             string             `msgpack:"name"`
	ID               string             `msgpack:"id"`
	GatewayNames     []string           `msgpack:"gatewayNames"`
	RouteDescriptors []*RouteDescriptor `msgpack:"httpRouteDescriptors"`

	socketIDs []string `msgpack:"-"`
}
