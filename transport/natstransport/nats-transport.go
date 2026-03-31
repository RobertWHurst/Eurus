package natstransport

import (
	"sync"

	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
)

type NatsTransport struct {
	NatsConnection        *nats.Conn
	unbindServiceAnnounce func() error
	unbindGatewayAnnounce func() error
	unbindMessageService  map[string]func() error
	unbindMessageGateway  map[string]func() error
	unbindSocketClosed    func() error
	unbindSocketHeartbeat func() error
	messageHandlerWg      sync.WaitGroup
}

var _ eurus.Transport = &NatsTransport{}

func New(natsConnection *nats.Conn) *NatsTransport {
	return &NatsTransport{
		NatsConnection:       natsConnection,
		unbindMessageService: map[string]func() error{},
		unbindMessageGateway: map[string]func() error{},
	}
}
