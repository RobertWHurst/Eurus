package eurus

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/RobertWHurst/velaros"
	"github.com/telemetryos/go-debug/debug"
)

var (
	gatewayDebug         = debug.Bind("eurus:gateway")
	gatewayRouteDebug    = debug.Bind("eurus:gateway:route")
	gatewayIndexerDebug  = debug.Bind("eurus:gateway:indexer")
	gatewayAnnounceDebug = debug.Bind("eurus:gateway:announce")
)

var GatewayAnnounceInterval = time.Duration(8+rand.IntN(2)) * time.Second

type Gateway struct {
	Name      string
	ID        string
	Transport Transport
	gsi       *GatewayServiceIndexer

	AnnounceInterval time.Duration

	socketsMu sync.Mutex
	sockets   map[string]*velaros.Socket

	lifecycleMu   sync.Mutex
	closeCancel   context.CancelFunc
	lifecycleDone chan struct{}
}

var _ velaros.Handler = &Gateway{}

func NewGateway(name string, transport Transport) *Gateway {
	return &Gateway{
		Name:             name,
		ID:               generateID(),
		Transport:        transport,
		AnnounceInterval: GatewayAnnounceInterval,
		sockets:          map[string]*velaros.Socket{},
	}
}

// Connect joins the gateway to the transport and blocks until the context is
// canceled, Close is called, or a required transport stream fails.
func (g *Gateway) Connect(ctx context.Context) error {
	gatewayDebug.Tracef("Connecting gateway %s", g.Name)
	if g.gsi != nil {
		gatewayDebug.Trace("Gateway already connected")
		return fmt.Errorf("gateway already connected")
	}

	gatewayIndexerDebug.Trace("Initializing service indexer")
	g.gsi = NewGatewayServiceIndexer()

	if g.Transport == nil {
		gatewayDebug.Trace("Transport not provided")
		return fmt.Errorf(
			"cannot connect local service. The associated gateway already handles " +
				"incoming requests",
		)
	}

	connectCtx, cancel := context.WithCancel(ctx)
	if err := g.setCloseCancel(cancel); err != nil {
		cancel()
		return err
	}
	defer close(g.lifecycleDone)
	defer g.clearCloseCancel()
	defer cancel()
	defer g.closeAllSockets(velaros.StatusGoingAway, "Gateway is stopping")
	defer func() {
		gatewayDebug.Trace("Closing service indexer")
		g.gsi.Close()
	}()

	ready := make(chan struct{}, 4)
	errCh := make(chan error, 6)
	var wg sync.WaitGroup
	goRun := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				select {
				case errCh <- fmt.Errorf("%s: %w", name, err):
				case <-connectCtx.Done():
				}
			}
		}()
	}

	goRun("service announcements", func() error {
		return g.Transport.HandleServiceAnnouncements(connectCtx, ready, func(serviceDescriptor *ServiceDescriptor) {
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
			}
		})
	})

	goRun("gateway messages", func() error {
		return g.Transport.HandleGatewayMessages(connectCtx, ready, g.ID, func(socketID string, msg *velaros.SocketMessage) {
			gatewayRouteDebug.Tracef("Received message for socket %s", socketID)

			g.socketsMu.Lock()
			socket, ok := g.sockets[socketID]
			g.socketsMu.Unlock()
			if !ok {
				gatewayRouteDebug.Tracef("No socket found with ID %s, dropping message", socketID)
				return
			}

			gatewayRouteDebug.Tracef("Routing message to socket %s", socketID)
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer sendCancel()
			err := socket.SendWithContext(sendCtx, msg.Type, msg.Data)
			if err != nil {
				gatewayRouteDebug.Tracef("Failed to route message to socket %s: %v", socketID, err)
				g.closeSocket(socketID, velaros.StatusInternalError, "Failed to send message")
			}

		})
	})

	goRun("socket closures", func() error {
		return g.Transport.HandleSocketClosures(connectCtx, ready, func(socketID string, status velaros.Status, reason string) {
			g.socketsMu.Lock()
			socket, ok := g.sockets[socketID]
			if ok {
				delete(g.sockets, socketID)
			}
			g.socketsMu.Unlock()
			if !ok {
				return
			}

			socket.Close(status, reason, velaros.ServerCloseSource)
			g.gsi.UnmapSocket(socketID)
		})
	})

	goRun("socket heartbeats", func() error {
		return g.Transport.HandleSocketHeartbeats(connectCtx, ready, g.ID, func(serviceID string, socketID string) bool {
			g.socketsMu.Lock()
			_, ok := g.sockets[socketID]
			g.socketsMu.Unlock()
			return ok
		})
	})
	goRun("announce loop", func() error {
		g.announceLoop(connectCtx)
		return nil
	})
	goRun("prune loop", func() error {
		g.pruneLoop(connectCtx)
		return nil
	})

	for i := 0; i < 4; i++ {
		select {
		case <-ready:
		case err := <-errCh:
			cancel()
			wg.Wait()
			return err
		case <-connectCtx.Done():
			cancel()
			wg.Wait()
			return nil
		}
	}

	gatewayDebug.Tracef("Announcing gateway %s", g.Name)

	if err := g.Transport.AnnounceGateway(&GatewayDescriptor{
		Name:               g.Name,
		ServiceDescriptors: g.gsi.ServiceDescriptors(),
	}); err != nil {
		cancel()
		wg.Wait()
		return err
	}

	select {
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	case <-connectCtx.Done():
		cancel()
		wg.Wait()
		return nil
	}
}

