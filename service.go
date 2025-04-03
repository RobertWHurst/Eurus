package eurus

import (
	"context"
	"fmt"

	"github.com/RobertWHurst/velaros"
)

type Service struct {

	// GatewayNames is a list of gateway names that the service should announce
	// itself to. If the list is empty, the service will announce itself to all
	// gateways on the connection.
	GatewayNames []string

	// Name is the name of the service. This is used to identify the service
	// when announcing it to the gateway.
	Name string

	// Transport is a struct that implements the Transport interface and
	// facilitates communication between services, gateways, and clients.
	Transport Transport

	// RouteDescriptors is a list of route descriptors that describe the routes
	// that this service can handle. If this is left empty, the service will
	// not be routable. This is automatically populated if the handler is a
	// Navaros router.
	RouteDescriptors []*RouteDescriptor

	// Handler is called when a request is made to the service. This can be
	// either a Navaros router or a standard http.Handler or http.HandlerFunc.
	Handler any

	MessageDecoder velaros.MessageDecoder
	MessageEncoder velaros.MessageEncoder

	stopChan chan struct{}
}

func NewService(name string, transport Transport, handler any) *Service {
	return &Service{
		Name:           name,
		Transport:      transport,
		Handler:        handler,
		MessageDecoder: velaros.DefaultMessageDecoder,
		MessageEncoder: velaros.DefaultMessageEncoder,
		stopChan:       make(chan struct{}),
	}
}

func (s *Service) Start() error {
	if s.Transport == nil {
		return fmt.Errorf(
			"cannot start local service. The associated gateway already handles " +
				"incoming requests",
		)
	}

	if err := s.Transport.BindGatewayAnnounce(func(gatewayDescriptor *GatewayDescriptor) {
		s.handleGatewayAnnounce(gatewayDescriptor)
	}); err != nil {
		return err
	}

	if err := s.Transport.BindInboundSocketMessage(s.Name, func(socketID string, messageData []byte) error {
		if s.Handler == nil {
			return fmt.Errorf("no handler set for service %s", s.Name)
		}

		message, err := s.MessageDecoder(messageData)
		if err != nil {
			return fmt.Errorf("failed to decode message: %v", err)
		}
		socketConnection := &remoteSocket{socketID: socketID, transport: s.Transport}
		socket := velaros.NewSocket(socketConnection, nil, s.MessageDecoder, s.MessageEncoder)
		ctx := velaros.NewContext(socket, message, s.Handler)
		ctx.Next()

		return ctx.Error
	}); err != nil {
		return err
	}

	if err := s.doAnnounce(); err != nil {
		return err
	}

	return nil
}

func (s *Service) Stop() error {
	return nil
}

func (s *Service) handleGatewayAnnounce(gatewayDescriptor *GatewayDescriptor) {
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
		return
	}

	foundSelf := false
	for _, descriptor := range gatewayDescriptor.ServiceDescriptors {
		if descriptor.Name == s.Name {
			foundSelf = true
			break
		}
	}
	if !foundSelf {
		if err := s.doAnnounce(); err != nil {
			panic(err)
		}
	}
}

func (s *Service) Run() error {
	return nil
}

func (s *Service) doAnnounce() error {
	routeDescriptors := s.RouteDescriptors

	if routeDescriptors == nil {
		if h, ok := s.Handler.(velaros.RouterHandler); ok {
			velarosRouteDescriptors := h.RouteDescriptors()
			for _, velarosRouteDescriptor := range velarosRouteDescriptors {
				routeDescriptors = append(routeDescriptors, &RouteDescriptor{
					Pattern: velarosRouteDescriptor.Pattern,
				})
			}
		}
	}

	return s.Transport.AnnounceService(&ServiceDescriptor{
		Name:             s.Name,
		RouteDescriptors: routeDescriptors,
	})
}

type remoteSocket struct {
	socketID  string
	transport Transport
}

func (s *remoteSocket) Write(ctx context.Context, messageData []byte) error {
	return s.transport.OutboundSocketMessage(s.socketID, messageData)
}

func (s *remoteSocket) Read(ctx context.Context) ([]byte, error) {
	panic("Cannot read from remote socket")
}

var _ velaros.SocketConnection = &remoteSocket{}
