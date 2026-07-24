package localtransport

import (
	"context"
	"sync"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/telemetryos/go-debug/debug"
)

var (
	transportLocalDebug         = debug.Bind("eurus:transport:local")
	transportLocalMessageDebug  = debug.Bind("eurus:transport:local:message")
	transportLocalAnnounceDebug = debug.Bind("eurus:transport:local:announce")
)

// LocalTransport is an in-process transport for development and testing.
// Handlers run synchronously on the caller's goroutine, so unlike the NATS
// transport it provides no cross-socket dispatch concurrency.
type LocalTransport struct {
	mu                      sync.RWMutex
	nextHandlerID           uint64
	gatewayAnnounceHandlers map[uint64]func(gatewayDescriptor *eurus.GatewayDescriptor)
	serviceAnnounceHandlers map[uint64]func(serviceDescriptor *eurus.ServiceDescriptor)
	serviceMessageHandlers  map[string]func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)
	gatewayMessageHandlers  map[string]func(socketID string, msg *velaros.SocketMessage)
	socketClosedHandlers    map[uint64]func(socketID string, status websocket.StatusCode, reason string)
	socketHeartbeatHandlers map[string]func(serviceID string, socketIDs []string) []string
}

var _ eurus.Transport = &LocalTransport{}

func New() *LocalTransport {
	transportLocalDebug.Trace("Creating new local transport")
	return &LocalTransport{
		gatewayAnnounceHandlers: map[uint64]func(gatewayDescriptor *eurus.GatewayDescriptor){},
		serviceAnnounceHandlers: map[uint64]func(serviceDescriptor *eurus.ServiceDescriptor){},
		serviceMessageHandlers:  map[string]func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage){},
		gatewayMessageHandlers:  map[string]func(socketID string, msg *velaros.SocketMessage){},
		socketClosedHandlers:    map[uint64]func(socketID string, status websocket.StatusCode, reason string){},
		socketHeartbeatHandlers: map[string]func(serviceID string, socketIDs []string) []string{},
	}
}

// registerHandlerID must be called with t.mu held.
func (t *LocalTransport) registerHandlerID() uint64 {
	t.nextHandlerID++
	return t.nextHandlerID
}

// untilDone returns a Subscription that blocks until the serve context is
// canceled, then runs unregister.
func untilDone(unregister func()) eurus.Subscription {
	return eurus.SubscriptionFunc(func(ctx context.Context) error {
		<-ctx.Done()
		unregister()
		return nil
	})
}
