package eurus

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/RobertWHurst/velaros"
	"github.com/telemetryos/go-debug/debug"
)

var (
	serviceDebug         = debug.Bind("eurus:service")
	serviceAnnounceDebug = debug.Bind("eurus:service:announce")
	serviceHandleDebug   = debug.Bind("eurus:service:handler")
)

var (
	ServicePruneInterval = 10 * time.Second
	ClosedSocketTTL      = 30 * time.Second
	HeartbeatInterval    = 5 * time.Second
)

type routeDescriptorProvider interface {
	RouteDescriptors() []*velaros.RouteDescriptor
}

// Service is a struct that facilitates communication between a go microservice
// and a eurus gateway. It will manage the announcement of the service to the
// gateway as well as calls any HTTP Handler or Navaros handler.
//
// If the service is given a Navaros router, it will automatically announce
// any public routes declared on the router. That said any handler compatible
// with go's http.HandlerFunc or http.Handler interface can be used.
//
// Note that if you opt to use something other than Navaros, you will need to
// assign your route descriptors manually. This can be done by using the
// eurus.NewRouteDescriptor function to create your route descriptors, then
// assigning them to the RouteDescriptors field on the service.
type Service struct {

	// GatewayNames is a list of gateway names that the service should announce
	// itself to. If the list is empty, the service will announce itself to all
	// gateways on the connection.
	GatewayNames []string

	// Name is the name of the service. This is used to identify the service
	// when announcing it to the gateway.
	Name string

	// ID is the unique identifier for the instance of the service. This is
	// automatically generated when the service is created.
	ID string

	// Transport is a struct that implements the Transport interface and
	// facilitates communication between services, gateways, and clients.
	Transport Transport

	// RouteDescriptors is a list of route descriptors that describe the routes
	// that this service can handle. If this is left empty, the service will
	// not be routable. This is automatically populated if the handler is a
	// Navaros router.
	RouteDescriptors []*RouteDescriptor

	// Router is called when a request is made to the service. This can be
	// either a Navaros router or a standard http.Router or http.HandlerFunc.
	Router *velaros.Router

	handler       any
	connectionsMu sync.Mutex
	connections   map[string]*Connection
	closedSockets map[string]time.Time

	PruneInterval     time.Duration
	ClosedSocketTTL   time.Duration
	HeartbeatInterval time.Duration

	lifecycleMu   sync.Mutex
	closeCancel   context.CancelFunc
	lifecycleDone chan struct{}
}

// NewService creates a new service with the given name, connection, and handler.
// The service will automatically announce itself to the gateway when it starts.
// If the handler is a Velaros router, the service will automatically announce
// any public routes declared on the router.
func NewService(name string, transport Transport, handler any) *Service {
	if handler == nil {
		panic("handler cannot be nil")
	}

	router := velaros.NewRouter()
	router.Use(handler)

	return &Service{
		Name:              name,
		ID:                generateID(),
		Transport:         transport,
		Router:            router,
		handler:           handler,
		connections:       map[string]*Connection{},
		closedSockets:     map[string]time.Time{},
		PruneInterval:     ServicePruneInterval,
		ClosedSocketTTL:   ClosedSocketTTL,
		HeartbeatInterval: HeartbeatInterval,
	}
}

