package eurus

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RobertWHurst/velaros"
	"github.com/telemetryos/go-debug/debug"
	"golang.org/x/sync/errgroup"
)

var (
	gatewayDebug         = debug.Bind("eurus:gateway")
	gatewayRouteDebug    = debug.Bind("eurus:gateway:route")
	gatewayIndexerDebug  = debug.Bind("eurus:gateway:indexer")
	gatewayAnnounceDebug = debug.Bind("eurus:gateway:announce")
)

// GatewayAnnounceInterval is the default interval between gateway
// announcements. It is jittered per process so a fleet of gateways does not
// announce in lockstep.
var GatewayAnnounceInterval = time.Duration(8+rand.IntN(2)) * time.Second

// GatewaySendTimeout is the default time allowed for delivering a single
// message to a client socket before the socket is considered failed.
var GatewaySendTimeout = 30 * time.Second

// ContextKeyRouteDescriptor is the key used to store the matched route
// descriptor on the velaros context.
const ContextKeyRouteDescriptor = "eurus:route-descriptor"

// ContextKeyServiceDescriptor is the key used to store the matched service
// descriptor on the velaros context.
const ContextKeyServiceDescriptor = "eurus:service-descriptor"

// ContextKeyServiceDescriptors is the key used to store all service
// descriptors on the velaros context.
const ContextKeyServiceDescriptors = "eurus:service-descriptors"

type Gateway struct {
	Name      string
	ID        string
	Transport Transport

	AnnounceInterval time.Duration
	SendTimeout      time.Duration

	gsi atomic.Pointer[GatewayServiceIndexer]

	socketsMu sync.RWMutex
	sockets   map[string]*velaros.Socket

	lifecycle lifecycle
}

var _ velaros.Handler = &Gateway{}

func NewGateway(name string, transport Transport) *Gateway {
	return &Gateway{
		Name:             name,
		ID:               generateID(),
		Transport:        transport,
		AnnounceInterval: GatewayAnnounceInterval,
		SendTimeout:      GatewaySendTimeout,
		sockets:          map[string]*velaros.Socket{},
	}
}

// Connect joins the gateway to the transport and blocks until the context is
// canceled, Close is called, or a transport stream fails. A gateway may be
// connected again after a clean shutdown.
func (g *Gateway) Connect(ctx context.Context) error {
	gatewayDebug.Tracef("Connecting gateway %s", g.Name)

	if g.Transport == nil {
		gatewayDebug.Trace("Transport not provided")
		return ErrNoTransport
	}

	connectCtx, err := g.lifecycle.begin(ctx, ErrGatewayAlreadyConnected)
	if err != nil {
		gatewayDebug.Trace("Gateway already connected")
		return err
	}
	defer g.lifecycle.end()

	gatewayIndexerDebug.Trace("Initializing service indexer")
	gsi := NewGatewayServiceIndexer()
	g.gsi.Store(gsi)
	defer g.closeAllSockets(velaros.StatusGoingAway, "Gateway is stopping")
	defer gsi.Close()

	grp, grpCtx := errgroup.WithContext(connectCtx)
	finish := func(err error) error {
		g.lifecycle.interrupt()
		if waitErr := grp.Wait(); waitErr != nil && err == nil {
			err = waitErr
		}
		return err
	}

	streams := []struct {
		name      string
		subscribe func(context.Context) (Subscription, error)
	}{
		{"service announcements", func(ctx context.Context) (Subscription, error) {
			return g.Transport.SubscribeServiceAnnouncements(ctx, func(serviceDescriptor *ServiceDescriptor) {
				g.handleServiceAnnouncement(gsi, serviceDescriptor)
			})
		}},
		{"gateway messages", func(ctx context.Context) (Subscription, error) {
			return g.Transport.SubscribeGatewayMessages(ctx, g.ID, g.handleGatewayMessage)
		}},
		{"socket closures", func(ctx context.Context) (Subscription, error) {
			return g.Transport.SubscribeSocketClosures(ctx, func(socketID string, status velaros.Status, reason string) {
				g.handleSocketClosure(gsi, socketID, status, reason)
			})
		}},
		{"socket heartbeats", func(ctx context.Context) (Subscription, error) {
			return g.Transport.SubscribeSocketHeartbeats(ctx, g.ID, g.handleSocketHeartbeats)
		}},
	}
	for _, stream := range streams {
		sub, err := stream.subscribe(grpCtx)
		if err != nil {
			return finish(fmt.Errorf("%s: %w", stream.name, err))
		}
		name := stream.name
		grp.Go(func() error {
			if err := sub.Serve(grpCtx); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		})
	}

	grp.Go(func() error {
		g.announceLoop(grpCtx, gsi)
		return nil
	})
	grp.Go(func() error {
		g.pruneLoop(grpCtx, gsi)
		return nil
	})

	gatewayDebug.Tracef("Announcing gateway %s", g.Name)
	if err := g.Transport.AnnounceGateway(&GatewayDescriptor{
		Name:               g.Name,
		ServiceDescriptors: gsi.ServiceDescriptors(),
	}); err != nil {
		return finish(err)
	}

	g.lifecycle.markReady()
	return finish(grp.Wait())
}

