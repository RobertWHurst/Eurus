package eurus

type Transport interface {
	AnnounceGateway(gatewayDescriptor *GatewayDescriptor) error
	BindGatewayAnnounce(handler func(gatewayDescriptor *GatewayDescriptor)) error
	UnbindGatewayAnnounce() error

	AnnounceService(serviceDescriptor *ServiceDescriptor) error
	BindServiceAnnounce(handler func(serviceDescriptor *ServiceDescriptor)) error
	UnbindServiceAnnounce() error

	InboundSocketMessage(serviceName string, socketID string, messageData []byte) error
	BindInboundSocketMessage(serviceName string, handler func(socketID string, messageData []byte) error) error
	UnbindInboundSocketMessage(serviceName string) error

	OutboundSocketMessage(socketID string, messageData []byte) error
	BindOutboundSocketMessage(socketID string, handler func(messageData []byte) error) error
	UnbindOutboundSocketMessage(socketID string) error

	SocketClose(socketID string, reason int) error
	BindSocketClose(socketID string, handler func(reason int) error) error
	UnbindSocketClose(socketID string) error
}