// Listen starts the service and blocks until the context is canceled, Close is
// called, or a required transport stream fails.
func (s *Service) Listen(ctx context.Context) error {
	serviceDebug.Tracef("Listening as service %s", s.Name)
	if s.Transport == nil {
		serviceDebug.Trace("Transport not provided")
		return fmt.Errorf(
			"cannot listen as local service. The associated gateway already handles " +
				"incoming requests",
		)
	}

	listenCtx, cancel := context.WithCancel(ctx)
	if err := s.setCloseCancel(cancel); err != nil {
		cancel()
		return err
	}
	defer close(s.lifecycleDone)
	defer s.clearCloseCancel()
	defer cancel()
	defer s.closeAllConnections()

	ready := make(chan struct{}, 3)
	errCh := make(chan error, 5)
	var wg sync.WaitGroup
	goRun := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				select {
				case errCh <- fmt.Errorf("%s: %w", name, err):
				case <-listenCtx.Done():
				}
			}
		}()
	}

	goRun("gateway announcements", func() error {
		return s.Transport.HandleGatewayAnnouncements(listenCtx, ready, func(gatewayDescriptor *GatewayDescriptor) {
			s.handleGatewayAnnounce(listenCtx, gatewayDescriptor)
		})
	})
	goRun("service messages", func() error {
		return s.Transport.HandleServiceMessages(listenCtx, ready, s.ID, func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
			serviceHandleDebug.Tracef("Handling message from socket %s and gateway %s", socketID, gatewayID)
			connection := s.ensureConnection(gatewayID, socketID, connInfo)
			if connection == nil {
				serviceHandleDebug.Tracef("Dropping message for recently closed socket %s", socketID)
				return
			}
			connection.HandleMessage(msg)
		})
	})
	goRun("socket closures", func() error {
		return s.Transport.HandleSocketClosures(listenCtx, ready, func(socketID string, status velaros.Status, reason string) {
			serviceHandleDebug.Tracef("Handling close for socket %s", socketID)
			s.closeConnection(socketID, status, reason)
		})
	})
	goRun("prune loop", func() error {
		s.pruneLoop(listenCtx)
		return nil
	})
	goRun("heartbeat loop", func() error {
		s.heartbeatLoop(listenCtx)
		return nil
	})

	for i := 0; i < 3; i++ {
		select {
		case <-ready:
		case err := <-errCh:
			cancel()
			wg.Wait()
			return err
		case <-listenCtx.Done():
			cancel()
			wg.Wait()
			return nil
		}
	}

	serviceDebug.Trace("Announcing service to gateways")
	if err := s.doAnnounce(); err != nil {
		cancel()
		wg.Wait()
		serviceDebug.Tracef("Failed to announce service: %v", err)
		return err
	}

	select {
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	case <-listenCtx.Done():
		cancel()
		wg.Wait()
		return nil
	}
}

