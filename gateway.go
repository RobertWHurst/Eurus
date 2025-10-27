package eurus

import (
	"fmt"
	"sync"

	"github.com/RobertWHurst/velaros"
	"github.com/telemetrytv/trace"
)

var (
	gatewayDebug        = trace.Bind("eurus:gateway")
	gatewayRouteDebug   = trace.Bind("eurus:gateway:route")
	gatewayIndexerDebug = trace.Bind("eurus:gateway:indexer")
)

type Gateway struct {
	Name      string
	ID        string
	Transport Transport
	gsi       *GatewayServiceIndexer

	socketsMu sync.Mutex
	sockets   map[string]*velaros.Socket

	stopChan chan struct{}
}

var _ velaros.Handler = &Gateway{}

func NewGateway(name string, transport Transport) *Gateway {
	return &Gateway{
		Name:      name,
		ID:        generateID(),
		Transport: transport,
		sockets:   map[string]*velaros.Socket{},
		stopChan:  make(chan struct{}),
	}
}

// Start initializes the gateway and begins listening for service announcements
// and routing messages between clients and services.
func (g *Gateway) Start() error {
	gatewayDebug.Tracef("Starting gateway %s", g.Name)

	if g.gsi != nil {
		gatewayDebug.Trace("Gateway already started")
		return fmt.Errorf("gateway already started")
	}

	gatewayIndexerDebug.Trace("Initializing service indexer")
	g.gsi = NewGatewayServiceIndexer()

	if g.Transport == nil {
		gatewayDebug.Trace("Transport not provided")
		return fmt.Errorf(
			"cannot start local service. The associated gateway already handles " +
				"incoming requests",
		)
	}

	gatewayDebug.Trace("Binding service announcement handler")
	if err := g.Transport.BindServiceAnnounce(func(serviceDescriptor *ServiceDescriptor) {
		gatewayIndexerDebug.Tracef("Received service announcement from %s", serviceDescriptor.Name)

		announcingToThisGateway := len(serviceDescriptor.GatewayNames) == 0
		if !announcingToThisGateway {
			for _, gatewayName := range serviceDescriptor.GatewayNames {
				if gatewayName == g.Name {
					announcingToThisGateway = true
					break
				}
			}
		}
		if !announcingToThisGateway {
			gatewayIndexerDebug.Tracef("Service %s not announcing to this gateway", serviceDescriptor.Name)
			return
		}

		gatewayIndexerDebug.Tracef("Indexing service %s with %d routes",
			serviceDescriptor.Name, len(serviceDescriptor.RouteDescriptors))
		if err := g.gsi.SetServiceDescriptor(serviceDescriptor); err != nil {
			gatewayIndexerDebug.Tracef("Failed to index service %s: %v", serviceDescriptor.Name, err)
			panic(err)
		}
	}); err != nil {
		gatewayDebug.Tracef("Failed to bind service announcement handler: %v", err)
		return err
	}

	if err := g.Transport.BindMessageGateway(g.ID, func(socketID string, msgType velaros.MessageType, msgData []byte) {
		gatewayRouteDebug.Tracef("Received message for socket %s", socketID)

		g.socketsMu.Lock()
		socket, ok := g.sockets[socketID]
		g.socketsMu.Unlock()
		if !ok {
			gatewayRouteDebug.Tracef("No socket found with ID %s, dropping message", socketID)
			return
		}

		gatewayRouteDebug.Tracef("Routing message to socket %s", socketID)
		if err := socket.Send(msgType, msgData); err != nil {
			gatewayRouteDebug.Tracef("Failed to route message to socket %s: %v", socketID, err)
			gatewayRouteDebug.Tracef("Closing socket %s due to send failure", socketID)
			socket.Close(velaros.StatusInternalError, "Failed to send message", velaros.ServerCloseSource)

			g.socketsMu.Lock()
			delete(g.sockets, socketID)
			g.socketsMu.Unlock()
			g.gsi.UnmapSocket(socketID)

			if err := g.Transport.ClosedSocket(socketID, velaros.StatusInternalError, "Failed to send message"); err != nil {
				gatewayRouteDebug.Tracef("Error notifying closed socket %s: %v", socketID, err)
			}
		}

	}); err != nil {
		gatewayDebug.Tracef("Failed to bind message to gateway handler: %v", err)
		return err
	}

	if err := g.Transport.BindSocketClosed(func(socketID string, status velaros.Status, reason string) {
		g.socketsMu.Lock()
		socket, ok := g.sockets[socketID]
		g.socketsMu.Unlock()
		if !ok {
			return
		}

		socket.Close(status, reason, velaros.ServerCloseSource)
		delete(g.sockets, socketID)

		if err := g.Transport.ClosedSocket(socketID, status, reason); err != nil {
			gatewayDebug.Tracef("Error notifying closed socket %s: %v", socketID, err)
		}
		g.gsi.UnmapSocket(socketID)
	}); err != nil {
		gatewayDebug.Tracef("Failed to bind closed socket handler: %v", err)
		return err
	}

	gatewayDebug.Tracef("Announcing gateway %s", g.Name)

	if err := g.Transport.AnnounceGateway(&GatewayDescriptor{
		Name:               g.Name,
		ServiceDescriptors: g.gsi.descriptors,
	}); err != nil {
		return err
	}

	return nil
}

