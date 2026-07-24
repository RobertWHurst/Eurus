package eurus

import "errors"

var (
	// ErrGatewayAlreadyConnected is returned by Gateway.Connect when the
	// gateway is already connected.
	ErrGatewayAlreadyConnected = errors.New("eurus: gateway already connected")

	// ErrServiceAlreadyListening is returned by Service.Listen when the
	// service is already listening.
	ErrServiceAlreadyListening = errors.New("eurus: service already listening")

	// ErrNoTransport is returned by Gateway.Connect and Service.Listen when no
	// transport was provided. Local services attached directly to a gateway
	// are driven by the gateway and must not be started on their own.
	ErrNoTransport = errors.New("eurus: no transport provided")

	// ErrSlowConsumer is returned by transport message delivery when the
	// receiving side rejected the message because the socket's dispatch queue
	// overflowed. It indicates a problem with the socket, not the receiving
	// instance.
	ErrSlowConsumer = errors.New("eurus: slow consumer")

	// ErrNoRouteMetadata is returned by RouteDescriptor.UnmarshalMetadata
	// when the route has no metadata attached.
	ErrNoRouteMetadata = errors.New("eurus: route has no metadata")
)
