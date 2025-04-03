package eurus

type GatewayDescriptor struct {
	Name               string               `msgpack:"name"`
	ServiceDescriptors []*ServiceDescriptor `msgpack:"serviceDescriptors"`
}