func (g *Gateway) Stop() {
	gatewayDebug.Tracef("Stopping gateway %s", g.Name)

	if g.gsi == nil {
		gatewayDebug.Trace("Gateway already stopped")
		return
	}

	close(g.stopChan)

	gatewayDebug.Trace("Clearing service indexer")
	g.gsi = nil

	gatewayDebug.Trace("Unbinding service announce handler")
	if err := g.Transport.UnbindServiceAnnounce(); err != nil {
		gatewayDebug.Tracef("Failed to unbind service announce: %v", err)
		panic(err)
	}

	gatewayDebug.Trace("Gateway stopped successfully")
}

func (g *Gateway) CanServePath(path string) bool {
	gatewayRouteDebug.Tracef("Checking if gateway can serve path %s", path)

	if g.gsi == nil {
		gatewayRouteDebug.Trace("Gateway not started, cannot serve request")
		return false
	}

	_, ok := g.gsi.ResolveService(path)
	if ok {
		gatewayRouteDebug.Tracef("Can serve %s", path)
	} else {
		gatewayRouteDebug.Tracef("Cannot serve %s, no matching service", path)
	}
	return ok
}

func (g *Gateway) CanHandle(ctx *velaros.Context) bool {
	path := ctx.Path()
	gatewayRouteDebug.Tracef("Checking if gateway can handle Velaros message %s", path)

	if g.gsi == nil {
		gatewayRouteDebug.Trace("Gateway not started, cannot handle message")
		return false
	}

	_, ok := g.gsi.ResolveService(path)
	if ok {
		gatewayRouteDebug.Tracef("Can handle %s", path)
	} else {
		gatewayRouteDebug.Tracef("Cannot handle %s, no matching service", path)
	}
	return ok
}

func (g *Gateway) Handle(ctx *velaros.Context) {
	path := ctx.Path()
	gatewayRouteDebug.Tracef("Received Velaros message %s", path)

	if g.gsi == nil {
		gatewayRouteDebug.Trace("Gateway not started, skipping to next handler")
		ctx.Next()
		return
	}

	gatewayRouteDebug.Tracef("Resolving service for path %s", path)
	serviceName, ok := g.gsi.ResolveService(path)
	if !ok {
		gatewayRouteDebug.Tracef("No service found for %s, skipping to next handler", path)
		ctx.Next()
		return
	}
	gatewayRouteDebug.Tracef("Resolved %s to service %s", path, serviceName)

	socket := velaros.CtxSocket(ctx)
	g.socketsMu.Lock()
	g.sockets[socket.ID()] = socket
	g.socketsMu.Unlock()

	for {
		gatewayRouteDebug.Tracef("Mapping socket for service %s", serviceName)
		serviceID, isNewConnection, err := g.gsi.MapSocket(serviceName, socket.ID())
		if err != nil {
			panic(err)
		}
		if serviceID == "" {
			g.closeSocket(socket.ID())
		}

		err = g.Transport.MessageService(serviceID, g.ID, socket.ID(), ctx.Headers(), ctx.MessageType(), ctx.Data())
		if err == nil {
			break
		}

		if err := g.gsi.UnsetService(serviceID); err != nil {
			panic(err)
		}

		if !isNewConnection {
			g.closeSocket(socket.ID())
			break
		}
	}
}

func (g *Gateway) HandleClose(ctx *velaros.Context) {
	gatewayRouteDebug.Tracef("Handling close for socket %s", ctx.SocketID())
	g.closeSocket(ctx.SocketID())
}

func (g *Gateway) closeSocket(socketID string) {
	g.socketsMu.Lock()
	delete(g.sockets, socketID)
	g.socketsMu.Unlock()

	if err := g.Transport.ClosedSocket(socketID, velaros.StatusGoingAway, "Socket closed by client"); err != nil {
		gatewayDebug.Tracef("Error notifying closed socket %s: %v", socketID, err)
	}
	g.gsi.UnmapSocket(socketID)
}
