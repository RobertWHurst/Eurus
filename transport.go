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

	MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msgType velaros.MessageType, msgData []byte) error
	BindMessageService(serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msgType velaros.MessageType, msgData []byte)) error
	UnbindMessageService(serviceID string) error

	MessageGateway(gatewayID string, socketID string, msgType velaros.MessageType, msgData []byte) error
	BindMessageGateway(gatewayID string, handler func(socketID string, msgType velaros.MessageType, msgData []byte)) error
	UnbindMessageGateway(gatewayID string) error

	ClosedSocket(socketID string, status velaros.Status, reason string) error
	BindSocketClosed(handler func(socketID string, status velaros.Status, reason string)) error
	UnbindSocketClosed() error
}
