package localtransport

import (
	"sync"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/telemetryos/go-debug/debug"
)

var (
	transportLocalDebug         = debug.Bind("eurus:transport:local")
	transportLocalMessageDebug  = debug.Bind("eurus:transport:local:message")
	transportLocalAnnounceDebug = debug.Bind("eurus:transport:local:announce")
)

type LocalTransport struct {
	mu                      sync.RWMutex
	nextHandlerID           uint64
	gatewayAnnounceHandlers map[uint64]func(gatewayDescriptor *eurus.GatewayDescriptor)
	serviceAnnounceHandlers map[uint64]func(serviceDescriptor *eurus.ServiceDescriptor)
	serviceMessageHandlers  map[string]func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)
	gatewayMessageHandlers  map[string]func(socketID string, msg *velaros.SocketMessage)
	socketClosedHandlers    map[uint64]func(socketID string, status websocket.StatusCode, reason string)
	socketHeartbeatHandlers map[string]func(serviceID string, socketID string) bool
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
		socketHeartbeatHandlers: map[string]func(serviceID string, socketID string) bool{},
	}
}

func (c *LocalTransport) registerHandler() uint64 {
	c.nextHandlerID++
	return c.nextHandlerID
}

func signalReady(ready chan<- struct{}) {
	if ready == nil {
		return
	}
	select {
	case ready <- struct{}{}:
	default:
	}
}
