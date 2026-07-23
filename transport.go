package eurus

import (
	"context"

	"github.com/RobertWHurst/velaros"
)

type Transport interface {
	AnnounceGateway(gatewayDescriptor *GatewayDescriptor) error
	HandleGatewayAnnouncements(ctx context.Context, ready chan<- struct{}, handler func(gatewayDescriptor *GatewayDescriptor)) error

	AnnounceService(serviceDescriptor *ServiceDescriptor) error
	HandleServiceAnnouncements(ctx context.Context, ready chan<- struct{}, handler func(serviceDescriptor *ServiceDescriptor)) error

	MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error
	HandleServiceMessages(ctx context.Context, ready chan<- struct{}, serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) error

	MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error
	HandleGatewayMessages(ctx context.Context, ready chan<- struct{}, gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) error

	ClosedSocket(socketID string, status velaros.Status, reason string) error
	HandleSocketClosures(ctx context.Context, ready chan<- struct{}, handler func(socketID string, status velaros.Status, reason string)) error

	HeartbeatSocket(gatewayID string, serviceID string, socketID string) error
	HandleSocketHeartbeats(ctx context.Context, ready chan<- struct{}, gatewayID string, handler func(serviceID string, socketID string) bool) error
}
