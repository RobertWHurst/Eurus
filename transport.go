package eurus

import (
	"github.com/RobertWHurst/velaros"
)

type Transport interface {
	AnnounceGateway(gatewayDescriptor *GatewayDescriptor) error
	BindGatewayAnnounce(handler func(gatewayDescriptor *GatewayDescriptor)) error
	UnbindGatewayAnnounce() error

	AnnounceService(serviceDescriptor *ServiceDescriptor) error
	BindServiceAnnounce(handler func(serviceDescriptor *ServiceDescriptor)) error
	UnbindServiceAnnounce() error

	MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error
	BindMessageService(serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) error
	UnbindMessageService(serviceID string) error

	MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error
	BindMessageGateway(gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) error
	UnbindMessageGateway(gatewayID string) error

	ClosedSocket(socketID string, status velaros.Status, reason string) error
	BindSocketClosed(handler func(socketID string, status velaros.Status, reason string)) error
	UnbindSocketClosed() error
}