// Close interrupts a running Connect call and waits for it to return. It is
// safe to call multiple times, and is a no-op when the gateway is not
// connected.
func (g *Gateway) Close() error {
	gatewayDebug.Tracef("Closing gateway %s", g.Name)
	g.lifecycle.close()
	return nil
}

// Ready returns a channel that is closed once the current Connect call has
// subscribed its transport streams and announced the gateway. It is intended
// for startup sequencing and readiness probes.
func (g *Gateway) Ready() <-chan struct{} {
	return g.lifecycle.readyChan()
}

func (g *Gateway) handleServiceAnnouncement(gsi *GatewayServiceIndexer, serviceDescriptor *ServiceDescriptor) {
	gatewayIndexerDebug.Tracef("Received service announcement from %s", serviceDescriptor.Name)

	announcingToThisGateway := len(serviceDescriptor.GatewayNames) == 0 ||
		slices.Contains(serviceDescriptor.GatewayNames, g.Name)
	if !announcingToThisGateway {
		gatewayIndexerDebug.Tracef("Service %s not announcing to this gateway", serviceDescriptor.Name)
		return
	}

	gatewayIndexerDebug.Tracef("Indexing service %s with %d routes",
		serviceDescriptor.Name, len(serviceDescriptor.RouteDescriptors))
	if err := gsi.SetServiceDescriptor(serviceDescriptor); err != nil {
		gatewayIndexerDebug.Tracef("Failed to index service %s: %v", serviceDescriptor.Name, err)
	}
}

func (g *Gateway) handleGatewayMessage(socketID string, msg *velaros.SocketMessage) {
	gatewayRouteDebug.Tracef("Received message for socket %s", socketID)

	g.socketsMu.RLock()
	socket, ok := g.sockets[socketID]
	g.socketsMu.RUnlock()
	if !ok {
		gatewayRouteDebug.Tracef("No socket found with ID %s, dropping message", socketID)
		return
	}

	gatewayRouteDebug.Tracef("Routing message to socket %s", socketID)
	sendCtx, sendCancel := context.WithTimeout(context.Background(), g.SendTimeout)
	defer sendCancel()
	if err := socket.SendWithContext(sendCtx, msg.Type, msg.Data); err != nil {
		gatewayRouteDebug.Tracef("Failed to route message to socket %s: %v", socketID, err)
		g.closeSocket(socketID, velaros.StatusInternalError, "Failed to send message")
	}
}

func (g *Gateway) handleSocketClosure(gsi *GatewayServiceIndexer, socketID string, status velaros.Status, reason string) {
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
	gsi.UnmapSocket(socketID)
}

func (g *Gateway) handleSocketHeartbeats(serviceID string, socketIDs []string) []string {
	g.socketsMu.RLock()
	defer g.socketsMu.RUnlock()

	var dead []string
	for _, socketID := range socketIDs {
		if _, ok := g.sockets[socketID]; !ok {
			dead = append(dead, socketID)
		}
	}
	return dead
}

func (g *Gateway) CanServePath(path string) bool {
	gatewayRouteDebug.Tracef("Checking if gateway can serve path %s", path)

	gsi := g.gsi.Load()
	if gsi == nil {
		gatewayRouteDebug.Trace("Gateway not connected, cannot serve request")
		return false
	}

	_, ok := gsi.ResolveService(path)
	if ok {
		gatewayRouteDebug.Tracef("Can serve %s", path)
	} else {
		gatewayRouteDebug.Tracef("Cannot serve %s, no matching service", path)
	}
	return ok
}

func (g *Gateway) CanHandle(ctx *velaros.Context) bool {
	return g.CanServePath(ctx.Path())
}

func (g *Gateway) Handle(ctx *velaros.Context) {
	path := ctx.Path()
	gatewayRouteDebug.Tracef("Received Velaros message %s", path)

	gsi := g.gsi.Load()
	if gsi == nil {
		gatewayRouteDebug.Trace("Gateway not connected, skipping to next handler")
		ctx.Next()
		return
	}

	gatewayRouteDebug.Tracef("Resolving service for path %s", path)
	serviceDescriptor, routeDescriptor, ok := gsi.Resolve(path)
	if !ok {
		gatewayRouteDebug.Tracef("No service found for %s, skipping to next handler", path)
		ctx.Next()
		return
	}
	serviceName := serviceDescriptor.Name
	gatewayRouteDebug.Tracef("Resolved %s to service %s", path, serviceName)

	if routeDescriptor != nil {
		ctx.Set(ContextKeyRouteDescriptor, routeDescriptor)
	}

	socket := velaros.CtxSocket(ctx)
	g.socketsMu.Lock()
	g.sockets[socket.ID()] = socket
	g.socketsMu.Unlock()

	for {
		gatewayRouteDebug.Tracef("Mapping socket for service %s", serviceName)
		serviceID, isNewConnection, err := gsi.MapSocket(serviceName, socket.ID())
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

		if errors.Is(err, ErrSlowConsumer) {
			gatewayRouteDebug.Tracef("Socket %s rejected as slow consumer", socket.ID())
			g.closeSocket(socket.ID(), velaros.StatusPolicyViolation, "Slow consumer")
			break
		}

		if err := gsi.UnsetService(serviceID); err != nil {
			gatewayRouteDebug.Tracef("Failed to unset service %s: %v", serviceID, err)
		}

		if !isNewConnection {
			g.closeSocket(socket.ID(), velaros.StatusGoingAway, "Service unreachable")
			break
		}
	}
}

