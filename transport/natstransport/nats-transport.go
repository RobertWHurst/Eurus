package natstransport

import (
	"github.com/nats-io/nats.go"
	"github.com/telemetrytv/eurus"
)

type NatsTransport struct {
	NatsConnection        *nats.Conn
	unbindServiceAnnounce func() error
	unbindGatewayAnnounce func() error
	unbindMessageService  map[string]func() error
	unbindMessageGateway  map[string]func() error
	unbindSocketClosed    func() error
}

var _ eurus.Transport = &NatsTransport{}

func New(natsConnection *nats.Conn) *NatsTransport {
	return &NatsTransport{
		NatsConnection:       natsConnection,
		unbindMessageService: map[string]func() error{},
		unbindMessageGateway: map[string]func() error{},
	}
}