// Close interrupts a running Listen call. It is safe to call multiple times.
func (s *Service) Close() error {
	s.lifecycleMu.Lock()
	cancel := s.closeCancel
	done := s.lifecycleDone
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

func (s *Service) handleGatewayAnnounce(ctx context.Context, gatewayDescriptor *GatewayDescriptor) {
	serviceAnnounceDebug.Tracef("Received gateway announcement from %s", gatewayDescriptor.Name)
	if ctx.Err() != nil {
		return
	}
	isWantedGateway := len(s.GatewayNames) == 0
	if !isWantedGateway {
		if slices.Contains(s.GatewayNames, gatewayDescriptor.Name) {
			isWantedGateway = true
		}
	}
	if !isWantedGateway {
		serviceAnnounceDebug.Tracef("Ignoring announcement from unwanted gateway %s", gatewayDescriptor.Name)
		return
	}

	serviceAnnounceDebug.Trace("Checking if service is registered with gateway")
	foundSelf := false
	for _, descriptor := range gatewayDescriptor.ServiceDescriptors {
		if descriptor.ID == s.ID {
			serviceAnnounceDebug.Trace("Service found in gateway's service index")
			foundSelf = true
			break
		}
	}

	if !foundSelf {
		serviceAnnounceDebug.Trace("Service not found in gateway's service index, announcing service")
		if err := s.doAnnounce(); err != nil {
			serviceAnnounceDebug.Tracef("Failed to announce service: %v", err)
		}
	}
}

func (s *Service) doAnnounce() error {
	serviceAnnounceDebug.Tracef("Service %s announcing to gateways", s.Name)

	routeDescriptors := s.RouteDescriptors

	if routeDescriptors == nil {
		if provider, ok := s.handler.(routeDescriptorProvider); ok {
			serviceAnnounceDebug.Trace("Extracting route descriptors from handler")
			velarosRouteDescriptors := provider.RouteDescriptors()
			for _, velarosRouteDescriptor := range velarosRouteDescriptors {
				routeDescriptors = append(routeDescriptors, &RouteDescriptor{
					Pattern: velarosRouteDescriptor.Pattern,
				})
			}
		}
	}

	if len(routeDescriptors) > 0 {
		serviceAnnounceDebug.Tracef("Announcing %d routes", len(routeDescriptors))
		for _, route := range routeDescriptors {
			serviceAnnounceDebug.Tracef("Route: %s", route.Pattern)
		}
	} else {
		serviceAnnounceDebug.Trace("No routes to announce")
	}

	return s.Transport.AnnounceService(&ServiceDescriptor{
		Name:             s.Name,
		ID:               s.ID,
		GatewayNames:     s.GatewayNames,
		RouteDescriptors: routeDescriptors,
	})
}

func (s *Service) ensureConnection(gatewayID, socketID string, info *velaros.ConnectionInfo) *Connection {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()

	if _, wasClosed := s.closedSockets[socketID]; wasClosed {
		serviceHandleDebug.Tracef("Rejecting connection creation for recently closed socket %s", socketID)
		return nil
	}

	if connection, ok := s.connections[socketID]; ok {
		return connection
	}

	connection := NewConnection(s.Transport, gatewayID, socketID, info, func() {
		s.connectionsMu.Lock()
		delete(s.connections, socketID)
		s.connectionsMu.Unlock()
	})

	go s.Router.HandleConnection(info, connection)
	s.connections[socketID] = connection

	return connection
}

func (s *Service) closeConnection(socketID string, status velaros.Status, reason string) {
	s.connectionsMu.Lock()
	connection, ok := s.connections[socketID]
	delete(s.connections, socketID)
	s.closedSockets[socketID] = time.Now()
	s.connectionsMu.Unlock()

	if ok {
		connection.HandleClose(status, reason)
	}
}

func (s *Service) closeAllConnections() {
	s.connectionsMu.Lock()
	connections := make([]*Connection, 0, len(s.connections))
	for _, c := range s.connections {
		connections = append(connections, c)
	}
	s.connections = make(map[string]*Connection)
	s.connectionsMu.Unlock()

	for _, c := range connections {
		c.HandleClose(velaros.StatusGoingAway, "Service is stopping")
	}
}

func (s *Service) pruneLoop(ctx context.Context) {
	ticker := time.NewTicker(s.PruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pruneStaleResources()
		}
	}
}

func (s *Service) pruneStaleResources() {
	s.connectionsMu.Lock()
	now := time.Now()
	for socketID, closedAt := range s.closedSockets {
		if now.Sub(closedAt) > s.ClosedSocketTTL {
			serviceHandleDebug.Tracef("Pruning closed socket record for %s", socketID)
			delete(s.closedSockets, socketID)
		}
	}
	s.connectionsMu.Unlock()
}

func (s *Service) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(s.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.connectionsMu.Lock()
			type entry struct {
				gatewayID string
				socketID  string
			}
			var entries []entry
			for socketID, conn := range s.connections {
				entries = append(entries, entry{conn.GatewayID(), socketID})
			}
			s.connectionsMu.Unlock()

			for _, e := range entries {
				if err := s.Transport.HeartbeatSocket(e.gatewayID, s.ID, e.socketID); err != nil {
					serviceHandleDebug.Tracef("No heartbeat response for socket %s from gateway %s: %v, closing", e.socketID, e.gatewayID, err)
					s.closeConnection(e.socketID, velaros.StatusGoingAway, "No heartbeat response")
					if err := s.Transport.ClosedSocket(e.socketID, velaros.StatusGoingAway, "No heartbeat response"); err != nil {
						serviceHandleDebug.Tracef("Error notifying closed socket %s: %v", e.socketID, err)
					}
				}
			}
		}
	}
}

func (s *Service) setCloseCancel(cancel context.CancelFunc) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closeCancel != nil {
		return fmt.Errorf("service already listening")
	}
	s.closeCancel = cancel
	s.lifecycleDone = make(chan struct{})
	return nil
}

func (s *Service) clearCloseCancel() {
	s.lifecycleMu.Lock()
	s.closeCancel = nil
	s.lifecycleDone = nil
	s.lifecycleMu.Unlock()
}