// DescriptorMiddleware returns a velaros middleware that resolves the
// matching route descriptor for the incoming message and sets it on the
// context. Downstream middleware can retrieve it with
// RouteDescriptorFromContext — for example to enforce auth or rate-limit
// policy declared with velaros.WithMetadata before the message is dispatched.
func (g *Gateway) DescriptorMiddleware() velaros.HandlerFunc {
	return func(ctx *velaros.Context) {
		if gsi := g.gsi.Load(); gsi != nil {
			ctx.Set(ContextKeyServiceDescriptors, gsi.ServiceDescriptors())

			if serviceDescriptor, routeDescriptor, ok := gsi.Resolve(ctx.Path()); ok {
				ctx.Set(ContextKeyServiceDescriptor, serviceDescriptor)
				if routeDescriptor != nil {
					ctx.Set(ContextKeyRouteDescriptor, routeDescriptor)
				}
			}
		}
		ctx.Next()
	}
}

// RouteDescriptorFromContext retrieves the route descriptor that was set on
// the context by DescriptorMiddleware or Handle. Returns nil if no descriptor
// was set.
func RouteDescriptorFromContext(ctx *velaros.Context) *RouteDescriptor {
	v, ok := ctx.Get(ContextKeyRouteDescriptor)
	if !ok {
		return nil
	}
	routeDescriptor, _ := v.(*RouteDescriptor)
	return routeDescriptor
}

// ServiceDescriptorFromContext retrieves the service descriptor that was set
// on the context by DescriptorMiddleware. Returns nil if no descriptor was
// set.
func ServiceDescriptorFromContext(ctx *velaros.Context) *ServiceDescriptor {
	v, ok := ctx.Get(ContextKeyServiceDescriptor)
	if !ok {
		return nil
	}
	serviceDescriptor, _ := v.(*ServiceDescriptor)
	return serviceDescriptor
}

// ServiceDescriptorsFromContext retrieves the service descriptors that were
// set on the context by DescriptorMiddleware. Returns nil if no descriptors
// were set.
func ServiceDescriptorsFromContext(ctx *velaros.Context) []*ServiceDescriptor {
	v, ok := ctx.Get(ContextKeyServiceDescriptors)
	if !ok {
		return nil
	}
	serviceDescriptors, _ := v.([]*ServiceDescriptor)
	return serviceDescriptors
}

func (g *Gateway) HandleClose(ctx *velaros.Context) {
	gatewayRouteDebug.Tracef("Handling close for socket %s", ctx.SocketID())
	g.closeSocket(ctx.SocketID(), velaros.StatusGoingAway, "Client disconnected")
}

func (g *Gateway) announceLoop(ctx context.Context, gsi *GatewayServiceIndexer) {
	ticker := time.NewTicker(g.AnnounceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			freshThreshold := 3 * g.AnnounceInterval
			fresh := gsi.FreshServiceDescriptors(freshThreshold)

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

func (g *Gateway) pruneLoop(ctx context.Context, gsi *GatewayServiceIndexer) {
	ticker := time.NewTicker(g.AnnounceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneThreshold := 5 * g.AnnounceInterval
			_, affectedSocketIDs := gsi.PruneStaleServices(pruneThreshold)

			for _, socketID := range affectedSocketIDs {
				gatewayAnnounceDebug.Tracef("Closing socket %s due to stale service", socketID)
				g.closeSocket(socketID, velaros.StatusGoingAway, "Service unreachable")
			}
		}
	}
}

func (g *Gateway) closeAllSockets(status velaros.Status, reason string) {
	g.socketsMu.Lock()
	sockets := g.sockets
	g.sockets = map[string]*velaros.Socket{}
	g.socketsMu.Unlock()

	gsi := g.gsi.Load()
	for socketID, socket := range sockets {
		socket.Close(status, reason, velaros.ServerCloseSource)
		if gsi != nil {
			gsi.UnmapSocket(socketID)
		}
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
	if gsi := g.gsi.Load(); gsi != nil {
		gsi.UnmapSocket(socketID)
	}
}
