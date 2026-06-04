package natstransport

import (
	"sync"

	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
)

type NatsTransport struct {
	NatsConnection *nats.Conn

	// mu guards the unbind handles below. A single NatsTransport can be shared
	// across multiple services/gateways (the handles are keyed by ID), so
	// concurrent Bind*/Unbind* calls must not race on these fields/maps.
	mu                    sync.Mutex
	unbindServiceAnnounce func() error
	unbindGatewayAnnounce func() error
	unbindMessageService  map[string]func() error
	unbindMessageGateway  map[string]func() error
	unbindSocketClosed    func() error
	unbindSocketHeartbeat map[string]func() error
}

var _ eurus.Transport = &NatsTransport{}

func New(natsConnection *nats.Conn) *NatsTransport {
	return &NatsTransport{
		NatsConnection:        natsConnection,
		unbindMessageService:  map[string]func() error{},
		unbindMessageGateway:  map[string]func() error{},
		unbindSocketHeartbeat: map[string]func() error{},
	}
}
