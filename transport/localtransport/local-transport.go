package localtransport

import (
	"sync"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/telemetrytv/trace"
)

var (
	transportLocalDebug         = trace.Bind("eurus:transport:local")
	transportLocalMessageDebug  = trace.Bind("eurus:transport:local:message")
	transportLocalAnnounceDebug = trace.Bind("eurus:transport:local:announce")
)

type LocalTransport struct {
	mu                      sync.RWMutex
	gatewayAnnounceHandlers []func(gatewayDescriptor *eurus.GatewayDescriptor)
	serviceAnnounceHandlers []func(serviceDescriptor *eurus.ServiceDescriptor)
	serviceMessageHandlers  map[string]func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msgType websocket.MessageType, msgData []byte)
	gatewayMessageHandlers  map[string]func(socketID string, msgType websocket.MessageType, msgData []byte)
	socketClosedHandlers    []func(socketID string, status websocket.StatusCode, reason string)
}

var _ eurus.Transport = &LocalTransport{}

func New() *LocalTransport {
	transportLocalDebug.Trace("Creating new local transport")
	return &LocalTransport{
		serviceMessageHandlers: map[string]func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msgType websocket.MessageType, msgData []byte){},
		gatewayMessageHandlers: map[string]func(socketID string, msgType websocket.MessageType, msgData []byte){},
	}
}
