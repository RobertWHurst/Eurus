package eurus

import (
	"context"

	"github.com/RobertWHurst/velaros"
)

// Subscription is a live transport stream created by one of the Subscribe
// methods on Transport. By the time a Subscription is returned the stream is
// registered with the transport backend — messages published from this moment
// on will be observed.
//
// Serve pumps the stream, invoking the handler passed to Subscribe. It must be
// called exactly once, blocks until the given context is canceled (returning
// nil) or the stream fails (returning the error), and releases the stream's
// resources before returning.
type Subscription interface {
	Serve(ctx context.Context) error
}

// SubscriptionFunc adapts a function to the Subscription interface.
type SubscriptionFunc func(ctx context.Context) error

func (f SubscriptionFunc) Serve(ctx context.Context) error { return f(ctx) }

// Transport moves announcements, socket messages, closures, and heartbeats
// between gateways and services.
//
// Handler concurrency contract: announcement, closure, and heartbeat handlers
// may be invoked concurrently. Message handlers may be invoked concurrently
// for different sockets, but calls for the same socket are serialized in
// arrival order.
type Transport interface {
	// AnnounceGateway broadcasts a gateway descriptor to all services.
	AnnounceGateway(gatewayDescriptor *GatewayDescriptor) error
	// SubscribeGatewayAnnouncements subscribes to gateway announcements.
	SubscribeGatewayAnnouncements(ctx context.Context, handler func(gatewayDescriptor *GatewayDescriptor)) (Subscription, error)

	// AnnounceService broadcasts a service descriptor to all gateways.
	AnnounceService(serviceDescriptor *ServiceDescriptor) error
	// SubscribeServiceAnnouncements subscribes to service announcements.
	SubscribeServiceAnnouncements(ctx context.Context, handler func(serviceDescriptor *ServiceDescriptor)) (Subscription, error)

	// MessageService delivers a client message to a service instance. An error
	// indicates the message was not delivered and the caller may re-route.
	MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error
	// SubscribeServiceMessages subscribes to messages addressed to a service
	// instance.
	SubscribeServiceMessages(ctx context.Context, serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) (Subscription, error)

	// MessageGateway delivers a service message to the gateway that owns a
	// socket. An error indicates the message was not delivered.
	MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error
	// SubscribeGatewayMessages subscribes to messages addressed to a gateway.
	SubscribeGatewayMessages(ctx context.Context, gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) (Subscription, error)

	// ClosedSocket broadcasts that a socket has been closed.
	ClosedSocket(socketID string, status velaros.Status, reason string) error
	// SubscribeSocketClosures subscribes to socket closure broadcasts.
	SubscribeSocketClosures(ctx context.Context, handler func(socketID string, status velaros.Status, reason string)) (Subscription, error)

	// HeartbeatSockets asks a gateway which of the given sockets are no longer
	// alive, and returns their IDs. An error indicates the gateway could not
	// be reached at all.
	HeartbeatSockets(gatewayID string, serviceID string, socketIDs []string) (dead []string, err error)
	// SubscribeSocketHeartbeats subscribes a gateway to heartbeat requests.
	// The handler returns the subset of socketIDs that are no longer alive.
	SubscribeSocketHeartbeats(ctx context.Context, gatewayID string, handler func(serviceID string, socketIDs []string) (dead []string)) (Subscription, error)
}