// Close interrupts a running Connect call. It is safe to call multiple times.
func (g *Gateway) Close() error {
	g.lifecycleMu.Lock()
	cancel := g.closeCancel
	done := g.lifecycleDone
	g.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
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
			gatewayRouteDebug.Tracef("Failed to map socket for service %s: %v", serviceName, err)
			g.closeSocket(socket.ID(), velaros.StatusGoingAway, "Service unreachable")
			break
		}
		if serviceID == "" {
			g.closeSocket(socket.ID(), velaros.StatusGoingAway, "Service unreachable")
			break
		}

		connInfo := &velaros.ConnectionInfo{
			Headers:    ctx.Headers(),
			RemoteAddr: ctx.RemoteAddr(),
		}
		err = g.Transport.MessageService(serviceID, g.ID, socket.ID(), connInfo, &velaros.SocketMessage{
			Type:    ctx.MessageType(),
			RawData: ctx.RawData(),
			Data:    ctx.Data(),
			Meta:    velaros.CtxMeta(ctx),
		})
		if err == nil {
			break
		}

		if err := g.gsi.UnsetService(serviceID); err != nil {
			gatewayRouteDebug.Tracef("Failed to unset service %s: %v", serviceID, err)
		}

		if !isNewConnection {
			g.closeSocket(socket.ID(), velaros.StatusGoingAway, "Service unreachable")
			break
		}
	}
}

func (g *Gateway) HandleClose(ctx *velaros.Context) {
	gatewayRouteDebug.Tracef("Handling close for socket %s", ctx.SocketID())
	g.closeSocket(ctx.SocketID(), velaros.StatusGoingAway, "Client disconnected")
}

func (g *Gateway) announceLoop(ctx context.Context) {
	ticker := time.NewTicker(g.AnnounceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			freshThreshold := 3 * g.AnnounceInterval
			fresh := g.gsi.FreshServiceDescriptors(freshThreshold)

			gatewayAnnounceDebug.Tracef("Announcing gateway with %d fresh services", len(fresh))
			if err := g.Transport.AnnounceGateway(&GatewayDescriptor{
				Name:               g.Name,
				ServiceDescriptors: fresh,
			}); err != nil {
				gatewayAnnounceDebug.Tracef("Failed to announce gateway: %v", err)
			}
		}
	}
}

func (g *Gateway) pruneLoop(ctx context.Context) {
	ticker := time.NewTicker(g.AnnounceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneThreshold := 5 * g.AnnounceInterval
			_, affectedSocketIDs := g.gsi.PruneStaleServices(pruneThreshold)

			for _, socketID := range affectedSocketIDs {
				gatewayAnnounceDebug.Tracef("Closing socket %s due to stale service", socketID)
				g.closeSocket(socketID, velaros.StatusGoingAway, "Service unreachable")
			}
		}
	}
}

func (g *Gateway) closeAllSockets(status velaros.Status, reason string) {
	g.socketsMu.Lock()
	sockets := make(map[string]*velaros.Socket, len(g.sockets))
	for socketID, socket := range g.sockets {
		sockets[socketID] = socket
		delete(g.sockets, socketID)
	}
	g.socketsMu.Unlock()

	for socketID, socket := range sockets {
		socket.Close(status, reason, velaros.ServerCloseSource)
		g.gsi.UnmapSocket(socketID)
	}
}

func (g *Gateway) closeSocket(socketID string, status velaros.Status, reason string) {
	g.socketsMu.Lock()
	socket, ok := g.sockets[socketID]
	delete(g.sockets, socketID)
	g.socketsMu.Unlock()

	if !ok {
		return
	}

	socket.Close(status, reason, velaros.ServerCloseSource)

	if err := g.Transport.ClosedSocket(socketID, status, reason); err != nil {
		gatewayDebug.Tracef("Error notifying closed socket %s: %v", socketID, err)
	}
	g.gsi.UnmapSocket(socketID)
}

func (g *Gateway) setCloseCancel(cancel context.CancelFunc) error {
	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()
	if g.closeCancel != nil {
		return fmt.Errorf("gateway already connected")
	}
	g.closeCancel = cancel
	g.lifecycleDone = make(chan struct{})
	return nil
}

func (g *Gateway) clearCloseCancel() {
	g.lifecycleMu.Lock()
	g.closeCancel = nil
	g.lifecycleDone = nil
	g.lifecycleMu.Unlock()
}
