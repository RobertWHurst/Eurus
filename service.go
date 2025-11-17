package eurus

import (
	"fmt"
	"sync"

	"github.com/RobertWHurst/velaros"
	"github.com/telemetrytv/trace"
)

var (
	serviceDebug         = trace.Bind("eurus:service")
	serviceAnnounceDebug = trace.Bind("eurus:service:announce")
	serviceHandleDebug   = trace.Bind("eurus:service:handler")
)

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

	connectionsMu sync.Mutex
	connections   map[string]*Connection

	stopChan chan struct{}
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
		Name:        name,
		ID:          generateID(),
		Transport:   transport,
		Router:      router,
		connections: map[string]*Connection{},
		stopChan:    make(chan struct{}),
	}
}

// Start starts the service. This will bind the service to the connection and
// announce the service to the gateway. If the service is already bound to the
// connection, this will return an error. Start must be called before the
// service will receive requests.
func (s *Service) Start() error {
	serviceDebug.Tracef("Starting service %s", s.Name)

	if s.Transport == nil {
		serviceDebug.Trace("Transport not provided")
		return fmt.Errorf(
			"cannot start local service. The associated gateway already handles " +
				"incoming requests",
		)
	}

	serviceDebug.Trace("Binding gateway announcement handler")
	err := s.Transport.BindGatewayAnnounce(func(gatewayDescriptor *GatewayDescriptor) {
		s.handleGatewayAnnounce(gatewayDescriptor)
	})
	if err != nil {
		serviceDebug.Tracef("Failed to bind gateway announcement handler: %v", err)
		return err
	}

	serviceDebug.Trace("Binding service message handler")
	err = s.Transport.BindMessageService(s.ID, func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
		serviceHandleDebug.Tracef("Handling message from socket %s and gateway %s", socketID, gatewayID)
		connection := s.ensureConnection(gatewayID, socketID, connInfo)
		connection.HandleMessage(msg)
	})
	if err != nil {
		serviceDebug.Tracef("Failed to bind service message handler: %v", err)
		return err
	}

	serviceDebug.Trace("Binding close socket handler")
	err = s.Transport.BindSocketClosed(func(socketID string, status velaros.Status, reason string) {
		serviceHandleDebug.Tracef("Handling close for socket %s", socketID)
		s.closeConnection(socketID, status, reason)
	})

	serviceDebug.Trace("Announcing service to gateways")
	if err := s.doAnnounce(); err != nil {
		serviceDebug.Tracef("Failed to announce service: %v", err)
		return err
	}

	serviceDebug.Tracef("Service %s started successfully", s.Name)
	return nil
}

// Stop stops the service. This will unbind the service from the connection.
// This provides a way to dispose of the service if need be.
func (s *Service) Stop() {
	if s.Router == nil {
		return
	}
	s.Router = nil

	serviceDebug.Tracef("Stopping service %s", s.Name)

	s.closeAllConnections()

	serviceDebug.Trace("Unbinding gateway announcement handler")
	if err := s.Transport.UnbindGatewayAnnounce(); err != nil {
		serviceDebug.Tracef("Failed to unbind gateway announcement handler: %v", err)
		panic(err)
	}

	serviceDebug.Trace("Unbinding dispatch handler")
	if err := s.Transport.UnbindMessageService(s.ID); err != nil {
		serviceDebug.Tracef("Failed to unbind dispatch handler: %v", err)
		panic(err)
	}

	close(s.stopChan)
	serviceDebug.Tracef("Service %s stopped successfully", s.Name)
}

func (s *Service) handleGatewayAnnounce(gatewayDescriptor *GatewayDescriptor) {
	serviceAnnounceDebug.Tracef("Received gateway announcement from %s", gatewayDescriptor.Name)

	isWantedGateway := len(s.GatewayNames) == 0
	if !isWantedGateway {
		for _, name := range s.GatewayNames {
			if name == gatewayDescriptor.Name {
				isWantedGateway = true
				break
			}
		}
	}
	if !isWantedGateway {
		serviceAnnounceDebug.Tracef("Ignoring announcement from unwanted gateway %s", gatewayDescriptor.Name)
		return
	}

	serviceAnnounceDebug.Trace("Checking if service is registered with gateway")
	foundSelf := false
	for _, descriptor := range gatewayDescriptor.ServiceDescriptors {
		if descriptor.Name == s.Name {
			serviceAnnounceDebug.Trace("Service found in gateway's service index")
			foundSelf = true
			break
		}
	}

	if !foundSelf {
		serviceAnnounceDebug.Trace("Service not found in gateway's service index, announcing service")
		if err := s.doAnnounce(); err != nil {
			serviceAnnounceDebug.Tracef("Failed to announce service: %v", err)
			panic(err)
		}
	}
}

// Run starts the service and blocks until the service is stopped.
func (s *Service) Run() error {
	serviceDebug.Tracef("Running service %s", s.Name)

	if err := s.Start(); err != nil {
		serviceDebug.Tracef("Failed to start service: %v", err)
		return err
	}

	serviceDebug.Trace("Waiting for service to be stopped")
	<-s.stopChan

	serviceDebug.Trace("Service run completed")
	return nil
}

func (s *Service) doAnnounce() error {
	serviceAnnounceDebug.Tracef("Service %s announcing to gateways", s.Name)

	routeDescriptors := s.RouteDescriptors

	if routeDescriptors == nil {
		serviceAnnounceDebug.Trace("Extracting route descriptors from Navaros router")
		velarosRouteDescriptors := s.Router.RouteDescriptors()
		for _, velarosRouteDescriptor := range velarosRouteDescriptors {
			routeDescriptors = append(routeDescriptors, &RouteDescriptor{
				Pattern: velarosRouteDescriptor.Pattern,
			})
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

	if connection, ok := s.connections[socketID]; ok {
		return connection
	}

	connection := NewConnection(s.Transport, gatewayID, socketID, info, func() {
		delete(s.connections, socketID)
	})

	go s.Router.HandleConnection(info, connection)
	s.connections[socketID] = connection

	return connection
}

func (s *Service) closeConnection(socketID string, status velaros.Status, reason string) {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()

	connection, ok := s.connections[socketID]
	if ok {
		connection.HandleClose(status, reason)
	}
}

func (s *Service) closeAllConnections() {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()

	for _, connection := range s.connections {
		connection.HandleClose(velaros.StatusGoingAway, "Service is stopping")
	}
}
